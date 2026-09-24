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

package egressmitm

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/internal/e2e"
	"github.com/agent-substrate/substrate/internal/localca"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

// TestEgressMITMCARotation rotates the egress MITM CA from the CA the gateway
// signs with to a new one, the way an operator does with `kubectl-ate admin
// add-ca`, `activate-ca` and `retire-ca`, while actors keep making HTTPS
// requests through the gateway, and requires that none of them fails.
//
// The actors cover every way an actor holds its trust anchors:
//
//   - a long-running actor that pinned the anchors it had before the rotation
//     began ("pinned": loaded once, like Go's system pool) and never suspends,
//     fetching throughout, alternately with the pinned and the re-read bundle;
//   - an actor that pinned them, suspended before the rotation and resumes
//     after the gateway switched: its snapshot restores the old anchors;
//   - new actors started in every phase, the last ones after the old root was
//     retired, so that they trust only the new one.
//
// The pinned actors keep working only because the new CA is cross-certified by
// the old one (localca.ConcretePool.AddCA) and the gateway presents that
// certificate; a rotation that relied on actors reloading their anchors would
// fail them the moment the gateway switched.
//
// It rotates the cluster-wide pool, so it runs only where nothing else uses
// the pool concurrently: CI runs it as its own step per sandbox class, after
// the egressmitm suite (see pr-workflow.yaml). Locally, against an install
// with the MITM egress gateway (see TestActorEgressMITMTrust):
//
//	E2E_EGRESS_MITM=1 E2E_EGRESS_MITM_ROTATION=1 hack/run-e2e-kind.sh ./internal/e2e/suites/egressmitm -run TestEgressMITMCARotation -timeout 20m -v -args --no-color
func TestEgressMITMCARotation(t *testing.T) {
	if os.Getenv("E2E_EGRESS_MITM") == "" || os.Getenv("E2E_EGRESS_MITM_ROTATION") == "" {
		t.Skip("rotates the cluster-wide egress MITM CA: needs the MITM egress gateway (E2E_EGRESS_MITM=1) and E2E_EGRESS_MITM_ROTATION=1, with no other suite running")
	}
	env, err := e2e.CheckEnv("BUCKET_NAME", "KO_DOCKER_REPO")
	if err != nil {
		t.Fatalf("CheckEnv failed: %v", err)
	}
	ctx := context.Background()
	clients := e2e.GetClients()

	e2e.EnsureEgressTrustBundle(t, ctx, clients)
	atespace, _ := e2e.DeployProbe(t, env["BUCKET_NAME"], "egressrotation", e2e.WithTrustBundle())
	rc, err := e2e.NewRouterClient(ctx)
	if err != nil {
		t.Fatalf("NewRouterClient: %v", err)
	}
	defer rc.Close()
	r := &rotation{t: t, ctx: ctx, clients: clients, rc: rc, atespace: atespace}

	oldCA, err := e2e.EgressTrustPool(t, ctx, clients).SigningCA()
	if err != nil {
		t.Fatalf("the egress CA pool has no signing CA: %v", err)
	}
	r.phase.Store("before the rotation")

	const longRunning, suspended = "long-running", "suspended"
	for _, id := range []string{longRunning, suspended} {
		createAndResumeActor(t, ctx, clients, atespace, id)
		waitForActorState(t, ctx, clients, atespace, id, ateapipb.ActorState_ACTOR_STATE_RUNNING)
	}
	// Pin only once the gateway serves the pool's signing CA: an earlier
	// rotation's switch may still be propagating to it.
	r.waitForSigner(longRunning, oldCA)
	for _, id := range []string{longRunning, suspended} {
		r.expectFetch(id, "pinned", oldCA)
	}
	if _, err := clients.SubstrateAPI.SuspendActor(ctx, &ateapipb.SuspendActorRequest{Actor: &ateapipb.ObjectRef{Atespace: atespace, Name: suspended}}); err != nil {
		t.Fatalf("SuspendActor %q: %v", suspended, err)
	}
	waitForActorState(t, ctx, clients, atespace, suspended, ateapipb.ActorState_ACTOR_STATE_SUSPENDED)

	stop := r.startTraffic(longRunning)
	defer stop()

	// Step 1: add a CA. It is trusted from now on, but does not sign.
	keyType, err := oldCA.KeyType()
	if err != nil {
		t.Fatalf("CA %q: %v", oldCA.ID, err)
	}
	newID := fmt.Sprintf("e2e-ca-rotation-%d", time.Now().Unix())
	var newCA *localca.CA
	e2e.UpdateEgressTrustPool(t, ctx, clients, func(p *localca.ConcretePool) error {
		var err error
		newCA, err = p.AddCA(newID, keyType, 365*24*time.Hour)
		return err
	})
	r.phase.Store("CA added")
	r.newActor(oldCA)

	// Step 2: activate it. The gateway switches once kubelet refreshes its
	// Secret volume; new actors are started all the while.
	e2e.UpdateEgressTrustPool(t, ctx, clients, func(p *localca.ConcretePool) error {
		_, err := p.Activate(newID, time.Now())
		return err
	})
	r.phase.Store("CA activated")
	r.waitForSigner("", newCA)
	r.phase.Store("gateway signs with the new CA")

	if _, err := clients.SubstrateAPI.ResumeActor(ctx, &ateapipb.ResumeActorRequest{Actor: &ateapipb.ObjectRef{Atespace: atespace, Name: suspended}}); err != nil {
		t.Fatalf("ResumeActor %q: %v", suspended, err)
	}
	waitForActorState(t, ctx, clients, atespace, suspended, ateapipb.ActorState_ACTOR_STATE_RUNNING)
	r.expectFetch(suspended, "pinned", newCA)
	r.expectFetch(suspended, "bundle", newCA)

	// Step 3: retire the old CA. retire-ca waits 20 minutes after the
	// activation by default, for the gateways whose leaves outlive a CA load
	// (sdsmint mints them for 15). agentgateway, the gateway this lane runs,
	// starts a fresh leaf cache with every CA it loads, and it has been
	// observed signing with the new CA, so none of its leaves chains to the
	// old one any more.
	e2e.UpdateEgressTrustPool(t, ctx, clients, func(p *localca.ConcretePool) error {
		return p.Retire(oldCA.ID, time.Now(), 0)
	})
	r.phase.Store("old CA retired")
	for range 3 {
		r.newActor(newCA)
	}
	r.expectFetch(longRunning, "pinned", newCA)
	r.expectFetch(suspended, "pinned", newCA)

	stop()
	sent, failures := r.results()
	if sent < 20 {
		t.Errorf("the long-running actor made only %d fetches during the rotation; the traffic did not run", sent)
	}
	for i, f := range failures {
		if i == 10 {
			t.Errorf("... %d more failures", len(failures)-i)
			break
		}
		t.Error(f)
	}
	t.Logf("rotation from CA %q to %q: %d fetches by the long-running actor, %d new actors, %d failures", oldCA.ID, newID, sent, r.newActors, len(failures))
}

