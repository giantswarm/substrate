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

package controlapi

import (
	"context"
	"strings"
	"testing"

	"github.com/agent-substrate/substrate/internal/proto/ateletpb"
	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	listersv1alpha1 "github.com/agent-substrate/substrate/pkg/client/listers/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

// sandboxConfigListerFor builds a lister over the given SandboxConfigs, using
// the same key function the informers use.
func sandboxConfigListerFor(t *testing.T, configs []*atev1alpha1.SandboxConfig) listersv1alpha1.SandboxConfigLister {
	t.Helper()
	configIdx := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, c := range configs {
		if err := configIdx.Add(c); err != nil {
			t.Fatalf("adding SandboxConfig: %v", err)
		}
	}
	return listersv1alpha1.NewSandboxConfigLister(configIdx)
}

func testAssets() map[string]map[string]atev1alpha1.AssetFile {
	return map[string]map[string]atev1alpha1.AssetFile{
		"amd64": {"gvisor": {URL: "gs://bucket/gvisor.tar.bz2", SHA256: "abc"}},
	}
}

// TestResolveSandboxAssets pins the template-side resolution: the config the
// template names is resolved (with its class checked), an empty name or an
// unrecognized class is an error, and the pause image travels with the
// sandbox binaries.
func TestResolveSandboxAssets(t *testing.T) {
	const namedPause = "gcr.io/gke-release/pause@sha256:named"
	namedConfig := &atev1alpha1.SandboxConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "gvisor-custom"},
		Spec: atev1alpha1.SandboxConfigSpec{
			SandboxClass: atev1alpha1.SandboxClassGvisor,
			PauseImage:   namedPause,
			Assets:       testAssets(),
		},
	}

	tests := []struct {
		name           string
		sandbox        *ateapipb.SandboxConfig
		wantPauseImage string
		wantErr        string
	}{{
		name: "named config",
		sandbox: &ateapipb.SandboxConfig{
			SandboxClass: ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR,
			ConfigName:   "gvisor-custom",
		},
		wantPauseImage: namedPause,
	}, {
		name: "named config class mismatch",
		sandbox: &ateapipb.SandboxConfig{
			SandboxClass: ateapipb.SandboxClass_SANDBOX_CLASS_MICROVM,
			ConfigName:   "gvisor-custom",
		},
		wantErr: `has class "gvisor"`,
	}, {
		name: "missing named config",
		sandbox: &ateapipb.SandboxConfig{
			SandboxClass: ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR,
			ConfigName:   "does-not-exist",
		},
		wantErr: `SandboxConfig "does-not-exist" not found`,
	}, {
		name: "unrecognized sandbox class",
		sandbox: &ateapipb.SandboxConfig{
			SandboxClass: ateapipb.SandboxClass_SANDBOX_CLASS_UNSPECIFIED,
			ConfigName:   "gvisor-custom",
		},
		wantErr: "unrecognized sandbox_class",
	}, {
		name:    "empty config name",
		sandbox: &ateapipb.SandboxConfig{SandboxClass: ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR},
		wantErr: "names no sandbox_config.config_name",
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configLister := sandboxConfigListerFor(t, []*atev1alpha1.SandboxConfig{namedConfig})

			got, err := resolveSandboxAssets(configLister, tt.sandbox)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveSandboxAssets() error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveSandboxAssets() error: %v", err)
			}
			if got.GetPauseImage() != tt.wantPauseImage {
				t.Errorf("pause image = %q, want %q", got.GetPauseImage(), tt.wantPauseImage)
			}
		})
	}
}

// TestSandboxAssetsStatus proves the sandbox binaries an atelet reports reach
// ActorStatus.sandbox_assets sorted by name, and that a report beyond the
// status's validation bounds is left out rather than stored.
func TestSandboxAssetsStatus(t *testing.T) {
	file := func(url, sha string) *ateletpb.AssetFile { return &ateletpb.AssetFile{Url: url, Sha256: sha} }
	tooMany := &ateletpb.ArchAssets{Files: map[string]*ateletpb.AssetFile{}}
	for i := range maxStatusSandboxAssets + 1 {
		tooMany.Files[strings.Repeat("a", i+1)] = file("gs://b/a", "a")
	}
	for _, tc := range []struct {
		name     string
		reported *ateletpb.ArchAssets
		want     []*ateapipb.SandboxAsset
	}{
		{name: "nothing reported", reported: nil, want: nil},
		{
			name: "sorted by name",
			reported: &ateletpb.ArchAssets{Files: map[string]*ateletpb.AssetFile{
				"kata-shim":        file("gs://b/kata-shim", "2"),
				"cloud-hypervisor": file("gs://b/ch", "1"),
			}},
			want: []*ateapipb.SandboxAsset{
				{Name: "cloud-hypervisor", Url: "gs://b/ch", Sha256: "1"},
				{Name: "kata-shim", Url: "gs://b/kata-shim", Sha256: "2"},
			},
		},
		{name: "too many assets", reported: tooMany, want: nil},
		{
			name: "a URL beyond the bound",
			reported: &ateletpb.ArchAssets{Files: map[string]*ateletpb.AssetFile{
				"gvisor": file("gs://b/"+strings.Repeat("x", maxStatusSandboxURLLen), "a"),
			}},
			want: nil,
		},
		{
			name: "a sha256 beyond the bound",
			reported: &ateletpb.ArchAssets{Files: map[string]*ateletpb.AssetFile{
				"gvisor": file("gs://b/gvisor", strings.Repeat("a", maxStatusSandboxSHA256+1)),
			}},
			want: nil,
		},
		{
			name: "an asset without a URL",
			reported: &ateletpb.ArchAssets{Files: map[string]*ateletpb.AssetFile{
				"gvisor": file("", "a"),
			}},
			want: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := sandboxAssetsStatus(context.Background(), tc.reported)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("sandboxAssetsStatus() (-want +got):\n%s", diff)
			}
		})
	}
}
