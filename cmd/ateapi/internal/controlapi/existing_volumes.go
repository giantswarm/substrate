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
	"fmt"
	"path"

	"github.com/agent-substrate/substrate/internal/proto/ateletpb"
	"github.com/agent-substrate/substrate/internal/volume"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

// existingVolume returns the actor's reference for the template's existing
// volume volumeName, or nil.
func existingVolume(actor *ateapipb.Actor, volumeName string) *ateapipb.ExistingVolume {
	for _, ev := range actor.GetExistingVolumes() {
		if ev.GetName() == volumeName {
			return ev
		}
	}
	return nil
}

// unsuppliedVolume reports whether volumeName is an existing volume of the
// template that the actor does not supply: such a volume contributes neither
// itself nor its mounts.
func unsuppliedVolume(template *ateapipb.ActorTemplate, actor *ateapipb.Actor, volumeName string) bool {
	for _, vol := range template.GetVolumes() {
		if vol.GetName() == volumeName {
			return vol.GetExistingVolume() != nil && existingVolume(actor, volumeName) == nil
		}
	}
	return false
}

// accessModeToVolume maps the API's access mode to the volume plugins'.
func accessModeToVolume(mode ateapipb.VolumeAccessMode) volume.AccessMode {
	switch mode {
	case ateapipb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_ONLY_MANY:
		return volume.ReadOnlyMany
	case ateapipb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_MANY:
		return volume.ReadWriteMany
	default:
		return volume.ReadWriteOnce
	}
}

// accessModeToAtelet maps the API's access mode to atelet's.
func accessModeToAtelet(mode ateapipb.VolumeAccessMode) ateletpb.VolumeAccessMode {
	return ateletpb.VolumeAccessMode(mode)
}

// pvAccessModes are the PersistentVolume access modes that permit each mode:
// a read-write-many volume may also be mounted read-only on many nodes.
var pvAccessModes = map[ateapipb.VolumeAccessMode][]corev1.PersistentVolumeAccessMode{
	ateapipb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_ONLY_MANY:  {corev1.ReadOnlyMany, corev1.ReadWriteMany},
	ateapipb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_MANY: {corev1.ReadWriteMany},
}

// findPersistentVolume returns the PersistentVolume that holds the CSI
// volume handle of driver, or nil.
func findPersistentVolume(pvLister corev1listers.PersistentVolumeLister, driver, handle string) (*corev1.PersistentVolume, error) {
	pvs, err := pvLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}
	for _, pv := range pvs {
		if csi := pv.Spec.CSI; csi != nil && csi.Driver == driver && csi.VolumeHandle == handle {
			return pv, nil
		}
	}
	return nil, nil
}

// resolveExistingVolume finds the PersistentVolume of an actor's existing
// volume and checks that it permits the volume's access mode.
func resolveExistingVolume(pvLister corev1listers.PersistentVolumeLister, ev *ateapipb.ExistingVolume) (*corev1.PersistentVolume, error) {
	name := ev.GetName()
	pv, err := findPersistentVolume(pvLister, ev.GetDriver(), ev.GetVolumeHandle())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "existing volume %q: listing PersistentVolumes: %v", name, err)
	}
	if pv == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "existing volume %q: no PersistentVolume of driver %q holds volume %q", name, ev.GetDriver(), ev.GetVolumeHandle())
	}
	for _, want := range pvAccessModes[ev.GetAccessMode()] {
		for _, got := range pv.Spec.AccessModes {
			if got == want {
				return pv, nil
			}
		}
	}
	return nil, status.Errorf(codes.FailedPrecondition, "existing volume %q: PersistentVolume %q permits %v, not %s", name, pv.Name, pv.Spec.AccessModes, ev.GetAccessMode())
}

