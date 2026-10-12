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

// Package egresscredinject e2e-tests egress credential injection: a matching
// EgressPolicy https rule with a replace_headers effect makes the egress
// gateway's MITM leg resolve the credential through the
// k8s-credential-provider and replace the actor's placeholder header with it
// before re-originating upstream. See TestActorEgressCredentialInjection for
// the proof structure and how to run this locally.
package egresscredinject

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/internal/e2e"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const probeTemplate = "probe"

var probeNamespace string

// The suite's hostnames, one per injection outcome: each is covered by exactly
// one https rule, so each selects one CredentialHeader per request path.
// echoHost is the only one whose response matters — it echoes the request
// headers it received back as JSON, which is what proves the header was on
// the wire.
const (
	echoHost           = "httpbin.org"
	unfetchableHost    = "example.com"
	unservedHost       = "example.org"
	unauthorizedHost   = "example.net"
	unfetchableOrigin  = "https://" + unfetchableHost + "/"
	unservedOrigin     = "https://" + unservedHost + "/"
	unauthorizedOrigin = "https://" + unauthorizedHost + "/"
)

// placeholder is the Authorization value the actor sends. replace_headers
// replaces a header only when the request carries it, so every fetch that
// should trigger injection sends it.
const placeholder = "Bearer actor-placeholder"

// The echo host takes two schemes on one host, as a Git server does for Git
// over HTTPS and for its REST API: Bearer on the paths under echoAPIPrefix,
// Basic on every other path.
const (
	echoAPIPrefix = "/anything/api/"
	wantHostWide  = "Basic " + e2e.CredentialInjectionToken
	wantAPI       = "Bearer " + e2e.CredentialInjectionToken
)

var withPlaceholder = []string{"header=" + url.QueryEscape("Authorization:"+placeholder)}

