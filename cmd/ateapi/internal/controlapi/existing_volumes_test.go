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
	"strings"
	"testing"

	"github.com/agent-substrate/substrate/internal/proto/ateletpb"
	"github.com/agent-substrate/substrate/internal/volume"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	nfsDriver = "nfs.csi.k8s.io"
	nfsHandle = "nfs-server#share#workspace-1"
)

// workspaceTemplate mounts the existing volume "workspace" at /workspace and
// a second existing volume "mirrors" read-only at /mirrors, beside an
// image volume.
func workspaceTemplate() *ateapipb.ActorTemplate {
	return &ateapipb.ActorTemplate{
		Containers: []*ateapipb.Container{{
			Name:  "agent",
			Image: "agent@sha256:0000000000000000000000000000000000000000000000000000000000000000",
			VolumeMounts: []*ateapipb.VolumeMount{
				{Name: "tools", MountPath: "/tools"},
				{Name: "workspace", MountPath: "/workspace"},
				{Name: "mirrors", MountPath: "/mirrors", ReadOnly: true},
			},
		}},
		Volumes: []*ateapipb.Volume{
			{Name: "tools", Image: &ateapipb.ImageVolumeSource{Reference: "tools@sha256:0000000000000000000000000000000000000000000000000000000000000000"}},
			{Name: "workspace", ExistingVolume: &ateapipb.ExistingVolumeSource{}},
			{Name: "mirrors", ExistingVolume: &ateapipb.ExistingVolumeSource{}},
		},
	}
}

// sessionVolumes supply both existing volumes from one handle: the
// session's own directory read-write and the mirrors read-only.
func sessionVolumes(session string) []*ateapipb.ExistingVolume {
	return []*ateapipb.ExistingVolume{
		{Name: "workspace", Driver: nfsDriver, VolumeHandle: nfsHandle, AccessMode: ateapipb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_MANY, SubPath: "sessions/" + session},
		{Name: "mirrors", Driver: nfsDriver, VolumeHandle: nfsHandle, AccessMode: ateapipb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_ONLY_MANY, SubPath: "mirrors"},
	}
}

func pvLister(t *testing.T, pvs ...*corev1.PersistentVolume) corev1listers.PersistentVolumeLister {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, pv := range pvs {
		if err := indexer.Add(pv); err != nil {
			t.Fatalf("adding PersistentVolume: %v", err)
		}
	}
	return corev1listers.NewPersistentVolumeLister(indexer)
}

func workspacePV(modes ...corev1.PersistentVolumeAccessMode) *corev1.PersistentVolume {
	return &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-workspace-1"},
		Spec: corev1.PersistentVolumeSpec{
			AccessModes: modes,
			PersistentVolumeSource: corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{
				Driver:           nfsDriver,
				VolumeHandle:     nfsHandle,
				VolumeAttributes: map[string]string{"server": "nfs-server", "share": "/"},
			}},
		},
	}
}

