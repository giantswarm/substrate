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

package imagecache

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/internal/ateerrors"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// newTestRegistry starts an in-memory OCI registry and returns its host
// ("127.0.0.1:<port>", which the store treats as a local registry and pulls
// over plain HTTP).
func newTestRegistry(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parsing registry URL: %v", err)
	}
	return srv, u.Host
}

func layerFromEntries(t *testing.T, entries []tarEntry) v1.Layer {
	t.Helper()
	b := buildTar(t, entries)
	l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(b)), nil
	})
	if err != nil {
		t.Fatalf("tarball.LayerFromOpener: %v", err)
	}
	return l
}

func pushImage(t *testing.T, ref string, cfg v1.Config, layers ...v1.Layer) v1.Image {
	t.Helper()
	img, err := mutate.ConfigFile(empty.Image, &v1.ConfigFile{Config: cfg})
	if err != nil {
		t.Fatalf("mutate.ConfigFile: %v", err)
	}
	img, err = mutate.AppendLayers(img, layers...)
	if err != nil {
		t.Fatalf("mutate.AppendLayers: %v", err)
	}
	tag, err := name.ParseReference(ref, name.Insecure)
	if err != nil {
		t.Fatalf("name.ParseReference(%q): %v", ref, err)
	}
	if err := remote.Write(tag, img); err != nil {
		t.Fatalf("remote.Write(%q): %v", ref, err)
	}
	return img
}

func newTestStore(t *testing.T, opts ...Option) *Store {
	t.Helper()
	s, err := New(t.TempDir(), opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// TestRetryBackoffFor pins the per-registry split: well-known communal
// registries get the patient fleet-throttling backoff, everything else —
// registries the deployment has its own quota with, including local ones —
// retries fast (their pulls sit on the Run/Restore critical path).
func TestRetryBackoffFor(t *testing.T) {
	for registry, want := range map[string]remote.Backoff{
		"gcr.io":                              dedicatedRegistryBackoff,
		"us.gcr.io":                           dedicatedRegistryBackoff,
		"us-docker.pkg.dev":                   dedicatedRegistryBackoff,
		"123.dkr.ecr.us-east-1.amazonaws.com": dedicatedRegistryBackoff,
		"harbor.example.com":                  dedicatedRegistryBackoff,
		"127.0.0.1:5000":                      dedicatedRegistryBackoff,
		"registry.k8s.io":                     sharedRegistryBackoff,
		"docker.io":                           sharedRegistryBackoff,
		"index.docker.io":                     sharedRegistryBackoff,
		"quay.io":                             sharedRegistryBackoff,
		"ghcr.io":                             sharedRegistryBackoff,
	} {
		if got := retryBackoffFor(registry); got != want {
			t.Errorf("retryBackoffFor(%q) = %+v, want %+v", registry, got, want)
		}
	}
}

// TestEnsureImage_RetriesRateLimit proves a pull survives transient 429s.
// go-containerregistry's retry transport, configured with the per-registry
// backoff picked by retryBackoffFor, covers every request including the /v2/
// auth ping — the first request a throttling registry rejects (as
// registry.k8s.io does per source IP).
func TestEnsureImage_RetriesRateLimit(t *testing.T) {
	origBackoff := dedicatedRegistryBackoff
	dedicatedRegistryBackoff = remote.Backoff{Duration: time.Millisecond, Factor: 2.0, Jitter: 0.1, Steps: 4}
	t.Cleanup(func() { dedicatedRegistryBackoff = origBackoff })

	// throttle > 0 makes the registry reject the next request with a 429;
	// it stays at zero until the test image is pushed.
	var throttle, rejections atomic.Int32
	inner := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if throttle.Add(-1) >= 0 {
			rejections.Add(1)
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parsing registry URL: %v", err)
	}

	ref := u.Host + "/test/pause:1"
	pushImage(t, ref, v1.Config{}, layerFromEntries(t, []tarEntry{
		{name: "pause", typeflag: tar.TypeReg, body: "pause"},
	}))
	throttle.Store(2)

	s := newTestStore(t)
	if _, err := s.EnsureImage(context.Background(), ref); err != nil {
		t.Fatalf("EnsureImage through a throttling registry: %v", err)
	}
	if got := rejections.Load(); got != 2 {
		t.Errorf("registry rejected %d requests, want 2 (the throttling was not exercised)", got)
	}
}

// TestClassifyRegistryErr pins the split the RPC boundaries classify on: a
// registry's final word (a 4xx other than a timeout or a throttle) carries
// ReasonFailedGetExternalObject; everything else — a registry that keeps
// failing transiently, connection trouble, a canceled pull, an error another
// layer already tagged — passes through as it came.
func TestClassifyRegistryErr(t *testing.T) {
	regErr := func(code int) error { return &transport.Error{StatusCode: code} }
	tests := []struct {
		name string
		err  error
		// wantReason is the Reason the returned error must carry; empty means
		// it must carry none.
		wantReason ateerrors.Reason
	}{
		{name: "nil", err: nil},
		{name: "manifest unknown", err: regErr(http.StatusNotFound), wantReason: ateerrors.ReasonFailedGetExternalObject},
		{name: "unauthorized", err: regErr(http.StatusUnauthorized), wantReason: ateerrors.ReasonFailedGetExternalObject},
		{name: "denied", err: regErr(http.StatusForbidden), wantReason: ateerrors.ReasonFailedGetExternalObject},
		{name: "wrapped rejection", err: fmt.Errorf("in remote.Image: %w", regErr(http.StatusNotFound)), wantReason: ateerrors.ReasonFailedGetExternalObject},
		{name: "request timeout", err: regErr(http.StatusRequestTimeout)},
		{name: "throttled", err: regErr(http.StatusTooManyRequests)},
		{name: "server error", err: regErr(http.StatusInternalServerError)},
		{name: "unavailable", err: regErr(http.StatusServiceUnavailable)},
		{name: "connection trouble", err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}},
		{name: "canceled", err: fmt.Errorf("while waiting for pull: %w", context.Canceled)},
		{name: "local cache", err: fmt.Errorf("while reading image record: %w", os.ErrPermission)},
		{name: "already tagged", err: fmt.Errorf("%w: %w", ateerrors.ReasonInvalidObjectURL, regErr(http.StatusNotFound)), wantReason: ateerrors.ReasonInvalidObjectURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyRegistryErr(tt.err)
			if tt.err == nil {
				if got != nil {
					t.Fatalf("classifyRegistryErr(nil) = %v, want nil", got)
				}
				return
			}
			if !errors.Is(got, tt.err) {
				t.Errorf("classifyRegistryErr(%v) = %v, want the cause kept in the chain", tt.err, got)
			}
			r, tagged := errors.AsType[ateerrors.Reason](got)
			switch {
			case tt.wantReason == "" && tagged:
				t.Errorf("classifyRegistryErr(%v) carries reason %v, want none (the failure must stay retriable)", tt.err, r)
			case tt.wantReason != "" && (!tagged || r != tt.wantReason):
				t.Errorf("classifyRegistryErr(%v) reason = %v (tagged=%v), want %v", tt.err, r, tagged, tt.wantReason)
			}
		})
	}
}