// TestActorEgressCredentialInjection proves the injected credential reaches
// the upstream, and that every way injection can go wrong lands on the
// documented side of fail-open vs fail-closed (see egress.Handler.applyEffects):
//
//   - replaced: a fetch of the echo origin carrying the placeholder echoes the
//     injected "Authorization: Bearer <token>" among the headers the origin
//     received — the on-the-wire proof, not an inference from a status code —
//     and not the placeholder, so an actor cannot choose the value that
//     leaves.
//   - no header: the same fetch without the placeholder echoes no
//     Authorization at all — the gateway replaces only a header the request
//     carries, and does not add one.
//   - cleartext: the same fetch over plain HTTP, allowed by an http rule
//     with the same effect, echoes the injected credential too.
//   - per path: the echo host binds Basic host-wide and Bearer under
//     echoAPIPrefix, and each request echoes the binding with the longest
//     path prefix it is under, the host-wide one otherwise.
//   - fail closed: a credential the policy requires but the provider will
//     not or cannot produce denies the request — 403 for an unfetchable
//     secret and for a namespace outside the atespace's authorization
//     (default-deny), 403 or 500 for a URI naming a provider this gateway
//     does not serve.
//
// This needs the install made with the bundled provider, which
// deploys the k8s-credential-provider and points the egress gateway at it.
// Locally:
//
//	hack/install-ate-kind.sh --deploy-atenet --credential-provider='{"name":"k8s.io"}'
//	hack/run-e2e-kind.sh ./internal/e2e/suites/egresscredinject -v -args --no-color
func TestActorEgressCredentialInjection(t *testing.T) {
	env, err := e2e.CheckEnv("BUCKET_NAME", "KO_DOCKER_REPO")
	if err != nil {
		t.Fatalf("CheckEnv failed: %v", err)
	}
	ctx := context.Background()
	clients := e2e.GetClients()

	e2e.ConfigureCredentialProvider(t)

	probeNamespace, _ = e2e.DeployProbe(t, env["BUCKET_NAME"], "egresscredinject", e2e.WithTrustBundle())

	const id = "probe-credinject"
	createAndResumeActor(t, ctx, clients, id)
	waitForActorState(t, ctx, clients, id, ateapipb.ActorState_ACTOR_STATE_RUNNING)

	rc, err := e2e.NewRouterClient(ctx)
	if err != nil {
		t.Fatalf("NewRouterClient: %v", err)
	}
	defer rc.Close()

	for _, scheme := range []string{"https", "http"} {
		base := scheme + "://" + echoHost
		t.Run(scheme, func(t *testing.T) {
			// The upstream must receive the policy's credential instead of the
			// actor's placeholder, on both HTTP and HTTPS.
			replaced := fetchEcho(t, ctx, rc, id, base+"/headers", withPlaceholder)
			if got := assertEchoedAuthorization(t, "injection fetch", replaced); got != wantHostWide {
				t.Errorf("upstream received Authorization %q, want the injected %q", got, wantHostWide)
			}

			// No header is added when the actor did not send one.
			unasked := fetchEcho(t, ctx, rc, id, base+"/headers", nil)
			if got := assertEchoedAuthorization(t, "fetch without the header", unasked); got != "" {
				t.Errorf("upstream received Authorization %q on a request that did not carry it, want none", got)
			}

			// Each request gets the binding of the longest path prefix it is
			// under, the host-wide one otherwise; a prefix ends at a segment
			// boundary.
			for _, step := range []struct {
				path string
				want string
			}{
				{path: echoAPIPrefix + "v4/projects?per_page=1", want: wantAPI},
				{path: echoAPIPrefix, want: wantAPI},
				{path: "/anything/api", want: wantHostWide},
				{path: "/anything/apis/v4", want: wantHostWide},
				{path: "/anything/group/repo.git/info/refs?service=git-upload-pack", want: wantHostWide},
			} {
				resp := fetchEcho(t, ctx, rc, id, base+step.path, withPlaceholder)
				if got := assertEchoedAuthorization(t, "fetch of "+step.path, resp); got != step.want {
					t.Errorf("upstream received Authorization %q on %s, want %q", got, step.path, step.want)
				}
			}
		})
	}

	t.Run("duplicate path prefix refused", func(t *testing.T) {
		assertDuplicatePathPrefixRefused(t, ctx, clients, &ateapipb.ObjectRef{Atespace: probeNamespace, Name: id})
	})

	// Fail closed: each of these rules names a credential that cannot be
	// injected, and the denial must be the mapped status, not a request that
	// went out without the credential. No retries: the gateway answers these
	// itself.
	tests := []struct {
		name         string
		origin       string
		wantStatuses []string
	}{{
		name:         "unfetchable secret",
		origin:       unfetchableOrigin,
		wantStatuses: []string{"403"},
	}, {
		// Agentgateway denies an unserved provider as 403; Envoy uses 500.
		name:         "unserved provider",
		origin:       unservedOrigin,
		wantStatuses: []string{"403", "500"},
	}, {
		name:         "unauthorized namespace",
		origin:       unauthorizedOrigin,
		wantStatuses: []string{"403"},
	}}
	for _, tt := range tests {
		for _, origin := range []string{tt.origin, strings.Replace(tt.origin, "https://", "http://", 1)} {
			t.Run(tt.name+"/"+origin, func(t *testing.T) {
				got := probeFetch(t, ctx, rc, id, origin, withPlaceholder)
				if got.Error != "" {
					t.Fatalf("fetch of %s failed at the transport (%s), want an HTTP denial %v from the gateway", origin, got.Error, tt.wantStatuses)
				}
				if !slices.Contains(tt.wantStatuses, got.Status) {
					t.Errorf("fetch of %s returned status %s, want one of %v", origin, got.Status, tt.wantStatuses)
				}
			})
		}
	}
}

// assertDuplicatePathPrefixRefused proves admission refuses two entries for
// one host, header and path prefix, and that the refused update leaves the
// stored policy as it was.
func assertDuplicatePathPrefixRefused(t *testing.T, ctx context.Context, clients *e2e.Clients, actor *ateapipb.ObjectRef) {
	t.Helper()
	stored, err := clients.SubstrateAPI.GetActorEgressPolicy(ctx, &ateapipb.GetActorEgressPolicyRequest{Actor: actor})
	if err != nil {
		t.Fatalf("GetActorEgressPolicy: %v", err)
	}
	dup := proto.Clone(stored).(*ateapipb.EgressPolicy)
	i := slices.IndexFunc(dup.GetRules(), func(r *ateapipb.EgressRule) bool {
		return slices.Contains(r.GetHttps().GetHostnames(), echoHost)
	})
	if i < 0 {
		t.Fatalf("the stored policy has no https rule for %s: %v", echoHost, stored)
	}
	effects := dup.GetRules()[i].GetHttps().GetEffects()
	effects.ReplaceHeaders = append(effects.ReplaceHeaders, &ateapipb.CredentialHeader{
		Header:        "authorization",
		Prefix:        "Token ",
		CredentialUri: e2e.CredentialInjectionURI,
		PathPrefix:    echoAPIPrefix,
	})
	_, err = clients.SubstrateAPI.UpdateActorEgressPolicy(ctx, &ateapipb.UpdateActorEgressPolicyRequest{Actor: actor, EgressPolicy: dup})
	if status.Code(err) != codes.InvalidArgument || !strings.Contains(err.Error(), "path_prefix") {
		t.Fatalf("UpdateActorEgressPolicy with a second Authorization entry on %s: err %v, want InvalidArgument naming path_prefix", echoAPIPrefix, err)
	}
	after, err := clients.SubstrateAPI.GetActorEgressPolicy(ctx, &ateapipb.GetActorEgressPolicyRequest{Actor: actor})
	if err != nil {
		t.Fatalf("GetActorEgressPolicy after the refused update: %v", err)
	}
	if !proto.Equal(after, stored) {
		t.Errorf("the refused update changed the stored policy:\n got %v\nwant %v", after, stored)
	}
}

