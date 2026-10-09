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

package volume

import (
	"context"
)

// CreateVolumeRequest describes a volume to provision.
type CreateVolumeRequest struct {
	// Name is the name to provision the volume under.
	Name string
	// Capacity is the requested size as a Kubernetes resource.Quantity string.
	Capacity string
	// Parameters are the driver-specific parameters from the StorageClass.
	Parameters map[string]string
	// DriverName selects the provisioner.
	DriverName string
	// SourceSnapshotID seeds the new volume from an existing snapshot. Empty
	// provisions an empty volume. The snapshot must belong to the same driver:
	// a handle means nothing to any other one.
	SourceSnapshotID string
}

// CreateVolumeResponse describes a provisioned volume.
type CreateVolumeResponse struct {
	// VolumeID is the globally unique ID assigned by the storage system.
	VolumeID string
	// VolumeContext is driver-defined metadata that the node plugin needs to
	// mount the volume.
	VolumeContext map[string]string
	// ContentSourceSnapshotID is the snapshot the driver reports it restored
	// from, empty for an empty volume. Callers that requested a source must
	// check this: a driver that ignores the request returns an empty volume and
	// reports success, which for a restore is silent data loss.
	ContentSourceSnapshotID string
}

// Snapshot is a point-in-time copy of a volume held by the storage system.
type Snapshot struct {
	// SnapshotID is the storage system's handle for the snapshot.
	SnapshotID string
	// SourceVolumeID is the volume it was captured from.
	SourceVolumeID string
	// ReadyToUse is whether the storage system has finished the copy. Drivers
	// may return a handle before it is usable and finish in the background, so
	// this is a point-in-time observation rather than a durable property.
	ReadyToUse bool
	// SizeBytes is the snapshot's size, or 0 if the driver did not report one.
	SizeBytes int64
}

// VolumePluginControlPlane abstracts storage operations performed on the control plane.
type VolumePluginControlPlane interface {
	DriverName(ctx context.Context) (string, error)
	CreateVolume(ctx context.Context, req CreateVolumeRequest) (CreateVolumeResponse, error)
	// GetSnapshot looks a snapshot up by handle, reporting whether the storage
	// system still has it. A snapshot deleted out from under us is a missing
	// snapshot, not an error; a driver that cannot list snapshots is an error
	// with code Unimplemented, since it cannot tell either way.
	GetSnapshot(ctx context.Context, snapshotID string) (snapshot Snapshot, found bool, err error)
	DeleteVolume(ctx context.Context, volumeID string) error
	AttachVolume(ctx context.Context, volumeID string, node string) error
	DetachVolume(ctx context.Context, volumeID string, node string) error
}

// VolumePluginWorkerPlane abstracts storage operations performed on worker nodes.
type VolumePluginWorkerPlane interface {
	MountVolume(ctx context.Context, volumeID string, targetPath string, volumeContext map[string]string) error
	UnmountVolume(ctx context.Context, volumeID string, targetPath string) error
}
