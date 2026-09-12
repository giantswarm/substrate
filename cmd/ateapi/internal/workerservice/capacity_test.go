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

package workerservice

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/ateletauth/ateletauthtest"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/scheduling"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store/storetest"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	capWorkerName = "8f1c2d34-5e6a-4b7c-9d8e-0f1a2b3c4d5e"
	capNode       = "node-1"
)

// seedReportedWorker registers a Worker on nodeName that has already reported
// capacity: identity from the pod, the way the syncer writes it, plus the
// result of an earlier report. A Worker that has never reported carries none,
// so what a fresh report replaces is what this seeds.
func seedReportedWorker(t *testing.T, st store.Interface, nodeName string, capacity *ateapipb.WorkerResources) *ateapipb.Worker {
	t.Helper()
	created, err := st.CreateWorker(context.Background(), &ateapipb.Worker{
		Metadata:        &ateapipb.ResourceMetadata{Name: capWorkerName},
		WorkerNamespace: "ate-system",
		WorkerPool:      "pool-1",
		WorkerPod:       "worker-pod-1",
		WorkerPodUid:    capWorkerName,
		NodeName:        nodeName,
		Ip:              "10.1.2.3",
		SandboxClass:    "gvisor",
		Status:          &ateapipb.WorkerStatus{State: ateapipb.WorkerState_WORKER_STATE_ACTIVE, Capacity: capacity},
	})
	if err != nil {
		t.Fatalf("seeding worker: %v", err)
	}
	return created
}

func setRequest(actors int32) *ateapipb.SetWorkerCapacityRequest {
	return &ateapipb.SetWorkerCapacityRequest{
		Worker:   &ateapipb.ObjectRef{Name: capWorkerName},
		Capacity: &ateapipb.WorkerResources{Actors: actors},
	}
}

// The point of the whole path: a Worker moves from what it reported before to
// what its ateom reports now.
func TestSetWorkerCapacity(t *testing.T) {
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	s := New(st)
	seedReportedWorker(t, st, capNode, &ateapipb.WorkerResources{Actors: 1, Resources: resources.CPUMemory(2000, 0)})

	got, err := s.SetWorkerCapacity(ateletauthtest.ContextWith(ateletauthtest.CertOn(t, capNode)), setRequest(4094))
	if err != nil {
		t.Fatalf("SetWorkerCapacity() failed: %v", err)
	}
	if want := int32(4094); got.GetWorker().GetStatus().GetCapacity().GetActors() != want {
		t.Errorf("capacity.actors = %d, want %d", got.GetWorker().GetStatus().GetCapacity().GetActors(), want)
	}
	// A report replaces what is recorded. The Worker reports everything it has,
	// so a dimension this one leaves out is one it no longer supplies -- keeping
	// the old value would advertise compute nothing claims to have.
	if got := got.GetWorker().GetStatus().GetCapacity().GetResources(); got != nil {
		t.Errorf("capacity resources = %v, want the report's own (none)", got)
	}
}

// An atelet speaks for the Workers it herds and no others. A Worker on another
// node is reported as absent rather than forbidden, so a caller learns nothing
// about what runs elsewhere.
func TestSetWorkerCapacity_OtherNodeIsNotFound(t *testing.T) {
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	s := New(st)
	seedReportedWorker(t, st, capNode, &ateapipb.WorkerResources{Actors: 1})

	_, err := s.SetWorkerCapacity(ateletauthtest.ContextWith(ateletauthtest.CertOn(t, "some-other-node")), setRequest(4094))
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("code = %v (err %v), want NotFound", got, err)
	}

	// And the report must not have landed.
	after, err := st.GetWorker(context.Background(), capWorkerName)
	if err != nil {
		t.Fatalf("GetWorker: %v", err)
	}
	if got := after.GetStatus().GetCapacity().GetActors(); got != 1 {
		t.Errorf("capacity.actors = %d, want 1 unchanged", got)
	}
}

// Re-sending the same capacity is not an update. An ateom reports once, but it
// retries until accepted and reports again if it restarts, so a repeat must not
// churn the Worker's version.
func TestSetWorkerCapacity_UnchangedDoesNotWrite(t *testing.T) {
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	s := New(st)
	seeded := seedReportedWorker(t, st, capNode, &ateapipb.WorkerResources{Actors: 4094})

	for range 3 {
		if _, err := s.SetWorkerCapacity(ateletauthtest.ContextWith(ateletauthtest.CertOn(t, capNode)), setRequest(4094)); err != nil {
			t.Fatalf("SetWorkerCapacity() failed: %v", err)
		}
	}
	after, err := st.GetWorker(context.Background(), capWorkerName)
	if err != nil {
		t.Fatalf("GetWorker: %v", err)
	}
	if got, want := after.GetMetadata().GetVersion(), seeded.GetMetadata().GetVersion(); got != want {
		t.Errorf("version = %d after three identical reports, want %d unchanged", got, want)
	}
}

func TestSetWorkerCapacity_Errors(t *testing.T) {
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	s := New(st)
	seedReportedWorker(t, st, capNode, &ateapipb.WorkerResources{Actors: 1})
	authed := ateletauthtest.ContextWith(ateletauthtest.CertOn(t, capNode))

	tests := []struct {
		name string
		ctx  context.Context
		req  *ateapipb.SetWorkerCapacityRequest
		want codes.Code
	}{
		{"unauthenticated", ateletauthtest.ContextWith(nil), setRequest(2), codes.Unauthenticated},
		{"no worker ref", authed, &ateapipb.SetWorkerCapacityRequest{
			Capacity: &ateapipb.WorkerResources{Actors: 2},
		}, codes.InvalidArgument},
		{"no capacity", authed, &ateapipb.SetWorkerCapacityRequest{
			Worker: &ateapipb.ObjectRef{Name: capWorkerName},
		}, codes.InvalidArgument},
		{"absent worker", authed, &ateapipb.SetWorkerCapacityRequest{
			Worker:   &ateapipb.ObjectRef{Name: "3b9f1e77-2c4d-4a80-91be-6d5c8f0a7e21"},
			Capacity: &ateapipb.WorkerResources{Actors: 2},
		}, codes.NotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.SetWorkerCapacity(tc.ctx, tc.req)
			if got := status.Code(err); got != tc.want {
				t.Errorf("code = %v (err %v), want %v", got, err, tc.want)
			}
		})
	}
}

