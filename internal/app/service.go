// Package app is the Wails service the frontend talks to, plus the sampling
// loop that drives the analyzer, the store, the menu bar icon and alerts.
package app

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Prime-Cave/headroom/internal/actions"
	"github.com/Prime-Cave/headroom/internal/analyzer"
	"github.com/Prime-Cave/headroom/internal/collector"
	"github.com/Prime-Cave/headroom/internal/machine"
	"github.com/Prime-Cave/headroom/internal/notify"
	"github.com/Prime-Cave/headroom/internal/store"
)

// Sampling cadence (section 3).
const (
	FastInterval    = 2 * time.Second  // while the dropdown or dashboard is open
	SlowInterval    = 5 * time.Second  // otherwise
	ProcessInterval = 10 * time.Second // the expensive call
	MaintInterval   = time.Minute      // rollups, retention, learning
)

// UI is what the service needs from the windowing layer. Implemented in
// cmd/headroom with Wails; a fake in tests.
type UI interface {
	Emit(event string, data any)
	PanelsVisible() bool
	SetTrayIcon(state analyzer.State, memFill float64, simple bool)
	ShowDashboard(section string)
	HideDropdown()
	ResizeDropdown(height int)
	ShowSafety(appKey string)
	HideSafety()
	ResizeSafety(height int)
	// SaveFileDialog asks where to save a file; "" if cancelled.
	SaveFileDialog(defaultName string) (string, error)
	// Quit quits Headroom itself.
	Quit()
}

// Config wires the service.
type Config struct {
	Collector collector.Collector
	Store     *store.Store
	Profile   machine.Profile
	Demo      bool // replaying synthetic data: actions never touch real apps or files
	Logger    *slog.Logger
	// System performs actions (AppKit in the app). Demo mode never calls it
	// for quits or trash.
	System actions.System
	// LoginItem registers Headroom to start at login (SMAppService); nil in
	// demo mode and selftests, so they never change the Mac's login items.
	LoginItem LoginItem
	// AppIcon renders an app bundle's icon as PNG.
	AppIcon func(bundlePath string) ([]byte, bool)
	// Selftest, if set, receives rendered page text (headroom --selftest).
	Selftest func(page, text string)
}

// Service is bound to the frontend. Exported methods are callable from JS.
type Service struct {
	cfg      Config
	an       *analyzer.Analyzer
	ui       UI
	log      *slog.Logger
	mu       sync.Mutex
	settings Settings
	wake     chan struct{}
	started  atomic.Bool
	hooks    []func(analyzer.Status) // extra per-sample consumers (notifications)

	act            *actions.Manager
	decider        *notify.Decider
	pendingSafety  string
	notifier       Notifier // nil when notifications are unavailable
	iconCache      map[string]string
	forceProcesses atomic.Bool // re-read processes now (after a quit)
}

// New creates the service. Call Run to start sampling.
func New(cfg Config) *Service {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	s := &Service{cfg: cfg, log: cfg.Logger, wake: make(chan struct{}, 1), iconCache: map[string]string{}}
	s.settings = loadSettings(cfg.Store.Settings())
	s.cfg.Store.TenMinRetention = time.Duration(s.settings.RetentionDays) * 24 * time.Hour
	s.an = analyzer.New(s.thresholds())
	s.an.OnEpisode = s.onEpisode
	s.setupActions()
	return s
}

// Analyzer exposes the analyzer (for actions and notifications).
func (s *Service) Analyzer() *analyzer.Analyzer { return s.an }

// OnSample registers a consumer called after every system sample.
func (s *Service) OnSample(fn func(analyzer.Status)) { s.hooks = append(s.hooks, fn) }

func (s *Service) thresholds() machine.Thresholds {
	def := s.cfg.Profile.Defaults().Apply(machine.Sensitivity(s.settings.Sensitivity))
	since := s.cfg.Store.HistoryStart()
	if since.IsZero() {
		return def
	}
	eps, err := s.cfg.Store.Episodes(time.Now().Add(-store.EpisodeRetention), time.Now())
	if err != nil {
		return def
	}
	return analyzer.Learn(def, eps, time.Since(since))
}

func (s *Service) onEpisode(e analyzer.Episode, started, ended bool) {
	id, err := s.cfg.Store.SaveEpisode(e)
	if err != nil {
		s.log.Warn("save episode", "err", err)
		return
	}
	if started {
		s.an.SetEpisodeID(id)
	}
}

