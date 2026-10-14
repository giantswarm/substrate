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

package ateomexit

import (
	"context"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/testing/protocmp"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/agent-substrate/substrate/internal/proto/ateletpb"
)

// capturingAtelet records every report and answers the nth with errs[n]; once
// errs runs out it accepts.
type capturingAtelet struct {
	ateletpb.UnimplementedAteomSupportServer

	errs []error

	mu  sync.Mutex
	got []*ateletpb.ReportActorExitRequest
}

func (a *capturingAtelet) ReportActorExit(_ context.Context, in *ateletpb.ReportActorExitRequest) (*ateletpb.ReportActorExitResponse, error) {
	a.mu.Lock()
	attempt := len(a.got)
	a.got = append(a.got, in)
	a.mu.Unlock()
	if attempt < len(a.errs) {
		return nil, a.errs[attempt]
	}
	return &ateletpb.ReportActorExitResponse{}, nil
}

func (a *capturingAtelet) requests() []*ateletpb.ReportActorExitRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.got)
}

func testBackoff() wait.Backoff {
	return wait.Backoff{Steps: retryBackoff().Steps, Duration: time.Millisecond}
}

func dialCapturingAtelet(t *testing.T, atelet *capturingAtelet) *grpc.ClientConn {
	t.Helper()
	srv := grpc.NewServer()
	ateletpb.RegisterAteomSupportServer(srv, atelet)
	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}))
	if err != nil {
		t.Fatalf("connecting to the fake atelet: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

var testActor = Actor{Atespace: "team-a", Name: "actor-1", UID: "actor-uid-1"}

func TestReportExitNamesTheActorAndReason(t *testing.T) {
	atelet := &capturingAtelet{}
	if err := reportWithRetry(context.Background(), dialCapturingAtelet(t, atelet), testActor, "sandbox process 42 exited", testBackoff()); err != nil {
		t.Fatalf("reportWithRetry() failed: %v", err)
	}
	want := []*ateletpb.ReportActorExitRequest{{
		ActorAtespace: "team-a",
		ActorName:     "actor-1",
		ActorUid:      "actor-uid-1",
		Reason:        "sandbox process 42 exited",
	}}
	if diff := cmp.Diff(want, atelet.requests(), protocmp.Transform()); diff != "" {
		t.Errorf("request mismatch (-want +got):\n%s", diff)
	}
}

// An operation holding the actor's lease and a restarting atelet are waited
// out: until the report lands, the actor's callers wait on a sandbox that is
// gone.
func TestReportExitRidesOutTransientFailures(t *testing.T) {
	atelet := &capturingAtelet{errs: []error{
		status.Error(codes.Unavailable, "atelet restarting"),
		status.Error(codes.Aborted, "another operation is in progress for this actor"),
	}}
	if err := reportWithRetry(context.Background(), dialCapturingAtelet(t, atelet), testActor, "exited", testBackoff()); err != nil {
		t.Fatalf("reportWithRetry() failed: %v", err)
	}
	if got := len(atelet.requests()); got != 3 {
		t.Errorf("reported %d times, want 3", got)
	}
}

func TestReportExitReturnsARefusal(t *testing.T) {
	atelet := &capturingAtelet{errs: []error{status.Error(codes.FailedPrecondition, "Actor is SUSPENDING")}}
	err := reportWithRetry(context.Background(), dialCapturingAtelet(t, atelet), testActor, "exited", testBackoff())
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Errorf("code = %v (err %v), want FailedPrecondition", got, err)
	}
	if got := len(atelet.requests()); got != 1 {
		t.Errorf("reported %d times, want 1: a refusal is not retried", got)
	}
}

func TestReportExitGivesUpWithTheLastError(t *testing.T) {
	steps := retryBackoff().Steps
	errs := make([]error, steps)
	for i := range errs {
		errs[i] = status.Error(codes.Unavailable, "atelet restarting")
	}
	atelet := &capturingAtelet{errs: errs}
	err := reportWithRetry(context.Background(), dialCapturingAtelet(t, atelet), testActor, "exited", testBackoff())
	if got := status.Code(err); got != codes.Unavailable {
		t.Errorf("code = %v (err %v), want the last Unavailable", got, err)
	}
	if got := len(atelet.requests()); got != steps {
		t.Errorf("reported %d times, want %d", got, steps)
	}
}
