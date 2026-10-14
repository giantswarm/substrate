//go:build linux

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

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/internal/actorlock"
	"github.com/agent-substrate/substrate/internal/ateomexit"
	"github.com/agent-substrate/substrate/internal/resources"
)

// startSandbox starts a process that stands in for a sandbox and writes its
// pid where runsc writes the root container's. The process is killed and
// reaped when the test ends.
func startSandbox(t *testing.T) (*exec.Cmd, string) {
	t.Helper()
	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the stand-in sandbox: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	pidFile := filepath.Join(t.TempDir(), "pause.pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		t.Fatalf("writing the pid file: %v", err)
	}
	return cmd, pidFile
}

// waitExited polls p until it reports an exit or the deadline passes.
func waitExited(t *testing.T, p *sandboxProcess) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		exited, err := p.exited()
		if err != nil {
			t.Fatalf("exited() = %v", err)
		}
		if exited {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func TestSandboxProcessReportsItsExit(t *testing.T) {
	for _, reaped := range []bool{true, false} {
		t.Run("reaped="+strconv.FormatBool(reaped), func(t *testing.T) {
			cmd, pidFile := startSandbox(t)
			p, err := openSandboxProcess(pidFile)
			if err != nil {
				t.Fatalf("openSandboxProcess() = %v", err)
			}
			defer p.close()
			if exited, err := p.exited(); err != nil || exited {
				t.Fatalf("exited() = %v, %v before the kill, want false", exited, err)
			}

			if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
				t.Fatalf("kill: %v", err)
			}
			// ateom's reaper may or may not have collected the sandbox when
			// the sweep looks: a zombie has exited all the same.
			if reaped {
				_ = cmd.Wait()
			}
			if !waitExited(t, p) {
				t.Error("exited() never reported the killed sandbox")
			}
		})
	}
}

func TestOpenSandboxProcessOfAGoneSandbox(t *testing.T) {
	cmd, pidFile := startSandbox(t)
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if _, err := watchSandbox(context.Background(), pidFile); !errors.Is(err, errSandboxGone) {
		t.Errorf("watchSandbox() = %v, want errSandboxGone: a boot whose sandbox is already gone fails", err)
	}
}

func TestWatchSandboxLeavesAnUnreadablePidUnwatched(t *testing.T) {
	sandbox, err := watchSandbox(context.Background(), filepath.Join(t.TempDir(), "missing.pid"))
	if err != nil || sandbox != nil {
		t.Errorf("watchSandbox() = %v, %v, want an unwatched workload", sandbox, err)
	}
}

// capturingReporter records the reports watchSandboxes sends.
type capturingReporter struct {
	mu      sync.Mutex
	reports []ateomexit.Actor
	reasons []string
	sent    chan struct{}
}

func newCapturingReporter() *capturingReporter {
	return &capturingReporter{sent: make(chan struct{}, 16)}
}

func (r *capturingReporter) ReportExit(_ context.Context, actor ateomexit.Actor, reason string) error {
	r.mu.Lock()
	r.reports = append(r.reports, actor)
	r.reasons = append(r.reasons, reason)
	r.mu.Unlock()
	r.sent <- struct{}{}
	return nil
}

func (r *capturingReporter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.reports)
}

var watchedActor = resources.ActorAttribution{Ref: resources.ActorRef{Atespace: "team-a", Name: "actor-1"}, UID: "actor-uid-1"}

// hostWatched hosts watchedActor with a session watching a stand-in sandbox,
// as RunWorkload and RestoreWorkload leave it, and starts the watch.
// uidLog records the actor UIDs a seam was called for.
type uidLog struct {
	mu   sync.Mutex
	uids []string
}

func (l *uidLog) add(uid string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.uids = append(l.uids, uid)
}

func (l *uidLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.uids)
}

func hostWatched(t *testing.T) (*AteomService, *exec.Cmd, *capturingReporter, *uidLog) {
	t.Helper()
	old := sandboxWatchInterval
	sandboxWatchInterval = 10 * time.Millisecond
	t.Cleanup(func() { sandboxWatchInterval = old })

	cmd, pidFile := startSandbox(t)
	sandbox, err := watchSandbox(context.Background(), pidFile)
	if err != nil || sandbox == nil {
		t.Fatalf("watchSandbox() = %v, %v", sandbox, err)
	}
	deactivated := &uidLog{}
	s := &AteomService{
		locks:  actorlock.New(),
		actors: map[string]*hostedActor{},
		deactivateActor: func(_ context.Context, a resources.ActorAttribution) error {
			deactivated.add(a.UID)
			return nil
		},
	}
	s.actors[watchedActor.UID] = &hostedActor{attribution: watchedActor}
	s.setSession(watchedActor.UID, &workloadSession{sandbox: sandbox})

	reporter := newCapturingReporter()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.watchSandboxes(ctx, reporter)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return s, cmd, reporter, deactivated
}

