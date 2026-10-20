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
	"path/filepath"
	"time"

	"github.com/agent-substrate/substrate/cmd/atelet/internal/ateletpath"
	"github.com/agent-substrate/substrate/internal/proto/ateletpb"
	"github.com/agent-substrate/substrate/internal/volume"
	"github.com/agent-substrate/substrate/internal/volume/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func (s *AteomHerder) mountExternalVolumes(ctx context.Context, actorUID string, spec *ateletpb.WorkloadSpec) error {
	for _, vol := range spec.GetVolumes() {
		ext := vol.GetExternal()
		if ext == nil {
			continue
		}
		hostPath := ateletpath.VolumeHostPath(actorUID, vol.GetName())
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
			Mode:           accessMode(ext),
			ReadOnly:       ext.GetReadOnly(),
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

// unmountExternalVolumes unmounts the spec's external volumes, retrying a
// failed unmount until unmountRetryWindow ends, and returns the failures that
// outlast it.
func (s *AteomHerder) unmountExternalVolumes(ctx context.Context, actorUID string, spec *ateletpb.WorkloadSpec) error {
	deadline := time.Now().Add(unmountRetryWindow)
	var errs []error
	for _, vol := range spec.GetVolumes() {
		ext := vol.GetExternal()
		if ext == nil {
			continue
		}
		hostPath := ateletpath.VolumeHostPath(actorUID, vol.GetName())
		slog.InfoContext(ctx, "Unmounting volume", slog.String("volume_id", ext.GetStorageVolumeId()), slog.String("host_path", hostPath), slog.String("volume_type", ext.GetVolumeType()))
		// TODO: Standardize volume plugin lookup and error handling across control plane
		// and worker plane (e.g. via a shared helper).
		plugin, err := s.getPlugin(ctx, ext.GetVolumeType())
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to get volume plugin for %q (volume %q): %w", ext.GetVolumeType(), ext.GetStorageVolumeId(), err))
			continue
		}
		if err := unmountVolumeUntil(ctx, plugin, ext, hostPath, deadline); err != nil {
			errs = append(errs, &volumeUnmountError{volumeID: ext.GetStorageVolumeId(), hostPath: hostPath, shared: accessMode(ext) != volume.ReadWriteOnce, err: err})
		}
	}
	return errors.Join(errs...)
}

// volumeUnmountError is an external volume that stayed mounted.
type volumeUnmountError struct {
	volumeID, hostPath string
	// shared is set for an access mode other nodes may mount the volume in
	// meanwhile, so detaching it from this node is no step of its own.
	shared bool
	err    error
}

func (e *volumeUnmountError) Error() string {
	return fmt.Sprintf("failed to unmount volume %q from %q: %v", e.volumeID, e.hostPath, e.err)
}

func (e *volumeUnmountError) Unwrap() error { return e.err }

// onlySharedVolumesStayMounted reports whether every failure of
// unmountExternalVolumes in err is a shared volume that stayed mounted.
func onlySharedVolumesStayMounted(err error) bool {
	errs := []error{err}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		errs = joined.Unwrap()
	}
	for _, e := range errs {
		var v *volumeUnmountError
		if !errors.As(e, &v) || !v.shared {
			return false
		}
	}
	return len(errs) > 0
}

