// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build unix

package childreap

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// Short subprocesses must stay short while a long one is always in flight
// beside them. This needs continuous arrival with mixed durations, not just N
// concurrent commands.
func TestShortSubprocessesAreNotPacedByLongOnes(t *testing.T) {
	const (
		shortRunners = 8
		short        = 20 * time.Millisecond
		long         = 400 * time.Millisecond
		testFor      = 3 * time.Second
	)

	r := New()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go r.Run(ctx)

	run, stop := context.WithTimeout(ctx, testFor)
	defer stop()
	var wg sync.WaitGroup

	// Keep SIGCHLD pending throughout the test.
	for range 4 {
		wg.Go(func() {
			for run.Err() == nil {
				cmd := exec.Command("true")
				if cmd.Start() != nil {
					return
				}
				_ = cmd.Wait()
			}
		})
	}

	// Keep one long subprocess in flight.
	wg.Go(func() {
		for run.Err() == nil {
			_ = runUnder(r, exec.Command("/bin/sleep", "0.4"))
		}
	})

	var mu sync.Mutex
	var samples []time.Duration
	for range shortRunners {
		wg.Go(func() {
			for run.Err() == nil {
				started := time.Now()
				err := runUnder(r, exec.Command("/bin/sleep", "0.02"))
				elapsed := time.Since(started)
				mu.Lock()
				if err != nil {
					t.Errorf("short subprocess: %v", err)
				}
				samples = append(samples, elapsed)
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	if len(samples) < 50 {
		t.Fatalf("only %d short subprocesses completed in %s; too few to judge", len(samples), testFor)
	}
	slices.Sort(samples)
	p90 := samples[len(samples)*90/100]

	// Distinguish normal overhead from pacing by the long subprocess.
	if limit := 8 * short; p90 > limit {
		t.Errorf("p90 of %d short subprocesses was %s, want under %s; they are being paced by the long one (median %s, max %s)",
			len(samples), p90.Round(time.Millisecond), limit,
			samples[len(samples)/2].Round(time.Millisecond), samples[len(samples)-1].Round(time.Millisecond))
	}
	t.Logf("%d short subprocesses: p50=%s p90=%s max=%s", len(samples),
		samples[len(samples)/2].Round(time.Millisecond), p90.Round(time.Millisecond),
		samples[len(samples)-1].Round(time.Millisecond))
}

func runUnder(r *Reaper, cmd *exec.Cmd) error {
	return r.RunCommand(cmd)
}

func TestReapCollectsOrphans(t *testing.T) {
	r := New()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go r.Run(ctx)

	// Leave the child for the reaper.
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := cmd.Process.Pid

	deadline := time.Now().Add(10 * time.Second)
	for {
		// An unreaped zombie still responds to signal 0.
		if err := cmd.Process.Signal(nil); err != nil {
			return // reaped
		}
		if time.Now().After(deadline) {
			t.Fatalf("child %d was never reaped", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestExecIsExcludedWhileReaping verifies that wait4 cannot race Enter.
func TestExecIsExcludedWhileReaping(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New()

		// Enter the reaper's critical section directly.
		if !r.acquire(context.Background()) {
			t.Fatal("acquire failed on an idle reaper")
		}

		entered := make(chan struct{})
		go func() {
			defer r.Enter()()
			close(entered)
		}()
		synctest.Wait()

		select {
		case <-entered:
			t.Fatal("Enter returned while a reap was in progress")
		default:
		}

		r.release()
		<-entered
	})
}

func TestMaxDeferDrainsInFlightSubprocesses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New()
		leave := r.Enter()

		acquired := make(chan bool)
		go func() {
			acquired <- r.acquire(t.Context())
		}()
		synctest.Wait()
		time.Sleep(MaxDefer)
		synctest.Wait()

		r.mu.Lock()
		draining := r.draining
		r.mu.Unlock()
		if !draining {
			t.Fatal("reaper never began draining")
		}

		entered := make(chan func(), 1)
		go func() {
			entered <- r.Enter()
		}()
		synctest.Wait()
		select {
		case <-entered:
			t.Fatal("new subprocess entered while the reaper was draining")
		default:
		}

		leave()
		if ok := <-acquired; !ok {
			t.Fatal("reaper did not acquire after in-flight subprocess exited")
		}
		r.release()

		leaveEntered := <-entered
		leaveEntered()
	})
}

// An abandoned drain must not block later subprocesses.
func TestAnEntryThatNeverLeavesDoesNotBlockLaterCommands(t *testing.T) {
	const horizon = 10 * time.Minute

	synctest.Test(t, func(t *testing.T) {
		r := New()

		defer r.Enter()()

		gaveUp := make(chan bool, 1)
		go func() { gaveUp <- r.acquire(t.Context()) }()

		// The reaper must not consume the tracked child's status.
		synctest.Wait()
		select {
		case got := <-gaveUp:
			t.Fatalf("acquire returned %v immediately, want it to wait for the entry", got)
		default:
		}

		time.Sleep(horizon)
		synctest.Wait()
		select {
		case got := <-gaveUp:
			if got {
				t.Fatal("acquire reported it reaped, want it to have given the round up")
			}
		default:
			t.Fatalf("acquire still waiting after %s; the held entry blocks reaping forever", horizon)
		}

		done := make(chan struct{})
		go func() {
			defer close(done)
			defer r.Enter()()
		}()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("a later Enter is still blocked by the abandoned drain")
		}
	})
}

// A timer callback after acquire returns must not alter reaper state.
func TestALateDeadlineCallbackLeavesNothingBehind(t *testing.T) {
	r := New()

	if !r.acquire(t.Context()) {
		t.Fatal("acquire failed on an idle reaper")
	}
	r.release()

	r.wake()

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer r.Enter()()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a later Enter is blocked: a deadline callback left reaper state behind")
	}
}

// An abandoned round leaves children unreaped, and SIGCHLD fires on a
// transition, so nothing will announce them again. Releasing the entry that
// held the reaper off has to be what brings it back.
func TestAnAbandonedRoundRetriesOnceTheEntryLeaves(t *testing.T) {
	const horizon = 10 * time.Minute

	synctest.Test(t, func(t *testing.T) {
		r := New()

		leave := r.Enter()

		gaveUp := make(chan bool, 1)
		go func() { gaveUp <- r.acquire(t.Context()) }()

		time.Sleep(horizon)
		synctest.Wait()
		if got := <-gaveUp; got {
			t.Fatal("acquire reported it reaped, want it to have given the round up")
		}

		select {
		case <-r.retry:
			t.Fatal("the reaper was re-armed while the entry was still held")
		default:
		}

		leave()
		synctest.Wait()
		select {
		case <-r.retry:
		default:
			t.Fatal("leaving the entry did not re-arm the reaper; the orphans it gave up on stay zombies")
		}
	})
}

// TestOrphansAreReapedWhileACommandRuns is runsc deleting a sandbox the pod's
// init inherited: the command waits for an orphan's PID to go away, which
// takes the reaper collecting the orphan while the command runs.
func TestOrphansAreReapedWhileACommandRuns(t *testing.T) {
	r := New()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go r.Run(ctx)

	// Left for the reaper, like a killed sandbox.
	orphan := exec.Command("/bin/sleep", "0.2")
	if err := orphan.Start(); err != nil {
		t.Fatalf("start orphan: %v", err)
	}
	pid := strconv.Itoa(orphan.Process.Pid)

	// An unreaped zombie still answers signal 0.
	waitCtx, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	started := time.Now()
	waiter := exec.CommandContext(waitCtx, "/bin/sh", "-c", "while kill -0 "+pid+" 2>/dev/null; do sleep 0.02; done")
	if err := r.RunCommand(waiter); err != nil {
		t.Fatalf("the command waiting for the orphan failed after %s: %v", time.Since(started).Round(time.Millisecond), err)
	}
}

// TestCommandsKeepTheirExitStatus runs commands with known exit statuses while
// orphans keep the reaper busy.
func TestCommandsKeepTheirExitStatus(t *testing.T) {
	r := New()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go r.Run(ctx)

	run, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for run.Err() == nil {
				if exec.Command("true").Start() != nil {
					return
				}
			}
		})
	}
	for range 8 {
		wg.Go(func() {
			for run.Err() == nil {
				err := r.RunCommand(exec.Command("/bin/sh", "-c", "exit 3"))
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 3 {
					t.Errorf("RunCommand = %v, want exit status 3", err)
					return
				}
			}
		})
	}
	wg.Wait()
}