func TestValidateExistingVolumes(t *testing.T) {
	rwx := workspacePV(corev1.ReadWriteMany)
	registry := &mockPluginRegistry{plugins: map[string]volume.VolumePluginControlPlane{nfsDriver: &volume.MockVolumePlugin{}}}
	with := func(mod func(ev *ateapipb.ExistingVolume)) []*ateapipb.ExistingVolume {
		evs := sessionVolumes("a")
		mod(evs[0])
		return evs
	}
	for _, tc := range []struct {
		name     string
		evs      []*ateapipb.ExistingVolume
		pvs      []*corev1.PersistentVolume
		wantCode codes.Code
		wantMsg  string
	}{
		{name: "none", wantCode: codes.OK},
		{name: "read-write-many volume, mounted read-write and read-only", evs: sessionVolumes("a"), pvs: []*corev1.PersistentVolume{rwx}, wantCode: codes.OK},
		{
			name:     "volume the template does not declare as existing",
			evs:      with(func(ev *ateapipb.ExistingVolume) { ev.Name = "tools" }),
			pvs:      []*corev1.PersistentVolume{rwx},
			wantCode: codes.InvalidArgument,
			wantMsg:  `ActorTemplate declares no existing volume "tools"`,
		},
		{
			name:     "unknown driver",
			evs:      with(func(ev *ateapipb.ExistingVolume) { ev.Driver = "unknown.csi.example.com" }),
			pvs:      []*corev1.PersistentVolume{rwx},
			wantCode: codes.FailedPrecondition,
			wantMsg:  `existing volume "workspace": unknown driver "unknown.csi.example.com"`,
		},
		{
			name:     "missing volume",
			evs:      with(func(ev *ateapipb.ExistingVolume) { ev.VolumeHandle = "no-such-volume" }),
			pvs:      []*corev1.PersistentVolume{rwx},
			wantCode: codes.FailedPrecondition,
			wantMsg:  `no PersistentVolume of driver "nfs.csi.k8s.io" holds volume "no-such-volume"`,
		},
		{
			name: "volume of another driver",
			evs:  sessionVolumes("a"),
			pvs: []*corev1.PersistentVolume{func() *corev1.PersistentVolume {
				pv := workspacePV(corev1.ReadWriteMany)
				pv.Spec.CSI.Driver = "other"
				return pv
			}()},
			wantCode: codes.FailedPrecondition,
			wantMsg:  `no PersistentVolume of driver "nfs.csi.k8s.io" holds volume`,
		},
		{
			name:     "single-node volume",
			evs:      sessionVolumes("a"),
			pvs:      []*corev1.PersistentVolume{workspacePV(corev1.ReadWriteOnce)},
			wantCode: codes.FailedPrecondition,
			wantMsg:  `PersistentVolume "pv-workspace-1" permits [ReadWriteOnce], not VOLUME_ACCESS_MODE_READ_WRITE_MANY`,
		},
		{
			name:     "read-only-many volume mounted read-write",
			evs:      sessionVolumes("a"),
			pvs:      []*corev1.PersistentVolume{workspacePV(corev1.ReadOnlyMany)},
			wantCode: codes.FailedPrecondition,
			wantMsg:  `not VOLUME_ACCESS_MODE_READ_WRITE_MANY`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateExistingVolumes(context.Background(), registry, pvLister(t, tc.pvs...), workspaceTemplate(), tc.evs)
			if got := status.Code(err); got != tc.wantCode {
				t.Fatalf("validateExistingVolumes code = %v (%v), want %v", got, err, tc.wantCode)
			}
			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("validateExistingVolumes error = %q, want it to contain %q", err, tc.wantMsg)
			}
		})
	}
}