// unmountVolumeUntil unmounts one external volume, retrying a failure with a
// doubling wait while the next attempt still starts before deadline and ctx
// lasts. A volume the plugin no longer finds counts as unmounted.
func unmountVolumeUntil(ctx context.Context, plugin volume.VolumePluginWorkerPlane, ext *ateletpb.ExternalVolumeSource, hostPath string, deadline time.Time) error {
	wait := unmountRetryInterval
	for {
		err := plugin.UnmountVolume(ctx, ext.GetStorageVolumeId(), hostPath, accessMode(ext))
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

// unmountLeftoverVolumes unmounts the spec's external volumes whose mount
// point outlived the actor's last checkpoint on this node, before an
// activation resets the actor's directories: a mount point left in place
// means its unmount had failed.
func (s *AteomHerder) unmountLeftoverVolumes(ctx context.Context, actorUID string, spec *ateletpb.WorkloadSpec) error {
	s.stopLeftoverSweep(actorUID)
	var leftover []*ateletpb.Volume
	for _, vol := range spec.GetVolumes() {
		if vol.GetExternal() == nil {
			continue
		}
		if _, err := os.Lstat(ateletpath.VolumeHostPath(actorUID, vol.GetName())); err == nil {
			leftover = append(leftover, vol)
		}
	}
	if len(leftover) == 0 {
		return nil
	}
	slog.WarnContext(ctx, "Unmounting volumes left mounted by an earlier checkpoint", slog.String("actor_uid", actorUID), slog.Int("volumes", len(leftover)))
	if err := s.unmountExternalVolumes(ctx, actorUID, &ateletpb.WorkloadSpec{Volumes: leftover}); err != nil {
		return err
	}
	if err := os.Remove(leftoverVolumesPath(actorUID)); err != nil && !os.IsNotExist(err) {
		return wrapFileSystemErr("while removing the record of leftover volumes", err)
	}
	return nil
}

// leftoverSweepInterval is the first wait of a sweep of volumes a checkpoint
// left mounted; it doubles up to leftoverSweepMaxInterval.
var leftoverSweepInterval = time.Minute

const leftoverSweepMaxInterval = 15 * time.Minute

// leftoverVolumesFile names the record of the volumes a checkpoint left
// mounted, in the actor's directory, so a restarted atelet sweeps them again.
const leftoverVolumesFile = "leftover-volumes.pb"

func leftoverVolumesPath(actorUID string) string {
	return filepath.Join(ateletpath.ActorPath(actorUID), leftoverVolumesFile)
}

// leftoverSweep is the background sweep of one actor's leftover volumes.
type leftoverSweep struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// leftoverVolumes returns the spec's external volumes whose unmount failed in
// err, an error of unmountExternalVolumes.
func leftoverVolumes(actorUID string, spec *ateletpb.WorkloadSpec, err error) []*ateletpb.Volume {
	errs := []error{err}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		errs = joined.Unwrap()
	}
	failed := map[string]bool{}
	for _, e := range errs {
		var v *volumeUnmountError
		if errors.As(e, &v) {
			failed[v.hostPath] = true
		}
	}
	var leftover []*ateletpb.Volume
	for _, vol := range spec.GetVolumes() {
		if vol.GetExternal() != nil && failed[ateletpath.VolumeHostPath(actorUID, vol.GetName())] {
			leftover = append(leftover, vol)
		}
	}
	return leftover
}

// sweepLeftoverVolumes records the volumes a checkpoint left mounted and
// keeps unmounting them in the background, with a doubling wait between the
// attempts, until they let go; then it removes their mount points and the
// record. The actor need not come back to this node: a suspended actor is
// deleted without a terminate here. Its next activation or terminate on this
// node stops the sweep first.
func (s *AteomHerder) sweepLeftoverVolumes(ctx context.Context, actorUID string, volumes []*ateletpb.Volume) error {
	spec := &ateletpb.WorkloadSpec{Volumes: volumes}
	record, err := proto.Marshal(spec)
	if err != nil {
		return fmt.Errorf("while encoding the leftover volumes: %w", err)
	}
	if err := os.WriteFile(leftoverVolumesPath(actorUID), record, 0o600); err != nil {
		return wrapFileSystemErr("while recording the leftover volumes", err)
	}
	s.startLeftoverSweep(ctx, actorUID, spec)
	return nil
}

func (s *AteomHerder) startLeftoverSweep(ctx context.Context, actorUID string, spec *ateletpb.WorkloadSpec) {
	s.stopLeftoverSweep(actorUID)
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	sweep := &leftoverSweep{cancel: cancel, done: make(chan struct{})}
	s.sweepMu.Lock()
	if s.leftoverSweeps == nil {
		s.leftoverSweeps = map[string]*leftoverSweep{}
	}
	s.leftoverSweeps[actorUID] = sweep
	s.sweepMu.Unlock()

	go func() {
		defer close(sweep.done)
		defer s.forgetLeftoverSweep(actorUID, sweep)
		wait := leftoverSweepInterval
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			err := s.unmountExternalVolumes(ctx, actorUID, spec)
			if ctx.Err() != nil {
				return
			}
			if err == nil {
				removeLeftoverMountPoints(ctx, actorUID, spec)
				slog.InfoContext(ctx, "Unmounted volumes left mounted by a checkpoint", slog.String("actor_uid", actorUID), slog.Int("volumes", len(spec.GetVolumes())))
				return
			}
			wait = min(2*wait, leftoverSweepMaxInterval)
			slog.WarnContext(ctx, "Volumes left mounted by a checkpoint still do not unmount", slog.String("actor_uid", actorUID), slog.Duration("retry_in", wait), slog.Any("err", err))
		}
	}()
}

