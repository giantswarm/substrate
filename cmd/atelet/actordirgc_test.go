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
	"os"
	"path/filepath"
	"slices"
	"testing"

	"google.golang.org/grpc"

	"github.com/agent-substrate/substrate/cmd/atelet/internal/ateletpath"
	"github.com/agent-substrate/substrate/internal/nodepath"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

const (
	runningActorUID   = "11111111-1111-4111-8111-111111111111"
	suspendedActorUID = "22222222-2222-4222-8222-222222222222"
	goneActorUID      = "33333333-3333-4333-8333-333333333333"
)

// fakeControlPlane answers ListGoneActors from the actors it holds, or fails
// every call with err.
type fakeControlPlane struct {
	existing map[string]bool
	// extra is named gone in every answer, asked about or not.
	extra []string
	err   error
	calls [][]string
}

func (f *fakeControlPlane) ListGoneActors(_ context.Context, req *ateapipb.ListGoneActorsRequest, _ ...grpc.CallOption) (*ateapipb.ListGoneActorsResponse, error) {
	f.calls = append(f.calls, req.GetActorUids())
	if f.err != nil {
		return nil, f.err
	}
	resp := &ateapipb.ListGoneActorsResponse{GoneActorUids: slices.Clone(f.extra)}
	for _, uid := range req.GetActorUids() {
		if !f.existing[uid] {
			resp.GoneActorUids = append(resp.GoneActorUids, uid)
		}
	}
	return resp, nil
}

// seedActorDir lays out an actor's directory as a run leaves it.
func seedActorDir(t *testing.T, actorUID string) {
	t.Helper()
	if err := resetActorDirs(actorUID); err != nil {
		t.Fatalf("resetActorDirs(%s): %v", actorUID, err)
	}
	if err := os.WriteFile(ateletpath.ActorSandboxAssetsFile(actorUID), []byte("{}"), 0o600); err != nil {
		t.Fatalf("writing sandbox assets file: %v", err)
	}
}

func actorDirExists(t *testing.T, actorUID string) bool {
	t.Helper()
	_, err := os.Stat(ateletpath.ActorPath(actorUID))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat actor dir: %v", err)
	}
	return err == nil
}

func newTestActorDirGC(cp *fakeControlPlane) *actorDirGC {
	return &actorDirGC{workers: cp, remove: removeActorDirs}
}

// The directory of an actor the control plane no longer has goes; those of a
// running actor and a suspended one, which has no node, stay.
func TestActorDirGCRemovesOnlyGoneActors(t *testing.T) {
	useTempNodeDirs(t)
	for _, uid := range []string{runningActorUID, suspendedActorUID, goneActorUID} {
		seedActorDir(t, uid)
	}
	cp := &fakeControlPlane{existing: map[string]bool{runningActorUID: true, suspendedActorUID: true}}

	if got := newTestActorDirGC(cp).pass(t.Context()); got != 1 {
		t.Errorf("pass() removed %d dirs, want 1", got)
	}
	if actorDirExists(t, goneActorUID) {
		t.Errorf("directory of the gone actor survived the pass")
	}
	for _, uid := range []string{runningActorUID, suspendedActorUID} {
		if !actorDirExists(t, uid) {
			t.Errorf("directory of existing actor %s was removed", uid)
		}
		if _, err := os.Stat(ateletpath.ActorSandboxAssetsFile(uid)); err != nil {
			t.Errorf("sandbox assets of existing actor %s: %v", uid, err)
		}
	}
}

// A control plane that cannot answer removes nothing.
func TestActorDirGCKeepsEverythingOnError(t *testing.T) {
	useTempNodeDirs(t)
	for _, uid := range []string{runningActorUID, goneActorUID} {
		seedActorDir(t, uid)
	}
	cp := &fakeControlPlane{err: errors.New("rpc error: code = Unavailable")}

	if got := newTestActorDirGC(cp).pass(t.Context()); got != 0 {
		t.Errorf("pass() removed %d dirs on a control-plane error, want 0", got)
	}
	for _, uid := range []string{runningActorUID, goneActorUID} {
		if !actorDirExists(t, uid) {
			t.Errorf("directory of actor %s was removed on a control-plane error", uid)
		}
	}
}

