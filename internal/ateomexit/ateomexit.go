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

// Package ateomexit reports to the control plane, through the node-local
// atelet, that the sandbox of an actor an ateom hosts exited on its own.
//
// The report travels ateom -> atelet -> ateapi, and the crash the control
// plane records travels back down the ordinary path as a Terminate into the
// ateom that reported. A caller must therefore hold no lock that Terminate
// needs while a report is in flight.
package ateomexit

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/agent-substrate/substrate/internal/ateletdial"
	"github.com/agent-substrate/substrate/internal/proto/ateletpb"
)

// reportTimeout bounds one attempt: the control plane answers once the crash
// it records has torn the actor down on this worker.
const reportTimeout = 2 * time.Minute

// An exit is a fact that does not decay, unlike an idleness signal, and until
// it lands the actor's callers wait on a sandbox that is gone. The budget
// therefore outlasts what a lifecycle operation holding the actor's lease or
// a rolling atelet or ateapi takes: about two minutes over ten attempts.
func retryBackoff() wait.Backoff {
	return wait.Backoff{
		Steps:    10,
		Duration: 500 * time.Millisecond,
		Factor:   2.0,
		Jitter:   0.2,
		Cap:      30 * time.Second,
	}
}

// Actor names the actor whose sandbox exited. The UID pins the report to the
// incarnation the ateom was hosting.
type Actor struct {
	Atespace string
	Name     string
	UID      string
}

// Reporter reports the sandbox exits of the actors one ateom hosts.
type Reporter struct {
	socketPath string
	tlsConfig  *tls.Config
}

// NewReporter reaches atelet at socketPath with tlsConfig
// (ateletdial.TLSConfig), loaded once at startup so a misconfiguration
// surfaces there rather than at the first exit.
func NewReporter(socketPath string, tlsConfig *tls.Config) *Reporter {
	return &Reporter{socketPath: socketPath, tlsConfig: tlsConfig}
}

// ReportExit tells the control plane that actor's sandbox exited, reason
// saying what was observed. It retries a report that could not be delivered
// or that met an operation holding the actor, within the budget above; a
// refusal (the actor is no longer RUNNING here) is the answer and is returned.
func (r *Reporter) ReportExit(ctx context.Context, actor Actor, reason string) error {
	// A fresh connection per report picks up rotated worker credentials; exits
	// are rare.
	conn, err := ateletdial.Dial(r.socketPath, r.tlsConfig)
	if err != nil {
		return fmt.Errorf("dial atelet: %w", err)
	}
	defer conn.Close()
	return reportWithRetry(ctx, conn, actor, reason, retryBackoff())
}

// reportWithRetry sends the report until it is answered, the budget runs out,
// or ctx ends.
func reportWithRetry(ctx context.Context, conn grpc.ClientConnInterface, actor Actor, reason string, backoff wait.Backoff) error {
	var lastErr error
	err := wait.ExponentialBackoffWithContext(ctx, backoff, func(context.Context) (bool, error) {
		err := reportOnce(ctx, conn, actor, reason)
		switch {
		case err == nil:
			return true, nil
		case retryable(err):
			lastErr = err
			slog.WarnContext(ctx, "Retrying a sandbox exit report",
				slog.String("actor_atespace", actor.Atespace),
				slog.String("actor_name", actor.Name),
				slog.Any("err", err))
			return false, nil
		default:
			return false, err
		}
	})
	if wait.Interrupted(err) && lastErr != nil {
		return fmt.Errorf("giving up on sandbox exit report after %d attempts: %w", backoff.Steps, lastErr)
	}
	return err
}

// retryable reports whether err is the control plane briefly out of reach or
// busy: Unavailable is atelet or ateapi restarting, ResourceExhausted is
// backpressure, and Aborted is another operation holding the actor's lease,
// whose end the retry waits out. Every other code is an answer.
func retryable(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.Aborted, codes.ResourceExhausted:
		return true
	default:
		return false
	}
}

func reportOnce(ctx context.Context, conn grpc.ClientConnInterface, actor Actor, reason string) error {
	callCtx, cancel := context.WithTimeout(ctx, reportTimeout)
	defer cancel()
	_, err := ateletpb.NewAteomSupportClient(conn).ReportActorExit(callCtx, &ateletpb.ReportActorExitRequest{
		ActorAtespace: actor.Atespace,
		ActorName:     actor.Name,
		ActorUid:      actor.UID,
		Reason:        reason,
	})
	return err
}