// stopLeftoverSweep stops the actor's sweep, if one runs, and waits for it.
func (s *AteomHerder) stopLeftoverSweep(actorUID string) {
	s.sweepMu.Lock()
	sweep := s.leftoverSweeps[actorUID]
	delete(s.leftoverSweeps, actorUID)
	s.sweepMu.Unlock()
	if sweep != nil {
		sweep.cancel()
		<-sweep.done
	}
}

func (s *AteomHerder) forgetLeftoverSweep(actorUID string, sweep *leftoverSweep) {
	s.sweepMu.Lock()
	defer s.sweepMu.Unlock()
	if s.leftoverSweeps[actorUID] == sweep {
		delete(s.leftoverSweeps, actorUID)
	}
}

// removeLeftoverMountPoints removes the mount points of unmounted leftover
// volumes and their record.
func removeLeftoverMountPoints(ctx context.Context, actorUID string, spec *ateletpb.WorkloadSpec) {
	for _, vol := range spec.GetVolumes() {
		if err := os.Remove(ateletpath.VolumeHostPath(actorUID, vol.GetName())); err != nil && !os.IsNotExist(err) {
			slog.WarnContext(ctx, "Failed to remove the mount point of an unmounted leftover volume", slog.String("actor_uid", actorUID), slog.Any("err", err))
		}
	}
	if err := os.Remove(leftoverVolumesPath(actorUID)); err != nil && !os.IsNotExist(err) {
		slog.WarnContext(ctx, "Failed to remove the record of leftover volumes", slog.String("actor_uid", actorUID), slog.Any("err", err))
	}
}

// resumeLeftoverSweeps starts a sweep for every record of leftover volumes on
// the node, as an earlier atelet left them.
func (s *AteomHerder) resumeLeftoverSweeps(ctx context.Context) {
	records, err := filepath.Glob(leftoverVolumesPath("*"))
	if err != nil {
		slog.WarnContext(ctx, "Failed to list records of leftover volumes", slog.Any("err", err))
		return
	}
	for _, path := range records {
		actorUID := filepath.Base(filepath.Dir(path))
		record, err := os.ReadFile(path)
		if err != nil {
			slog.WarnContext(ctx, "Failed to read a record of leftover volumes", slog.String("actor_uid", actorUID), slog.Any("err", err))
			continue
		}
		spec := &ateletpb.WorkloadSpec{}
		if err := proto.Unmarshal(record, spec); err != nil {
			slog.WarnContext(ctx, "Failed to decode a record of leftover volumes", slog.String("actor_uid", actorUID), slog.Any("err", err))
			continue
		}
		slog.InfoContext(ctx, "Sweeping volumes an earlier checkpoint left mounted", slog.String("actor_uid", actorUID), slog.Int("volumes", len(spec.GetVolumes())))
		s.startLeftoverSweep(ctx, actorUID, spec)
	}
}

// accessMode maps an external volume's access mode to the volume plugins'.
func accessMode(ext *ateletpb.ExternalVolumeSource) volume.AccessMode {
	switch ext.GetAccessMode() {
	case ateletpb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_ONLY_MANY:
		return volume.ReadOnlyMany
	case ateletpb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_MANY:
		return volume.ReadWriteMany
	default:
		return volume.ReadWriteOnce
	}
}

func (s *AteomHerder) getPlugin(ctx context.Context, driverName string) (volume.VolumePluginWorkerPlane, error) {
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
