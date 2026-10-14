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
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/agent-substrate/substrate/internal/ateomexit"
	"github.com/agent-substrate/substrate/internal/resources"
)

// sandboxWatchInterval is how often ateom checks the sandboxes it hosts for an
// exit. A var so tests can shorten it.
var sandboxWatchInterval = time.Second

// errSandboxGone is a sandbox process that exited before it could be watched.
var errSandboxGone = errors.New("the sandbox process is gone")

// sandboxProcess is a pidfd on an actor's sandbox process, the one runsc
// writes to the root container's pid file. It turns readable once the process
// exits, whether the reaper collected it yet or not, and never names a later
// process that reuses the pid.
type sandboxProcess struct {
	pid int
	fd  int
}

// openSandboxProcess opens the sandbox process pidFile names.
func openSandboxProcess(pidFile string) (*sandboxProcess, error) {
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		return nil, fmt.Errorf("while reading the sandbox pid file: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return nil, fmt.Errorf("sandbox pid file %s names no process: %q", pidFile, raw)
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil, fmt.Errorf("%w: pid %d", errSandboxGone, pid)
	}
	if err != nil {
		return nil, fmt.Errorf("while opening a pidfd on sandbox pid %d: %w", pid, err)
	}
	return &sandboxProcess{pid: pid, fd: fd}, nil
}

// exited reports, without blocking, whether the process has exited.
func (p *sandboxProcess) exited() (bool, error) {
	fds := []unix.PollFd{{Fd: int32(p.fd), Events: unix.POLLIN}}
	for {
		n, err := unix.Poll(fds, 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("while polling sandbox pid %d: %w", p.pid, err)
		}
		return n > 0 && fds[0].Revents&unix.POLLIN != 0, nil
	}
}

func (p *sandboxProcess) close() {
	_ = unix.Close(p.fd)
}

// watchSandbox opens the sandbox of a workload that just booted or restored,
// for watchSandboxes. A sandbox already gone fails the boot, which the
// control plane records as the crash it is. A pidfd the kernel does not grant
// leaves the workload unwatched, as before.
func watchSandbox(ctx context.Context, pidFile string) (*sandboxProcess, error) {
	sandbox, err := openSandboxProcess(pidFile)
	if errors.Is(err, errSandboxGone) {
		return nil, err
	}
	if err != nil {
		slog.WarnContext(ctx, "Not watching the sandbox for an exit", slog.Any("err", err))
		return nil, nil
	}
	return sandbox, nil
}

// sandboxExitReporter reports an actor whose sandbox exited to the control
// plane. *ateomexit.Reporter satisfies it.
type sandboxExitReporter interface {
	ReportExit(ctx context.Context, actor ateomexit.Actor, reason string) error
}

// sandboxExit is a hosted sandbox found exited. pid is read under actorsMu
// with it: the session's watch is closed when the actor is unhosted.
type sandboxExit struct {
	attribution resources.ActorAttribution
	session     *workloadSession
	pid         int
}

// watchSandboxes reports every hosted sandbox that exits while no lifecycle
// RPC stops it, until ctx ends. Without it, an actor whose sandbox died stays
// RUNNING in the control plane, and every request routed to it waits out its
// caller's deadline.
func (s *AteomService) watchSandboxes(ctx context.Context, reporter sandboxExitReporter) {
	ticker := time.NewTicker(sandboxWatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		for _, exit := range s.exitedSandboxes(ctx) {
			go s.handleSandboxExit(ctx, exit, reporter)
		}
	}
}

// exitedSandboxes returns the hosted sandboxes found exited, each once. None
// during shutdown, which stops them itself.
func (s *AteomService) exitedSandboxes(ctx context.Context) []sandboxExit {
	if s.shuttingDown.Load() {
		return nil
	}
	s.actorsMu.Lock()
	defer s.actorsMu.Unlock()
	var out []sandboxExit
	for _, hosted := range s.actors {
		session := hosted.session
		if session == nil || session.sandbox == nil || session.exitSeen {
			continue
		}
		exited, err := session.sandbox.exited()
		if err != nil {
			slog.WarnContext(ctx, "Failed to check the sandbox for an exit", slog.String("actorUID", hosted.attribution.UID), slog.Any("err", err))
			continue
		}
		if exited {
			session.exitSeen = true
			out = append(out, sandboxExit{attribution: hosted.attribution, session: session, pid: session.sandbox.pid})
		}
	}
	return out
}

// handleSandboxExit reports exit unless a lifecycle RPC caused it. Under the
// actor's lock no RPC is midway, and one that stopped the sandbox itself (a
// checkpoint, a terminate) has unhosted the actor or replaced its session by
// the time it releases the lock. The actor's traffic is cut first, so its
// callers fail now; the report follows outside the lock, which the crash's
// Terminate takes.
func (s *AteomService) handleSandboxExit(ctx context.Context, exit sandboxExit, reporter sandboxExitReporter) {
	uid := exit.attribution.UID
	if !s.locks.Lock(ctx, uid) {
		return
	}
	hosted := s.lookupActor(uid)
	unprompted := hosted != nil && hosted.session == exit.session && !s.shuttingDown.Load()
	if unprompted {
		if err := s.deactivate(ctx, exit.attribution); err != nil {
			slog.WarnContext(ctx, "Failed to cut the traffic of an actor whose sandbox exited", slog.String("actorUID", uid), slog.Any("err", err))
		}
	}
	s.locks.Unlock(uid)
	if !unprompted {
		return
	}

	reason := fmt.Sprintf("sandbox process %d exited", exit.pid)
	slog.ErrorContext(ctx, "Actor's sandbox exited while it was running; reporting it",
		slog.String("actor", exit.attribution.Ref.String()), slog.String("actorUID", uid), slog.String("reason", reason))
	if s.actorLogger != nil {
		s.actorLogger.EmitLifecycleLog(ctx, "Actor sandbox exited", exit.attribution)
	}
	err := reporter.ReportExit(ctx, ateomexit.Actor{
		Atespace: exit.attribution.Ref.Atespace,
		Name:     exit.attribution.Ref.Name,
		UID:      uid,
	}, reason)
	if err != nil {
		slog.ErrorContext(ctx, "The control plane did not take the sandbox exit report",
			slog.String("actorUID", uid), slog.Any("err", err))
		return
	}
	slog.InfoContext(ctx, "Reported the sandbox exit; the actor is crashed", slog.String("actorUID", uid))
}

// deactivate cuts the actor's traffic through the tunnel.
func (s *AteomService) deactivate(ctx context.Context, attribution resources.ActorAttribution) error {
	if s.deactivateActor != nil {
		return s.deactivateActor(ctx, attribution)
	}
	return s.tunnel.Deactivate(ctx, attribution)
}