// TestEnsureImage_RegistryRejectionIsTagged drives the classification through
// a real registry: an image the registry has never seen — by tag or by digest,
// a 404 the transport does not retry — comes back tagged
// ReasonFailedGetExternalObject with the registry's answer still in the chain,
// while a registry that keeps failing with a 5xx comes back untagged once the
// retries are spent.
func TestEnsureImage_RegistryRejectionIsTagged(t *testing.T) {
	origBackoff := dedicatedRegistryBackoff
	dedicatedRegistryBackoff = remote.Backoff{Duration: time.Millisecond, Factor: 2.0, Jitter: 0.1, Steps: 2}
	t.Cleanup(func() { dedicatedRegistryBackoff = origBackoff })

	_, host := newTestRegistry(t)
	for name, ref := range map[string]string{
		"missing tag":    host + "/test/missing:1",
		"missing digest": host + "/test/missing@sha256:" + strings.Repeat("0", 64),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newTestStore(t).EnsureImage(context.Background(), ref)
			if !errors.Is(err, ateerrors.ReasonFailedGetExternalObject) {
				t.Fatalf("EnsureImage(%q) = %v, want it tagged ReasonFailedGetExternalObject", ref, err)
			}
			regErr, ok := errors.AsType[*transport.Error](err)
			if !ok {
				t.Fatalf("EnsureImage(%q) = %v, want the registry's answer kept in the chain", ref, err)
			}
			if regErr.StatusCode != http.StatusNotFound {
				t.Errorf("registry answered %d, want 404", regErr.StatusCode)
			}
		})
	}

	t.Run("registry that keeps failing", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv.Close)
		u, err := url.Parse(srv.URL)
		if err != nil {
			t.Fatalf("parsing registry URL: %v", err)
		}
		_, err = newTestStore(t).EnsureImage(context.Background(), u.Host+"/test/flaky:1")
		if err == nil {
			t.Fatal("EnsureImage through a registry that only answers 503 succeeded")
		}
		if r, tagged := errors.AsType[ateerrors.Reason](err); tagged {
			t.Errorf("EnsureImage = %v: carries reason %v, want none (a 5xx must stay retriable)", err, r)
		}
	})
}

