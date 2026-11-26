//go:build linux

// Copyright 2026 The Agent Substrate Authors
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
	"path/filepath"
	"slices"
	"time"

	"golang.org/x/sys/unix"

	"go.ate.dev/substrate/internal/ateworker/register"
	"go.ate.dev/substrate/internal/proto/ateworkerpb"
	"go.ate.dev/substrate/internal/resources"
)

// crashReportTimeout bounds how long a crash report keeps retrying.
const crashReportTimeout = 5 * time.Minute

// crashReporter is the slice of register.CrashReporter the service uses.
type crashReporter interface {
	ReportCrashed(ctx context.Context, actor resources.ActorAttribution, message string) error
}

var _ crashReporter = (*register.CrashReporter)(nil)

// watchSandbox handles the actor's sandbox dying while nothing is stopping it:
// the actor is torn down here, since the control plane will not reach this
// worker for it again, and then reported crashed.
func (s *AteWorkerService) watchSandbox(ctx context.Context, hosted *hostedActor, runscPath string, actorDirs *ateworkerpb.ActorDirs, containers []*ateworkerpb.Container) {
	attribution := hosted.attribution
	if hosted.sandbox == nil {
		slog.WarnContext(ctx, "Cannot watch the sandbox, a crash will go unreported: it was never pinned", slog.String("actorUID", attribution.UID))
		return
	}
	exited, err := hosted.sandbox.exited()
	if err != nil {
		slog.WarnContext(ctx, "Cannot watch the sandbox, a crash will go unreported", slog.String("actorUID", attribution.UID), slog.Any("err", err))
		return
	}
	go func() {
		<-exited
		if s.shuttingDown.Load() {
			return
		}
		message := "gVisor sandbox exited"
		if hosted.memory.Killed() {
			message = "killed for exceeding its memory limit"
		}
		s.crashHosted(hosted, runscPath, actorDirs, containers, message)
	}()
}

// crashHosted tears hosted down, unless ateworker is stopping it or it was
// replaced, and then reports it crashed with message.
func (s *AteWorkerService) crashHosted(hosted *hostedActor, runscPath string, actorDirs *ateworkerpb.ActorDirs, containers []*ateworkerpb.Container, message string) {
	attribution := hosted.attribution
	ctx := context.Background()
	// stopping is checked under the actor's lock, so an exit during a
	// checkpoint that then fails is still handled once it gives up.
	// Not reported under the lock: a suspend in flight holds the actor's
	// lease while it waits on that lock for its Checkpoint.
	if !s.locks.Lock(ctx, attribution.UID) {
		return
	}
	ours := s.lookupActor(attribution.UID) == hosted && !hosted.stopping.Load()
	if ours {
		slog.WarnContext(ctx, "Actor crashed", slog.String("actorUID", attribution.UID), slog.String("message", message))
		if err := s.terminateWorkload(ctx, attribution.Ref, attribution.UID, runscPath, actorDirs, containers); err != nil {
			slog.WarnContext(ctx, "Failed to tear down the crashed actor", slog.String("actorUID", attribution.UID), slog.Any("err", err))
		}
		// not measured first: the sandbox, and its cgroup, are gone
		s.recordFinalIfEnded(ctx, hosted)
	}
	s.locks.Unlock(attribution.UID)
	if !ours || s.crashReporter == nil {
		return
	}
	reportCtx, cancel := context.WithTimeout(ctx, crashReportTimeout)
	defer cancel()
	if err := s.crashReporter.ReportCrashed(reportCtx, attribution, message); err != nil {
		slog.ErrorContext(ctx, "Failed to report the actor crashed", slog.String("actorUID", attribution.UID), slog.Any("err", err))
	}
}

// durableRestoreFailedMessage is the crash message of an actor whose durable
// volumes runsc did not restore from the snapshot.
const durableRestoreFailedMessage = "durable volumes were not restored from the snapshot"

// watchDurableRestore waits, in the background, for runsc to finish restoring
// the durable-dir volumes of each container in durableContainers, and calls
// crash when runsc reports that it restored none of them for a container, or
// failed to: the workload would otherwise run on empty or partial volumes.
func (s *AteWorkerService) watchDurableRestore(hosted *hostedActor, rcmd fsRestoreWaiter, durableContainers []string, crash func(message string)) {
	if len(durableContainers) == 0 {
		return
	}
	go func() {
		for _, name := range durableContainers {
			err := rcmd.cmdWaitFSRestore(hosted.ctx, name)
			if err == nil {
				continue
			}
			if hosted.ctx.Err() != nil || hosted.stopping.Load() || s.shuttingDown.Load() {
				return
			}
			slog.ErrorContext(hosted.ctx, "Durable volumes were not restored",
				slog.String("actorUID", hosted.attribution.UID), slog.String("container", name), slog.Any("err", err))
			crash(durableRestoreFailedMessage)
			return
		}
	}()
}

// fsRestoreWaiter is the slice of runsc watchDurableRestore uses.
type fsRestoreWaiter interface {
	cmdWaitFSRestore(ctx context.Context, containerName string) error
}

// durableRestoreContainers returns the containers among fsRestoreContainers
// that mount a durable-dir volume, when restoreArgs restores the durable
// filesystem checkpoint in checkpointDir.
func durableRestoreContainers(checkpointDir string, restoreArgs []string, fidelity ateworkerpb.SnapshotFidelity, containers []*ateworkerpb.Container) []string {
	if !slices.Contains(restoreArgs, filepath.Join(checkpointDir, resources.FSCheckpointSubdir)) {
		return nil
	}
	mounts := make(map[string]bool)
	for _, c := range containers {
		mounts[c.GetName()] = len(c.GetDurableDirVolumeMounts()) > 0
	}
	var out []string
	for _, name := range fsRestoreContainers(fidelity, containers) {
		if mounts[name] {
			out = append(out, name)
		}
	}
	return out
}

// containerExitWaiter blocks until an application container's process exits
// and returns its exit status as `runsc wait` reports it. *runsc satisfies it.
type containerExitWaiter interface {
	cmdWaitExit(ctx context.Context, containerName string) (int, error)
}

// watchContainers waits on every application container of an actor that was
// just booted or restored. The sandbox outlives an application container that
// exits on its own: the sandbox container keeps it running, so watchSandbox
// never sees it, and the actor would stay RUNNING with nothing left to answer
// it. A container found exited calls crash with its exit status, under the
// same rule as a sandbox exit: an exit ateworker caused while stopping the
// actor (a checkpoint, a terminate) is never reported. The waits end with
// hosted.ctx.
func (s *AteWorkerService) watchContainers(hosted *hostedActor, waiter containerExitWaiter, names []string, crash func(message string)) {
	for _, name := range names {
		go func() {
			status, err := waiter.cmdWaitExit(hosted.ctx, name)
			if hosted.ctx.Err() != nil || hosted.stopping.Load() || s.shuttingDown.Load() {
				return
			}
			if err != nil {
				// The sandbox is gone (watchSandbox reports that) or runsc
				// could not wait on the container; there is no exit status.
				slog.Debug("Not watching the container for an exit", slog.String("actorUID", hosted.attribution.UID), slog.String("container", name), slog.Any("err", err))
				return
			}
			crash(containerExitReason(name, status))
		}()
	}
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
