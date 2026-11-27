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

// Package forkconvert converts the PostgreSQL rows of a Substrate 1.x (Giant
// Swarm line) database into the rows Substrate 2.x reads, from one schema
// into another, and lists the snapshots whose objects need converting.
package forkconvert

import (
	"fmt"
	"slices"
	"sort"

	ateapi "go.ate.dev/substrate/api/ateapi/v1"
	"go.ate.dev/substrate/cmd/ateapi/forkconvert/internal/forkpb"
	"go.ate.dev/substrate/pkg/sandboxclass"
	"google.golang.org/protobuf/proto"
)

// goldenActorAtespace is the atespace the 1.x line ran golden actors in.
const goldenActorAtespace = "ate-golden"

// Row is one row of a table holding a proto. Atespace and Name are the
// table's key columns: (atespace, name) for actors, templates and tags,
// (atespace, actor_name) for egress policies, name alone for atespaces,
// atespace_name alone for atespace access policies, neither for the global
// access policy.
type Row struct {
	Atespace string
	Name     string
	UID      string
	Version  int64
	Proto    []byte
}

func (r Row) key() string {
	if r.Atespace == "" {
		return r.Name
	}
	return r.Atespace + "/" + r.Name
}

// Table is a table copied as it is.
type Table struct {
	Columns []string
	Rows    [][]any
}

// Source is what the 1.x schema holds.
type Source struct {
	Atespaces        []Row
	ActorTemplates   []Row
	Tags             []Row
	Actors           []Row
	EgressPolicies   []Row
	GlobalPolicy     []Row
	AtespacePolicies []Row
	// OpenFGA holds the OpenFGA tables by name.
	OpenFGA map[string]*Table
}

// openFGATables are the OpenFGA tables, copied verbatim.
var openFGATables = []string{"store", "authorization_model", "assertion", "tuple", "changelog"}

// Mount is one durable-dir volume mount of a container.
type Mount struct {
	Volume string `json:"volume"`
	Path   string `json:"path"`
}

// ContainerMounts are the durable-dir volume mounts of one container of the
// template a snapshot is restored into, in the template's order.
type ContainerMounts struct {
	Name   string  `json:"name"`
	Mounts []Mount `json:"mounts"`
}

// SnapshotJob is one snapshot whose objects need converting: every
// converted actor or tag that records it, and the container layout of the
// template they restore into.
type SnapshotJob struct {
	URI        string
	Holders    []string
	Containers []ContainerMounts
}

// TableCount is how many rows a table had and how many convert.
type TableCount struct {
	Table  string
	Source int
	Target int
}

// Plan is the conversion of a Source: the target rows, the snapshots to
// convert, and every refusal. Nothing may be written while Refusals is not
// empty.
type Plan struct {
	Atespaces        []Row
	ActorTemplates   []Row
	Tags             []Row
	Actors           []Row
	EgressPolicies   []Row
	GlobalPolicy     []Row
	AtespacePolicies []Row
	OpenFGA          map[string]*Table

	Snapshots []SnapshotJob
	Counts    []TableCount
	// Refusals name each row that does not convert, and why.
	Refusals []string
	// Notes name each row that converts with a change a reader must know of.
	Notes []string
}

func (p *Plan) refuse(format string, args ...any) {
	p.Refusals = append(p.Refusals, fmt.Sprintf(format, args...))
}

func (p *Plan) note(format string, args ...any) {
	p.Notes = append(p.Notes, fmt.Sprintf(format, args...))
}

// Build converts src. It reads nothing else and writes nothing.
func Build(src *Source) *Plan {
	p := &Plan{OpenFGA: src.OpenFGA}
	b := &builder{plan: p, templatesByUID: map[string]*forkpb.ActorTemplate{}, templatesByRef: map[string]*forkpb.ActorTemplate{}, targetTemplates: map[string]*ateapi.ActorTemplate{}, goldenTags: map[string]bool{}, snapshots: map[string]*SnapshotJob{}, snapshotOwners: map[string]*ateapi.ActorTemplate{}, refusedSnapshots: map[string]bool{}}
	b.copyRows("atespaces", src.Atespaces, &p.Atespaces, &forkpb.Atespace{}, &ateapi.Atespace{})
	b.copyRows("global_access_policy", src.GlobalPolicy, &p.GlobalPolicy, &forkpb.AccessPolicy{}, &ateapi.AccessPolicy{})
	b.copyRows("atespace_access_policies", src.AtespacePolicies, &p.AtespacePolicies, &forkpb.AccessPolicy{}, &ateapi.AccessPolicy{})
	b.templates(src.ActorTemplates)
	b.tags(src.Tags)
	dropped := b.actors(src.Actors)
	var policies []Row
	for _, r := range src.EgressPolicies {
		if !dropped[r.key()] {
			policies = append(policies, r)
		}
	}
	b.copyRows("actor_egress_policies", policies, &p.EgressPolicies, &forkpb.EgressPolicy{}, &ateapi.EgressPolicy{})
	if n := len(src.EgressPolicies) - len(policies); n > 0 {
		p.note("actor_egress_policies: %d rows of dropped golden actors not converted", n)
		p.Counts[len(p.Counts)-1].Source = len(src.EgressPolicies)
	}
	for _, name := range openFGATables {
		n := 0
		if t := src.OpenFGA[name]; t != nil {
			n = len(t.Rows)
		}
		p.Counts = append(p.Counts, TableCount{Table: name, Source: n, Target: n})
	}
	b.finishSnapshots()
	return p
}

