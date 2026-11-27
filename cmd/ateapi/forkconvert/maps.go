// Copyright 2026 The Agent Substrate Authors
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

package forkconvert

import (
	"errors"
	"fmt"
	"strings"

	ateapi "go.ate.dev/substrate/api/ateapi/v1"
	"go.ate.dev/substrate/cmd/ateapi/forkconvert/internal/forkpb"
	"go.ate.dev/substrate/internal/resources"
	"go.ate.dev/substrate/pkg/sandboxclass"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"k8s.io/apimachinery/pkg/api/resource"
)

// noTrustBundleNamesCrash is the crash message of an actor whose template
// projected a trust bundle with no names. The read migration of the
// template's row gives the projection its name, so such an actor resumes.
const noTrustBundleNamesCrash = "no trust bundle names given"

// noDurableSnapshotCrash is the crash recorded on a PAUSED actor that has
// no durable copy of any state.
const noDurableSnapshotCrash = "actor has no durable snapshot"

// recode copies src into dst through the wire format. It is only correct for
// messages whose fields have the same numbers and types in both packages, and
// refuses src when dst does not declare one of its fields.
func recode(src, dst proto.Message) error {
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(src)
	if err != nil {
		return err
	}
	if err := proto.Unmarshal(b, dst); err != nil {
		return err
	}
	if hasUnknown(dst.ProtoReflect()) {
		return fmt.Errorf("%s holds fields %s does not declare", src.ProtoReflect().Descriptor().FullName(), dst.ProtoReflect().Descriptor().FullName())
	}
	return nil
}

func recodeAs[T proto.Message](src proto.Message, dst T) (T, error) {
	var zero T
	if src == nil || !src.ProtoReflect().IsValid() {
		return zero, nil
	}
	if err := recode(src, dst); err != nil {
		return zero, err
	}
	return dst, nil
}

// hasUnknown reports whether m or any message it holds has unknown fields.
func hasUnknown(m protoreflect.Message) bool {
	if len(m.GetUnknown()) > 0 {
		return true
	}
	found := false
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.IsList() && fd.Message() != nil:
			l := v.List()
			for i := 0; i < l.Len() && !found; i++ {
				found = hasUnknown(l.Get(i).Message())
			}
		case fd.IsMap() && fd.MapValue().Message() != nil:
			v.Map().Range(func(_ protoreflect.MapKey, mv protoreflect.Value) bool {
				found = hasUnknown(mv.Message())
				return !found
			})
		case fd.Message() != nil && !fd.IsList() && !fd.IsMap():
			found = hasUnknown(v.Message())
		}
		return !found
	})
	return found
}

// sameBytes checks that a row's proto decodes to the same message with the
// 1.x and the target descriptors, so the bytes can be copied as they are.
func sameBytes(b []byte, fork, target proto.Message) error {
	if err := proto.Unmarshal(b, fork); err != nil {
		return fmt.Errorf("decoding as %s: %w", fork.ProtoReflect().Descriptor().FullName(), err)
	}
	if err := proto.Unmarshal(b, target); err != nil {
		return fmt.Errorf("decoding as %s: %w", target.ProtoReflect().Descriptor().FullName(), err)
	}
	if hasUnknown(target.ProtoReflect()) {
		return fmt.Errorf("row holds fields %s does not declare", target.ProtoReflect().Descriptor().FullName())
	}
	opts := proto.MarshalOptions{Deterministic: true}
	fb, err := opts.Marshal(fork)
	if err != nil {
		return err
	}
	tb, err := opts.Marshal(target)
	if err != nil {
		return err
	}
	if string(fb) != string(tb) {
		return fmt.Errorf("%s and %s encode the row differently", fork.ProtoReflect().Descriptor().FullName(), target.ProtoReflect().Descriptor().FullName())
	}
	return nil
}

