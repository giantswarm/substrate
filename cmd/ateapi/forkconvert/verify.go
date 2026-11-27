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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	ateapi "go.ate.dev/substrate/api/ateapi/v1"
	"go.ate.dev/substrate/cmd/ateapi/forkconvert/internal/forkpb"
	"google.golang.org/protobuf/proto"
)

// actorProjection is what a conversion preserves of an actor. Verify hashes
// it on both sides and prints only the hashes.
type actorProjection struct {
	Atespace        string            `json:"atespace"`
	Name            string            `json:"name"`
	UID             string            `json:"uid"`
	Version         int64             `json:"version"`
	CreateTime      int64             `json:"createTime"`
	Template        string            `json:"template"`
	Selector        map[string]string `json:"selector"`
	SourceTag       string            `json:"sourceTag"`
	State           string            `json:"state"`
	Crash           string            `json:"crash"`
	ExistingVolumes []string          `json:"existingVolumes"`
	FencingToken    string            `json:"fencingToken"`
	ExternalVolumes []string          `json:"externalVolumes"`
	Snapshot        string            `json:"snapshot"`
}

func (p actorProjection) hash() string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

func ref(atespace, name string) string {
	if atespace == "" && name == "" {
		return ""
	}
	return atespace + "/" + name
}

// forkActorProjection projects a 1.x actor onto what its conversion must
// hold, from its 1.x fields and the state mapping of actorSnapshot.
func forkActorProjection(f *forkpb.Actor) (actorProjection, error) {
	m := f.GetMetadata()
	p := actorProjection{
		Atespace:   m.GetAtespace(),
		Name:       m.GetName(),
		UID:        m.GetUid(),
		Version:    m.GetVersion(),
		CreateTime: m.GetCreateTime().AsTime().UnixNano(),
		Template:   ref(f.GetActorTemplate().GetAtespace(), f.GetActorTemplate().GetName()),
		Selector:   f.GetWorkerSelector().GetMatchLabels(),
		SourceTag:  ref(f.GetSourceTag().GetAtespace(), f.GetSourceTag().GetName()),
	}
	es, state, crash, err := actorSnapshot(f.GetStatus())
	if err != nil {
		return p, err
	}
	p.State = state.String()
	p.Crash = crash.GetMessage()
	for _, v := range f.GetExistingVolumes() {
		p.ExistingVolumes = append(p.ExistingVolumes, strings.Join([]string{v.GetName(), v.GetDriver(), v.GetVolumeHandle(), fmt.Sprint(int32(v.GetAccessMode())), v.GetSubPath()}, "|"))
	}
	if t := f.GetStatus().GetFencingToken(); t != nil {
		p.FencingToken = fmt.Sprintf("%s|%d", t.GetHolder(), t.GetGeneration())
	}
	for _, v := range f.GetStatus().GetActorVolumes() {
		p.ExternalVolumes = append(p.ExternalVolumes, externalVolumeKey(v.GetVolumeName(), v.GetStorageVolumeId(), v.GetVolumeType(), v.GetVolumeContext()))
	}
	if es.GetSnapshotUri() != "" {
		p.Snapshot = fmt.Sprintf("%s|%s|%s", es.GetSnapshotUri(), snapshotURIName(es.GetSnapshotUri()), es.GetActorTemplateUid())
	}
	return p, nil
}

func targetActorProjection(a *ateapi.Actor) actorProjection {
	m := a.GetMetadata()
	p := actorProjection{
		Atespace:   m.GetAtespace(),
		Name:       m.GetName(),
		UID:        m.GetUid(),
		Version:    m.GetVersion(),
		CreateTime: m.GetCreateTime().AsTime().UnixNano(),
		Template:   ref(a.GetActorTemplate().GetAtespace(), a.GetActorTemplate().GetName()),
		Selector:   a.GetWorkerSelector().GetMatchLabels(),
		SourceTag:  ref(a.GetSourceTag().GetAtespace(), a.GetSourceTag().GetName()),
		State:      a.GetStatus().GetState().String(),
		Crash:      a.GetStatus().GetCrash().GetMessage(),
	}
	for _, v := range a.GetExistingVolumes() {
		p.ExistingVolumes = append(p.ExistingVolumes, strings.Join([]string{v.GetName(), v.GetDriver(), v.GetVolumeHandle(), fmt.Sprint(int32(v.GetAccessMode())), v.GetSubPath()}, "|"))
	}
	if t := a.GetStatus().GetFencingToken(); t != nil {
		p.FencingToken = fmt.Sprintf("%s|%d", t.GetHolder(), t.GetGeneration())
	}
	for _, v := range a.GetStatus().GetExternalVolumes().GetVolumes() {
		p.ExternalVolumes = append(p.ExternalVolumes, externalVolumeKey(v.GetName(), v.GetStorageVolumeId(), v.GetVolumeType(), v.GetVolumeContext()))
	}
	if snaps := a.GetStatus().GetSnapshots(); len(snaps) > 0 {
		s := snaps[len(snaps)-1]
		p.Snapshot = fmt.Sprintf("%s|%s|%s", s.GetDurableSnapshot().GetObject().GetSnapshotUri(), s.GetUuid(), s.GetActorTemplateUid())
	}
	return p
}

