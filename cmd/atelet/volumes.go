// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
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
	"log/slog"
	"os"
	"time"

	"go.ate.dev/substrate/cmd/atelet/internal/ateletpath"
	"go.ate.dev/substrate/internal/proto/ateletpb"
	"go.ate.dev/substrate/internal/volume"
	"go.ate.dev/substrate/internal/volume/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Atelet) mountExternalVolumes(ctx context.Context, actorUID string, volumes []*ateletpb.Volume) error {
	for _, vol := range volumes {
		// Given that external volumes has been in the codebase for a while and
		// its preview-ness is solely around the API, and that the impact of
		// disabling the gate while in use would be an orphaned volume
		// attachment, we are NOT checking the ExternalVolumes gate in the
		// mount/unmount logic.
		ext := vol.GetExternal()
		if ext == nil {
			continue
		}
		hostPath := ateletpath.ExternalVolumeHostPath(actorUID, vol.GetName())
		if err := os.MkdirAll(hostPath, 0o750); err != nil {
			return fmt.Errorf("failed to create mount point %q: %w", hostPath, err)
		}
		slog.InfoContext(ctx, "Mounting volume", slog.String("volume_id", ext.GetStorageVolumeId()), slog.String("host_path", hostPath), slog.String("volume_type", ext.GetVolumeType()))
		plugin, err := s.getPlugin(ctx, ext.GetVolumeType())
		if err != nil {
			return fmt.Errorf("failed to get volume plugin for %q: %w", ext.GetVolumeType(), err)
		}
		if err := plugin.MountVolume(ctx, volume.MountVolumeRequest{
			VolumeID:       ext.GetStorageVolumeId(),
			TargetPath:     hostPath,
			VolumeContext:  ext.GetVolumeContext(),
			PublishContext: ext.GetPublishContext(),
		}); err != nil {
			return fmt.Errorf("failed to mount volume %q to %q: %w", ext.GetStorageVolumeId(), hostPath, err)
		}
	}
	return nil
}

// unmountRetryWindow bounds how long unmountExternalVolumes retries a failed
// unmount. A target is busy while anything on the node still holds a path
// beneath it, which is often let go seconds later.
var unmountRetryWindow = time.Minute

// unmountRetryInterval is the first wait between two unmount attempts; it
// doubles up to unmountRetryMaxInterval.
var unmountRetryInterval = time.Second

const unmountRetryMaxInterval = 10 * time.Second

// unmountExternalVolumes unmounts the external volumes, retrying a failed
// unmount until unmountRetryWindow ends, and returns the failures that
// outlast it.
func (s *Atelet) unmountExternalVolumes(ctx context.Context, actorUID string, volumes []*ateletpb.Volume) error {
	deadline := time.Now().Add(unmountRetryWindow)
	var errs []error
	for _, vol := range volumes {
		// Given that external volumes has been in the codebase for a while and
		// its preview-ness is solely around the API, and that the impact of
		// disabling the gate while in use would be an orphaned volume
		// attachment, we are NOT checking the ExternalVolumes gate in the
		// mount/unmount logic.
		ext := vol.GetExternal()
		if ext == nil {
			continue
		}
		hostPath := ateletpath.ExternalVolumeHostPath(actorUID, vol.GetName())
		slog.InfoContext(ctx, "Unmounting volume", slog.String("volume_id", ext.GetStorageVolumeId()), slog.String("host_path", hostPath), slog.String("volume_type", ext.GetVolumeType()))
		// TODO: Standardize volume plugin lookup and error handling across control plane
		// and worker plane (e.g. via a shared helper).
		plugin, err := s.getPlugin(ctx, ext.GetVolumeType())
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to get volume plugin for %q (volume %q): %w", ext.GetVolumeType(), ext.GetStorageVolumeId(), err))
			continue
		}
		if err := unmountVolumeUntil(ctx, plugin, ext, hostPath, deadline); err != nil {
			errs = append(errs, fmt.Errorf("failed to unmount volume %q from %q: %w", ext.GetStorageVolumeId(), hostPath, err))
		}
	}
	return errors.Join(errs...)
}

// unmountVolumeUntil unmounts one external volume, retrying a failure with a
// doubling wait while the next attempt still starts before deadline and ctx
// lasts. A volume the plugin no longer finds counts as unmounted.
func unmountVolumeUntil(ctx context.Context, plugin volume.VolumePluginWorkerPlane, ext *ateletpb.ExternalVolumeSource, hostPath string, deadline time.Time) error {
	wait := unmountRetryInterval
	for {
		err := plugin.UnmountVolume(ctx, ext.GetStorageVolumeId(), hostPath)
		if err == nil {
			return nil
		}
		if status.Code(err) == codes.NotFound || errors.Is(err, os.ErrNotExist) {
			slog.WarnContext(ctx, "Volume not found during unmount, assuming already unmounted", slog.String("volume_id", ext.GetStorageVolumeId()), slog.Any("error", err))
			return nil
		}
		if time.Now().Add(wait).After(deadline) {
			return err
		}
		slog.WarnContext(ctx, "Unmounting volume failed, retrying", slog.String("volume_id", ext.GetStorageVolumeId()), slog.String("host_path", hostPath), slog.Duration("retry_in", wait), slog.Any("error", err))
		select {
		case <-ctx.Done():
			return err
		case <-time.After(wait):
		}
		wait = min(2*wait, unmountRetryMaxInterval)
	}
}

// unmountLeftoverVolumes unmounts the external volumes whose mount point
// outlived the actor's last checkpoint on this node, before an activation
// resets the actor's directories: a mount point left in place means its
// unmount had failed.
func (s *Atelet) unmountLeftoverVolumes(ctx context.Context, actorUID string, volumes []*ateletpb.Volume) error {
	var leftover []*ateletpb.Volume
	for _, vol := range volumes {
		if vol.GetExternal() == nil {
			continue
		}
		if _, err := os.Lstat(ateletpath.ExternalVolumeHostPath(actorUID, vol.GetName())); err == nil {
			leftover = append(leftover, vol)
		}
	}
	if len(leftover) == 0 {
		return nil
	}
	slog.WarnContext(ctx, "Unmounting volumes left mounted by an earlier checkpoint", slog.String("actor_uid", actorUID), slog.Int("volumes", len(leftover)))
	return s.unmountExternalVolumes(ctx, actorUID, leftover)
}

func (s *Atelet) getPlugin(ctx context.Context, driverName string) (volume.VolumePluginWorkerPlane, error) {
	s.mu.RLock()
	plugin, ok := s.volumePlugins[driverName]
	s.mu.RUnlock()
	if ok {
		return plugin, nil
	}

	csiPlugin, err := csi.NewCSIPlugin(ctx, s.csiDriverConfigGetter, driverName, false /*isController*/)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.volumePlugins[driverName] = csiPlugin
	s.mu.Unlock()
	return csiPlugin, nil
}
