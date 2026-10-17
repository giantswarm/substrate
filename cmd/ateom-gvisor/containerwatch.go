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
	"fmt"
	"log/slog"

	"golang.org/x/sys/unix"
)

// containerExitWaiter blocks until an application container's process exits
// and returns its exit status as `runsc wait` reports it: the exit code, or
// 128 plus the signal that killed it. *runsc satisfies it.
type containerExitWaiter interface {
	cmdWaitExit(ctx context.Context, containerName string) (int, error)
}

// watchContainers waits on every application container of a session that was
// just recorded for its actor. The sandbox outlives an application container
// that exits on its own: the pause container keeps it running, so the sandbox
// watch never sees it, the actor stays RUNNING with nothing left to answer it,
// and its sockets close with the process. A container found exited is recorded
// on the session, and the next sweep of watchSandboxes reports it as the
// actor's exit, under the same rule as a sandbox exit: an exit a lifecycle RPC
// caused (a checkpoint stops the containers) is never reported. The waits end
// when the session's watch is closed.
func (s *AteomService) watchContainers(session *workloadSession, waiter containerExitWaiter) {
	ctx, cancel := context.WithCancel(context.Background())
	s.actorsMu.Lock()
	if session.closed {
		s.actorsMu.Unlock()
		cancel()
		return
	}
	session.stopContainerWatch = cancel
	s.actorsMu.Unlock()

	for _, name := range session.containers {
		go s.watchContainer(ctx, session, waiter, name)
	}
}

func (s *AteomService) watchContainer(ctx context.Context, session *workloadSession, waiter containerExitWaiter, name string) {
	status, err := waiter.cmdWaitExit(ctx, name)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		// The sandbox is gone (the sandbox watch reports that) or runsc could
		// not wait on the container; either way there is no exit status.
		slog.DebugContext(ctx, "Not watching the container for an exit", slog.String("container", name), slog.Any("err", err))
		return
	}
	reason := containerExitReason(name, status)
	s.actorsMu.Lock()
	if !session.closed && session.containerExit == "" {
		session.containerExit = reason
	}
	s.actorsMu.Unlock()
}

// containerExitReason words an application container's exit status for the
// actor's crash message.
func containerExitReason(name string, status int) string {
	if status > 128 {
		signal := unix.Signal(status - 128)
		return fmt.Sprintf("container %q was killed by signal %d (%s) while the sandbox kept running", name, int(signal), unix.SignalName(signal))
	}
	return fmt.Sprintf("container %q exited with status %d while the sandbox kept running", name, status)
}
