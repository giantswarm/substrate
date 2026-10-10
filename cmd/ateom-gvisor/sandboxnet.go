//go:build linux

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
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/agent-substrate/substrate/internal/ateomnet"
	"github.com/agent-substrate/substrate/internal/ateomnet/dns"
	"github.com/agent-substrate/substrate/internal/imagecache"
	"github.com/agent-substrate/substrate/internal/ocispec"
	"github.com/agent-substrate/substrate/internal/proto/ateompb"
)

// actorResolvConf writes the resolver bind source outside the actor's rootfs.
func actorResolvConf(actorDirs *ateompb.ActorDirs) (string, error) {
	pod, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return "", fmt.Errorf("reading the worker pod resolv.conf: %w", err)
	}
	path := resolvConfPath(actorDirs)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("creating the actor directory: %w", err)
	}
	if err := os.WriteFile(path, dns.SandboxResolvConf(ateomnet.ActorVethGateway, pod), 0o644); err != nil {
		return "", fmt.Errorf("writing the actor resolv.conf: %w", err)
	}
	return path, nil
}

// removeActorResolvConf drops the file with the actor's network; atelet's
// per-activation reset clears directories under the actor's path, not files.
func removeActorResolvConf(ctx context.Context, path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.WarnContext(ctx, "Failed to remove the actor resolv.conf", slog.Any("err", err))
	}
}

// composeActorRootfs composes an application container's bundle rootfs and
// names the actor's hostname in its /etc/hosts. The file lands in the bundle's
// private upper, so the OCI spec's mounts stay those of every existing
// snapshot (runsc restore refuses a mount set that differs).
func composeActorRootfs(bundle string) error {
	if err := imagecache.SetupBundleRootfs(bundle); err != nil {
		return err
	}
	if err := dns.WriteRootfsHosts(filepath.Join(bundle, "rootfs"), ocispec.Hostname); err != nil {
		return fmt.Errorf("while writing the actor hosts: %w", err)
	}
	return nil
}