// Run samples until ctx ends. Safe to call once.
func (s *Service) Run(ctx context.Context, ui UI) {
	if !s.started.CompareAndSwap(false, true) {
		return
	}
	s.ui = ui
	s.warmup()
	s.sampleProcesses()
	s.sampleSystem()
	lastSys, lastProc, lastMaint := time.Now(), time.Now(), time.Time{}
	// Sleep until the next sample is due rather than ticking: an idle
	// Headroom wakes about once every 5 s. Opening a panel calls Wake.
	timer := time.NewTimer(SlowInterval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-timer.C:
		}
		now := time.Now()
		interval := SlowInterval
		if ui.PanelsVisible() {
			interval = FastInterval
		}
		if s.forceProcesses.Swap(false) || now.Sub(lastProc) >= ProcessInterval {
			s.sampleProcesses()
			lastProc = now
		}
		if now.Sub(lastSys) >= interval {
			s.sampleSystem()
			lastSys = now
		}
		if now.Sub(lastMaint) >= MaintInterval {
			s.maintain(now)
			lastMaint = now
		}
		next := min(time.Until(lastSys.Add(interval)), time.Until(lastProc.Add(ProcessInterval)),
			time.Until(lastMaint.Add(MaintInterval)))
		timer.Reset(max(next, 50*time.Millisecond))
	}
}

// Wake makes the loop re-check immediately (e.g. when the dropdown opens).
func (s *Service) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// warmer is implemented by the demo collector: history to replay at start.
type warmer interface {
	Warmup() ([]collector.Snapshot, []collector.ProcessTable)
}

func (s *Service) warmup() {
	w, ok := s.cfg.Collector.(warmer)
	if !ok {
		return
	}
	snaps, tables := w.Warmup()
	ti := 0
	for _, snap := range snaps {
		for ti < len(tables) && !tables[ti].Time.After(snap.Time) {
			s.an.AddProcesses(tables[ti])
			ti++
		}
		st := s.an.AddSystem(snap)
		_ = s.cfg.Store.AddSample(store.SampleFromStatus(st, int(snap.Pressure), int(snap.Thermal)))
	}
}

func (s *Service) sampleProcesses() {
	pt, err := s.cfg.Collector.Processes()
	if err != nil {
		s.log.Warn("process sample", "err", err)
		return
	}
	s.an.AddProcesses(pt)
}

func (s *Service) sampleSystem() {
	snap, err := s.cfg.Collector.System()
	if err != nil {
		s.log.Warn("system sample", "err", err)
		return
	}
	st := s.an.AddSystem(snap)
	if err := s.cfg.Store.AddSample(store.SampleFromStatus(st, int(snap.Pressure), int(snap.Thermal))); err != nil {
		s.log.Warn("store sample", "err", err)
	}
	fill := 0.0
	if snap.Memory.Total > 0 {
		fill = float64(snap.Memory.Used) / float64(snap.Memory.Total)
	}
	s.mu.Lock()
	simple := s.settings.IconStyle == "simple"
	s.mu.Unlock()
	if s.ui != nil {
		s.ui.SetTrayIcon(st.State, fill, simple)
		// Only open panels listen; they fetch the status themselves on open.
		if s.ui.PanelsVisible() {
			s.ui.Emit("status", st)
		}
	}
	for _, h := range s.hooks {
		h(st)
	}
}

func (s *Service) maintain(now time.Time) {
	if err := s.cfg.Store.Rollup(now); err != nil {
		s.log.Warn("rollup", "err", err)
	}
	if err := s.cfg.Store.Prune(now); err != nil {
		s.log.Warn("prune", "err", err)
	}
	s.an.SetThresholds(s.thresholds())
}

// ---- bound methods: status and machine -----------------------------------------

// Status is the latest analyzer status (Ready=false during the first seconds).
func (s *Service) Status() analyzer.Status { return s.an.Status() }

// MachineInfo is the machine line and profile.
type MachineInfo struct {
	Profile machine.Profile `json:"profile"`
	Line    string          `json:"line"`
	Demo    bool            `json:"demo"`
}

// Machine returns the machine profile and its one-line description.
func (s *Service) Machine() MachineInfo {
	return MachineInfo{Profile: s.cfg.Profile, Line: s.cfg.Profile.Describe(time.Now()), Demo: s.cfg.Demo}
}