// A dry run names the directory and removes nothing.
func TestActorDirGCDryRun(t *testing.T) {
	useTempNodeDirs(t)
	seedActorDir(t, goneActorUID)
	gc := newTestActorDirGC(&fakeControlPlane{})
	gc.dryRun = true

	if got := gc.pass(t.Context()); got != 1 {
		t.Errorf("pass() counted %d dirs, want 1", got)
	}
	if !actorDirExists(t, goneActorUID) {
		t.Errorf("a dry run removed the directory of the gone actor")
	}
}

// Only a UID the pass asked about is removed, and an entry not named by a UID
// is neither asked about nor touched.
func TestActorDirGCTouchesOnlyAskedActorDirs(t *testing.T) {
	useTempNodeDirs(t)
	seedActorDir(t, runningActorUID)
	notAnActor := filepath.Join(nodepath.ActorsDir, "not-an-actor")
	if err := os.MkdirAll(notAnActor, 0o755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	cp := &fakeControlPlane{
		existing: map[string]bool{runningActorUID: true},
		extra:    []string{"not-an-actor", "../../etc", goneActorUID},
	}

	if got := newTestActorDirGC(cp).pass(t.Context()); got != 0 {
		t.Errorf("pass() removed %d dirs, want 0", got)
	}
	if want := [][]string{{runningActorUID}}; !slices.EqualFunc(cp.calls, want, slices.Equal) {
		t.Errorf("asked about %v, want %v", cp.calls, want)
	}
	if _, err := os.Stat(notAnActor); err != nil {
		t.Errorf("entry not named by a UID: %v", err)
	}
	if !actorDirExists(t, runningActorUID) {
		t.Errorf("directory of the running actor was removed")
	}
}

// A volume directory with contents is a mount that did not let go: its data
// is never deleted with the gone actor's directory.
func TestActorDirGCKeepsPopulatedVolume(t *testing.T) {
	useTempNodeDirs(t)
	seedActorDir(t, goneActorUID)
	stillMounted := filepath.Join(ateletpath.VolumeHostPath(goneActorUID, "data"), "payload")
	if err := os.MkdirAll(filepath.Dir(stillMounted), 0o755); err != nil {
		t.Fatalf("creating volume dir: %v", err)
	}
	if err := os.WriteFile(stillMounted, []byte("volume contents"), 0o600); err != nil {
		t.Fatalf("writing volume contents: %v", err)
	}

	newTestActorDirGC(&fakeControlPlane{}).pass(t.Context())

	if _, err := os.Stat(stillMounted); err != nil {
		t.Errorf("the pass deleted the contents of a volume that was still populated: %v", err)
	}
}

// An actor whose leftover volumes a sweep still owns is not asked about: the
// sweep removes its record once they let go.
func TestActorDirGCSkipsSweptActor(t *testing.T) {
	useTempNodeDirs(t)
	seedActorDir(t, goneActorUID)
	if err := os.WriteFile(leftoverVolumesPath(goneActorUID), nil, 0o600); err != nil {
		t.Fatalf("writing leftover volumes record: %v", err)
	}
	cp := &fakeControlPlane{}

	if got := newTestActorDirGC(cp).pass(t.Context()); got != 0 {
		t.Errorf("pass() removed %d dirs, want 0", got)
	}
	if len(cp.calls) != 0 {
		t.Errorf("asked about %v, want no call", cp.calls)
	}
	if !actorDirExists(t, goneActorUID) {
		t.Errorf("directory of an actor with a leftover sweep was removed")
	}
}

// More directories than one request takes are asked about in batches.
func TestActorDirGCBatches(t *testing.T) {
	useTempNodeDirs(t)
	const n = actorDirGCBatch + 3
	for i := range n {
		uid := fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
		if err := os.MkdirAll(ateletpath.ActorPath(uid), 0o755); err != nil {
			t.Fatalf("creating actor dir: %v", err)
		}
	}
	cp := &fakeControlPlane{}

	if got := newTestActorDirGC(cp).pass(t.Context()); got != n {
		t.Errorf("pass() removed %d dirs, want %d", got, n)
	}
	if len(cp.calls) != 2 || len(cp.calls[0]) != actorDirGCBatch || len(cp.calls[1]) != 3 {
		t.Errorf("batches of %d calls, want %d and 3", len(cp.calls), actorDirGCBatch)
	}
}
