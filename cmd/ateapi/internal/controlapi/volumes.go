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

package controlapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/volume"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	storagev1listers "k8s.io/client-go/listers/storage/v1"
)

// initialActorVolumes constructs initial volume objects in PENDING state before volume creation.
// A seeded external volume is included only when the actor names a seed for it.
func initialActorVolumes(ctx context.Context, scLister storagev1listers.StorageClassLister, template *ateapipb.ActorTemplate, seeds []*ateapipb.VolumeSeed) ([]*ateapipb.ExternalVolume, error) {
	if template == nil {
		return nil, status.Error(codes.InvalidArgument, "template is required")
	}
	var volumes []*ateapipb.ExternalVolume
	for _, vol := range template.GetVolumes() {
		evt := vol.GetExternalVolumeTemplate()
		if evt == nil {
			continue
		}
		if evt.GetSeeded() && volumeSeed(seeds, vol.GetName()) == nil {
			continue
		}
		scName := evt.GetStorageClassName()
		sc, err := scLister.Get(scName)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return nil, status.Errorf(codes.FailedPrecondition, "StorageClass %q not found", scName)
			}
			return nil, status.Errorf(codes.Internal, "failed to get StorageClass %q: %v", scName, err)
		}

		volumes = append(volumes, &ateapipb.ExternalVolume{
			VolumeName: vol.GetName(),
			VolumeType: sc.Provisioner,
			Status:     ateapipb.ExternalVolume_STATUS_PENDING,
		})
	}
	return volumes, nil
}

// volumeSeed returns the seed naming volumeName, or nil.
func volumeSeed(seeds []*ateapipb.VolumeSeed, volumeName string) *ateapipb.VolumeSeed {
	for _, seed := range seeds {
		if seed.GetVolumeName() == volumeName {
			return seed
		}
	}
	return nil
}

// validateVolumeSeeds checks a CreateActor's seeds against the template and
// the storage system, so that a seed that cannot be restored is refused at
// create rather than at the actor's first resume: each seed must name a
// seeded external volume of the template, its driver must be the volume's
// StorageClass provisioner and registered, and the driver must hold the
// snapshot, ready to use.
func validateVolumeSeeds(ctx context.Context, registry VolumePluginRegistry, scLister storagev1listers.StorageClassLister, template *ateapipb.ActorTemplate, seeds []*ateapipb.VolumeSeed) error {
	for _, seed := range seeds {
		volName := seed.GetVolumeName()
		var evt *ateapipb.ExternalVolumeTemplate
		for _, vol := range template.GetVolumes() {
			if vol.GetName() == volName {
				evt = vol.GetExternalVolumeTemplate()
				break
			}
		}
		if !evt.GetSeeded() {
			return status.Errorf(codes.InvalidArgument, "seed for volume %q: ActorTemplate has no seeded external volume %q", volName, volName)
		}

		scName := evt.GetStorageClassName()
		sc, err := scLister.Get(scName)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return status.Errorf(codes.FailedPrecondition, "seed for volume %q: StorageClass %q not found", volName, scName)
			}
			return status.Errorf(codes.Internal, "seed for volume %q: failed to get StorageClass %q: %v", volName, scName, err)
		}
		if seed.GetDriver() != sc.Provisioner {
			return status.Errorf(codes.InvalidArgument, "seed for volume %q names driver %q, but the volume's StorageClass %q provisions with %q", volName, seed.GetDriver(), scName, sc.Provisioner)
		}
		plugin, err := registry.GetPlugin(ctx, seed.GetDriver())
		if err != nil {
			return status.Errorf(codes.FailedPrecondition, "seed for volume %q: unknown driver %q: %v", volName, seed.GetDriver(), err)
		}

		handle := seed.GetSnapshotHandle()
		snap, found, err := plugin.GetSnapshot(ctx, handle)
		switch {
		case status.Code(err) == codes.Unimplemented:
			return status.Errorf(codes.FailedPrecondition, "seed for volume %q: driver %q cannot list snapshots, so snapshot %q cannot be checked: %v", volName, seed.GetDriver(), handle, err)
		case err != nil:
			return status.Errorf(codes.Unavailable, "seed for volume %q: looking up snapshot %q in driver %q: %v", volName, handle, seed.GetDriver(), err)
		case !found:
			return status.Errorf(codes.FailedPrecondition, "seed for volume %q: driver %q holds no snapshot %q", volName, seed.GetDriver(), handle)
		case !snap.ReadyToUse:
			return status.Errorf(codes.FailedPrecondition, "seed for volume %q: snapshot %q in driver %q is not ready to use", volName, handle, seed.GetDriver())
		}
	}
	return nil
}