// convertActorTemplate is map A: a 1.x ActorTemplate as the target stores
// it. Its status is cleared, so the target takes every golden snapshot again.
func convertActorTemplate(f *forkpb.ActorTemplate) (*ateapi.ActorTemplate, error) {
	var errs []error
	t := &ateapi.ActorTemplate{}
	var err error
	if t.Metadata, err = recodeAs(f.GetMetadata(), &ateapi.ResourceMetadata{}); err != nil {
		errs = append(errs, fmt.Errorf("metadata: %w", err))
	}
	if t.WorkerSelector, err = recodeAs(f.GetWorkerSelector(), &ateapi.Selector{}); err != nil {
		errs = append(errs, fmt.Errorf("worker_selector: %w", err))
	}
	for i, fc := range f.GetContainers() {
		c, err := convertContainer(fc)
		if err != nil {
			errs = append(errs, fmt.Errorf("containers[%d]: %w", i, err))
			continue
		}
		t.Containers = append(t.Containers, c)
	}
	for i, fv := range f.GetVolumes() {
		v, err := convertVolume(fv)
		if err != nil {
			errs = append(errs, fmt.Errorf("volumes[%d]: %w", i, err))
			continue
		}
		t.Volumes = append(t.Volumes, v)
	}
	if sc := f.GetSnapshotConfig(); sc != nil {
		t.SnapshotConfig = &ateapi.SnapshotConfig{
			StorageLocation:   sc.GetStorageLocation(),
			PreferredFidelity: preferredFidelity(sc.GetOnCommit()),
		}
	}
	if t.Resources, err = convertResources(f.GetResources()); err != nil {
		errs = append(errs, fmt.Errorf("resources: %w", err))
	}
	if t.GoldenEgressPolicy, err = recodeAs(f.GetDefaultEgressPolicy(), &ateapi.EgressPolicyTemplate{}); err != nil {
		errs = append(errs, fmt.Errorf("default_egress_policy: %w", err))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return t, nil
}

// preferredFidelity maps on_commit: FULL kept memory, DATA only volumes.
func preferredFidelity(onCommit forkpb.SnapshotContentScope) ateapi.SnapshotFidelity {
	if onCommit == forkpb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA {
		return ateapi.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES
	}
	return ateapi.SnapshotFidelity_SNAPSHOT_FIDELITY_MEMORY
}

func convertContainer(f *forkpb.Container) (*ateapi.Container, error) {
	var errs []error
	c := &ateapi.Container{
		Name:    f.GetName(),
		Image:   f.GetImage(),
		Command: f.GetCommand(),
		Args:    f.GetArgs(),
	}
	for _, fe := range f.GetEnv() {
		e, err := recodeAs(fe, &ateapi.EnvVar{})
		if err != nil {
			errs = append(errs, fmt.Errorf("env: %w", err))
			continue
		}
		c.Env = append(c.Env, e)
	}
	var err error
	if c.WakeupProbe, err = recodeAs(f.GetWakeupProbe(), &ateapi.WakeupProbe{}); err != nil {
		errs = append(errs, fmt.Errorf("wakeup_probe: %w", err))
	}
	for _, fm := range f.GetVolumeMounts() {
		m, err := recodeAs(fm, &ateapi.VolumeMount{})
		if err != nil {
			errs = append(errs, fmt.Errorf("volume_mounts: %w", err))
			continue
		}
		c.VolumeMounts = append(c.VolumeMounts, m)
	}
	if c.SecurityContext, err = recodeAs(f.GetSecurityContext(), &ateapi.SecurityContext{}); err != nil {
		errs = append(errs, fmt.Errorf("security_context: %w", err))
	}
	if c.Resources, err = convertResources(f.GetResources()); err != nil {
		errs = append(errs, fmt.Errorf("resources: %w", err))
	}
	return c, errors.Join(errs...)
}

// convertResources turns named quantities into the target's typed limits.
// Only cpu and memory have a target field, so any other name is refused.
func convertResources(f *forkpb.Resources) (*ateapi.ResourceRequirements, error) {
	if f == nil {
		return nil, nil
	}
	limits := &ateapi.Resources{}
	var errs []error
	for _, l := range f.GetLimits() {
		q, err := resource.ParseQuantity(l.GetQuantity())
		if err != nil {
			errs = append(errs, fmt.Errorf("limit %q: %w", l.GetName(), err))
			continue
		}
		switch l.GetName() {
		case "cpu":
			limits.CpuMillicores = q.MilliValue()
		case "memory":
			limits.MemoryBytes = q.Value()
		default:
			errs = append(errs, fmt.Errorf("limit %q has no field in the target API", l.GetName()))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &ateapi.ResourceRequirements{Limits: limits}, nil
}

func convertVolume(f *forkpb.Volume) (*ateapi.Volume, error) {
	v := &ateapi.Volume{Name: f.GetName()}
	if f.GetDurableDir() != nil {
		v.DurableDir = &ateapi.DurableDirVolumeSource{}
	}
	if ev := f.GetExternalVolumeTemplate(); ev != nil {
		tmpl := &ateapi.ExternalVolumeTemplate{StorageClassName: ev.GetStorageClassName()}
		if ev.GetCapacity() != "" {
			q, err := resource.ParseQuantity(ev.GetCapacity())
			if err != nil {
				return nil, fmt.Errorf("external_volume_template.capacity: %w", err)
			}
			tmpl.CapacityBytes = q.Value()
		}
		v.External = &ateapi.ExternalVolumeSource{Template: tmpl}
	}
	if si := f.GetSystemInfo(); si != nil {
		v.SystemInfo = &ateapi.SystemInfoVolumeSource{}
		for _, fds := range si.GetDataSources() {
			ds := &ateapi.SystemInfoDataSource{}
			var err error
			if ds.ActorMetadata, err = recodeAs(fds.GetActorMetadata(), &ateapi.ActorMetadataDataSource{}); err != nil {
				return nil, fmt.Errorf("system_info.actor_metadata: %w", err)
			}
			if tb := fds.GetTrustBundle(); tb != nil {
				ds.TrustBundle = &ateapi.TrustBundleDataSource{Names: tb.GetNames(), Path: tb.GetPath()}
			}
			v.SystemInfo.DataSources = append(v.SystemInfo.DataSources, ds)
		}
	}
	if img := f.GetImage(); img != nil {
		v.Image = &ateapi.ImageVolumeSource{Reference: img.GetReference()}
	}
	if f.GetExistingVolume() != nil {
		v.ExistingVolume = &ateapi.ExistingVolumeSource{}
	}
	return v, nil
}

// sandboxClassOf returns the target sandbox class name of a 1.x template.
func sandboxClassOf(t *forkpb.ActorTemplate) (string, error) {
	switch c := t.GetSandboxConfig().GetSandboxClass(); c {
	case forkpb.SandboxClass_SANDBOX_CLASS_GVISOR:
		return sandboxclass.GVisor, nil
	case forkpb.SandboxClass_SANDBOX_CLASS_MICROVM:
		return sandboxclass.MicroVM, nil
	default:
		return "", fmt.Errorf("sandbox_config.sandbox_class %v is not a sandbox class", c)
	}
}

// convertSnapshot is the one durable snapshot the target records for a 1.x
// external snapshot: at VOLUMES fidelity and committed, at the same URI.
func convertSnapshot(es *forkpb.ExternalSnapshot, owner ateapi.SnapshotOwner, class string) (*ateapi.Snapshot, error) {
	uri, err := resources.ParseSnapshotURI(es.GetSnapshotUri())
	if err != nil {
		return nil, err
	}
	snap := &ateapi.Snapshot{
		Uuid:             uri.Name(),
		Generation:       1,
		Owner:            owner,
		Fidelity:         ateapi.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES,
		ActorTemplateUid: es.GetActorTemplateUid(),
		DurableSnapshot: &ateapi.SnapshotStorage{
			State:  ateapi.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED,
			Object: &ateapi.ObjectSnapshot{SnapshotUri: es.GetSnapshotUri()},
		},
	}
	if class != "" {
		snap.SandboxRuntime = &ateapi.ActorSandboxRuntime{SandboxClass: class}
	}
	return snap, nil
}

// convertTag is map B. class is the sandbox class of the template the tag's
// snapshot was built on, or "" when that template is gone.
func convertTag(f *forkpb.Tag, class string) (*ateapi.Tag, error) {
	t := &ateapi.Tag{Scope: ateapi.TagScope(f.GetScope())}
	var err error
	if t.Metadata, err = recodeAs(f.GetMetadata(), &ateapi.ResourceMetadata{}); err != nil {
		return nil, fmt.Errorf("metadata: %w", err)
	}
	if t.SourceActor, err = recodeAs(f.GetSourceActor(), &ateapi.ObjectRef{}); err != nil {
		return nil, fmt.Errorf("source_actor: %w", err)
	}
	if st := f.GetStatus(); st != nil {
		t.Status = &ateapi.TagStatus{ActorTemplateUid: st.GetActorTemplateUid()}
		if es := st.GetSnapshot(); es != nil {
			uri, err := resources.ParseSnapshotURI(es.GetSnapshotUri())
			if err != nil {
				return nil, fmt.Errorf("status.snapshot: %w", err)
			}
			if !uri.OwnedBy(resources.TagSnapshotOwner(f.GetMetadata().GetAtespace(), f.GetMetadata().GetUid())) {
				return nil, errors.New("status.snapshot is not under the tag's own prefix")
			}
			if t.Status.Snapshot, err = convertSnapshot(es, ateapi.SnapshotOwner_SNAPSHOT_OWNER_TAG, class); err != nil {
				return nil, fmt.Errorf("status.snapshot: %w", err)
			}
		}
	}
	return t, nil
}

// actorSnapshot returns the 1.x durable snapshot a converted actor resumes
// from, and the target state it is in. A PAUSED actor resumes from the
// durable copy of its pause. One whose pause was not uploaded yet would lose
// that state for an older one, so it is refused; one with no durable state
// at all crashes.
func actorSnapshot(st *forkpb.ActorStatus) (*forkpb.ExternalSnapshot, ateapi.ActorState, *ateapi.ActorCrash, error) {
	switch st.GetState() {
	case forkpb.ActorState_ACTOR_STATE_SUSPENDED:
		return st.GetExternalSnapshot(), ateapi.ActorState_ACTOR_STATE_SUSPENDED, nil, nil
	case forkpb.ActorState_ACTOR_STATE_CRASHED:
		crash, err := recodeAs(st.GetCrash(), &ateapi.ActorCrash{})
		if err != nil {
			return nil, 0, nil, fmt.Errorf("status.crash: %w", err)
		}
		if strings.Contains(st.GetCrash().GetMessage(), noTrustBundleNamesCrash) {
			return st.GetExternalSnapshot(), ateapi.ActorState_ACTOR_STATE_SUSPENDED, nil, nil
		}
		return st.GetExternalSnapshot(), ateapi.ActorState_ACTOR_STATE_CRASHED, crash, nil
	case forkpb.ActorState_ACTOR_STATE_PAUSED:
		ls := st.GetLocalSnapshot()
		if dc := ls.GetDurableCopy(); dc.GetSnapshotUri() != "" {
			return dc, ateapi.ActorState_ACTOR_STATE_SUSPENDED, nil, nil
		}
		if st.GetExternalSnapshot().GetSnapshotUri() != "" {
			return nil, 0, nil, errors.New("PAUSED with a pause that was not uploaded: its last durable snapshot is older than its state; resume and suspend it on the 1.x release first")
		}
		return nil, ateapi.ActorState_ACTOR_STATE_CRASHED, &ateapi.ActorCrash{Message: noDurableSnapshotCrash}, nil
	default:
		return nil, 0, nil, fmt.Errorf("state %v: only SUSPENDED, CRASHED and PAUSED actors convert", st.GetState())
	}
}

// convertActor is map C. class is the sandbox class of the actor's template.
func convertActor(f *forkpb.Actor, class string) (*ateapi.Actor, error) {
	a := &ateapi.Actor{}
	var err error
	if a.Metadata, err = recodeAs(f.GetMetadata(), &ateapi.ResourceMetadata{}); err != nil {
		return nil, fmt.Errorf("metadata: %w", err)
	}
	if a.ActorTemplate, err = recodeAs(f.GetActorTemplate(), &ateapi.ObjectRef{}); err != nil {
		return nil, fmt.Errorf("actor_template: %w", err)
	}
	if a.WorkerSelector, err = recodeAs(f.GetWorkerSelector(), &ateapi.Selector{}); err != nil {
		return nil, fmt.Errorf("worker_selector: %w", err)
	}
	if a.SourceTag, err = recodeAs(f.GetSourceTag(), &ateapi.ObjectRef{}); err != nil {
		return nil, fmt.Errorf("source_tag: %w", err)
	}
	for _, fe := range f.GetExistingVolumes() {
		ev, err := recodeAs(fe, &ateapi.ExistingVolume{})
		if err != nil {
			return nil, fmt.Errorf("existing_volumes: %w", err)
		}
		a.ExistingVolumes = append(a.ExistingVolumes, ev)
	}

	fst := f.GetStatus()
	es, state, crash, err := actorSnapshot(fst)
	if err != nil {
		return nil, err
	}
	st := &ateapi.ActorStatus{State: state, Crash: crash}
	if st.FencingToken, err = recodeAs(fst.GetFencingToken(), &ateapi.FencingToken{}); err != nil {
		return nil, fmt.Errorf("status.fencing_token: %w", err)
	}
	if vols := fst.GetActorVolumes(); len(vols) > 0 {
		st.ExternalVolumes = &ateapi.ExternalVolumes{}
		for _, v := range vols {
			switch v.GetStatus() {
			case forkpb.ExternalVolume_STATUS_PENDING, forkpb.ExternalVolume_STATUS_DELETING:
				return nil, fmt.Errorf("external volume %q is %v", v.GetVolumeName(), v.GetStatus())
			}
			st.ExternalVolumes.Volumes = append(st.ExternalVolumes.Volumes, &ateapi.ExternalVolumeStatus{
				Name:            v.GetVolumeName(),
				StorageVolumeId: v.GetStorageVolumeId(),
				VolumeType:      v.GetVolumeType(),
				VolumeContext:   v.GetVolumeContext(),
			})
		}
	}
	if es.GetSnapshotUri() != "" {
		owner, err := snapshotOwner(es.GetSnapshotUri(), f.GetMetadata())
		if err != nil {
			return nil, err
		}
		snap, err := convertSnapshot(es, owner, class)
		if err != nil {
			return nil, fmt.Errorf("snapshot: %w", err)
		}
		st.Snapshots = []*ateapi.Snapshot{snap}
		st.LastAssignedGeneration = 1
	}
	a.Status = st
	return a, nil
}

// snapshotOwner returns who owns the objects of an actor's snapshot: the
// actor, or a tag whose snapshot the actor borrows.
func snapshotOwner(snapshotURI string, meta *forkpb.ResourceMetadata) (ateapi.SnapshotOwner, error) {
	uri, err := resources.ParseSnapshotURI(snapshotURI)
	if err != nil {
		return 0, err
	}
	switch {
	case uri.OwnedBy(resources.ActorSnapshotOwner(meta.GetAtespace(), meta.GetUid())):
		return ateapi.SnapshotOwner_SNAPSHOT_OWNER_ACTOR, nil
	case uri.OwnedBy(resources.TagSnapshotOwner(uri.Atespace(), uri.Name())):
		return ateapi.SnapshotOwner_SNAPSHOT_OWNER_TAG, nil
	default:
		return 0, errors.New("snapshot is under another actor's prefix")
	}
}