// The regression: a sandbox killed right after its actor was recorded
// RUNNING, with no RPC in flight, is reported within the watch interval, and
// its traffic is cut, instead of leaving the actor RUNNING and its turn
// waiting out the client's deadline.
func TestWatchSandboxesReportsAnExitAfterRestore(t *testing.T) {
	_, cmd, reporter, deactivated := hostWatched(t)
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("kill: %v", err)
	}

	select {
	case <-reporter.sent:
	case <-time.After(5 * time.Second):
		t.Fatal("the sandbox exit was never reported")
	}
	reporter.mu.Lock()
	report, reason := reporter.reports[0], reporter.reasons[0]
	reporter.mu.Unlock()
	want := ateomexit.Actor{Atespace: "team-a", Name: "actor-1", UID: "actor-uid-1"}
	if report != want {
		t.Errorf("reported %+v, want %+v", report, want)
	}
	if wantReason := "sandbox process " + strconv.Itoa(cmd.Process.Pid) + " exited"; reason != wantReason {
		t.Errorf("reason = %q, want %q", reason, wantReason)
	}
	if got := deactivated.get(); len(got) != 1 {
		t.Errorf("deactivated %v, want the actor's traffic cut once", got)
	}

	// Reported once, not on every sweep.
	time.Sleep(10 * sandboxWatchInterval)
	if n := reporter.count(); n != 1 {
		t.Errorf("reported %d times, want 1", n)
	}
}

// A sandbox a lifecycle RPC stops (a checkpoint, a terminate) is not
// reported: the RPC holds the actor's lock while it does, and unhosts the
// actor before it lets go.
func TestWatchSandboxesIgnoresAnExitALifecycleRPCCaused(t *testing.T) {
	s, cmd, reporter, deactivated := hostWatched(t)
	if !s.locks.Lock(context.Background(), watchedActor.UID) {
		t.Fatal("Lock() = false")
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("kill: %v", err)
	}
	// Let a sweep find the exit while the "RPC" holds the lock.
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.actorsMu.RLock()
		seen := s.actors[watchedActor.UID].session.exitSeen
		s.actorsMu.RUnlock()
		if seen {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no sweep found the exit")
		}
		time.Sleep(sandboxWatchInterval)
	}
	if err := s.unhostActor(context.Background(), watchedActor.UID); err != nil {
		t.Fatalf("unhostActor() = %v", err)
	}
	s.locks.Unlock(watchedActor.UID)

	time.Sleep(10 * sandboxWatchInterval)
	if n := reporter.count(); n != 0 {
		t.Errorf("reported %d exits, want none", n)
	}
	if got := deactivated.get(); len(got) != 0 {
		t.Errorf("deactivated %v, want none", got)
	}
}

func TestWatchSandboxesIgnoresExitsDuringShutdown(t *testing.T) {
	s, cmd, reporter, _ := hostWatched(t)
	s.shuttingDown.Store(true)
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("kill: %v", err)
	}
	time.Sleep(10 * sandboxWatchInterval)
	if n := reporter.count(); n != 0 {
		t.Errorf("reported %d exits during shutdown, want none", n)
	}
}

// Unhosting releases the watch: the session's pidfd is closed with it.
func TestUnhostActorClosesTheSandboxWatch(t *testing.T) {
	_, pidFile := startSandbox(t)
	sandbox, err := openSandboxProcess(pidFile)
	if err != nil {
		t.Fatalf("openSandboxProcess() = %v", err)
	}
	session := &workloadSession{sandbox: sandbox}
	s := &AteomService{actors: map[string]*hostedActor{"uid": {session: session}}}
	if err := s.unhostActor(context.Background(), "uid"); err != nil {
		t.Fatalf("unhostActor() = %v", err)
	}
	if session.sandbox != nil {
		t.Error("the session still holds its sandbox watch")
	}
}