func TestEnsureImage_TagPullAndDigestHit(t *testing.T) {
	srv, host := newTestRegistry(t)
	ref := host + "/test/app:latest"

	base := layerFromEntries(t, []tarEntry{
		{name: "bin/", typeflag: tar.TypeDir},
		{name: "bin/sh", typeflag: tar.TypeReg, mode: 0o755, body: "#!/sh\n"},
	})
	top := layerFromEntries(t, []tarEntry{
		{name: "app/", typeflag: tar.TypeDir},
		{name: "app/main", typeflag: tar.TypeReg, mode: 0o755, body: "main"},
		{name: "bin/.wh.sh", typeflag: tar.TypeReg},
		{name: "app/.wh..wh..opq", typeflag: tar.TypeReg},
	})
	pushImage(t, ref, v1.Config{Env: []string{"FOO=bar"}}, base, top)

	store := newTestStore(t)
	ctx := context.Background()

	// Tag refs must be cacheable: they resolve to a digest via one HEAD.
	img, err := store.EnsureImage(ctx, ref)
	if err != nil {
		t.Fatalf("EnsureImage(tag): %v", err)
	}
	if len(img.LayerDirs) != 2 {
		t.Fatalf("LayerDirs = %v, want 2 entries", img.LayerDirs)
	}
	if !slices.Equal(img.Config.Env, []string{"FOO=bar"}) {
		t.Errorf("Config.Env = %v, want [FOO=bar]", img.Config.Env)
	}

	// Bottom-first order: the base layer's file is in LayerDirs[0].
	if _, err := os.Stat(filepath.Join(img.LayerDirs[0], layerFSDirName, "bin/sh")); err != nil {
		t.Errorf("base layer content missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(img.LayerDirs[1], layerFSDirName, "app/main")); err != nil {
		t.Errorf("top layer content missing: %v", err)
	}

	// The top layer's whiteout state is recorded beside its tree, not in it.
	wh, err := readWhiteouts(img.LayerDirs[1])
	if err != nil {
		t.Fatalf("readWhiteouts: %v", err)
	}
	if want := []string{"bin/sh"}; !slices.Equal(wh.Whiteouts, want) {
		t.Errorf("whiteouts = %v, want %v", wh.Whiteouts, want)
	}
	if want := []string{"app"}; !slices.Equal(wh.Opaques, want) {
		t.Errorf("opaques = %v, want %v", wh.Opaques, want)
	}
	if _, err := os.Lstat(filepath.Join(img.LayerDirs[1], layerFSDirName, "bin/.wh.sh")); err == nil {
		t.Errorf("whiteout entry written into the layer tree")
	}

	// A digest-pinned ref must now hit the cache with zero registry traffic:
	// kill the registry and pull again through a fresh store on the same root
	// (which also exercises startup recovery over a populated pool).
	srv.Close()
	store2, err := New(store.root)
	if err != nil {
		t.Fatalf("New(existing root): %v", err)
	}
	img2, err := store2.EnsureImage(ctx, host+"/test/app@"+img.Digest.String())
	if err != nil {
		t.Fatalf("EnsureImage(digest, registry down): %v", err)
	}
	if !slices.Equal(img2.LayerDirs, img.LayerDirs) {
		t.Errorf("cache hit LayerDirs = %v, want %v", img2.LayerDirs, img.LayerDirs)
	}
	if !slices.Equal(img2.Config.Env, img.Config.Env) {
		t.Errorf("cache hit Config.Env = %v, want %v", img2.Config.Env, img.Config.Env)
	}
}

// Two images sharing a base layer must share its unpacked tree: the pool
// holds three layer dirs, not four.
func TestEnsureImage_SharedLayersDeduplicated(t *testing.T) {
	_, host := newTestRegistry(t)

	shared := layerFromEntries(t, []tarEntry{
		{name: "lib/", typeflag: tar.TypeDir},
		{name: "lib/base.so", typeflag: tar.TypeReg, body: "base"},
	})
	topA := layerFromEntries(t, []tarEntry{{name: "a", typeflag: tar.TypeReg, body: "a"}})
	topB := layerFromEntries(t, []tarEntry{{name: "b", typeflag: tar.TypeReg, body: "b"}})
	pushImage(t, host+"/test/a:latest", v1.Config{}, shared, topA)
	pushImage(t, host+"/test/b:latest", v1.Config{}, shared, topB)

	store := newTestStore(t)
	ctx := context.Background()

	imgA, err := store.EnsureImage(ctx, host+"/test/a:latest")
	if err != nil {
		t.Fatalf("EnsureImage(a): %v", err)
	}
	imgB, err := store.EnsureImage(ctx, host+"/test/b:latest")
	if err != nil {
		t.Fatalf("EnsureImage(b): %v", err)
	}

	if imgA.LayerDirs[0] != imgB.LayerDirs[0] {
		t.Errorf("shared base layer not deduplicated: %q vs %q", imgA.LayerDirs[0], imgB.LayerDirs[0])
	}
	entries, err := os.ReadDir(store.layersDir())
	if err != nil {
		t.Fatalf("ReadDir(layers): %v", err)
	}
	if len(entries) != 3 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("layer pool holds %d dirs (%v), want 3 (shared base stored once)", len(entries), names)
	}
}

