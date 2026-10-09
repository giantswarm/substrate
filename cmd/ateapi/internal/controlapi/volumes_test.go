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
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/volume"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	storagev1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	storagev1listers "k8s.io/client-go/listers/storage/v1"
)

type fakeStorageClassLister struct {
	storageClasses map[string]*storagev1.StorageClass
}

func (f *fakeStorageClassLister) List(selector k8slabels.Selector) (ret []*storagev1.StorageClass, err error) {
	return nil, nil
}

func (f *fakeStorageClassLister) Get(name string) (*storagev1.StorageClass, error) {
	sc, ok := f.storageClasses[name]
	if !ok {
		return nil, k8serrors.NewNotFound(storagev1.Resource("storageclass"), name)
	}
	return sc, nil
}

var _ storagev1listers.StorageClassLister = (*fakeStorageClassLister)(nil)

func TestInitialActorVolumes_PendingState(t *testing.T) {
	tmpl := &ateapipb.ActorTemplate{
		Volumes: []*ateapipb.Volume{
			{
				Name: "data-vol-1",
				ExternalVolumeTemplate: &ateapipb.ExternalVolumeTemplate{
					StorageClassName: "standard",
				},
			},
			{
				Name: "scratch-vol",
			},
			{
				Name:       "durable-vol",
				DurableDir: &ateapipb.DurableDirVolumeSource{},
			},
			{
				Name: "data-vol-2",
				ExternalVolumeTemplate: &ateapipb.ExternalVolumeTemplate{
					StorageClassName: "fast",
				},
			},
		},
	}

	want := []*ateapipb.ExternalVolume{
		{
			VolumeName: "data-vol-1",
			VolumeType: "mock-standard",
			Status:     ateapipb.ExternalVolume_STATUS_PENDING,
		},
		{
			VolumeName: "data-vol-2",
			VolumeType: "mock-fast",
			Status:     ateapipb.ExternalVolume_STATUS_PENDING,
		},
	}

	scLister := &fakeStorageClassLister{
		storageClasses: map[string]*storagev1.StorageClass{
			"standard": {
				ObjectMeta:  metav1.ObjectMeta{Name: "standard"},
				Provisioner: "mock-standard",
			},
			"fast": {
				ObjectMeta:  metav1.ObjectMeta{Name: "fast"},
				Provisioner: "mock-fast",
			},
		},
	}
	initVols, err := initialActorVolumes(context.Background(), scLister, tmpl, nil)
	if err != nil {
		t.Fatalf("initialActorVolumes failed: %v", err)
	}
	if diff := cmp.Diff(want, initVols, protocmp.Transform()); diff != "" {
		t.Errorf("initialActorVolumes mismatch (-want +got):\n%s", diff)
	}
}