// TestAnotherReaperLeavesACommandItsExitStatus is a reaper whose round outlives
// its context, or a second one in the process: it must not take the exit
// status of a command it did not start.
func TestAnotherReaperLeavesACommandItsExitStatus(t *testing.T) {
	r, other := New(), New()
	cmd := exec.Command("/bin/sh", "-c", "exit 3")
	if err := r.start(cmd); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer r.untrack(cmd.Process.Pid)

	// Exited and not yet waited for: the window the other reaper races.
	stat := "/proc/" + strconv.Itoa(cmd.Process.Pid) + "/stat"
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, err := os.ReadFile(stat)
		if err != nil {
			t.Fatalf("read %s: %v", stat, err)
		}
		if end := bytes.LastIndexByte(b, ')'); end >= 0 && bytes.HasPrefix(b[end+1:], []byte(" Z")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("command %d never exited", cmd.Process.Pid)
		}
		time.Sleep(time.Millisecond)
	}

	other.reapOnce(t.Context())

	err := cmd.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("Wait = %v, want exit status 3", err)
	}
}

func TestCombinedOutput(t *testing.T) {
	out, err := New().CombinedOutput(exec.Command("/bin/sh", "-c", "echo out; echo err >&2"))
	if err != nil {
		t.Fatalf("CombinedOutput: %v", err)
	}
	if got, want := string(out), "out\nerr\n"; got != want {
		t.Errorf("CombinedOutput = %q, want %q", got, want)
	}
	cmd := exec.Command("true")
	cmd.Stdout = &strings.Builder{}
	if _, err := New().CombinedOutput(cmd); err == nil {
		t.Error("CombinedOutput with Stdout set succeeded, want an error")
	}
}