type builder struct {
	plan            *Plan
	templatesByUID  map[string]*forkpb.ActorTemplate
	templatesByRef  map[string]*forkpb.ActorTemplate
	targetTemplates map[string]*ateapi.ActorTemplate
	goldenTags      map[string]bool
	snapshots       map[string]*SnapshotJob
	snapshotOwners  map[string]*ateapi.ActorTemplate
	// refusedSnapshots are the snapshots already refused.
	refusedSnapshots map[string]bool
}

// copyRows copies the rows of a table whose message has the same fields in
// both APIs, once each row decodes to the same message with both.
func (b *builder) copyRows(table string, rows []Row, dst *[]Row, fork, target proto.Message) {
	for _, r := range rows {
		if err := sameBytes(r.Proto, proto.Clone(fork), proto.Clone(target)); err != nil {
			b.plan.refuse("%s %s: %v", table, r.key(), err)
			continue
		}
		*dst = append(*dst, r)
	}
	b.plan.Counts = append(b.plan.Counts, TableCount{Table: table, Source: len(rows), Target: len(*dst)})
}

func (b *builder) templates(rows []Row) {
	for _, r := range rows {
		f, err := decodeActorTemplate(r.Proto)
		if err != nil {
			b.plan.refuse("actor_templates %s: %v", r.key(), err)
			continue
		}
		b.templatesByUID[r.UID] = f
		b.templatesByRef[r.key()] = f
		if g := f.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag(); g.GetName() != "" {
			b.goldenTags[g.GetAtespace()+"/"+g.GetName()] = true
		}
		t, err := convertActorTemplate(f)
		if err != nil {
			b.plan.refuse("actor_templates %s: %v", r.key(), err)
			continue
		}
		if _, err := sandboxClassOf(f); err != nil {
			b.plan.refuse("actor_templates %s: %v", r.key(), err)
			continue
		}
		if l := t.GetResources().GetLimits(); l.GetCpuMillicores() <= 0 || l.GetMemoryBytes() <= 0 {
			b.plan.note("actor_templates %s: resources lack a positive cpu or memory limit; the 2.x API refuses its next update until they are set", r.key())
		}
		b.targetTemplates[r.key()] = t
		b.plan.ActorTemplates = append(b.plan.ActorTemplates, b.row(r, t))
	}
	b.plan.Counts = append(b.plan.Counts, TableCount{Table: "actor_templates", Source: len(rows), Target: len(b.plan.ActorTemplates)})
	if len(b.plan.ActorTemplates) > 0 {
		b.plan.note("actor_templates: status cleared on %d templates, so 2.x takes their golden snapshots again", len(b.plan.ActorTemplates))
	}
}

// row is r with its proto replaced by m's encoding.
func (b *builder) row(r Row, m proto.Message) Row {
	out, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		b.plan.refuse("%s: encoding: %v", r.key(), err)
	}
	r.Proto = out
	return r
}

func (b *builder) tags(rows []Row) {
	for _, r := range rows {
		if b.goldenTags[r.key()] {
			b.plan.note("tags %s: golden tag not converted; 2.x takes the golden snapshot again", r.key())
			continue
		}
		f, err := decodeTag(r.Proto)
		if err != nil {
			b.plan.refuse("tags %s: %v", r.key(), err)
			continue
		}
		tmplUID := f.GetStatus().GetActorTemplateUid()
		if tmplUID == "" {
			tmplUID = f.GetStatus().GetSnapshot().GetActorTemplateUid()
		}
		class := ""
		if tmpl := b.templatesByUID[tmplUID]; tmpl != nil {
			class, _ = sandboxClassOf(tmpl)
		}
		t, err := convertTag(f, class)
		if err != nil {
			b.plan.refuse("tags %s: %v", r.key(), err)
			continue
		}
		if t.GetStatus().GetSnapshot() == nil {
			b.plan.note("tags %s: has no snapshot", r.key())
		} else {
			tmpl := b.templatesByUID[tmplUID]
			var target *ateapi.ActorTemplate
			if tmpl != nil {
				target = b.targetTemplates[tmpl.GetMetadata().GetAtespace()+"/"+tmpl.GetMetadata().GetName()]
			}
			b.addSnapshot(t.GetStatus().GetSnapshot(), "tag "+r.key(), target, true)
		}
		b.plan.Tags = append(b.plan.Tags, b.row(r, t))
	}
	b.plan.Counts = append(b.plan.Counts, TableCount{Table: "tags", Source: len(rows), Target: len(b.plan.Tags)})
}

