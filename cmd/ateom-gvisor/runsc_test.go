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
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	"github.com/agent-substrate/substrate/internal/actorlock"
	"github.com/agent-substrate/substrate/internal/actorlog"
	"github.com/agent-substrate/substrate/internal/apierror"
	"github.com/agent-substrate/substrate/internal/ocispec"
	"github.com/agent-substrate/substrate/internal/proto/ateompb"
	"github.com/agent-substrate/substrate/internal/resources"
)

var testActorDirs = &ateompb.ActorDirs{
	RootDir:      "/node/actors/test-actor-123",
	OciBundleDir: "/node/actors/test-actor-123/bundle",
}

func TestKillArgs(t *testing.T) {
	r := &runsc{
		path:      "/usr/bin/runsc",
		actorUID:  "test-actor-123",
		actorDirs: testActorDirs,
	}

	got := r.killArgs("my-container", "SIGTERM")
	want := []string{
		"-log-format", "json",
		"--alsologtostderr",
		"-root", "/node/actors/test-actor-123/runsc-state",
		"kill",
		"my-container",
		"SIGTERM",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("killArgs() = %v, want %v", got, want)
	}
}

func TestWaitArgs(t *testing.T) {
	r := &runsc{
		path:      "/usr/bin/runsc",
		actorUID:  "test-actor-123",
		actorDirs: testActorDirs,
	}

	got := r.waitArgs("my-container")
	want := []string{
		"-log-format", "json",
		"--alsologtostderr",
		"-root", "/node/actors/test-actor-123/runsc-state",
		"wait",
		"my-container",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("waitArgs() = %v, want %v", got, want)
	}
}

func TestPauseArgs(t *testing.T) {
	r := &runsc{
		path:      "/usr/bin/runsc",
		actorUID:  "test-actor-123",
		actorDirs: testActorDirs,
	}

	got := r.pauseArgs(ocispec.PauseContainer)
	want := []string{
		"-log-format", "json",
		"--alsologtostderr",
		"-root", "/node/actors/test-actor-123/runsc-state",
		"pause",
		ocispec.PauseContainer,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("pauseArgs() = %v, want %v", got, want)
	}
}

func TestResumeArgs(t *testing.T) {
	r := &runsc{
		path:      "/usr/bin/runsc",
		actorUID:  "test-actor-123",
		actorDirs: testActorDirs,
	}

	got := r.resumeArgs(ocispec.PauseContainer)
	want := []string{
		"-log-format", "json",
		"--alsologtostderr",
		"-root", "/node/actors/test-actor-123/runsc-state",
		"resume",
		ocispec.PauseContainer,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("resumeArgs() = %v, want %v", got, want)
	}
}

func TestRestoreArgs(t *testing.T) {
	r := &runsc{
		path:      "/usr/bin/runsc",
		actorUID:  "test-actor-123",
		actorDirs: testActorDirs,
	}
	checkpointDir := "/node/actors/test-actor-123/checkpoints/snap-1"

	got := r.restoreArgs(ocispec.PauseContainer, checkpointDir)
	want := []string{
		"-log-format", "json",
		"--alsologtostderr",
		"-root", "/node/actors/test-actor-123/runsc-state",
		"--cpu-num-from-quota",
		"restore",
		"-bundle", "/node/actors/test-actor-123/bundle/" + ocispec.PauseContainer,
		"-image-path", checkpointDir,
		"-pid-file", "/node/actors/test-actor-123/pidfiles/" + ocispec.PauseContainer + ".pid",
		"-background",
		"-detach",
		ocispec.PauseContainer,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("restoreArgs() = %v, want %v", got, want)
	}
}

