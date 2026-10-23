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

package credentialprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
)

// DockerConfigKeychain resolves registry credentials from .dockerconfigjson
// files, the content of a kubernetes.io/dockerconfigjson Secret, matched to an
// image like the kubelet matches a Pod's imagePullSecrets. Every lookup reads
// the files again, so a rotated Secret mounted into the pod takes effect
// without a restart. It is safe for concurrent use.
type DockerConfigKeychain struct {
	paths []string
}

var (
	_ authn.Keychain        = (*DockerConfigKeychain)(nil)
	_ authn.ContextKeychain = (*DockerConfigKeychain)(nil)
)

// NewDockerConfig returns a keychain over the .dockerconfigjson files at
// paths, tried in order. Each file must parse now, so a misconfigured mount
// fails at start-up instead of at the first private pull.
func NewDockerConfig(paths []string) (*DockerConfigKeychain, error) {
	for _, path := range paths {
		if _, err := readDockerConfig(path); err != nil {
			return nil, err
		}
	}
	slog.Info("Loaded image pull secrets", slog.Any("files", paths))
	return &DockerConfigKeychain{paths: paths}, nil
}

// Resolve implements authn.Keychain.
func (k *DockerConfigKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	return k.ResolveContext(context.Background(), target)
}

// ResolveContext implements authn.ContextKeychain. It returns the first
// candidate, or authn.Anonymous if there are none.
func (k *DockerConfigKeychain) ResolveContext(ctx context.Context, target authn.Resource) (authn.Authenticator, error) {
	return firstCandidate(k.Candidates(ctx, target))
}

// Candidates returns the credentials of every file entry matching target, in
// the order to try them: most specific registry key first, then file order.
// A file that cannot be read any more is logged and skipped.
func (k *DockerConfigKeychain) Candidates(ctx context.Context, target authn.Resource) ([]authn.Authenticator, error) {
	image := target.String()
	byKey := make(map[string][]authn.AuthConfig)
	for _, path := range k.paths {
		auths, err := readDockerConfig(path)
		if err != nil {
			slog.WarnContext(ctx, "Image pull secret unreadable; skipping it",
				slog.String("file", path), slog.Any("err", err))
			continue
		}
		keys, err := matchingAuthKeys(auths, image)
		if err != nil {
			slog.WarnContext(ctx, "Image pull secret has an invalid registry key; skipping it",
				slog.String("file", path), slog.String("image", image), slog.Any("err", err))
			continue
		}
		if len(keys) > 0 {
			slog.InfoContext(ctx, "Resolved image credentials from image pull secret",
				slog.String("file", path), slog.String("image", image))
		}
		for _, key := range keys {
			byKey[key] = append(byKey[key], auths[key]...)
		}
	}

	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sortAuthKeys(keys)
	var candidates []authn.Authenticator
	seen := make(map[authn.AuthConfig]struct{})
	for _, key := range keys {
		for _, auth := range byKey[key] {
			if _, dup := seen[auth]; dup {
				continue
			}
			seen[auth] = struct{}{}
			candidates = append(candidates, authn.FromConfig(auth))
		}
	}
	return candidates, nil
}

// readDockerConfig returns the auths of the .dockerconfigjson file at path,
// keyed by registry in the scheme-less form matchesImage compares. Keys that
// differ only in scheme or trailing slash share an entry, in key order.
func readDockerConfig(path string) (map[string][]authn.AuthConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("while reading image pull secret: %w", err)
	}
	var config struct {
		Auths map[string]authn.AuthConfig `json:"auths"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("while parsing image pull secret %s: %w", path, err)
	}
	auths := make(map[string][]authn.AuthConfig, len(config.Auths))
	for _, key := range slices.Sorted(maps.Keys(config.Auths)) {
		auth := config.Auths[key]
		// The encoded form is derived from Username and Password; dropping it
		// keeps equal credentials equal for deduplication.
		auth.Auth = ""
		normalized := normalizeRegistryKey(key)
		auths[normalized] = append(auths[normalized], auth)
	}
	return auths, nil
}

// normalizeRegistryKey strips a registry key's scheme and trailing slash and
// maps Docker Hub's aliases (the legacy "https://index.docker.io/v1/" that
// docker login writes among them) onto the name go-containerregistry gives
// its repositories, "index.docker.io".
func normalizeRegistryKey(key string) string {
	key = strings.TrimPrefix(strings.TrimPrefix(key, "https://"), "http://")
	key = strings.TrimSuffix(key, "/")
	switch key {
	case "docker.io", "index.docker.io/v1", "registry-1.docker.io":
		return "index.docker.io"
	}
	return key
}

// CandidateSource is a keychain that returns several credentials to try.
type CandidateSource interface {
	Candidates(ctx context.Context, target authn.Resource) ([]authn.Authenticator, error)
}

// Chain is a keychain that tries the candidates of its keychains in order.
type Chain []CandidateSource

var (
	_ authn.Keychain        = Chain(nil)
	_ authn.ContextKeychain = Chain(nil)
)

// Resolve implements authn.Keychain.
func (c Chain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	return c.ResolveContext(context.Background(), target)
}

// ResolveContext implements authn.ContextKeychain.
func (c Chain) ResolveContext(ctx context.Context, target authn.Resource) (authn.Authenticator, error) {
	return firstCandidate(c.Candidates(ctx, target))
}

// Candidates returns every keychain's candidates, the first keychain's first.
func (c Chain) Candidates(ctx context.Context, target authn.Resource) ([]authn.Authenticator, error) {
	var candidates []authn.Authenticator
	for _, kc := range c {
		got, err := kc.Candidates(ctx, target)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, got...)
	}
	return candidates, nil
}

func firstCandidate(candidates []authn.Authenticator, err error) (authn.Authenticator, error) {
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return authn.Anonymous, nil
	}
	return candidates[0], nil
}