// createActorVolumes provisions external volumes specified in volumesToCreate using the provided volume plugin.
// A volume with a seed in seeds is restored from the seed's snapshot.
// It returns the list of external volumes (with updated status and storage IDs), or an error if any creation fails.
// Any volumes processed before or during a failure are returned alongside the error so they can be persisted on the actor.
func createActorVolumes(ctx context.Context, registry VolumePluginRegistry, scLister storagev1listers.StorageClassLister, actorUID string, template *ateapipb.ActorTemplate, seeds []*ateapipb.VolumeSeed, volumesToCreate []*ateapipb.ExternalVolume) (resultVolumes []*ateapipb.ExternalVolume, err error) {
	resultVolumes = make([]*ateapipb.ExternalVolume, 0, len(volumesToCreate))

	var currentIdx int
	defer func() {
		if err != nil {
			// If we encounter an error, append the rest of the volumes to the result with the last
			// known state. This allows the caller to persist the state of the volumes.
			resultVolumes = append(resultVolumes, volumesToCreate[currentIdx:]...)
		}
	}()

	for idx, vol := range volumesToCreate {
		currentIdx = idx

		var specVol *ateapipb.Volume
		volName := vol.GetVolumeName()
		for _, tVol := range template.GetVolumes() {
			if tVol.GetName() == volName {
				specVol = tVol
				break
			}
		}
		if specVol == nil || specVol.GetExternalVolumeTemplate() == nil {
			return resultVolumes, status.Errorf(codes.NotFound, "volume %q not found in template", volName)
		}

		switch vol.GetStatus() {
		case ateapipb.ExternalVolume_STATUS_PENDING:
			// proceed with volume creation
		case ateapipb.ExternalVolume_STATUS_CREATED:
			resultVolumes = append(resultVolumes, vol)
			continue
		case ateapipb.ExternalVolume_STATUS_DELETING:
			return resultVolumes, status.Errorf(codes.FailedPrecondition, "cannot create volume %q in DELETING status", volName)
		default:
			return resultVolumes, status.Errorf(codes.Internal, "unexpected status %s for volume %q", vol.GetStatus(), volName)
		}

		actVolID := actorVolumeID(actorUID, volName)

		scName := specVol.GetExternalVolumeTemplate().GetStorageClassName()
		sc, err := scLister.Get(scName)
		if err != nil {
			return resultVolumes, status.Errorf(codes.Internal, "failed to get StorageClass %q: %v", scName, err)
		}

		if sc.Provisioner != vol.GetVolumeType() {
			return resultVolumes, status.Errorf(codes.FailedPrecondition, "volume %q has mismatched type %q (expected %q from StorageClass %q)", volName, vol.GetVolumeType(), sc.Provisioner, scName)
		}

		var sourceSnapshotID string
		if specVol.GetExternalVolumeTemplate().GetSeeded() {
			seed := volumeSeed(seeds, volName)
			if seed == nil {
				return resultVolumes, status.Errorf(codes.FailedPrecondition, "seeded volume %q has no seed on the actor", volName)
			}
			sourceSnapshotID = seed.GetSnapshotHandle()
		}

		plugin, err := registry.GetPlugin(ctx, vol.GetVolumeType())
		if err != nil {
			return resultVolumes, status.Errorf(codes.FailedPrecondition, "failed to get volume plugin for driver %q (StorageClass %q): %v", sc.Provisioner, scName, err)
		}

		resp, volErr := plugin.CreateVolume(ctx, volume.CreateVolumeRequest{
			Name:             actVolID,
			Capacity:         specVol.GetExternalVolumeTemplate().GetCapacity(),
			Parameters:       sc.Parameters,
			DriverName:       sc.Provisioner,
			SourceSnapshotID: sourceSnapshotID,
		})
		if volErr != nil {
			return resultVolumes, status.Errorf(codes.Internal, "failed to create volume %q: %v", specVol.GetName(), volErr)
		}
		if resp.ContentSourceSnapshotID != sourceSnapshotID {
			// The driver answered with a volume that is not the requested
			// restore. Release it, so the retry provisions afresh instead of
			// mounting it, and fail rather than hand the actor the wrong data.
			if delErr := plugin.DeleteVolume(ctx, resp.VolumeID); delErr != nil {
				slog.WarnContext(ctx, "Failed to delete a volume the driver did not restore as requested", slog.String("volume_id", resp.VolumeID), slog.Any("error", delErr))
			}
			return resultVolumes, status.Errorf(codes.FailedPrecondition, "driver %q created volume %q from snapshot %q, want %q", sc.Provisioner, volName, resp.ContentSourceSnapshotID, sourceSnapshotID)
		}

		resultVolumes = append(resultVolumes, &ateapipb.ExternalVolume{
			VolumeName:      volName,
			StorageVolumeId: resp.VolumeID,
			VolumeType:      sc.Provisioner,
			Status:          ateapipb.ExternalVolume_STATUS_CREATED,
			VolumeContext:   resp.VolumeContext,
		})
	}
	return resultVolumes, nil
}

