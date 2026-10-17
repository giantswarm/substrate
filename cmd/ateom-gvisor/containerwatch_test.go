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
	"testing"
	"time"

	"github.com/agent-substrate/substrate/internal/ateomexit"
)

// fakeContainers stands in for `runsc wait`: a container's wait returns the
// status sent on its channel, or fails when the channel closes, and ends
// with its context.
type fakeContainers struct {
	exits    map[string]chan int
	canceled chan string
}

func newFakeContainers(names ...string) *fakeContainers {
	f := &fakeContainers{exits: map[string]chan int{}, canceled: make(chan string, len(names))}
	for _, name := range names {
		f.exits[name] = make(chan int, 1)
	}
	return f
}

func (f *fakeContainers) cmdWaitExit(ctx context.Context, name string) (int, error) {
	select {
	case status, ok := <-f.exits[name]:
		if !ok {
			return 0, errors.New("the sandbox is gone")
		}
		return status, nil
	case <-ctx.Done():
		f.canceled <- name
		return 0, ctx.Err()
	}
}

// hostWatchedContainers hosts watchedActor as hostWatched does, with its
// application containers watched through containers.
func hostWatchedContainers(t *testing.T, containers *fakeContainers, names ...string) (*AteomService, *capturingReporter, *uidLog) {
	t.Helper()
	s, _, reporter, deactivated := hostWatched(t)
	s.actorsMu.Lock()
	session := s.actors[watchedActor.UID].session
	session.containers = names
	s.actorsMu.Unlock()
	s.watchContainers(session, containers)
	return s, reporter, deactivated
}

// The regression: an application container that exits while the pause
// container keeps the sandbox running is reported as the actor's exit, with
// its exit status, instead of leaving the actor RUNNING with nothing left in
// it to answer.
func TestWatchSandboxesReportsAContainerExitWhileTheSandboxRuns(t *testing.T) {
	containers := newFakeContainers("kagent")
	_, reporter, deactivated := hostWatchedContainers(t, containers, "kagent")
	containers.exits["kagent"] <- 128 + 9

	select {
	case <-reporter.sent:
	case <-time.After(5 * time.Second):
		t.Fatal("the container exit was never reported")
	}
	reporter.mu.Lock()
	report, reason := reporter.reports[0], reporter.reasons[0]
	reporter.mu.Unlock()
	if want := (ateomexit.Actor{Atespace: "team-a", Name: "actor-1", UID: "actor-uid-1"}); report != want {
		t.Errorf("reported %+v, want %+v", report, want)
	}
	if want := `container "kagent" was killed by signal 9 (SIGKILL) while the sandbox kept running`; reason != want {
		t.Errorf("reason = %q, want %q", reason, want)
	}
	if got := deactivated.get(); len(got) != 1 {
		t.Errorf("deactivated %v, want the actor's traffic cut once", got)
	}

	time.Sleep(10 * sandboxWatchInterval)
	if n := reporter.count(); n != 1 {
		t.Errorf("reported %d times, want 1", n)
	}
}

// A container a lifecycle RPC stops (a checkpoint stops them all) is not
// reported: the RPC holds the actor's lock and unhosts it before letting go.
func TestWatchSandboxesIgnoresAContainerExitALifecycleRPCCaused(t *testing.T) {
	containers := newFakeContainers("kagent")
	s, reporter, deactivated := hostWatchedContainers(t, containers, "kagent")
	if !s.locks.Lock(context.Background(), watchedActor.UID) {
		t.Fatal("Lock() = false")
	}
	containers.exits["kagent"] <- 0
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

// A wait that ends without an exit status (the sandbox went away) records
// nothing: the sandbox watch reports a sandbox exit itself.
func TestWatchContainersRecordsNoExitWithoutAStatus(t *testing.T) {
	containers := newFakeContainers("kagent")
	s, reporter, _ := hostWatchedContainers(t, containers, "kagent")
	close(containers.exits["kagent"])

	time.Sleep(10 * sandboxWatchInterval)
	s.actorsMu.RLock()
	exit := s.actors[watchedActor.UID].session.containerExit
	s.actorsMu.RUnlock()
	if exit != "" {
		t.Errorf("containerExit = %q, want none", exit)
	}
	if n := reporter.count(); n != 0 {
		t.Errorf("reported %d exits, want none", n)
	}
}

// Unhosting ends the container waits with the sandbox watch, so no `runsc
// wait` outlives the session it watches.
func TestUnhostActorEndsTheContainerWaits(t *testing.T) {
	containers := newFakeContainers("kagent", "sidecar")
	s, _, _ := hostWatchedContainers(t, containers, "kagent", "sidecar")
	if err := s.unhostActor(context.Background(), watchedActor.UID); err != nil {
		t.Fatalf("unhostActor() = %v", err)
	}
	ended := map[string]bool{}
	for len(ended) < 2 {
		select {
		case name := <-containers.canceled:
			ended[name] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("waits ended for %v, want kagent and sidecar", ended)
		}
	}
}

// A session already released (setSession found its actor gone) starts no
// waits.
func TestWatchContainersOfAClosedSessionStartsNoWait(t *testing.T) {
	session := &workloadSession{containers: []string{"kagent"}}
	s := &AteomService{actors: map[string]*hostedActor{}}
	s.setSession("gone", session)
	containers := newFakeContainers("kagent")
	s.watchContainers(session, containers)
	if session.stopContainerWatch != nil {
		t.Error("a closed session got a container watch")
	}
}

func TestContainerExitReason(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{0, `container "app" exited with status 0 while the sandbox kept running`},
		{2, `container "app" exited with status 2 while the sandbox kept running`},
		{128, `container "app" exited with status 128 while the sandbox kept running`},
		{128 + 9, `container "app" was killed by signal 9 (SIGKILL) while the sandbox kept running`},
		{128 + 11, `container "app" was killed by signal 11 (SIGSEGV) while the sandbox kept running`},
	} {
		if got := containerExitReason("app", tc.status); got != tc.want {
			t.Errorf("containerExitReason(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestParseWaitResult(t *testing.T) {
	if got, err := parseWaitResult([]byte(`{"id":"kagent","exitStatus":137}` + "\n")); err != nil || got != 137 {
		t.Errorf("parseWaitResult() = %d, %v, want 137", got, err)
	}
	for _, out := range []string{"", "not json", `{"id":"kagent"}`} {
		if _, err := parseWaitResult([]byte(out)); err == nil {
			t.Errorf("parseWaitResult(%q) succeeded, want an error", out)
		}
	}
}