// validateExistingVolumes checks a CreateActor's existing volumes against the
// template and the cluster, so that a volume that cannot be mounted is
// refused at create rather than at the actor's first resume: each must
// supply an existing volume of the template, its driver must be registered,
// and a PersistentVolume of the driver must hold its handle and permit its
// access mode.
func validateExistingVolumes(ctx context.Context, registry VolumePluginRegistry, pvLister corev1listers.PersistentVolumeLister, template *ateapipb.ActorTemplate, evs []*ateapipb.ExistingVolume) error {
	for _, ev := range evs {
		name := ev.GetName()
		declared := false
		for _, vol := range template.GetVolumes() {
			if vol.GetName() == name && vol.GetExistingVolume() != nil {
				declared = true
				break
			}
		}
		if !declared {
			return status.Errorf(codes.InvalidArgument, "existing volume %q: ActorTemplate declares no existing volume %q", name, name)
		}
		if _, err := registry.GetPlugin(ctx, ev.GetDriver()); err != nil {
			return status.Errorf(codes.FailedPrecondition, "existing volume %q: unknown driver %q: %v", name, ev.GetDriver(), err)
		}
		if _, err := resolveExistingVolume(pvLister, ev); err != nil {
			return err
		}
	}
	return nil
}

// appendExistingVolumes adds the actor's mounted existing volumes to
// workloadSpec. Their volume context comes from the PersistentVolume at
// resume (fillExistingVolumeContext): unmounting needs none.
func appendExistingVolumes(workloadSpec *ateletpb.WorkloadSpec, template *ateapipb.ActorTemplate, actor *ateapipb.Actor) {
	for _, vol := range template.GetVolumes() {
		if vol.GetExistingVolume() == nil || !isVolumeMounted(vol.GetName(), template) {
			continue
		}
		ev := existingVolume(actor, vol.GetName())
		if ev == nil {
			continue
		}
		workloadSpec.Volumes = append(workloadSpec.Volumes, &ateletpb.Volume{
			Name: vol.GetName(),
			Source: &ateletpb.Volume_External{
				External: &ateletpb.ExternalVolumeSource{
					StorageVolumeId: ev.GetVolumeHandle(),
					VolumeType:      ev.GetDriver(),
					AccessMode:      accessModeToAtelet(ev.GetAccessMode()),
				},
			},
		})
	}
}

// existingVolumeMount returns the atelet mount of an existing volume: its
// sub_path is under the actor's root of the volume, and a read-only volume
// mounts read-only.
func existingVolumeMount(mount *ateapipb.VolumeMount, ev *ateapipb.ExistingVolume) *ateletpb.VolumeMount {
	subPath := mount.GetSubPath()
	if root := ev.GetSubPath(); root != "" {
		subPath = path.Join(root, subPath)
	}
	return &ateletpb.VolumeMount{
		Name:      mount.GetName(),
		MountPath: mount.GetMountPath(),
		SubPath:   subPath,
		ReadOnly:  mount.GetReadOnly() || ev.GetAccessMode() == ateapipb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_ONLY_MANY,
	}
}

// fillExistingVolumeContext sets each existing volume's volume context in
// workloadSpec from its PersistentVolume, which the driver needs to mount it.
func fillExistingVolumeContext(workloadSpec *ateletpb.WorkloadSpec, pvLister corev1listers.PersistentVolumeLister, actor *ateapipb.Actor) error {
	for _, vol := range workloadSpec.GetVolumes() {
		ev := existingVolume(actor, vol.GetName())
		ext := vol.GetExternal()
		if ev == nil || ext == nil {
			continue
		}
		pv, err := resolveExistingVolume(pvLister, ev)
		if err != nil {
			return err
		}
		ext.VolumeContext = pv.Spec.CSI.VolumeAttributes
	}
	return nil
}

// attachExistingVolumes attaches the actor's mounted existing volumes to
// node. They are never detached by the actor: other actors on the node may
// be using them.
func attachExistingVolumes(ctx context.Context, registry VolumePluginRegistry, actor *ateapipb.Actor, template *ateapipb.ActorTemplate, node string) error {
	for _, ev := range actor.GetExistingVolumes() {
		if !isVolumeMounted(ev.GetName(), template) {
			continue
		}
		plugin, err := registry.GetPlugin(ctx, ev.GetDriver())
		if err != nil {
			return fmt.Errorf("failed to get volume plugin for %q: %w", ev.GetDriver(), err)
		}
		if err := plugin.AttachVolume(ctx, ev.GetVolumeHandle(), node, accessModeToVolume(ev.GetAccessMode())); err != nil {
			return fmt.Errorf("failed to attach existing volume %q to node %q: %w", ev.GetName(), node, err)
		}
	}
	return nil
}
