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
	"go.ate.dev/substrate/cmd/ateapi/forkconvert/internal/forkpb"
	"go.ate.dev/substrate/internal/resources"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The 1.x store rewrote rows of older releases when it read them. These are
// the fields those rows hold that forkpb no longer declares; a row decoded
// with forkpb keeps them as unknown fields until the migrate functions below
// have read them.
const (
	// ActorStatus.current_actor_template_uid up to the 1.3 line.
	legacyActorStatusBuiltOnTemplateUID protowire.Number = 11
	// ExternalSnapshot.source_local_snapshot_name as 1.4.0 to 1.5.0-rc.1
	// numbered it.
	legacyExternalSnapshotSourceLocalSnapshotName protowire.Number = 4
	// LocalSnapshot.node_vms_with_local_snapshots up to the 1.8 line.
	legacyLocalSnapshotNodes protowire.Number = 2
	// LocalSnapshot.content_scope as the 1.8 line numbered it.
	legacyLocalSnapshotContentScope protowire.Number = 3
	// TrustBundleDataSource.name before the repeated names.
	legacyTrustBundleDataSourceName protowire.Number = 1
)

// legacyTrustBundleName is the only trust bundle a projection written before
// the repeated names could name.
const legacyTrustBundleName = "egress-mitm.ate.dev"

// decodeActor decodes a 1.x actors row the way the 1.x store read it: the
// legacy fields are migrated, then every unknown field is dropped.
func decodeActor(b []byte) (*forkpb.Actor, error) {
	a := &forkpb.Actor{}
	if err := proto.Unmarshal(b, a); err != nil {
		return nil, err
	}
	migrateLegacyActor(a)
	discardUnknown(a.ProtoReflect())
	return a, nil
}

// decodeActorTemplate decodes a 1.x actor_templates row the way the 1.x
// store read it, defaults included.
func decodeActorTemplate(b []byte) (*forkpb.ActorTemplate, error) {
	t := &forkpb.ActorTemplate{}
	if err := proto.Unmarshal(b, t); err != nil {
		return nil, err
	}
	migrateLegacyActorTemplate(t)
	discardUnknown(t.ProtoReflect())
	applyActorTemplateDefaults(t)
	return t, nil
}

// decodeTag decodes a 1.x tags row.
func decodeTag(b []byte) (*forkpb.Tag, error) {
	t := &forkpb.Tag{}
	if err := proto.Unmarshal(b, t); err != nil {
		return nil, err
	}
	discardUnknown(t.ProtoReflect())
	return t, nil
}

// migrateLegacyActor rewrites the snapshot records of releases before 1.9
// into the 1.x shape: the 1.3 source_local_snapshot_name in field 3 and the
// template UID on the status, the 1.4 source_local_snapshot_name in field 4,
// and the 1.8 local snapshot fields. It also clears a crash kept on an actor
// that is not CRASHED.
func migrateLegacyActor(actor *forkpb.Actor) {
	st := actor.GetStatus()
	if st == nil {
		return
	}
	builtOn := unknownString(st.ProtoReflect(), legacyActorStatusBuiltOnTemplateUID)
	migrateLegacyExternalSnapshot(st.GetExternalSnapshot(), builtOn)
	migrateLegacyExternalSnapshot(st.GetLocalSnapshot().GetDurableCopy(), builtOn)
	migrateLegacyLocalSnapshot(st.GetLocalSnapshot())
	if st.AssignedNode == "" {
		if node := st.GetWorkerAssignment().GetNodeName(); node != "" {
			st.AssignedNode = node
		} else {
			st.AssignedNode = st.GetLocalSnapshot().GetNodeName()
		}
	}
	if st.GetState() != forkpb.ActorState_ACTOR_STATE_CRASHED {
		st.Crash = nil
	}
}

func migrateLegacyLocalSnapshot(ls *forkpb.LocalSnapshot) {
	if ls == nil {
		return
	}
	if ls.NodeName == "" {
		ls.NodeName = firstUnknownString(ls.ProtoReflect(), legacyLocalSnapshotNodes)
	}
	if ls.ContentScope == forkpb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_UNSPECIFIED {
		if v, ok := unknownVarint(ls.ProtoReflect(), legacyLocalSnapshotContentScope); ok {
			ls.ContentScope = forkpb.SnapshotContentScope(v)
		}
	}
}

