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
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"go.ate.dev/substrate/internal/actorlock"
	"go.ate.dev/substrate/internal/ocispec"
	"go.ate.dev/substrate/internal/proto/ateworkerpb"
	"go.ate.dev/substrate/internal/resources"
)

type fakeCrashReporter struct {
	reports chan string
}

func (f *fakeCrashReporter) ReportCrashed(_ context.Context, _ resources.ActorAttribution, message string) error {
	f.reports <- message
	return nil
}

// exitingSandbox records a process that exits at once as the actor's sandbox.
func exitingSandbox(t *testing.T) *ateworkerpb.ActorDirs {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Wait() })
	actorDirs := &ateworkerpb.ActorDirs{RootDir: t.TempDir()}
	if err := os.MkdirAll(pidFileDir(actorDirs), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidFilePath(actorDirs, ocispec.SandboxID), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	return actorDirs
}

func TestWatchSandboxIgnoresExpectedExits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*AteWorkerService, *hostedActor)
	}{
		{"stopped by ateworker", func(_ *AteWorkerService, h *hostedActor) { h.stopping.Store(true) }},
		{"worker shutting down", func(s *AteWorkerService, _ *hostedActor) { s.shuttingDown.Store(true) }},
		{"actor no longer hosted", func(s *AteWorkerService, _ *hostedActor) { delete(s.actors, "actor-a") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reporter := &fakeCrashReporter{reports: make(chan string, 1)}
			actorDirs := exitingSandbox(t)
			sandbox, err := openSandboxProcess(actorDirs)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(sandbox.Close)
			hosted := &hostedActor{attribution: resources.ActorAttribution{UID: "actor-a"}, sandbox: sandbox}
			s := &AteWorkerService{locks: actorlock.New(), actors: map[string]*hostedActor{"actor-a": hosted}, crashReporter: reporter}
			tc.setup(s, hosted)
			s.watchSandbox(context.Background(), hosted, "", actorDirs, nil)
			select {
			case msg := <-reporter.reports:
				t.Fatalf("reported %q for an expected exit", msg)
			case <-time.After(500 * time.Millisecond):
			}
		})
	}
}

type fakeFSRestoreWaiter struct {
	errs map[string]error
}

func (f fakeFSRestoreWaiter) cmdWaitFSRestore(_ context.Context, containerName string) error {
	return f.errs[containerName]
}

func TestWatchDurableRestore(t *testing.T) {
	for _, tc := range []struct {
		name       string
		errs       map[string]error
		stopping   bool
		wantReport bool
	}{
		{name: "every volume restored"},
		{name: "runsc restored no filesystem", errs: map[string]error{"app": errors.New("no filesystems restored for container app")}, wantReport: true},
		{name: "stopped by ateworker", errs: map[string]error{"app": errors.New("killed")}, stopping: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			crashes := make(chan string, 1)
			hosted := &hostedActor{attribution: resources.ActorAttribution{UID: "actor-a"}, ctx: t.Context()}
			hosted.stopping.Store(tc.stopping)
			s := &AteWorkerService{}
			s.watchDurableRestore(hosted, fakeFSRestoreWaiter{errs: tc.errs}, []string{"app"}, func(message string) { crashes <- message })
			select {
			case msg := <-crashes:
				if !tc.wantReport {
					t.Fatalf("reported %q, want no report", msg)
				}
				if msg != durableRestoreFailedMessage {
					t.Errorf("reported %q, want %q", msg, durableRestoreFailedMessage)
				}
			case <-time.After(500 * time.Millisecond):
				if tc.wantReport {
					t.Fatal("actor was not reported crashed")
				}
			}
		})
	}
}

func TestDurableRestoreContainers(t *testing.T) {
	checkpointDir := "/ckpt"
	containers := []*ateworkerpb.Container{
		{Name: "app", DurableDirVolumeMounts: []*ateworkerpb.VolumeMount{{Name: "data", MountPath: "/data"}}},
		{Name: "sidecar"},
	}
	fsArgs := []string{"--fs-restore-image-path", filepath.Join(checkpointDir, resources.FSCheckpointSubdir)}
	if got := durableRestoreContainers(checkpointDir, fsArgs, ateworkerpb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, containers); !slices.Equal(got, []string{"app"}) {
		t.Errorf("VOLUMES = %v, want [app]", got)
	}
	if got := durableRestoreContainers(checkpointDir, fsArgs, ateworkerpb.SnapshotFidelity_SNAPSHOT_FIDELITY_MEMORY, containers); !slices.Equal(got, []string{"app"}) {
		t.Errorf("MEMORY = %v, want [app]", got)
	}
	if got := durableRestoreContainers(checkpointDir, nil, ateworkerpb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, containers); len(got) != 0 {
		t.Errorf("without fs/ restored = %v, want none", got)
	}
}