const echoRetryWindow = 2 * time.Minute

// fetchEcho is probeFetch for echo fetches that should return 200. It retries
// transient failures for up to echoRetryWindow:
//
//   - certificate errors, until the gateway's signing pool propagates;
//   - 503, while the gateway reconnects to a redeployed provider;
//   - 502/503/504 from httpbin.org itself.
func fetchEcho(t *testing.T, ctx context.Context, rc *e2e.RouterClient, id, origin string, extraParams []string) fetchResponse {
	t.Helper()
	deadline := time.Now().Add(echoRetryWindow)
	for {
		resp := probeFetch(t, ctx, rc, id, origin, extraParams)
		if !transientEchoFailure(resp) || time.Now().After(deadline) {
			return resp
		}
		t.Logf("fetch of %s: transient failure (status %q, error %q), retrying", origin, resp.Status, resp.Error)
		time.Sleep(5 * time.Second)
	}
}

// transientEchoFailure reports whether fetchEcho should retry resp.
func transientEchoFailure(resp fetchResponse) bool {
	if resp.Error != "" {
		return strings.Contains(resp.Error, "certificate") || strings.Contains(resp.Error, "x509")
	}
	switch resp.Status {
	case "502", "503", "504":
		return true
	}
	return false
}

// echoedHeaders is the echo origin's response shape: the request headers it
// received, echoed back. (httpbin.org/headers returns {"headers": {...}}.)
type echoedHeaders struct {
	Headers map[string]string `json:"headers"`
}

// decodeEchoedHeaders parses the echo origin's body into the headers the
// upstream received.
func decodeEchoedHeaders(t *testing.T, step, body string) map[string]string {
	t.Helper()
	var echoed echoedHeaders
	if err := json.Unmarshal([]byte(body), &echoed); err != nil {
		t.Fatalf("%s: decoding the echo origin's body: %v (body %q)", step, err, body)
	}
	return echoed.Headers
}

// assertEchoedAuthorization fails on any transport- or HTTP-level failure of
// an echo fetch and returns the Authorization value the upstream received.
func assertEchoedAuthorization(t *testing.T, step string, resp fetchResponse) string {
	t.Helper()
	if resp.Error != "" {
		t.Fatalf("%s: TLS through the MITM egress gateway failed: %s", step, resp.Error)
	}
	if resp.Status != "200" {
		t.Fatalf("%s: status %s, want 200 (an injection failure would deny with 403/500/503; is the provider deployed and the gateway installed with --credential-provider='{\"name\":\"k8s.io\"}'?) body %q", step, resp.Status, resp.Body)
	}
	return decodeEchoedHeaders(t, step, resp.Body)["Authorization"]
}

type fetchResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Body   string `json:"body"`
}

