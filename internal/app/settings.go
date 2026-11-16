package app

import (
	"strconv"

	"github.com/Prime-Cave/anti-freeze/internal/machine"
)

// Settings are the user's choices, stored in the settings table.
type Settings struct {
	StartAtLogin    bool   `json:"startAtLogin"`
	Sensitivity     string `json:"sensitivity"`     // relaxed | balanced | early
	CooldownMinutes int    `json:"cooldownMinutes"` // per-cause notification cooldown
	IconStyle       string `json:"iconStyle"`       // fill | simple
	RetentionDays   int    `json:"retentionDays"`   // 10-minute rollups: 30 | 90
	ShowForceQuit   bool   `json:"showForceQuit"`   // force quit is hidden by default
	Notifications   bool   `json:"notifications"`

	// Login item state from macOS, filled in by Service.Settings.
	LoginAvailable     bool `json:"loginAvailable"`
	LoginNeedsApproval bool `json:"loginNeedsApproval"`
}

// DefaultSettings are used until the user changes anything.
func DefaultSettings() Settings {
	return Settings{StartAtLogin: true, Sensitivity: string(machine.Balanced), CooldownMinutes: 20,
		IconStyle: "fill", RetentionDays: 90, Notifications: true}
}

func loadSettings(kv map[string]string) Settings {
	s := DefaultSettings()
	if v, ok := kv["startAtLogin"]; ok {
		s.StartAtLogin = v == "true"
	}
	if v := kv["sensitivity"]; v == "relaxed" || v == "balanced" || v == "early" {
		s.Sensitivity = v
	}
	if v, err := strconv.Atoi(kv["cooldownMinutes"]); err == nil && v >= 5 && v <= 240 {
		s.CooldownMinutes = v
	}
	if v := kv["iconStyle"]; v == "fill" || v == "simple" {
		s.IconStyle = v
	}
	if v, err := strconv.Atoi(kv["retentionDays"]); err == nil && (v == 30 || v == 90) {
		s.RetentionDays = v
	}
	if v, ok := kv["showForceQuit"]; ok {
		s.ShowForceQuit = v == "true"
	}
	if v, ok := kv["notifications"]; ok {
		s.Notifications = v == "true"
	}
	return s
}

// validSetting checks a key/value pair from the UI before it is stored.
func validSetting(k, v string) bool {
	switch k {
	case "startAtLogin", "showForceQuit", "notifications":
		return v == "true" || v == "false"
	case "sensitivity":
		return v == "relaxed" || v == "balanced" || v == "early"
	case "cooldownMinutes":
		n, err := strconv.Atoi(v)
		return err == nil && n >= 5 && n <= 240
	case "iconStyle":
		return v == "fill" || v == "simple"
	case "retentionDays":
		return v == "30" || v == "90"
	}
	return false
}