// TestChildPIDsListsBothWays checks the children files and the /proc scan
// agree on a child of this process.
func TestChildPIDsListsBothWays(t *testing.T) {
	child := exec.Command("/bin/sleep", "5")
	if err := child.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	}()
	listers := map[string]func() ([]int, error){"scan": childPIDsFromScan}
	if hasChildrenFiles() {
		listers["children files"] = childPIDsFromTasks
	}
	for name, list := range listers {
		pids, err := list()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !slices.Contains(pids, child.Process.Pid) {
			t.Errorf("%s = %v, want it to hold the child %d", name, pids, child.Process.Pid)
		}
	}
}

func TestParentPID(t *testing.T) {
	tests := []struct {
		stat     string
		wantPPID int
		wantOK   bool
	}{
		{"42 (runsc) S 1 42 42 0 -1", 1, true},
		{"42 (a) (b) Z 7 42 42 0 -1", 7, true},
		{"42 (cut", 0, false},
		{"42 (x) S", 0, false},
	}
	for _, tt := range tests {
		ppid, ok := parentPID([]byte(tt.stat))
		if ppid != tt.wantPPID || ok != tt.wantOK {
			t.Errorf("parentPID(%q) = %d, %v, want %d, %v", tt.stat, ppid, ok, tt.wantPPID, tt.wantOK)
		}
	}
}