func TestWorkloadSpecExistingVolumes(t *testing.T) {
	t.Run("supplied volumes mount at their sub-paths", func(t *testing.T) {
		actor := &ateapipb.Actor{ExistingVolumes: sessionVolumes("a")}
		spec, err := workloadSpecFromActorTemplate(workspaceTemplate(), actor)
		if err != nil {
			t.Fatalf("workloadSpecFromActorTemplate: %v", err)
		}
		wantVolumes := []*ateletpb.Volume{
			{Name: "tools", Source: &ateletpb.Volume_Image{Image: &ateletpb.ImageVolumeSource{Reference: "tools@sha256:0000000000000000000000000000000000000000000000000000000000000000"}}},
			{Name: "workspace", Source: &ateletpb.Volume_External{External: &ateletpb.ExternalVolumeSource{StorageVolumeId: nfsHandle, VolumeType: nfsDriver, AccessMode: ateletpb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_MANY}}},
			{Name: "mirrors", Source: &ateletpb.Volume_External{External: &ateletpb.ExternalVolumeSource{StorageVolumeId: nfsHandle, VolumeType: nfsDriver, AccessMode: ateletpb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_ONLY_MANY}}},
		}
		if diff := cmp.Diff(wantVolumes, spec.GetVolumes(), protocmp.Transform()); diff != "" {
			t.Errorf("volumes mismatch (-want +got):\n%s", diff)
		}
		wantMounts := []*ateletpb.VolumeMount{
			{Name: "tools", MountPath: "/tools"},
			{Name: "workspace", MountPath: "/workspace", SubPath: "sessions/a"},
			{Name: "mirrors", MountPath: "/mirrors", SubPath: "mirrors", ReadOnly: true},
		}
		if diff := cmp.Diff(wantMounts, spec.GetContainers()[0].GetVolumeMounts(), protocmp.Transform()); diff != "" {
			t.Errorf("mounts mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a mount's sub_path is under the actor's root, and a read-only volume mounts read-only", func(t *testing.T) {
		tmpl := workspaceTemplate()
		tmpl.Containers[0].VolumeMounts[1].SubPath = "src"
		evs := sessionVolumes("a")
		evs[1].SubPath = ""
		spec, err := workloadSpecFromActorTemplate(tmpl, &ateapipb.Actor{ExistingVolumes: evs})
		if err != nil {
			t.Fatalf("workloadSpecFromActorTemplate: %v", err)
		}
		mounts := spec.GetContainers()[0].GetVolumeMounts()
		if got := mounts[1].GetSubPath(); got != "sessions/a/src" {
			t.Errorf("workspace sub_path = %q, want sessions/a/src", got)
		}
		tmpl.Containers[0].VolumeMounts[2].ReadOnly = false
		spec, _ = workloadSpecFromActorTemplate(tmpl, &ateapipb.Actor{ExistingVolumes: evs})
		if m := spec.GetContainers()[0].GetVolumeMounts()[2]; !m.GetReadOnly() || m.GetSubPath() != "" {
			t.Errorf("mirrors mount = %v, want read-only at the volume root", m)
		}
	})

	t.Run("without a reference the request is unchanged", func(t *testing.T) {
		// The same template without its existing volumes and their mounts.
		plain := workspaceTemplate()
		plain.Containers[0].VolumeMounts = plain.Containers[0].VolumeMounts[:1]
		plain.Volumes = plain.Volumes[:1]
		want, err := workloadSpecFromActorTemplate(plain, &ateapipb.Actor{})
		if err != nil {
			t.Fatalf("workloadSpecFromActorTemplate: %v", err)
		}
		for _, actor := range []*ateapipb.Actor{{}, nil} {
			got, err := workloadSpecFromActorTemplate(workspaceTemplate(), actor)
			if err != nil {
				t.Fatalf("workloadSpecFromActorTemplate: %v", err)
			}
			if !proto.Equal(want, got) {
				t.Errorf("spec without references = %v, want %v", got, want)
			}
		}
	})
}

func TestFillExistingVolumeContext(t *testing.T) {
	actor := &ateapipb.Actor{ExistingVolumes: sessionVolumes("a")}
	spec, err := workloadSpecFromActorTemplate(workspaceTemplate(), actor)
	if err != nil {
		t.Fatalf("workloadSpecFromActorTemplate: %v", err)
	}
	if err := fillExistingVolumeContext(spec, pvLister(t, workspacePV(corev1.ReadWriteMany)), actor); err != nil {
		t.Fatalf("fillExistingVolumeContext: %v", err)
	}
	for _, vol := range spec.GetVolumes()[1:] {
		if diff := cmp.Diff(map[string]string{"server": "nfs-server", "share": "/"}, vol.GetExternal().GetVolumeContext()); diff != "" {
			t.Errorf("%s volume context mismatch (-want +got):\n%s", vol.GetName(), diff)
		}
	}

	// A PersistentVolume gone since the create fails the resume with the reason.
	err = fillExistingVolumeContext(spec, pvLister(t), actor)
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "no PersistentVolume") {
		t.Errorf("fillExistingVolumeContext without the PersistentVolume = %v, want FailedPrecondition", err)
	}
}

// attachRecorder records AttachVolume calls.
type attachRecorder struct {
	volume.MockVolumePlugin
	attached []string
	modes    []volume.AccessMode
}

func (a *attachRecorder) AttachVolume(_ context.Context, volumeID, node string, mode volume.AccessMode) error {
	a.attached = append(a.attached, volumeID+"@"+node)
	a.modes = append(a.modes, mode)
	return nil
}

func TestAttachExistingVolumes(t *testing.T) {
	plugin := &attachRecorder{}
	registry := &mockPluginRegistry{plugins: map[string]volume.VolumePluginControlPlane{nfsDriver: plugin}}
	actor := &ateapipb.Actor{ExistingVolumes: sessionVolumes("a")}
	if err := attachExistingVolumes(context.Background(), registry, actor, workspaceTemplate(), "node-1"); err != nil {
		t.Fatalf("attachExistingVolumes: %v", err)
	}
	if diff := cmp.Diff([]string{nfsHandle + "@node-1", nfsHandle + "@node-1"}, plugin.attached); diff != "" {
		t.Errorf("attached mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]volume.AccessMode{volume.ReadWriteMany, volume.ReadOnlyMany}, plugin.modes); diff != "" {
		t.Errorf("modes mismatch (-want +got):\n%s", diff)
	}
}