// Apps returns every app, largest first.
func (s *Service) Apps() []analyzer.AppRow { return s.an.Apps() }

// ---- timeline -----------------------------------------------------------------

// Timeline is the dashboard chart data for a range.
type Timeline struct {
	Range        string             `json:"range"`
	From         time.Time          `json:"from"`
	To           time.Time          `json:"to"`
	Points       []store.Point      `json:"points"`
	Episodes     []analyzer.Episode `json:"episodes"`
	Summary      string             `json:"summary"`
	SlowdownLine uint64             `json:"slowdownLine"` // memory + swap budget: past this, headroom is gone
	MemTotal     uint64             `json:"memTotal"`
	Empty        bool               `json:"empty"`
}

var ranges = map[string]time.Duration{"1h": time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour}

// Timeline returns chart points, episodes and the summary sentence for a
// range ("1h", "24h", "7d", "30d").
func (s *Service) Timeline(rangeKey string) (Timeline, error) {
	d, ok := ranges[rangeKey]
	if !ok {
		rangeKey, d = "24h", ranges["24h"]
	}
	to := time.Now()
	from := to.Add(-d)
	pts, err := s.cfg.Store.Timeline(from, to, 360)
	if err != nil {
		return Timeline{}, err
	}
	eps, err := s.cfg.Store.Episodes(from, to)
	if err != nil {
		return Timeline{}, err
	}
	th := s.an.Thresholds()
	since := s.cfg.Store.HistoryStart()
	return Timeline{
		Range: rangeKey, From: from, To: to, Points: pts, Episodes: eps, Summary: analyzer.Explain(eps),
		SlowdownLine: s.cfg.Profile.MemoryBytes + th.SwapBudget, MemTotal: s.cfg.Profile.MemoryBytes,
		Empty: since.IsZero() || to.Sub(since) < 10*time.Minute,
	}, nil
}

// History returns the action log, newest first.
func (s *Service) History() ([]store.Action, error) { return s.cfg.Store.Actions(200) }

// ---- settings -------------------------------------------------------------------

// Settings returns the current settings.
func (s *Service) Settings() Settings {
	s.mu.Lock()
	cur := s.settings
	s.mu.Unlock()
	if li := s.cfg.LoginItem; li != nil {
		on, approval, ok := li.Status()
		cur.LoginAvailable, cur.LoginNeedsApproval = ok, approval
		if ok {
			cur.StartAtLogin = on || approval
		}
	}
	return cur
}

// SetSetting validates and stores one setting, then applies it.
func (s *Service) SetSetting(key, value string) (Settings, error) {
	if !validSetting(key, value) {
		return s.Settings(), errInvalid("That setting can't be changed to that value.")
	}
	if key == "startAtLogin" && s.cfg.LoginItem != nil {
		if err := s.cfg.LoginItem.Set(value == "true"); err != nil {
			return s.Settings(), errInvalid("macOS didn't accept the change: " + err.Error() + " You can also change it in System Settings › General › Login Items.")
		}
	}
	if err := s.cfg.Store.SetSetting(key, value); err != nil {
		return s.Settings(), err
	}
	s.mu.Lock()
	s.settings = loadSettings(s.cfg.Store.Settings())
	cur := s.settings
	s.mu.Unlock()
	switch key {
	case "sensitivity":
		s.an.SetThresholds(s.thresholds())
	case "retentionDays":
		days, _ := strconv.Atoi(value)
		s.cfg.Store.TenMinRetention = time.Duration(days) * 24 * time.Hour
	}
	s.Wake()
	return cur, nil
}

// ---- windows ---------------------------------------------------------------------

// OpenDashboard shows the dashboard at a section and hides the dropdown.
func (s *Service) OpenDashboard(section string) {
	if s.ui != nil {
		s.ui.HideDropdown()
		s.ui.ShowDashboard(section)
		s.Wake()
	}
}

// HideDropdown closes the dropdown panel.
func (s *Service) HideDropdown() {
	if s.ui != nil {
		s.ui.HideDropdown()
	}
}

// ResizeDropdown fits the dropdown window to its content height.
func (s *Service) ResizeDropdown(height int) {
	if s.ui != nil && height >= 120 && height <= 900 {
		s.ui.ResizeDropdown(height)
	}
}

// UserError is an error whose message is written for the user: it says what
// happened and what to do, and never apologizes.
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

func errInvalid(msg string) error { return &UserError{Msg: msg} }