// probeFetch asks the probe to fetch origin with the projected trust bundle,
// passing any extra pre-encoded query parameters through. Router-level
// failures are retried for up to 30s (a resume can return before the route
// reaches the router's xDS snapshot); probe-level TLS failures are results,
// returned for the caller to assert on.
func probeFetch(t *testing.T, ctx context.Context, rc *e2e.RouterClient, id, origin string, extraParams []string) fetchResponse {
	t.Helper()
	path := "/fetch?roots=bundle&url=" + url.QueryEscape(origin)
	for _, p := range extraParams {
		path += "&" + p
	}
	ref := resources.ActorRef{Atespace: probeNamespace, Name: id}

	deadline := time.Now().Add(30 * time.Second)
	for attempt := 1; ; attempt++ {
		resp, err := rc.Get(ctx, ref, path)
		if err != nil {
			t.Fatalf("GET %s for %q: %v", path, id, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatalf("reading %s response for %q: %v", path, id, readErr)
		}
		if resp.StatusCode == http.StatusOK {
			var out fetchResponse
			if err := json.Unmarshal(body, &out); err != nil {
				t.Fatalf("decoding %s response for %q: %v (body %q)", path, id, err, body)
			}
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s for %q: status %d after %d attempts, body %q", path, id, resp.StatusCode, attempt, body)
		}
		t.Logf("GET %s for %q: attempt %d: status %d, body %q; retrying", path, id, attempt, resp.StatusCode, body)
		time.Sleep(2 * time.Second)
	}
}

// createAndResumeActor first deletes any actor left by an earlier run, since
// actor records outlive the fixture namespace.
func createAndResumeActor(t *testing.T, ctx context.Context, clients *e2e.Clients, id string) {
	t.Helper()
	ref := &ateapipb.ObjectRef{Atespace: probeNamespace, Name: id}
	// NotFound is the normal case on a fresh run. Other errors don't stop the
	// test, but they are logged in case CreateActor then fails.
	if _, err := clients.SubstrateAPI.SuspendActor(ctx, &ateapipb.SuspendActorRequest{Actor: ref}); err != nil && status.Code(err) != codes.NotFound {
		t.Logf("removing leftover actor %q: SuspendActor: %v", id, err)
	}
	if _, err := clients.SubstrateAPI.DeleteActor(ctx, &ateapipb.DeleteActorRequest{Actor: ref}); err != nil && status.Code(err) != codes.NotFound {
		t.Logf("removing leftover actor %q: DeleteActor: %v", id, err)
	}
	if _, err := clients.SubstrateAPI.CreateActor(ctx, &ateapipb.CreateActorRequest{Actor: &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: probeNamespace, Name: id},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: probeNamespace, Name: probeTemplate},
	}}); err != nil {
		t.Fatalf("CreateActor %q: %v", id, err)
	}
	t.Cleanup(func() {
		if _, err := clients.SubstrateAPI.SuspendActor(ctx, &ateapipb.SuspendActorRequest{Actor: ref}); err != nil {
			t.Logf("cleanup: SuspendActor %q: %v", id, err)
		}
		if _, err := clients.SubstrateAPI.DeleteActor(ctx, &ateapipb.DeleteActorRequest{Actor: ref}); err != nil {
			t.Logf("cleanup: DeleteActor %q failed, actor leaked (remove with: kubectl ate delete actor %s -a %s): %v", id, id, probeNamespace, err)
		}
	})
	// Exercise each credential outcome on both HTTP and HTTPS.
	apiToken := &ateapipb.CredentialHeader{
		Header:        "Authorization",
		Prefix:        "Bearer ",
		CredentialUri: e2e.CredentialInjectionURI,
		PathPrefix:    echoAPIPrefix,
	}
	echoHTTPS := e2e.EgressInjectHeader("Authorization", "Basic ", e2e.CredentialInjectionURI, echoHost)
	echoHTTPS.Https.Effects.ReplaceHeaders = append(echoHTTPS.Https.Effects.ReplaceHeaders, apiToken)
	echoHTTP := e2e.EgressInjectHeaderHTTP("Authorization", "Basic ", e2e.CredentialInjectionURI, echoHost)
	echoHTTP.Http.Effects.ReplaceHeaders = append(echoHTTP.Http.Effects.ReplaceHeaders, apiToken)
	rules := []*ateapipb.EgressRule{echoHTTPS, echoHTTP}
	for _, credential := range []struct {
		host string
		uri  string
	}{
		{unfetchableHost, "ate-secret://k8s.io/default/" + e2e.CredentialSecretsNamespace + "/no-such-secret/token"},
		{unservedHost, "ate-secret://other.io/default/" + e2e.CredentialSecretsNamespace + "/api-token/token"},
		{unauthorizedHost, "ate-secret://k8s.io/default/kube-system/api-token/token"},
	} {
		rules = append(rules,
			e2e.EgressInjectHeader("Authorization", "Bearer ", credential.uri, credential.host),
			e2e.EgressInjectHeaderHTTP("Authorization", "Bearer ", credential.uri, credential.host),
		)
	}
	e2e.EnsureEgressPolicy(t, ctx, clients, ref, rules...)

	if _, err := clients.SubstrateAPI.ResumeActor(ctx, &ateapipb.ResumeActorRequest{Actor: ref}); err != nil {
		t.Fatalf("ResumeActor %q: %v", id, err)
	}
}

func waitForActorState(t *testing.T, ctx context.Context, clients *e2e.Clients, actorName string, want ateapipb.ActorState) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := clients.SubstrateAPI.GetActor(ctx, &ateapipb.GetActorRequest{
			Actor: &ateapipb.ObjectRef{Atespace: probeNamespace, Name: actorName},
		})
		if err == nil && resp.GetStatus().GetState() == want {
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("timed out waiting for actor %q to reach state %v", actorName, want)
}