// deleteActorVolumes deletes all external volumes in the list.
func deleteActorVolumes(ctx context.Context, registry VolumePluginRegistry, actorUID string, volumes []*ateapipb.ExternalVolume) error {
	if actorUID == "" {
		return errors.New("actorUID is required")
	}
	var errs []error
	for _, vol := range volumes {
		volID := vol.GetStorageVolumeId()
		if volID == "" {
			// If the volume hasn't been successfully created yet, it's possible
			// that it doesn't have a storage volume ID. In that case, fallback
			// to the original requested volID.
			volID = actorVolumeID(actorUID, vol.GetVolumeName())
		}
		// TODO: Standardize volume plugin lookup and error handling across control plane
		// and worker plane (e.g. via a shared helper).
		plugin, err := registry.GetPlugin(ctx, vol.GetVolumeType())
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to get volume plugin for %q: %w", vol.GetVolumeType(), err))
			continue
		}
		if err := plugin.DeleteVolume(ctx, volID); err != nil {
			if status.Code(err) == codes.NotFound {
				slog.WarnContext(ctx, "Volume not found during delete, assuming already deleted", slog.String("volume_id", volID))
				continue
			}
			errs = append(errs, fmt.Errorf("failed to delete volume %q: %w", volID, err))
		}
	}
	return errors.Join(errs...)
}

// getMountedActorVolumes filters the actor's volumes and returns only those that are declared and mounted in the ActorTemplate.
func getMountedActorVolumes(ctx context.Context, ref *ateapipb.ObjectRef, volumes []*ateapipb.ExternalVolume, template *ateapipb.ActorTemplate) []*ateapipb.ExternalVolume {
	var mounted []*ateapipb.ExternalVolume
	for _, vol := range volumes {
		// Find the corresponding volume in the ActorTemplate to check if it's mounted
		var matchedTemplateVol *ateapipb.Volume
		for _, tVol := range template.GetVolumes() {
			if vol.GetVolumeName() == tVol.GetName() {
				matchedTemplateVol = tVol
				break
			}
		}

		if matchedTemplateVol == nil {
			slog.WarnContext(ctx, "Volume not found in template, skipping", slog.String("volume_id", vol.GetStorageVolumeId()))
			continue
		}

		if !isVolumeMounted(matchedTemplateVol.GetName(), template) {
			slog.InfoContext(ctx, "Volume not mounted in template, skipping", slog.String("volume_id", vol.GetStorageVolumeId()))
			continue
		}
		mounted = append(mounted, vol)
	}
	return mounted
}

