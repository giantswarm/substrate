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
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/agent-substrate/substrate/internal/apierror"
	"github.com/agent-substrate/substrate/internal/proto/ateletpb"
	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	listersv1alpha1 "github.com/agent-substrate/substrate/pkg/client/listers/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
)

// resolveTemplateSandboxConfig resolves the SandboxConfig the ActorTemplate
// names via sandbox_config.config_name and checks that its class matches the
// template's sandbox_class.
func resolveTemplateSandboxConfig(
	sandboxConfigLister listersv1alpha1.SandboxConfigLister,
	templateSandbox *ateapipb.SandboxConfig,
) (*atev1alpha1.SandboxConfig, error) {
	name := templateSandbox.GetConfigName()
	sc, err := sandboxConfigLister.Get(name)
	if k8serrors.IsNotFound(err) {
		return nil, apierror.FailedPrecondition("SandboxConfig %q not found", name)
	}
	if err != nil {
		return nil, fmt.Errorf("while getting SandboxConfig %q: %w", name, err)
	}
	if class := sandboxClassString(templateSandbox.GetSandboxClass()); string(sc.Spec.SandboxClass) != class {
		return nil, apierror.FailedPrecondition(
			"SandboxConfig %q has class %q but sandbox_config.sandbox_class is %q", name, sc.Spec.SandboxClass, class)
	}
	return sc, nil
}

// resolveSandboxAssets determines the sandbox binaries and pause image an actor
// should boot with and projects them onto the ateletpb.SandboxAssets atelet
// fetches: the SandboxConfig the ActorTemplate names via
// sandbox_config.config_name (required; enforced by CreateActorTemplate),
// checked against the template's sandbox_class.
func resolveSandboxAssets(
	sandboxConfigLister listersv1alpha1.SandboxConfigLister,
	templateSandbox *ateapipb.SandboxConfig,
) (*ateletpb.SandboxAssets, error) {
	if sandboxClassString(templateSandbox.GetSandboxClass()) == "" {
		return nil, fmt.Errorf("ActorTemplate names unrecognized sandbox_class %v", templateSandbox.GetSandboxClass())
	}
	if templateSandbox.GetConfigName() == "" {
		return nil, fmt.Errorf("ActorTemplate names no sandbox_config.config_name")
	}

	sc, err := resolveTemplateSandboxConfig(sandboxConfigLister, templateSandbox)
	if err != nil {
		return nil, err
	}
	return sandboxAssetsProto(sc), nil
}

// sandboxAssetsProto converts a resolved SandboxConfig into the proto atelet
// consumes.
func sandboxAssetsProto(sc *atev1alpha1.SandboxConfig) *ateletpb.SandboxAssets {
	out := &ateletpb.SandboxAssets{
		SandboxClass: string(sc.Spec.SandboxClass),
		PauseImage:   sc.Spec.PauseImage,
		Assets:       make(map[string]*ateletpb.ArchAssets, len(sc.Spec.Assets)),
	}
	for arch, files := range sc.Spec.Assets {
		archAssets := &ateletpb.ArchAssets{Files: make(map[string]*ateletpb.AssetFile, len(files))}
		for name, f := range files {
			archAssets.Files[name] = &ateletpb.AssetFile{Url: f.URL, Sha256: f.SHA256}
		}
		out.Assets[arch] = archAssets
	}
	return out
}

// Bounds of ActorStatus.sandbox_assets, as its validation tags state them.
const (
	maxStatusSandboxAssets   = 16
	maxStatusSandboxAssetLen = 253
	maxStatusSandboxURLLen   = 4096
	maxStatusSandboxSHA256   = 64
)

// sandboxAssetsStatus converts the sandbox binaries an atelet reports it runs
// an actor on into ActorStatus.sandbox_assets, sorted by name. A report that
// does not fit the status's bounds is logged and left out, so it never makes
// the actor's later updates fail validation; an atelet that reports nothing
// yields none.
func sandboxAssetsStatus(ctx context.Context, reported *ateletpb.ArchAssets) []*ateapipb.SandboxAsset {
	files := reported.GetFiles()
	if len(files) == 0 {
		return nil
	}
	if len(files) > maxStatusSandboxAssets {
		slog.WarnContext(ctx, "Atelet reported more sandbox assets than the actor status holds; not recording them", slog.Int("assets", len(files)))
		return nil
	}
	out := make([]*ateapipb.SandboxAsset, 0, len(files))
	for name, f := range files {
		if name == "" || len(name) > maxStatusSandboxAssetLen || f.GetUrl() == "" || len(f.GetUrl()) > maxStatusSandboxURLLen ||
			f.GetSha256() == "" || len(f.GetSha256()) > maxStatusSandboxSHA256 {
			slog.WarnContext(ctx, "Atelet reported a sandbox asset the actor status cannot hold; not recording the assets", slog.String("asset", name))
			return nil
		}
		out = append(out, &ateapipb.SandboxAsset{Name: name, Url: f.GetUrl(), Sha256: f.GetSha256()})
	}
	slices.SortFunc(out, func(a, b *ateapipb.SandboxAsset) int { return cmp.Compare(a.GetName(), b.GetName()) })
	return out
}