func externalVolumeKey(name, id, typ string, ctx map[string]string) string {
	keys := make([]string, 0, len(ctx))
	for k := range ctx {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%q=%q,", k, ctx[k])
	}
	return strings.Join([]string{name, id, typ, b.String()}, "|")
}

// Verify compares the 1.x schema with the converted one: row counts per
// table, the migration ledger, and per actor a hash of what the conversion
// preserves. It writes hashes and counts to w, never a stored value, and
// fails on any difference.
func Verify(ctx context.Context, src *pgxpool.Pool, srcSchema string, dst *pgxpool.Pool, dstSchema string, w io.Writer) error {
	source, err := Load(ctx, src, srcSchema)
	if err != nil {
		return err
	}
	target, err := Load(ctx, dst, dstSchema)
	if err != nil {
		return err
	}
	plan := Build(source)
	var problems int
	fail := func(format string, args ...any) {
		problems++
		fmt.Fprintf(w, "MISMATCH "+format+"\n", args...)
	}
	for _, c := range plan.Counts {
		have := 0
		for _, t := range protoTables {
			if t.name == c.Table {
				have = len(*t.src(target))
			}
		}
		if tbl := target.OpenFGA[c.Table]; tbl != nil {
			have = len(tbl.Rows)
		}
		status := "ok"
		if have != c.Target {
			status = "MISMATCH"
			problems++
		}
		fmt.Fprintf(w, "table %-26s %s %d -> %s %d (expected %d) %s\n", c.Table, srcSchema, c.Source, dstSchema, have, c.Target, status)
	}

	tx, err := dst.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	ledgerErr := checkMigrationLedger(ctx, tx, dstSchema)
	_ = tx.Rollback(ctx)
	if ledgerErr != nil {
		fail("ledger: %v", ledgerErr)
	} else {
		fmt.Fprintf(w, "ledger %s.schema_migrations [0 1] ok\n", dstSchema)
	}

	converted := map[string][]byte{}
	for _, r := range target.Actors {
		converted[r.key()] = r.Proto
	}
	var keys []string
	forkActors := map[string]*forkpb.Actor{}
	for _, r := range source.Actors {
		f, err := decodeActor(r.Proto)
		if err != nil {
			fail("actor %s: %v", r.key(), err)
			continue
		}
		forkActors[r.key()] = f
		keys = append(keys, r.key())
	}
	slices.Sort(keys)
	for _, key := range keys {
		f := forkActors[key]
		b, ok := converted[key]
		if !ok {
			if f.GetMetadata().GetAtespace() == goldenActorAtespace {
				fmt.Fprintf(w, "actor %s golden, not converted\n", key)
				continue
			}
			fail("actor %s: missing in %s", key, dstSchema)
			continue
		}
		want, err := forkActorProjection(f)
		if err != nil {
			fail("actor %s: %v", key, err)
			continue
		}
		a := &ateapi.Actor{}
		if err := proto.Unmarshal(b, a); err != nil {
			fail("actor %s: decoding the converted row: %v", key, err)
			continue
		}
		got := targetActorProjection(a)
		if want.hash() != got.hash() {
			fail("actor %s %s %s", key, want.hash(), got.hash())
			continue
		}
		fmt.Fprintf(w, "actor %s %s ok\n", key, got.hash())
	}
	if problems > 0 {
		return fmt.Errorf("%d differences", problems)
	}
	return nil
}