func actorVolumeID(actorUID string, volumeName string) string {
	return fmt.Sprintf("substrate-%s-%s", actorUID, volumeName)
}

// detachActorVolumes detaches all mounted external volumes for an actor from its worker node.
func detachActorVolumes(ctx context.Context, st detachActorVolumesStore, registry VolumePluginRegistry, actor *ateapipb.Actor, template *ateapipb.ActorTemplate, action string) error {
	assignment := actor.GetStatus().GetWorkerAssignment()
	if assignment == nil {
		slog.WarnContext(ctx, fmt.Sprintf("Actor has no assigned worker pod during %s, skipping detach volumes", action), slog.String("actor_id", actor.GetMetadata().GetName()))
		return nil
	}

	worker, err := st.GetWorker(ctx, assignment.GetWorker().GetName())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			slog.WarnContext(ctx, fmt.Sprintf("Worker not found in store during %s, skipping detach volumes", action), slog.String("actor_id", actor.GetMetadata().GetName()))
			return nil
		}
		return fmt.Errorf("failed to get worker: %w", err)
	}

	node := worker.GetNodeName()
	if node == "" {
		slog.WarnContext(ctx, fmt.Sprintf("Worker has no assigned node name during %s, skipping detach volumes", action), slog.String("actor_id", actor.GetMetadata().GetName()))
		return nil
	}

	ref := &ateapipb.ObjectRef{Atespace: actor.GetMetadata().GetAtespace(), Name: actor.GetMetadata().GetName()}
	// If the template is available, only detach volumes that are actively mounted
	// in the template's containers. If the template is missing/deleted, fall back to
	// attempting detachment for all external volumes recorded on the actor so we do
	// not orphan attached disks on the worker node.
	volumesToDetach := actor.GetStatus().GetActorVolumes()
	if template != nil {
		volumesToDetach = getMountedActorVolumes(ctx, ref, actor.GetStatus().GetActorVolumes(), template)
	}
	// Collect errors for all volumes to detach, but continue processing so we attempt to detach all volumes.
	var errs []error
	for _, vol := range volumesToDetach {
		// StorageVolumeId is only populated once the volume is provisioned.
		// Skip volumes that were never created (e.g. failed during PENDING state).
		if vol.GetStorageVolumeId() == "" {
			slog.WarnContext(ctx, "Volume has no storage volume ID, skipping detach", slog.String("volume_name", vol.GetVolumeName()), slog.String("actor_id", actor.GetMetadata().GetName()))
			continue
		}
		slog.InfoContext(ctx, "Detaching volume from node", slog.String("volume_id", vol.GetStorageVolumeId()), slog.String("node", node))
		plugin, err := registry.GetPlugin(ctx, vol.GetVolumeType())
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to get volume plugin for %q: %w", vol.GetVolumeType(), err))
			continue
		}
		if err := plugin.DetachVolume(ctx, vol.GetStorageVolumeId(), node); err != nil {
			if status.Code(err) == codes.NotFound {
				slog.WarnContext(ctx, "Volume not found during detach, assuming already detached", slog.String("volume_id", vol.GetStorageVolumeId()), slog.String("node", node))
				continue
			}
			errs = append(errs, fmt.Errorf("failed to detach volume %q from node %q: %w", vol.GetStorageVolumeId(), node, err))
		}
	}
	return errors.Join(errs...)
}

// detachActorVolumesStore enumerates the subset of store methods needed to
// detach actor volumes.
type detachActorVolumesStore interface {
	GetWorker(ctx context.Context, name string) (*ateapipb.Worker, error)
}
