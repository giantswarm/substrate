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

package atepg

import (
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Fields older releases wrote into an Actor row that ateapi.proto no longer
// declares. A decoded row keeps them as unknown fields until
// migrateLegacyActor has read them.
const (
	// ActorStatus.current_actor_template_uid up to the 1.3 line: the template
	// the actor's guest state was built on, which ExternalSnapshot carries in
	// actor_template_uid since.
	legacyActorStatusBuiltOnTemplateUID protowire.Number = 11
	// ExternalSnapshot.source_local_snapshot_name as 1.4.0 to 1.5.0-rc.1
	// numbered it.
	legacyExternalSnapshotSourceLocalSnapshotName protowire.Number = 4
	// LocalSnapshot.node_vms_with_local_snapshots up to the 1.8 line: the
	// nodes holding the local snapshot, of which a pause records one.
	legacyLocalSnapshotNodes protowire.Number = 2
	// LocalSnapshot.content_scope as the 1.8 line numbered it.
	legacyLocalSnapshotContentScope protowire.Number = 3
)

// migrateLegacyActor rewrites the snapshot records an older release wrote
// into the shape this release reads. The 1.3 line numbered
// source_local_snapshot_name 3, the number of actor_template_uid since, and
// kept the template the guest state was built on on the actor's status
// instead of on its snapshots. Read as written, such a record names a local
// snapshot as its template, so a resume takes the template for replaced, a
// tag of the actor is refused, and the durable pause copy is not recognized
// and uploaded again. The rewritten record is persisted by the actor's next
// update.
func migrateLegacyActor(actor *ateapipb.Actor) {
	st := actor.GetStatus()
	if st == nil {
		return
	}
	builtOn := unknownString(st.ProtoReflect(), legacyActorStatusBuiltOnTemplateUID)
	migrateLegacyExternalSnapshot(st.GetExternalSnapshot(), builtOn)
	migrateLegacyExternalSnapshot(st.GetLocalSnapshot().GetDurableCopy(), builtOn)
	migrateLegacyLocalSnapshot(st.GetLocalSnapshot())
	migrateLegacyAssignedNode(st)
	migrateStaleCrash(st)
}

// migrateLegacyLocalSnapshot rewrites a local snapshot the 1.8 line recorded:
// its node was field 2, a list holding the one node the pause ran on, and its
// content scope field 3. Read as written, the snapshot has no node, so a
// resume cannot find the worker holding it, and no content scope, so a DATA
// snapshot would be restored as a FULL one.
func migrateLegacyLocalSnapshot(ls *ateapipb.LocalSnapshot) {
	if ls == nil {
		return
	}
	if ls.NodeName == "" {
		ls.NodeName = firstUnknownString(ls.ProtoReflect(), legacyLocalSnapshotNodes)
	}
	if ls.ContentScope == ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_UNSPECIFIED {
		if v, ok := unknownVarint(ls.ProtoReflect(), legacyLocalSnapshotContentScope); ok {
			ls.ContentScope = ateapipb.SnapshotContentScope(v)
		}
	}
}

// migrateLegacyAssignedNode records the node an actor of the 1.8 line is
// attached to, which that line kept only on the worker assignment and the
// local snapshot: the node of its worker, else the node holding its local
// snapshot. Without it a running actor's pause crashes it for an unknown node,
// and a delete or crash leaves its sandbox running.
func migrateLegacyAssignedNode(st *ateapipb.ActorStatus) {
	if st.AssignedNode != "" {
		return
	}
	if node := st.GetWorkerAssignment().GetNodeName(); node != "" {
		st.AssignedNode = node
		return
	}
	st.AssignedNode = st.GetLocalSnapshot().GetNodeName()
}

func migrateLegacyExternalSnapshot(s *ateapipb.ExternalSnapshot, builtOn string) {
	if s == nil {
		return
	}
	if s.SourceLocalSnapshotName == "" {
		s.SourceLocalSnapshotName = unknownString(s.ProtoReflect(), legacyExternalSnapshotSourceLocalSnapshotName)
	}
	// A pause copy is stored under the name of the local snapshot it was
	// uploaded from. Both that name and a template UID are UUIDs, so only the
	// URI tells a 1.3 record's source name from a template UID.
	if s.SourceLocalSnapshotName == "" && s.ActorTemplateUid != "" && snapshotURIName(s.GetSnapshotUri()) == s.ActorTemplateUid {
		s.SourceLocalSnapshotName, s.ActorTemplateUid = s.ActorTemplateUid, ""
	}
	if s.ActorTemplateUid == "" {
		s.ActorTemplateUid = builtOn
	}
}

func snapshotURIName(uri string) string {
	parsed, err := resources.ParseSnapshotURI(uri)
	if err != nil {
		return ""
	}
	return parsed.Name()
}

// unknownString returns the last value of a length-delimited unknown field of
// m, or "" if m holds none.
func unknownString(m protoreflect.Message, num protowire.Number) string {
	var value string
	for b := m.GetUnknown(); len(b) > 0; {
		n, typ, l := protowire.ConsumeTag(b)
		if l < 0 {
			return value
		}
		b = b[l:]
		if n == num && typ == protowire.BytesType {
			v, vl := protowire.ConsumeBytes(b)
			if vl < 0 {
				return value
			}
			value = string(v)
		}
		l = protowire.ConsumeFieldValue(n, typ, b)
		if l < 0 {
			return value
		}
		b = b[l:]
	}
	return value
}

// firstUnknownString returns the first value of a length-delimited unknown
// field of m, or "" if m holds none.
func firstUnknownString(m protoreflect.Message, num protowire.Number) string {
	for b := m.GetUnknown(); len(b) > 0; {
		n, typ, l := protowire.ConsumeTag(b)
		if l < 0 {
			return ""
		}
		b = b[l:]
		if n == num && typ == protowire.BytesType {
			v, vl := protowire.ConsumeBytes(b)
			if vl < 0 {
				return ""
			}
			return string(v)
		}
		l = protowire.ConsumeFieldValue(n, typ, b)
		if l < 0 {
			return ""
		}
		b = b[l:]
	}
	return ""
}

// unknownVarint returns the last value of a varint unknown field of m, and
// whether m holds one.
func unknownVarint(m protoreflect.Message, num protowire.Number) (int32, bool) {
	var value int32
	found := false
	for b := m.GetUnknown(); len(b) > 0; {
		n, typ, l := protowire.ConsumeTag(b)
		if l < 0 {
			return value, found
		}
		b = b[l:]
		if n == num && typ == protowire.VarintType {
			v, vl := protowire.ConsumeVarint(b)
			if vl < 0 {
				return value, found
			}
			value, found = int32(v), true
		}
		l = protowire.ConsumeFieldValue(n, typ, b)
		if l < 0 {
			return value, found
		}
		b = b[l:]
	}
	return value, found
}