// actors converts the actors and returns the keys of the golden actors it
// drops.
func (b *builder) actors(rows []Row) map[string]bool {
	dropped := map[string]bool{}
	for _, r := range rows {
		f, err := decodeActor(r.Proto)
		if err != nil {
			b.plan.refuse("actors %s: %v", r.key(), err)
			continue
		}
		state := f.GetStatus().GetState()
		switch state {
		case forkpb.ActorState_ACTOR_STATE_SUSPENDED, forkpb.ActorState_ACTOR_STATE_CRASHED, forkpb.ActorState_ACTOR_STATE_PAUSED:
		default:
			b.plan.refuse("actors %s: state %v; only SUSPENDED, CRASHED and PAUSED actors convert, so suspend it on the 1.x release first", r.key(), state)
			continue
		}
		if r.Atespace == goldenActorAtespace && b.templatesByUID[r.Name] != nil {
			b.plan.note("actors %s: golden actor not converted", r.key())
			dropped[r.key()] = true
			continue
		}
		ref := f.GetActorTemplate().GetAtespace() + "/" + f.GetActorTemplate().GetName()
		tmpl, target := b.templatesByRef[ref], b.targetTemplates[ref]
		if tmpl == nil || target == nil {
			b.plan.refuse("actors %s: its ActorTemplate %s is missing or does not convert", r.key(), ref)
			continue
		}
		class, _ := sandboxClassOf(tmpl)
		a, err := convertActor(f, class)
		if err != nil {
			b.plan.refuse("actors %s: %v", r.key(), err)
			continue
		}
		if state != forkpb.ActorState_ACTOR_STATE_SUSPENDED || a.GetStatus().GetState() != ateapi.ActorState_ACTOR_STATE_SUSPENDED {
			b.plan.note("actors %s: %v -> %v", r.key(), state, a.GetStatus().GetState())
		}
		for _, snap := range a.GetStatus().GetSnapshots() {
			b.addSnapshot(snap, "actor "+r.key(), target, false)
		}
		b.plan.Actors = append(b.plan.Actors, b.row(r, a))
	}
	b.plan.Counts = append(b.plan.Counts, TableCount{Table: "actors", Source: len(rows), Target: len(b.plan.Actors)})
	return dropped
}

// addSnapshot records that holder restores snap into target. A tag records
// the template its snapshot was built on, which an actor that borrows the
// snapshot overrides with the template it resumes on.
func (b *builder) addSnapshot(snap *ateapi.Snapshot, holder string, target *ateapi.ActorTemplate, isTag bool) {
	uri := snap.GetDurableSnapshot().GetObject().GetSnapshotUri()
	job := b.snapshots[uri]
	if job == nil {
		job = &SnapshotJob{URI: uri}
		b.snapshots[uri] = job
	}
	job.Holders = append(job.Holders, holder)
	if snap.GetSandboxRuntime().GetSandboxClass() == sandboxclass.MicroVM {
		b.plan.refuse("%s: snapshot %s is a microVM snapshot; ate-convert converts gVisor snapshots only", holder, snap.GetUuid())
		b.refusedSnapshots[uri] = true
		return
	}
	if target == nil {
		if isTag {
			return
		}
		b.plan.refuse("%s: snapshot %s has no template to take its container layout from", holder, snap.GetUuid())
		b.refusedSnapshots[uri] = true
		return
	}
	layout := durableLayout(target)
	if prev, ok := b.snapshotOwners[uri]; ok && prev != nil {
		if !slices.EqualFunc(durableLayout(prev), layout, containerMountsEqual) {
			b.plan.refuse("%s: snapshot %s is restored into templates with different durable mounts", holder, snap.GetUuid())
			b.refusedSnapshots[uri] = true
		}
		return
	}
	b.snapshotOwners[uri] = target
	job.Containers = layout
}

func (b *builder) finishSnapshots() {
	uris := make([]string, 0, len(b.snapshots))
	for uri := range b.snapshots {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	for _, uri := range uris {
		job := b.snapshots[uri]
		if b.refusedSnapshots[uri] {
			continue
		}
		if b.snapshotOwners[uri] == nil {
			b.plan.refuse("snapshot %s of %v: no template to take its container layout from; delete the holders on the 1.x release", uri, job.Holders)
			continue
		}
		b.plan.Snapshots = append(b.plan.Snapshots, *job)
	}
}

// durableLayout returns the durable-dir mounts of t's containers.
func durableLayout(t *ateapi.ActorTemplate) []ContainerMounts {
	durable := map[string]bool{}
	for _, v := range t.GetVolumes() {
		if v.GetDurableDir() != nil {
			durable[v.GetName()] = true
		}
	}
	var out []ContainerMounts
	for _, c := range t.GetContainers() {
		cm := ContainerMounts{Name: c.GetName()}
		for _, m := range c.GetVolumeMounts() {
			if durable[m.GetName()] {
				cm.Mounts = append(cm.Mounts, Mount{Volume: m.GetName(), Path: m.GetMountPath()})
			}
		}
		if len(cm.Mounts) > 0 {
			out = append(out, cm)
		}
	}
	return out
}

func containerMountsEqual(a, b ContainerMounts) bool {
	return a.Name == b.Name && slices.Equal(a.Mounts, b.Mounts)
}