func TestCreateArgs(t *testing.T) {
	r := &runsc{
		path:      "/usr/bin/runsc",
		actorUID:  "test-actor-123",
		actorDirs: testActorDirs,
	}
	create := []string{
		"-root", "/node/actors/test-actor-123/runsc-state",
		"--cpu-num-from-quota",
		"create",
		"-bundle", "/node/actors/test-actor-123/bundle/" + ocispec.PauseContainer,
		"-pid-file", "/node/actors/test-actor-123/pidfiles/" + ocispec.PauseContainer + ".pid",
		ocispec.PauseContainer,
	}

	for _, tc := range []struct {
		name       string
		sandboxLog bool
		want       []string
	}{
		{
			name: "without a sandbox log",
			want: append([]string{"-log-format", "json", "--alsologtostderr"}, create...),
		},
		{
			name:       "with a sandbox log",
			sandboxLog: true,
			want:       append([]string{"-log-format", "json", "--alsologtostderr", "-log", "/proc/self/fd/3"}, create...),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.createArgs(ocispec.PauseContainer, nil, tc.sandboxLog); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("createArgs() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSandboxLogReachesTheActorLog proves the path runsc is told to log to:
// what a command writes to sandboxLogPath, given the sandbox log pipe as its
// first extra file, is logged as the actor's pause container.
func TestSandboxLogReachesTheActorLog(t *testing.T) {
	var out bytes.Buffer
	done := make(chan struct{})
	logger := actorlog.NewActorLogger(actorlog.NewSyncedWriter(&notifyWriter{w: &out, done: done}), false)
	s := &AteomService{actorLogger: logger}
	sandboxLog, err := s.startSandboxLog(resources.ActorAttribution{Ref: resources.ActorRef{Atespace: "default", Name: "actor-a"}})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", `echo '{"level":"warning","msg":"Unexpected SwitchToApp error"}' >>`+sandboxLogPath)
	cmd.ExtraFiles = []*os.File{sandboxLog}
	if err := cmd.Run(); err != nil {
		t.Fatalf("write to %s: %v", sandboxLogPath, err)
	}
	if err := sandboxLog.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the sandbox log line was not logged")
	}
	got := out.String()
	for _, want := range []string{`"msg":"Unexpected SwitchToApp error"`, `"ate.actor.container.name":"` + ocispec.PauseContainer + `"`} {
		if !strings.Contains(got, want) {
			t.Errorf("actor log %q does not contain %s", got, want)
		}
	}
}

// notifyWriter closes done after its first write.
type notifyWriter struct {
	w    io.Writer
	done chan struct{}
	once sync.Once
}

func (n *notifyWriter) Write(p []byte) (int, error) {
	written, err := n.w.Write(p)
	n.once.Do(func() { close(n.done) })
	return written, err
}

func TestRestoreWorkloadRejectsEmptyRestoreDir(t *testing.T) {
	s := &AteomService{
		locks:    actorlock.New(),
		inFlight: actorlock.NewInFlight(),
	}

	for _, tc := range []struct {
		name string
		req  *ateompb.RestoreWorkloadRequest
	}{
		{
			name: "nil actor_dirs",
			req:  &ateompb.RestoreWorkloadRequest{ActorUid: "actor-a"},
		},
		{
			name: "empty restore_dir",
			req: &ateompb.RestoreWorkloadRequest{
				ActorUid: "actor-a",
				ActorDirs: &ateompb.ActorDirs{
					RootDir:                   "/node/actors/actor-a",
					OciBundleDir:              "/node/actors/actor-a/bundle",
					CheckpointDir:             "/node/actors/actor-a/checkpoint-state",
					DurableDirVolumeMountsDir: "/node/actors/actor-a/durable-dirs",
					SystemInfoVolumeRootsDir:  "/node/actors/actor-a/system-info",
					VolumesDir:                "/node/actors/actor-a/volumes",
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.RestoreWorkload(context.Background(), tc.req)
			if apierror.Code(err) != codes.InvalidArgument {
				t.Fatalf("RestoreWorkload() code = %v, want %v (err: %v)", apierror.Code(err), codes.InvalidArgument, err)
			}
		})
	}
}