// rotation drives the actors of TestEgressMITMCARotation.
type rotation struct {
	t        *testing.T
	ctx      context.Context
	clients  *e2e.Clients
	rc       *e2e.RouterClient
	atespace string

	// phase names the rotation step in progress, for failure messages.
	phase     atomic.Value
	newActors int

	mu       sync.Mutex
	sent     int
	failures []string
}

func keyID(ca *localca.CA) string { return hex.EncodeToString(ca.RootCertificate.SubjectKeyId) }

// check reports a fetch that did not return 200 as a failure.
func (r *rotation) check(id, roots string, out fetchResponse, err error) bool {
	if err == nil && out.Error == "" && out.Status == "200" {
		return true
	}
	msg := fmt.Sprintf("%s: actor %s, roots=%s: ", r.phase.Load(), id, roots)
	switch {
	case err != nil:
		msg += err.Error()
	case out.Error != "":
		msg += out.Error
	default:
		msg += "status " + out.Status
	}
	r.mu.Lock()
	r.failures = append(r.failures, time.Now().UTC().Format(time.TimeOnly)+" "+msg)
	r.mu.Unlock()
	return false
}

// expectFetch fetches the origin through actor id and requires a leaf signed
// by signer.
func (r *rotation) expectFetch(id, roots string, signer *localca.CA) {
	r.t.Helper()
	out, err := fetch(r.ctx, r.rc, r.atespace, id, "https://"+egressOriginHost+"/", roots)
	if r.check(id, roots, out, err) && out.LeafAuthorityKeyID != keyID(signer) {
		r.t.Errorf("%s: actor %s, roots=%s: served a leaf signed by key %s, want CA %q's %s", r.phase.Load(), id, roots, out.LeafAuthorityKeyID, signer.ID, keyID(signer))
	}
}

