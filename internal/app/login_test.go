package app

import (
	"path/filepath"
	"testing"

	"github.com/Prime-Cave/anti-freeze/internal/machine"
	"github.com/Prime-Cave/anti-freeze/internal/store"
)

type fakeLogin struct {
	installed bool
	on        bool
	sets      []bool
}

func (f *fakeLogin) Status() (on, needsApproval, available bool) { return f.on, false, true }
func (f *fakeLogin) Set(on bool) error                           { f.sets = append(f.sets, on); f.on = on; return nil }
func (f *fakeLogin) Installed() bool                             { return f.installed }

func newLoginService(t *testing.T, li LoginItem, demo bool) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(Config{Store: st, Profile: machine.Profile{MemoryBytes: 8 << 30, DiskBytes: 256 << 30}, LoginItem: li, Demo: demo})
}

func TestLoginItemTurnedOnOnceWhenInstalled(t *testing.T) {
	li := &fakeLogin{installed: true}
	s := newLoginService(t, li, false)
	s.SetupLoginItemOnce()
	if len(li.sets) != 1 || !li.sets[0] {
		t.Fatalf("first launch: want one Set(true), got %v", li.sets)
	}
	// The user turns it off; a later launch must leave that alone.
	_ = li.Set(false)
	li.sets = nil
	s.SetupLoginItemOnce()
	if len(li.sets) != 0 {
		t.Fatalf("second launch changed the login item: %v", li.sets)
	}
}

func TestLoginItemNotTouchedWhenNotInstalled(t *testing.T) {
	li := &fakeLogin{installed: false}
	s := newLoginService(t, li, false)
	s.SetupLoginItemOnce()
	if len(li.sets) != 0 {
		t.Fatalf("copy outside Applications registered itself: %v", li.sets)
	}
}

func TestLoginItemNotTouchedInDemo(t *testing.T) {
	li := &fakeLogin{installed: true}
	s := newLoginService(t, li, true)
	s.SetupLoginItemOnce()
	if len(li.sets) != 0 {
		t.Fatalf("demo mode changed the login item: %v", li.sets)
	}
}
