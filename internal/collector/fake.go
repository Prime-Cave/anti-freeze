package collector

import (
	"errors"
	"sync"
	"time"
)

// Fake replays a fixed sequence of snapshots and process tables. When the
// sequence runs out it keeps returning the last entry. Used by tests and by
// the app's demo mode (HEADROOM_DEMO=1), which never touches real processes.
type Fake struct {
	mu     sync.Mutex
	Snaps  []Snapshot
	Tables []ProcessTable
	si, ti int
	// Clock, if set, stamps each returned sample with Clock() instead of the
	// recorded time.
	Clock func() time.Time
}

// System returns the next recorded snapshot.
func (f *Fake) System() (Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Snaps) == 0 {
		return Snapshot{}, errors.New("fake: no snapshots")
	}
	s := f.Snaps[min(f.si, len(f.Snaps)-1)]
	f.si++
	if f.Clock != nil {
		s.Time = f.Clock()
	}
	return s, nil
}

// Processes returns the next recorded process table.
func (f *Fake) Processes() (ProcessTable, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Tables) == 0 {
		return ProcessTable{}, errors.New("fake: no process tables")
	}
	t := f.Tables[min(f.ti, len(f.Tables)-1)]
	f.ti++
	if f.Clock != nil {
		t.Time = f.Clock()
	}
	return t, nil
}