func migrateLegacyExternalSnapshot(s *forkpb.ExternalSnapshot, builtOn string) {
	if s == nil {
		return
	}
	if s.SourceLocalSnapshotName == "" {
		s.SourceLocalSnapshotName = unknownString(s.ProtoReflect(), legacyExternalSnapshotSourceLocalSnapshotName)
	}
	// A 1.3 pause copy is stored under the name of the local snapshot it was
	// uploaded from, which that release kept in field 3. Both are UUIDs, so
	// only the URI tells the two apart.
	if s.SourceLocalSnapshotName == "" && s.ActorTemplateUid != "" && snapshotURIName(s.GetSnapshotUri()) == s.ActorTemplateUid {
		s.SourceLocalSnapshotName, s.ActorTemplateUid = s.ActorTemplateUid, ""
	}
	if s.ActorTemplateUid == "" {
		s.ActorTemplateUid = builtOn
	}
}

// migrateLegacyActorTemplate gives a trust-bundle projection without names
// the one name it held before the repeated names: its legacy field 1, or
// legacyTrustBundleName once an update has dropped that field.
func migrateLegacyActorTemplate(template *forkpb.ActorTemplate) {
	for _, vol := range template.GetVolumes() {
		for _, ds := range vol.GetSystemInfo().GetDataSources() {
			tb := ds.GetTrustBundle()
			if tb == nil || len(tb.GetNames()) > 0 {
				continue
			}
			name := unknownString(tb.ProtoReflect(), legacyTrustBundleDataSourceName)
			if name == "" {
				name = legacyTrustBundleName
			}
			tb.Names = []string{name}
		}
	}
}

// applyActorTemplateDefaults applies the 1.x read defaults that change what
// a template means: an unset on_commit is FULL.
func applyActorTemplateDefaults(t *forkpb.ActorTemplate) {
	if sc := t.GetSnapshotConfig(); sc != nil && sc.OnCommit == forkpb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_UNSPECIFIED {
		sc.OnCommit = forkpb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL
	}
}

func snapshotURIName(uri string) string {
	parsed, err := resources.ParseSnapshotURI(uri)
	if err != nil {
		return ""
	}
	return parsed.Name()
}

// discardUnknown drops the unknown fields of m and of every message it holds.
func discardUnknown(m protoreflect.Message) {
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.IsList() && fd.Message() != nil:
			l := v.List()
			for i := 0; i < l.Len(); i++ {
				discardUnknown(l.Get(i).Message())
			}
		case fd.IsMap() && fd.MapValue().Message() != nil:
			v.Map().Range(func(_ protoreflect.MapKey, mv protoreflect.Value) bool {
				discardUnknown(mv.Message())
				return true
			})
		case fd.Message() != nil && !fd.IsList() && !fd.IsMap():
			discardUnknown(v.Message())
		}
		return true
	})
	if m.GetUnknown() != nil {
		m.SetUnknown(nil)
	}
}

// unknownString returns the last value of a length-delimited unknown field of
// m, or "" if m holds none.
func unknownString(m protoreflect.Message, num protowire.Number) string {
	var value string
	forEachUnknown(m, func(n protowire.Number, typ protowire.Type, b []byte) bool {
		if n == num && typ == protowire.BytesType {
			if v, l := protowire.ConsumeBytes(b); l >= 0 {
				value = string(v)
			}
		}
		return true
	})
	return value
}

// firstUnknownString returns the first value of a length-delimited unknown
// field of m, or "" if m holds none.
func firstUnknownString(m protoreflect.Message, num protowire.Number) string {
	var value string
	forEachUnknown(m, func(n protowire.Number, typ protowire.Type, b []byte) bool {
		if n == num && typ == protowire.BytesType {
			if v, l := protowire.ConsumeBytes(b); l >= 0 {
				value = string(v)
				return false
			}
		}
		return true
	})
	return value
}

// unknownVarint returns the last value of a varint unknown field of m, and
// whether m holds one.
func unknownVarint(m protoreflect.Message, num protowire.Number) (int32, bool) {
	var value int32
	found := false
	forEachUnknown(m, func(n protowire.Number, typ protowire.Type, b []byte) bool {
		if n == num && typ == protowire.VarintType {
			if v, l := protowire.ConsumeVarint(b); l >= 0 {
				value, found = int32(v), true
			}
		}
		return true
	})
	return value, found
}

// forEachUnknown calls f with each unknown field of m and the bytes from its
// value on, until f returns false or the fields do not parse.
func forEachUnknown(m protoreflect.Message, f func(protowire.Number, protowire.Type, []byte) bool) {
	for b := m.GetUnknown(); len(b) > 0; {
		n, typ, l := protowire.ConsumeTag(b)
		if l < 0 {
			return
		}
		b = b[l:]
		if !f(n, typ, b) {
			return
		}
		l = protowire.ConsumeFieldValue(n, typ, b)
		if l < 0 {
			return
		}
		b = b[l:]
	}
}