func TestCreateActorVolumes(t *testing.T) {
	ctx := context.Background()

	standardTmpl := &ateapipb.ActorTemplate{
		Volumes: []*ateapipb.Volume{
			{
				Name: "data-vol",
				ExternalVolumeTemplate: &ateapipb.ExternalVolumeTemplate{
					StorageClassName: "standard",
				},
			},
		},
	}

	multiVolTmpl := &ateapipb.ActorTemplate{
		Volumes: []*ateapipb.Volume{
			{
				Name: "vol1",
				ExternalVolumeTemplate: &ateapipb.ExternalVolumeTemplate{
					StorageClassName: "standard",
				},
			},
			{
				Name: "vol2",
				ExternalVolumeTemplate: &ateapipb.ExternalVolumeTemplate{
					StorageClassName: "standard",
				},
			},
			{
				Name: "vol3",
				ExternalVolumeTemplate: &ateapipb.ExternalVolumeTemplate{
					StorageClassName: "standard",
				},
			},
		},
	}

	tests := []struct {
		name           string
		tmpl           *ateapipb.ActorTemplate
		inputVolumes   []*ateapipb.ExternalVolume
		storageClasses map[string]*storagev1.StorageClass
		wantErr        bool
		wantRes        []*ateapipb.ExternalVolume
	}{
		{
			name: "partial failure returns error and preserves succeeded, failed, and remaining volumes",
			tmpl: multiVolTmpl,
			inputVolumes: []*ateapipb.ExternalVolume{
				{
					VolumeName: "vol1",
					VolumeType: "mock-standard",
					Status:     ateapipb.ExternalVolume_STATUS_PENDING,
				},
				{
					VolumeName: "vol2",
					Status:     ateapipb.ExternalVolume_STATUS_DELETING,
				},
				{
					VolumeName: "vol3",
					Status:     ateapipb.ExternalVolume_STATUS_PENDING,
				},
			},
			wantErr: true,
			wantRes: []*ateapipb.ExternalVolume{
				{
					VolumeName:      "vol1",
					StorageVolumeId: "mock-vol-substrate-actor-uid-123-vol1",
					VolumeType:      "mock-standard",
					Status:          ateapipb.ExternalVolume_STATUS_CREATED,
				},
				{
					VolumeName: "vol2",
					Status:     ateapipb.ExternalVolume_STATUS_DELETING,
				},
				{
					VolumeName: "vol3",
					Status:     ateapipb.ExternalVolume_STATUS_PENDING,
				},
			},
		},
		{
			name: "created volume status succeeds",
			tmpl: standardTmpl,
			inputVolumes: []*ateapipb.ExternalVolume{
				{
					VolumeName:      "data-vol",
					StorageVolumeId: "existing-vol-id",
					Status:          ateapipb.ExternalVolume_STATUS_CREATED,
				},
			},
			wantErr: false,
			wantRes: []*ateapipb.ExternalVolume{
				{
					VolumeName:      "data-vol",
					StorageVolumeId: "existing-vol-id",
					Status:          ateapipb.ExternalVolume_STATUS_CREATED,
				},
			},
		},
		{
			name: "unspecified volume status returns error",
			tmpl: standardTmpl,
			inputVolumes: []*ateapipb.ExternalVolume{
				{
					VolumeName: "data-vol",
					Status:     ateapipb.ExternalVolume_STATUS_UNSPECIFIED,
				},
			},
			wantErr: true,
			wantRes: []*ateapipb.ExternalVolume{
				{
					VolumeName: "data-vol",
					Status:     ateapipb.ExternalVolume_STATUS_UNSPECIFIED,
				},
			},
		},
		{
			name: "volume not found in template returns error",
			tmpl: &ateapipb.ActorTemplate{},
			inputVolumes: []*ateapipb.ExternalVolume{
				{
					VolumeName: "missing-vol",
					Status:     ateapipb.ExternalVolume_STATUS_PENDING,
				},
			},
			wantErr: true,
			wantRes: []*ateapipb.ExternalVolume{
				{
					VolumeName: "missing-vol",
					Status:     ateapipb.ExternalVolume_STATUS_PENDING,
				},
			},
		},
		{
			name: "storage class parameters are propagated to volume context",
			tmpl: standardTmpl,
			inputVolumes: []*ateapipb.ExternalVolume{
				{
					VolumeName: "data-vol",
					VolumeType: "mock-standard",
					Status:     ateapipb.ExternalVolume_STATUS_PENDING,
				},
			},
			storageClasses: map[string]*storagev1.StorageClass{
				"standard": {
					ObjectMeta:  metav1.ObjectMeta{Name: "standard"},
					Provisioner: "mock-standard",
					Parameters: map[string]string{
						"type":                      "pd-ssd",
						"csi.storage.k8s.io/fstype": "ext4",
					},
				},
			},
			wantErr: false,
			wantRes: []*ateapipb.ExternalVolume{
				{
					VolumeName:      "data-vol",
					StorageVolumeId: "mock-vol-substrate-actor-uid-123-data-vol",
					VolumeType:      "mock-standard",
					Status:          ateapipb.ExternalVolume_STATUS_CREATED,
					VolumeContext: map[string]string{
						"type":                      "pd-ssd",
						"csi.storage.k8s.io/fstype": "ext4",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := volume.NewMockVolumePlugin()
			registry := &mockPluginRegistry{
				plugins: map[string]volume.VolumePluginControlPlane{
					"mock-standard": plugin,
					"mock-fast":     plugin,
				},
			}
			scs := tt.storageClasses
			if scs == nil {
				scs = map[string]*storagev1.StorageClass{
					"standard": {
						ObjectMeta:  metav1.ObjectMeta{Name: "standard"},
						Provisioner: "mock-standard",
					},
					"fast": {
						ObjectMeta:  metav1.ObjectMeta{Name: "fast"},
						Provisioner: "mock-fast",
					},
				}
			}
			scLister := &fakeStorageClassLister{storageClasses: scs}
			res, err := createActorVolumes(ctx, registry, scLister, "actor-uid-123", tt.tmpl, nil, tt.inputVolumes)
			if (err != nil) != tt.wantErr {
				t.Errorf("createActorVolumes() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.wantRes, res, protocmp.Transform()); diff != "" {
				t.Errorf("createActorVolumes() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// seedVolumePlugin holds a fixed set of snapshots and records the volumes it
// creates and deletes. ignoreSource makes it answer a restore with an empty
// volume, as a driver that does not implement content sources does.
type seedVolumePlugin struct {
	volume.VolumePluginControlPlane
	snapshots    map[string]volume.Snapshot
	listErr      error
	ignoreSource bool
	created      []volume.CreateVolumeRequest
	deleted      []string
}

func (p *seedVolumePlugin) GetSnapshot(_ context.Context, id string) (volume.Snapshot, bool, error) {
	if p.listErr != nil {
		return volume.Snapshot{}, false, p.listErr
	}
	snap, ok := p.snapshots[id]
	return snap, ok, nil
}

func (p *seedVolumePlugin) CreateVolume(_ context.Context, req volume.CreateVolumeRequest) (volume.CreateVolumeResponse, error) {
	p.created = append(p.created, req)
	resp := volume.CreateVolumeResponse{VolumeID: "vol-" + req.Name, ContentSourceSnapshotID: req.SourceSnapshotID}
	if p.ignoreSource {
		resp.ContentSourceSnapshotID = ""
	}
	return resp, nil
}

func (p *seedVolumePlugin) DeleteVolume(_ context.Context, id string) error {
	p.deleted = append(p.deleted, id)
	return nil
}

// seededTemplate has one plain and one seeded external volume, both mounted.
func seededTemplate() *ateapipb.ActorTemplate {
	return &ateapipb.ActorTemplate{
		Containers: []*ateapipb.Container{{
			Name: "main",
			VolumeMounts: []*ateapipb.VolumeMount{
				{Name: "scratch", MountPath: "/scratch"},
				{Name: "workspace", MountPath: "/workspace"},
			},
		}},
		Volumes: []*ateapipb.Volume{
			{Name: "scratch", ExternalVolumeTemplate: &ateapipb.ExternalVolumeTemplate{Capacity: "1Gi", StorageClassName: "standard"}},
			{Name: "workspace", ExternalVolumeTemplate: &ateapipb.ExternalVolumeTemplate{Capacity: "2Gi", StorageClassName: "standard", Seeded: true}},
		},
	}
}

func seedStorageClasses() *fakeStorageClassLister {
	return &fakeStorageClassLister{storageClasses: map[string]*storagev1.StorageClass{
		"standard": {ObjectMeta: metav1.ObjectMeta{Name: "standard"}, Provisioner: "mock-standard"},
	}}
}

func TestInitialActorVolumes_Seeded(t *testing.T) {
	for _, tc := range []struct {
		name  string
		seeds []*ateapipb.VolumeSeed
		want  []string
	}{
		{name: "without a seed the seeded volume is left out", want: []string{"scratch"}},
		{
			name:  "with a seed the seeded volume is created",
			seeds: []*ateapipb.VolumeSeed{{VolumeName: "workspace", Driver: "mock-standard", SnapshotHandle: "snap-1"}},
			want:  []string{"scratch", "workspace"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vols, err := initialActorVolumes(context.Background(), seedStorageClasses(), seededTemplate(), tc.seeds)
			if err != nil {
				t.Fatalf("initialActorVolumes: %v", err)
			}
			var got []string
			for _, v := range vols {
				got = append(got, v.GetVolumeName())
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("volumes mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestValidateVolumeSeeds(t *testing.T) {
	ready := map[string]volume.Snapshot{
		"snap-1":   {SnapshotID: "snap-1", ReadyToUse: true},
		"snap-new": {SnapshotID: "snap-new"},
		"snap-3gi": {SnapshotID: "snap-3gi", ReadyToUse: true, SizeBytes: 3 << 30},
	}
	seed := func(vol, driver, handle string) []*ateapipb.VolumeSeed {
		return []*ateapipb.VolumeSeed{{VolumeName: vol, Driver: driver, SnapshotHandle: handle}}
	}
	sized := func(handle, capacity string) []*ateapipb.VolumeSeed {
		return []*ateapipb.VolumeSeed{{VolumeName: "workspace", Driver: "mock-standard", SnapshotHandle: handle, Capacity: capacity}}
	}
	for _, tc := range []struct {
		name     string
		seeds    []*ateapipb.VolumeSeed
		listErr  error
		wantCode codes.Code
		wantMsg  string
	}{
		{name: "no seeds", wantCode: codes.OK},
		{name: "ready snapshot", seeds: seed("workspace", "mock-standard", "snap-1"), wantCode: codes.OK},
		{name: "volume is not seeded", seeds: seed("scratch", "mock-standard", "snap-1"), wantCode: codes.InvalidArgument, wantMsg: `no seeded external volume "scratch"`},
		{name: "volume is not in the template", seeds: seed("other", "mock-standard", "snap-1"), wantCode: codes.InvalidArgument, wantMsg: `no seeded external volume "other"`},
		{name: "driver is not the provisioner", seeds: seed("workspace", "other.csi.example.com", "snap-1"), wantCode: codes.InvalidArgument, wantMsg: `names driver "other.csi.example.com", but the volume's StorageClass "standard" provisions with "mock-standard"`},
		{name: "snapshot is missing", seeds: seed("workspace", "mock-standard", "snap-gone"), wantCode: codes.FailedPrecondition, wantMsg: `driver "mock-standard" holds no snapshot "snap-gone"`},
		{name: "snapshot is not ready", seeds: seed("workspace", "mock-standard", "snap-new"), wantCode: codes.FailedPrecondition, wantMsg: `snapshot "snap-new" in driver "mock-standard" is not ready to use`},
		{name: "driver cannot list snapshots", seeds: seed("workspace", "mock-standard", "snap-1"), listErr: status.Error(codes.Unimplemented, "no ListSnapshots"), wantCode: codes.FailedPrecondition, wantMsg: `cannot list snapshots`},
		{name: "capacity holds the snapshot", seeds: sized("snap-3gi", "4Gi"), wantCode: codes.OK},
		{name: "template capacity below the snapshot", seeds: seed("workspace", "mock-standard", "snap-3gi"), wantCode: codes.InvalidArgument, wantMsg: `capacity 2Gi is smaller than snapshot "snap-3gi"`},
		{name: "seed capacity below the snapshot", seeds: sized("snap-3gi", "1Gi"), wantCode: codes.InvalidArgument, wantMsg: `capacity 1Gi is smaller than snapshot "snap-3gi"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := &mockPluginRegistry{plugins: map[string]volume.VolumePluginControlPlane{
				"mock-standard": &seedVolumePlugin{snapshots: ready, listErr: tc.listErr},
			}}
			err := validateVolumeSeeds(context.Background(), registry, seedStorageClasses(), seededTemplate(), tc.seeds)
			if got := status.Code(err); got != tc.wantCode {
				t.Fatalf("validateVolumeSeeds code = %v (%v), want %v", got, err, tc.wantCode)
			}
			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("validateVolumeSeeds error = %q, want it to contain %q", err, tc.wantMsg)
			}
		})
	}

	t.Run("unknown driver", func(t *testing.T) {
		// The StorageClass names a provisioner no CSIDriverConfig registers.
		registry := &mockPluginRegistry{plugins: map[string]volume.VolumePluginControlPlane{}}
		err := validateVolumeSeeds(context.Background(), registry, seedStorageClasses(), seededTemplate(), seed("workspace", "mock-standard", "snap-1"))
		if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), `unknown driver "mock-standard"`) {
			t.Fatalf("validateVolumeSeeds = %v, want FailedPrecondition naming the unknown driver", err)
		}
	})
}

func TestCreateActorVolumes_Seeded(t *testing.T) {
	seeds := []*ateapipb.VolumeSeed{{VolumeName: "workspace", Driver: "mock-standard", SnapshotHandle: "snap-1"}}
	pending := func() []*ateapipb.ExternalVolume {
		return []*ateapipb.ExternalVolume{
			{VolumeName: "scratch", VolumeType: "mock-standard", Status: ateapipb.ExternalVolume_STATUS_PENDING},
			{VolumeName: "workspace", VolumeType: "mock-standard", Status: ateapipb.ExternalVolume_STATUS_PENDING},
		}
	}

	t.Run("restores the seeded volume and creates the other empty", func(t *testing.T) {
		plugin := &seedVolumePlugin{}
		registry := &mockPluginRegistry{plugins: map[string]volume.VolumePluginControlPlane{"mock-standard": plugin}}
		vols, err := createActorVolumes(context.Background(), registry, seedStorageClasses(), "uid", seededTemplate(), seeds, pending())
		if err != nil {
			t.Fatalf("createActorVolumes: %v", err)
		}
		want := []volume.CreateVolumeRequest{
			{Name: "substrate-uid-scratch", Capacity: "1Gi", DriverName: "mock-standard"},
			{Name: "substrate-uid-workspace", Capacity: "2Gi", DriverName: "mock-standard", SourceSnapshotID: "snap-1"},
		}
		if diff := cmp.Diff(want, plugin.created); diff != "" {
			t.Errorf("CreateVolume requests mismatch (-want +got):\n%s", diff)
		}
		for _, v := range vols {
			if v.GetStatus() != ateapipb.ExternalVolume_STATUS_CREATED {
				t.Errorf("volume %q status = %v, want CREATED", v.GetVolumeName(), v.GetStatus())
			}
		}
	})

	t.Run("a seed's capacity replaces the template's", func(t *testing.T) {
		plugin := &seedVolumePlugin{}
		registry := &mockPluginRegistry{plugins: map[string]volume.VolumePluginControlPlane{"mock-standard": plugin}}
		sized := []*ateapipb.VolumeSeed{{VolumeName: "workspace", Driver: "mock-standard", SnapshotHandle: "snap-1", Capacity: "20Gi"}}
		if _, err := createActorVolumes(context.Background(), registry, seedStorageClasses(), "uid", seededTemplate(), sized, pending()); err != nil {
			t.Fatalf("createActorVolumes: %v", err)
		}
		want := []volume.CreateVolumeRequest{
			{Name: "substrate-uid-scratch", Capacity: "1Gi", DriverName: "mock-standard"},
			{Name: "substrate-uid-workspace", Capacity: "20Gi", DriverName: "mock-standard", SourceSnapshotID: "snap-1"},
		}
		if diff := cmp.Diff(want, plugin.created); diff != "" {
			t.Errorf("CreateVolume requests mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a driver that does not restore fails the create", func(t *testing.T) {
		plugin := &seedVolumePlugin{ignoreSource: true}
		registry := &mockPluginRegistry{plugins: map[string]volume.VolumePluginControlPlane{"mock-standard": plugin}}
		vols, err := createActorVolumes(context.Background(), registry, seedStorageClasses(), "uid", seededTemplate(), seeds, pending())
		if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), `from snapshot "", want "snap-1"`) {
			t.Fatalf("createActorVolumes = %v, want FailedPrecondition naming the missing restore", err)
		}
		if diff := cmp.Diff([]string{"vol-substrate-uid-workspace"}, plugin.deleted); diff != "" {
			t.Errorf("deleted volumes mismatch (-want +got):\n%s", diff)
		}
		if got := vols[1].GetStatus(); got != ateapipb.ExternalVolume_STATUS_PENDING {
			t.Errorf("unrestored volume status = %v, want PENDING", got)
		}
	})
}

type trackingVolumePlugin struct {
	volume.VolumePluginControlPlane
	deletedIDs []string
}

func (t *trackingVolumePlugin) DeleteVolume(ctx context.Context, volumeID string) error {
	t.deletedIDs = append(t.deletedIDs, volumeID)
	return nil
}

func TestDeleteActorVolumes(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		actorUID    string
		volumes     []*ateapipb.ExternalVolume
		wantDeleted []string
		wantErr     bool
	}{
		{
			name:     "uses storage volume ID when present",
			actorUID: "uid-abc",
			volumes: []*ateapipb.ExternalVolume{
				{VolumeName: "vol1", StorageVolumeId: "storage-vol-123", VolumeType: "mock"},
			},
			wantDeleted: []string{"storage-vol-123"},
			wantErr:     false,
		},
		{
			name:     "falls back to actorVolumeID when storage volume ID is empty regardless of status",
			actorUID: "uid-abc",
			volumes: []*ateapipb.ExternalVolume{
				{VolumeName: "vol1", StorageVolumeId: "", Status: ateapipb.ExternalVolume_STATUS_CREATED, VolumeType: "mock"},
			},
			wantDeleted: []string{"substrate-uid-abc-vol1"},
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &trackingVolumePlugin{}
			registry := &mockPluginRegistry{
				plugins: map[string]volume.VolumePluginControlPlane{
					"mock": plugin,
				},
			}
			err := deleteActorVolumes(ctx, registry, tt.actorUID, tt.volumes)
			if (err != nil) != tt.wantErr {
				t.Fatalf("deleteActorVolumes() error = %v, wantErr %v", err, tt.wantErr)
			}

			if diff := cmp.Diff(tt.wantDeleted, plugin.deletedIDs); diff != "" {
				t.Errorf("deletedIDs mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

type mockPluginRegistry struct {
	plugins map[string]volume.VolumePluginControlPlane
}

func (m *mockPluginRegistry) GetPlugin(ctx context.Context, name string) (volume.VolumePluginControlPlane, error) {
	p, ok := m.plugins[name]
	if !ok {
		return nil, fmt.Errorf("plugin %q not found in mock registry", name)
	}
	return p, nil
}

type mockDetachStore struct {
	workers map[string]*ateapipb.Worker
	err     error
}

func (m *mockDetachStore) GetWorker(ctx context.Context, name string) (*ateapipb.Worker, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.workers == nil {
		return nil, store.ErrNotFound
	}
	w, ok := m.workers[name]
	if !ok {
		return nil, store.ErrNotFound
	}
	return w, nil
}

type detachCall struct {
	VolumeID string
	Node     string
}

type mockDetachVolumePlugin struct {
	volume.VolumePluginControlPlane
	detachCalls []detachCall
	detachErrs  map[string]error
}

func (m *mockDetachVolumePlugin) DetachVolume(ctx context.Context, volumeID, node string) error {
	m.detachCalls = append(m.detachCalls, detachCall{VolumeID: volumeID, Node: node})
	if m.detachErrs != nil {
		if err, ok := m.detachErrs[volumeID]; ok {
			return err
		}
	}
	return nil
}

func TestDetachActorVolumes(t *testing.T) {
	ctx := context.Background()

	baseWorker := &ateapipb.Worker{
		Metadata: &ateapipb.ResourceMetadata{Name: "worker-1"},
		NodeName: "node-1",
	}

	tests := []struct {
		name            string
		actor           *ateapipb.Actor
		template        *ateapipb.ActorTemplate
		store           *mockDetachStore
		plugin          *mockDetachVolumePlugin
		pluginRegistry  *mockPluginRegistry
		wantDetachCalls []detachCall
		wantErr         bool
		wantErrContains string
	}{
		{
			name: "success with multiple mounted volumes",
			actor: &ateapipb.Actor{
				Metadata: &ateapipb.ResourceMetadata{Name: "actor-1", Atespace: "default"},
				Status: &ateapipb.ActorStatus{
					WorkerAssignment: &ateapipb.WorkerAssignment{
						Worker: &ateapipb.ObjectRef{Name: "worker-1"},
					},
					ActorVolumes: []*ateapipb.ExternalVolume{
						{VolumeName: "vol1", StorageVolumeId: "storage-vol-1", VolumeType: "mock"},
						{VolumeName: "vol2", StorageVolumeId: "storage-vol-2", VolumeType: "mock"},
					},
				},
			},
			template: &ateapipb.ActorTemplate{
				Volumes: []*ateapipb.Volume{
					{Name: "vol1"},
					{Name: "vol2"},
				},
				Containers: []*ateapipb.Container{
					{
						VolumeMounts: []*ateapipb.VolumeMount{
							{Name: "vol1"},
							{Name: "vol2"},
						},
					},
				},
			},
			store: &mockDetachStore{
				workers: map[string]*ateapipb.Worker{"worker-1": baseWorker},
			},
			wantDetachCalls: []detachCall{
				{VolumeID: "storage-vol-1", Node: "node-1"},
				{VolumeID: "storage-vol-2", Node: "node-1"},
			},
		},
		{
			name: "skips unmounted volume in template",
			actor: &ateapipb.Actor{
				Metadata: &ateapipb.ResourceMetadata{Name: "actor-1", Atespace: "default"},
				Status: &ateapipb.ActorStatus{
					WorkerAssignment: &ateapipb.WorkerAssignment{
						Worker: &ateapipb.ObjectRef{Name: "worker-1"},
					},
					ActorVolumes: []*ateapipb.ExternalVolume{
						{VolumeName: "mounted-vol", StorageVolumeId: "storage-vol-mounted", VolumeType: "mock"},
						{VolumeName: "unmounted-vol", StorageVolumeId: "storage-vol-unmounted", VolumeType: "mock"},
					},
				},
			},
			template: &ateapipb.ActorTemplate{
				Volumes: []*ateapipb.Volume{
					{Name: "mounted-vol"},
					{Name: "unmounted-vol"},
				},
				Containers: []*ateapipb.Container{
					{
						VolumeMounts: []*ateapipb.VolumeMount{
							{Name: "mounted-vol"},
						},
					},
				},
			},
			store: &mockDetachStore{
				workers: map[string]*ateapipb.Worker{"worker-1": baseWorker},
			},
			wantDetachCalls: []detachCall{
				{VolumeID: "storage-vol-mounted", Node: "node-1"},
			},
		},
		{
			name: "skips volume with empty StorageVolumeId",
			actor: &ateapipb.Actor{
				Metadata: &ateapipb.ResourceMetadata{Name: "actor-1", Atespace: "default"},
				Status: &ateapipb.ActorStatus{
					WorkerAssignment: &ateapipb.WorkerAssignment{
						Worker: &ateapipb.ObjectRef{Name: "worker-1"},
					},
					ActorVolumes: []*ateapipb.ExternalVolume{
						{VolumeName: "vol1", StorageVolumeId: "", VolumeType: "mock"},
						{VolumeName: "vol2", StorageVolumeId: "storage-vol-2", VolumeType: "mock"},
					},
				},
			},
			template: &ateapipb.ActorTemplate{
				Volumes: []*ateapipb.Volume{
					{Name: "vol1"},
					{Name: "vol2"},
				},
				Containers: []*ateapipb.Container{
					{
						VolumeMounts: []*ateapipb.VolumeMount{
							{Name: "vol1"},
							{Name: "vol2"},
						},
					},
				},
			},
			store: &mockDetachStore{
				workers: map[string]*ateapipb.Worker{"worker-1": baseWorker},
			},
			wantDetachCalls: []detachCall{
				{VolumeID: "storage-vol-2", Node: "node-1"},
			},
		},
		{
			name: "nil template falls back to detaching all actor volumes",
			actor: &ateapipb.Actor{
				Metadata: &ateapipb.ResourceMetadata{Name: "actor-1", Atespace: "default"},
				Status: &ateapipb.ActorStatus{
					WorkerAssignment: &ateapipb.WorkerAssignment{
						Worker: &ateapipb.ObjectRef{Name: "worker-1"},
					},
					ActorVolumes: []*ateapipb.ExternalVolume{
						{VolumeName: "vol1", StorageVolumeId: "storage-vol-1", VolumeType: "mock"},
						{VolumeName: "vol2", StorageVolumeId: "storage-vol-2", VolumeType: "mock"},
					},
				},
			},
			template: nil,
			store: &mockDetachStore{
				workers: map[string]*ateapipb.Worker{"worker-1": baseWorker},
			},
			wantDetachCalls: []detachCall{
				{VolumeID: "storage-vol-1", Node: "node-1"},
				{VolumeID: "storage-vol-2", Node: "node-1"},
			},
		},
		{
			name: "codes.NotFound from plugin is treated as already detached",
			actor: &ateapipb.Actor{
				Metadata: &ateapipb.ResourceMetadata{Name: "actor-1", Atespace: "default"},
				Status: &ateapipb.ActorStatus{
					WorkerAssignment: &ateapipb.WorkerAssignment{
						Worker: &ateapipb.ObjectRef{Name: "worker-1"},
					},
					ActorVolumes: []*ateapipb.ExternalVolume{
						{VolumeName: "vol1", StorageVolumeId: "storage-vol-1", VolumeType: "mock"},
					},
				},
			},
			plugin: &mockDetachVolumePlugin{
				detachErrs: map[string]error{
					"storage-vol-1": status.Error(codes.NotFound, "volume not found"),
				},
			},
			store: &mockDetachStore{
				workers: map[string]*ateapipb.Worker{"worker-1": baseWorker},
			},
			wantDetachCalls: []detachCall{
				{VolumeID: "storage-vol-1", Node: "node-1"},
			},
			wantErr: false,
		},
		{
			name: "partial failure attempts all volumes and joins errors",
			actor: &ateapipb.Actor{
				Metadata: &ateapipb.ResourceMetadata{Name: "actor-1", Atespace: "default"},
				Status: &ateapipb.ActorStatus{
					WorkerAssignment: &ateapipb.WorkerAssignment{
						Worker: &ateapipb.ObjectRef{Name: "worker-1"},
					},
					ActorVolumes: []*ateapipb.ExternalVolume{
						{VolumeName: "vol1", StorageVolumeId: "storage-vol-1", VolumeType: "mock"},
						{VolumeName: "vol2", StorageVolumeId: "storage-vol-2", VolumeType: "mock"},
					},
				},
			},
			plugin: &mockDetachVolumePlugin{
				detachErrs: map[string]error{
					"storage-vol-1": status.Error(codes.Internal, "disk detach failed"),
				},
			},
			store: &mockDetachStore{
				workers: map[string]*ateapipb.Worker{"worker-1": baseWorker},
			},
			wantDetachCalls: []detachCall{
				{VolumeID: "storage-vol-1", Node: "node-1"},
				{VolumeID: "storage-vol-2", Node: "node-1"},
			},
			wantErr:         true,
			wantErrContains: "failed to detach volume \"storage-vol-1\"",
		},
		{
			name: "unknown plugin returns error",
			actor: &ateapipb.Actor{
				Metadata: &ateapipb.ResourceMetadata{Name: "actor-1", Atespace: "default"},
				Status: &ateapipb.ActorStatus{
					WorkerAssignment: &ateapipb.WorkerAssignment{
						Worker: &ateapipb.ObjectRef{Name: "worker-1"},
					},
					ActorVolumes: []*ateapipb.ExternalVolume{
						{VolumeName: "vol1", StorageVolumeId: "storage-vol-1", VolumeType: "unknown-plugin"},
					},
				},
			},
			store: &mockDetachStore{
				workers: map[string]*ateapipb.Worker{"worker-1": baseWorker},
			},
			wantErr:         true,
			wantErrContains: "failed to get volume plugin for \"unknown-plugin\"",
		},
		{
			name: "no worker assignment skips detach",
			actor: &ateapipb.Actor{
				Metadata: &ateapipb.ResourceMetadata{Name: "actor-1", Atespace: "default"},
				Status: &ateapipb.ActorStatus{
					WorkerAssignment: nil,
					ActorVolumes: []*ateapipb.ExternalVolume{
						{VolumeName: "vol1", StorageVolumeId: "storage-vol-1", VolumeType: "mock"},
					},
				},
			},
			store:           &mockDetachStore{},
			wantDetachCalls: nil,
			wantErr:         false,
		},
		{
			name: "worker not found in store skips detach",
			actor: &ateapipb.Actor{
				Metadata: &ateapipb.ResourceMetadata{Name: "actor-1", Atespace: "default"},
				Status: &ateapipb.ActorStatus{
					WorkerAssignment: &ateapipb.WorkerAssignment{
						Worker: &ateapipb.ObjectRef{Name: "nonexistent-worker"},
					},
					ActorVolumes: []*ateapipb.ExternalVolume{
						{VolumeName: "vol1", StorageVolumeId: "storage-vol-1", VolumeType: "mock"},
					},
				},
			},
			store:           &mockDetachStore{workers: map[string]*ateapipb.Worker{}},
			wantDetachCalls: nil,
			wantErr:         false,
		},
		{
			name: "worker has empty node name skips detach",
			actor: &ateapipb.Actor{
				Metadata: &ateapipb.ResourceMetadata{Name: "actor-1", Atespace: "default"},
				Status: &ateapipb.ActorStatus{
					WorkerAssignment: &ateapipb.WorkerAssignment{
						Worker: &ateapipb.ObjectRef{Name: "worker-1"},
					},
					ActorVolumes: []*ateapipb.ExternalVolume{
						{VolumeName: "vol1", StorageVolumeId: "storage-vol-1", VolumeType: "mock"},
					},
				},
			},
			store: &mockDetachStore{
				workers: map[string]*ateapipb.Worker{
					"worker-1": {
						Metadata: &ateapipb.ResourceMetadata{Name: "worker-1"},
						NodeName: "",
					},
				},
			},
			wantDetachCalls: nil,
			wantErr:         false,
		},
		{
			name: "store internal error returns error",
			actor: &ateapipb.Actor{
				Metadata: &ateapipb.ResourceMetadata{Name: "actor-1", Atespace: "default"},
				Status: &ateapipb.ActorStatus{
					WorkerAssignment: &ateapipb.WorkerAssignment{
						Worker: &ateapipb.ObjectRef{Name: "worker-1"},
					},
					ActorVolumes: []*ateapipb.ExternalVolume{
						{VolumeName: "vol1", StorageVolumeId: "storage-vol-1", VolumeType: "mock"},
					},
				},
			},
			store: &mockDetachStore{
				err: errors.New("db connection failure"),
			},
			wantDetachCalls: nil,
			wantErr:         true,
			wantErrContains: "failed to get worker: db connection failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := tt.plugin
			if plugin == nil {
				plugin = &mockDetachVolumePlugin{}
			}
			registry := tt.pluginRegistry
			if registry == nil {
				registry = &mockPluginRegistry{
					plugins: map[string]volume.VolumePluginControlPlane{
						"mock": plugin,
					},
				}
			}

			err := detachActorVolumes(ctx, tt.store, registry, tt.actor, tt.template, "test")
			if (err != nil) != tt.wantErr {
				t.Fatalf("detachActorVolumes() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErrContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Fatalf("detachActorVolumes() error = %v, want error containing %q", err, tt.wantErrContains)
				}
			}
			if diff := cmp.Diff(tt.wantDetachCalls, plugin.detachCalls); diff != "" {
				t.Errorf("detachCalls mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
