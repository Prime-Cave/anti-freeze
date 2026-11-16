// Command stress makes memory pressure on purpose, so you can watch Headroom
// go from ok to tight to critical on your own Mac (make stress).
//
// It starts a child process that allocates incompressible memory in steps,
// holds it briefly, then exits on its own. It is capped:
//
//   - it never asks for more than 1.5 × this Mac's memory (default 1.25 ×),
//   - it stops growing if free disk drops below 15 GB (swap lives on disk),
//   - it exits after the hold time, or at once on Ctrl-C.
//
// When the child exits, all its memory is returned immediately.
//
//	go run ./scripts/stress [-target 1.25] [-step 512] [-every 4s] [-hold 2m]
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

const (
	maxFactor   = 1.5
	minDiskFree = 15 << 30
)

func main() {
	child := flag.Bool("child", false, "internal: run the allocating child")
	target := flag.Float64("target", 1.25, "how much to allocate, as a multiple of this Mac's memory (max 1.5)")
	stepMB := flag.Int("step", 512, "MB to add at each step")
	every := flag.Duration("every", 4*time.Second, "time between steps")
	hold := flag.Duration("hold", 2*time.Minute, "how long to hold the memory once the target is reached")
	flag.Parse()

	mem := memsize()
	if mem == 0 {
		fmt.Fprintln(os.Stderr, "stress: can't read this Mac's memory size")
		os.Exit(1)
	}
	if *target <= 0 || *target > maxFactor {
		fmt.Fprintf(os.Stderr, "stress: -target must be between 0 and %.1f\n", maxFactor)
		os.Exit(2)
	}
	if *stepMB < 16 || *stepMB > 4096 {
		fmt.Fprintln(os.Stderr, "stress: -step must be between 16 and 4096 MB")
		os.Exit(2)
	}
	goal := uint64(float64(mem) * *target)
	if *child {
		runChild(goal, uint64(*stepMB)<<20, *every, *hold)
		return
	}

	fmt.Printf("Allocating up to %.1f GB (%.2f × %.0f GB memory), %d MB every %s, then holding for %s.\n",
		gb(goal), *target, gb(mem), *stepMB, *every, *hold)
	fmt.Println("Watch the Headroom menu bar icon. Press Ctrl-C to stop at any time.")

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "stress:", err)
		os.Exit(1)
	}
	// Fixed program (this binary) and flags; no shell.
	cmd := exec.Command(self, "-child", // #nosec G204 -- re-runs this same binary
		"-target", fmt.Sprint(*target), "-step", fmt.Sprint(*stepMB),
		"-every", every.String(), "-hold", hold.String())
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "stress:", err)
		os.Exit(1)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-stop:
		_ = cmd.Process.Signal(syscall.SIGTERM)
		if err := <-done; err != nil {
			fmt.Println("Child stopped:", err) // expected after SIGTERM
		}
		fmt.Println("Stopped. Memory returned.")
	case err := <-done:
		if err != nil {
			fmt.Fprintln(os.Stderr, "stress:", err)
			os.Exit(1)
		}
		fmt.Println("Done. Memory returned.")
	}
}

func runChild(goal, step uint64, every, hold time.Duration) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	rng := rand.NewChaCha8([32]byte{1, 2, 3})
	var chunks [][]byte
	var have uint64
	tick := time.NewTicker(every)
	defer tick.Stop()
	for have < goal {
		if free := diskFree(); free > 0 && free < minDiskFree {
			fmt.Printf("Stopping growth: only %.1f GB of disk is free.\n", gb(free))
			break
		}
		n := min(step, goal-have)
		b := make([]byte, n)
		_, _ = rng.Read(b) // random bytes, so macOS can't compress them away
		chunks = append(chunks, b)
		have += n
		fmt.Printf("  %.1f GB allocated\n", gb(have))
		select {
		case <-stop:
			return
		case <-tick.C:
		}
	}
	fmt.Printf("Holding %.1f GB for %s.\n", gb(have), hold)
	select {
	case <-stop:
	case <-time.After(hold):
	}
	_ = chunks
}

func memsize() uint64 {
	s, err := syscall.Sysctl("hw.memsize")
	if err != nil {
		return 0
	}
	b := []byte(s)
	for len(b) < 8 { // Sysctl trims a trailing zero byte
		b = append(b, 0)
	}
	return binary.LittleEndian.Uint64(b[:8])
}

func diskFree() uint64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs("/System/Volumes/Data", &st); err != nil {
		return 0
	}
	return st.Bavail * uint64(st.Bsize)
}

func gb(b uint64) float64 { return float64(b) / (1 << 30) }
