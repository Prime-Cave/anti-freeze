//go:build darwin

package collector

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNativeSystemSane(t *testing.T) {
	c := NewNative()
	time.Sleep(200 * time.Millisecond)
	s, err := c.System()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Unavailable) > 0 {
		t.Logf("unavailable: %v", s.Unavailable)
	}
	m := s.Memory
	if m.Total == 0 || m.Used == 0 || m.Used > m.Total {
		t.Fatalf("implausible memory: %+v", m)
	}
	if s.Disk.Total == 0 || s.Disk.Available > s.Disk.Total*2 {
		t.Fatalf("implausible disk: %+v", s.Disk)
	}
	if s.Pressure != PressureNormal && s.Pressure != PressureWarn && s.Pressure != PressureCritical {
		t.Fatalf("pressure level %d", s.Pressure)
	}
	if s.CPU.Cores <= 0 || s.CPU.Usage < 0 || s.CPU.Usage > 1 {
		t.Fatalf("cpu %+v", s.CPU)
	}
}

// TestNativeOwnProcessMatchesPS checks that our CPU time conversion agrees
// with ps for a process we own (this test binary, after burning some CPU).
func TestNativeOwnProcessMatchesPS(t *testing.T) {
	deadline := time.Now().Add(400 * time.Millisecond)
	x := 0
	for time.Now().Before(deadline) {
		x++
	}
	_ = x
	c := NewNative()
	pt, err := c.Processes()
	if err != nil {
		t.Fatal(err)
	}
	var me *Process
	for i := range pt.Processes {
		if pt.Processes[i].PID == os.Getpid() {
			me = &pt.Processes[i]
		}
	}
	if me == nil || !me.Accessible {
		t.Fatalf("own process not found or not accessible: %+v", me)
	}
	out, err := exec.Command("/bin/ps", "-o", "time=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		t.Skip("ps unavailable")
	}
	psSecs := parsePSTime(strings.TrimSpace(string(out)))
	ours := float64(me.CPUTimeNs) / 1e9
	if ours < 0.2 || ours > psSecs+1.0 || ours < psSecs-1.0 {
		t.Fatalf("cpu time mismatch: ours %.2fs, ps %.2fs", ours, psSecs)
	}
	if me.Footprint == 0 {
		t.Fatal("zero footprint for own process")
	}
}

func parsePSTime(s string) float64 {
	// [[dd-]hh:]mm:ss.cc
	parts := strings.Split(s, ":")
	var secs float64
	for _, p := range parts {
		v, _ := strconv.ParseFloat(p, 64)
		secs = secs*60 + v
	}
	return secs
}