// A report goes straight to the store, so nothing else checks it. A negative
// ceiling is the case that matters: placement asks whether allocated is below
// capacity, so the Worker would take no Actor ever again.
func TestSetWorkerCapacity_RejectsNonsense(t *testing.T) {
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	s := New(st)
	seeded := seedReportedWorker(t, st, capNode, &ateapipb.WorkerResources{Actors: 4094})
	authed := ateletauthtest.ContextWith(ateletauthtest.CertOn(t, capNode))

	for _, tc := range []struct {
		name     string
		capacity *ateapipb.WorkerResources
	}{
		{"negative ceiling", &ateapipb.WorkerResources{Actors: -1}},
		{"int32 underflow", &ateapipb.WorkerResources{Actors: -2147483648}},
		{"negative quantity", &ateapipb.WorkerResources{Resources: resources.CPUMemory(-1, 0)}},
		{"unparseable quantity", &ateapipb.WorkerResources{
			Resources: &ateapipb.Resources{Limits: []*ateapipb.Limits{{Name: "cpu", Quantity: "lots"}}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.SetWorkerCapacity(authed, &ateapipb.SetWorkerCapacityRequest{
				Worker:   &ateapipb.ObjectRef{Name: capWorkerName},
				Capacity: tc.capacity,
			})
			if got := status.Code(err); got != codes.InvalidArgument {
				t.Fatalf("code = %v (err %v), want %v", got, err, codes.InvalidArgument)
			}
		})
	}

	after, err := st.GetWorker(context.Background(), capWorkerName)
	if err != nil {
		t.Fatalf("GetWorker: %v", err)
	}
	if diff := cmp.Diff(seeded.GetStatus().GetCapacity(), after.GetStatus().GetCapacity(), protocmp.Transform()); diff != "" {
		t.Errorf("capacity changed despite every report being refused (-want +got):\n%s", diff)
	}
}

// storedWorkers offers the store's Workers to the scheduler, so a test can ask
// the placement question the resume workflow asks.
type storedWorkers struct{ st store.Interface }

func (s storedWorkers) Workers() ([]*ateapipb.Worker, error) {
	page, err := s.st.ListWorkers(context.Background(), store.ListOptions{PageSize: 100})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// A Worker record can be replaced under a running ateom -- the syncer
// re-registers a worker whose pod IP changed -- and the replacement is created
// without capacity, so nothing is placed on it. The ateom re-asserts the report
// it already made; this is the seam that report lands on: the same report,
// against the same name, puts the capacity on the new record, and the scheduler
// then offers it.
func TestSetWorkerCapacity_ReassertedReportLandsOnAReplacementRecord(t *testing.T) {
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	s := New(st)
	authed := ateletauthtest.ContextWith(ateletauthtest.CertOn(t, capNode))
	ctx := context.Background()
	constraints := scheduling.Constraints{SandboxClass: "gvisor"}

	// The first report, accepted on the record that existed then.
	original := seedReportedWorker(t, st, capNode, nil)
	if _, err := s.SetWorkerCapacity(authed, setRequest(1)); err != nil {
		t.Fatalf("first SetWorkerCapacity() failed: %v", err)
	}

	// The re-registration: the record is deleted and a new one created under
	// the same name, the way registerWorker builds it, with no capacity.
	if _, err := st.DeleteWorker(ctx, capWorkerName, store.DeletePreconditions{}); err != nil {
		t.Fatalf("deleting the original worker: %v", err)
	}
	replacement := seedReportedWorker(t, st, capNode, nil)
	if replacement.GetMetadata().GetUid() == original.GetMetadata().GetUid() {
		t.Fatalf("replacement has the original's uid %q, want a new record", original.GetMetadata().GetUid())
	}
	if _, err := scheduling.New(storedWorkers{st}).Schedule(ctx, constraints); !errors.Is(err, scheduling.ErrNoCapacity) {
		t.Fatalf("Schedule() before the re-asserted report = %v, want ErrNoCapacity on a replacement without capacity", err)
	}

	// The ateom's next re-assertion: the same report, against the same name.
	if _, err := s.SetWorkerCapacity(authed, setRequest(1)); err != nil {
		t.Fatalf("re-asserted SetWorkerCapacity() failed: %v", err)
	}
	after, err := st.GetWorker(ctx, capWorkerName)
	if err != nil {
		t.Fatalf("GetWorker: %v", err)
	}
	if got, want := after.GetStatus().GetCapacity().GetActors(), int32(1); got != want {
		t.Errorf("replacement capacity.actors = %d, want %d from the re-asserted report", got, want)
	}
	placed, err := scheduling.New(storedWorkers{st}).Schedule(ctx, constraints)
	if err != nil {
		t.Fatalf("Schedule() after the re-asserted report failed: %v", err)
	}
	if got := placed.GetMetadata().GetName(); got != capWorkerName {
		t.Errorf("Schedule() placed on %q, want the re-registered worker %q", got, capWorkerName)
	}
}