// Startup recovery must sweep temp dirs orphaned by a crash mid-unpack, and
// reject a cache root with an unknown layout version.
func TestNew_RecoveryAndVersioning(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	orphan := filepath.Join(s.layersDir(), ".tmp-deadbeef-123")
	if err := os.MkdirAll(orphan, 0o700); err != nil {
		t.Fatalf("planting orphan: %v", err)
	}
	recOrphan := filepath.Join(s.manifestsDir(), ".deadbeef.json.tmp-456")
	if err := os.WriteFile(recOrphan, []byte("{"), 0o600); err != nil {
		t.Fatalf("planting record orphan: %v", err)
	}
	record := filepath.Join(s.manifestsDir(), "deadbeef.json")
	if err := os.WriteFile(record, []byte("{}"), 0o600); err != nil {
		t.Fatalf("planting record: %v", err)
	}
	if _, err := New(root); err != nil {
		t.Fatalf("New(recovery): %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("orphaned temp dir survived recovery")
	}
	if _, err := os.Stat(recOrphan); !os.IsNotExist(err) {
		t.Errorf("orphaned manifest temp file survived recovery")
	}
	if _, err := os.Stat(record); err != nil {
		t.Errorf("completed manifest record swept by recovery: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, versionFileName), []byte("99\n"), 0o600); err != nil {
		t.Fatalf("writing version marker: %v", err)
	}
	if _, err := New(root); err == nil {
		t.Errorf("New accepted an unknown layout version")
	}
}

func TestIsLocalRegistry(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{ref: "localhost/foo", want: true},
		{ref: "localhost:5001/foo", want: true},
		{ref: "127.0.0.1/foo", want: true},
		{ref: "127.0.0.1:5001/foo", want: true},
		{ref: "127.0.0.2/foo", want: true},
		{ref: "127.0.0.2:8080/foo", want: true},
		{ref: "kind-registry/foo", want: false},
		{ref: "kind-registry:5000/foo", want: false},
		{ref: "my-registry.local/foo", want: false},
		{ref: "my-registry.local:8080/foo", want: false},
		{ref: "gcr.io/foo", want: false},
		{ref: "example.com/foo", want: false},
		{ref: "foo", want: false},
		{ref: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			got := isLocalRegistry(tt.ref)
			if got != tt.want {
				t.Errorf("isLocalRegistry(%q) = %v; want %v", tt.ref, got, tt.want)
			}
		})
	}
}

func TestRewriteLocalRegistry(t *testing.T) {
	s := &Store{localhostRegistryReplacement: "kind-registry:5000"}

	tests := []struct {
		ref  string
		want string
	}{
		{ref: "localhost/foo", want: "kind-registry:5000/foo"},
		{ref: "localhost:5001/foo", want: "kind-registry:5000/foo"},
		{ref: "localhost:8080/foo", want: "kind-registry:5000/foo"},
		{ref: "127.0.0.1/foo", want: "kind-registry:5000/foo"},
		{ref: "127.0.0.1:3000/foo", want: "kind-registry:5000/foo"},
		{ref: "127.0.0.2/foo", want: "kind-registry:5000/foo"},
		{ref: "127.0.0.2:8080/foo", want: "kind-registry:5000/foo"},
		{ref: "kind-registry/foo", want: "kind-registry/foo"},
		{ref: "kind-registry:5000/foo", want: "kind-registry:5000/foo"},
		{ref: "my-registry.local/foo", want: "my-registry.local/foo"},
		{ref: "gcr.io/foo", want: "gcr.io/foo"},
		{ref: "foo", want: "foo"},
		{ref: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			got := s.rewriteLocalRegistry(tt.ref)
			if got != tt.want {
				t.Errorf("rewriteLocalRegistry(%q) = %q; want %q", tt.ref, got, tt.want)
			}
		})
	}
}