// newActor starts an actor, fetches through it with both anchor modes,
// requiring leaves signed by signer, and deletes it again.
func (r *rotation) newActor(signer *localca.CA) {
	r.t.Helper()
	r.newActors++
	id := fmt.Sprintf("new-%d", r.newActors)
	createAndResumeActor(r.t, r.ctx, r.clients, r.atespace, id)
	waitForActorState(r.t, r.ctx, r.clients, r.atespace, id, ateapipb.ActorState_ACTOR_STATE_RUNNING)
	r.expectFetch(id, "pinned", signer)
	r.expectFetch(id, "bundle", signer)
	ref := &ateapipb.ObjectRef{Atespace: r.atespace, Name: id}
	_, _ = r.clients.SubstrateAPI.SuspendActor(r.ctx, &ateapipb.SuspendActorRequest{Actor: ref})
	_, _ = r.clients.SubstrateAPI.DeleteActor(r.ctx, &ateapipb.DeleteActorRequest{Actor: ref})
}

// waitForSigner waits until the gateway serves leaves signed by want. With an
// actor ID it fetches through that actor; without one it starts a new actor
// per attempt, each of which has to verify what the gateway serves whichever
// CA signed it.
func (r *rotation) waitForSigner(id string, want *localca.CA) {
	r.t.Helper()
	deadline := time.Now().Add(4 * time.Minute)
	for {
		var got string
		if id != "" {
			out, err := fetch(r.ctx, r.rc, r.atespace, id, "https://"+egressOriginHost+"/", "bundle")
			if err == nil && out.Error == "" {
				got = out.LeafAuthorityKeyID
			}
		} else {
			r.newActors++
			actor := fmt.Sprintf("new-%d", r.newActors)
			createAndResumeActor(r.t, r.ctx, r.clients, r.atespace, actor)
			waitForActorState(r.t, r.ctx, r.clients, r.atespace, actor, ateapipb.ActorState_ACTOR_STATE_RUNNING)
			for _, roots := range []string{"pinned", "bundle"} {
				out, err := fetch(r.ctx, r.rc, r.atespace, actor, "https://"+egressOriginHost+"/", roots)
				if r.check(actor, roots, out, err) {
					got = out.LeafAuthorityKeyID
				}
			}
			ref := &ateapipb.ObjectRef{Atespace: r.atespace, Name: actor}
			_, _ = r.clients.SubstrateAPI.SuspendActor(r.ctx, &ateapipb.SuspendActorRequest{Actor: ref})
			_, _ = r.clients.SubstrateAPI.DeleteActor(r.ctx, &ateapipb.DeleteActorRequest{Actor: ref})
		}
		if got == keyID(want) {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("%s: the gateway still serves leaves signed by key %q after 4 minutes, want CA %q's %s", r.phase.Load(), got, want.ID, keyID(want))
		}
		time.Sleep(2 * time.Second)
	}
}

// startTraffic fetches through actor id, alternately with its pinned anchors
// and the re-read bundle, until the returned function is called.
func (r *rotation) startTraffic(id string) (stop func()) {
	ctx, cancel := context.WithCancel(r.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ctx.Err() == nil; i++ {
			roots := "pinned"
			if i%2 == 1 {
				roots = "bundle"
			}
			out, err := fetch(ctx, r.rc, r.atespace, id, "https://"+egressOriginHost+"/", roots)
			if ctx.Err() != nil {
				return
			}
			r.check(id, roots, out, err)
			r.mu.Lock()
			r.sent++
			r.mu.Unlock()
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
}

func (r *rotation) results() (int, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sent, r.failures
}
