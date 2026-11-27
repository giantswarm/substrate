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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	ateapi "go.ate.dev/substrate/api/ateapi/v1"
	"go.ate.dev/substrate/cmd/ateapi/internal/store"
	"go.ate.dev/substrate/cmd/ateapi/internal/store/atepg"
	"go.ate.dev/substrate/cmd/ateapi/internal/store/dockerenv"
	"go.ate.dev/substrate/internal/resources"
)

const targetSchema = "substrate"

// startPostgres returns a pool on a fresh PostgreSQL, whose public schema
// holds the 1.x tables and goose ledgers.
func startPostgres(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := t.Context()
	if err := dockerenv.Configure(ctx); err != nil {
		if dockerenv.Required() {
			t.Fatalf("PostgreSQL testcontainer unavailable and required: %v", err)
		}
		t.Skipf("PostgreSQL testcontainer unavailable (requires Docker): %v", err)
	}
	pg, err := postgres.Run(ctx, "postgres:18-alpine",
		postgres.WithDatabase("ate"), postgres.WithUsername("ate"), postgres.WithPassword("ate"))
	if err != nil {
		if dockerenv.Required() {
			t.Fatalf("PostgreSQL testcontainer unavailable and required: %v", err)
		}
		t.Skipf("PostgreSQL testcontainer unavailable (requires Docker): %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(context.Background()) })
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	// The image restarts its server once after initdb.
	require.Eventually(t, func() bool { return pool.Ping(ctx) == nil }, 30*time.Second, 200*time.Millisecond)

	files, err := filepath.Glob("testdata/fork_*.sql")
	require.NoError(t, err)
	require.Len(t, files, 4)
	for _, f := range files {
		ddl, err := os.ReadFile(f)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, string(ddl))
		require.NoError(t, err, f)
	}
	// The ledgers the 1.x release keeps: its upstream migrations 1-3, its own
	// lease-holder migration, and OpenFGA's.
	for _, stmt := range []string{
		`CREATE TABLE schema_migrations (id serial PRIMARY KEY, version_id bigint NOT NULL, is_applied boolean NOT NULL, tstamp timestamp DEFAULT now())`,
		`INSERT INTO schema_migrations (version_id, is_applied) VALUES (0, true), (1, true), (2, true), (3, true)`,
		`CREATE TABLE fork_schema_migrations (id serial PRIMARY KEY, version_id bigint NOT NULL, is_applied boolean NOT NULL, tstamp timestamp DEFAULT now())`,
		`INSERT INTO fork_schema_migrations (version_id, is_applied) VALUES (0, true), (1, true)`,
		`CREATE TABLE goose_db_version (id serial PRIMARY KEY, version_id bigint NOT NULL, is_applied boolean NOT NULL, tstamp timestamp DEFAULT now())`,
	} {
		_, err := pool.Exec(ctx, stmt)
		require.NoError(t, err)
	}
	return pool, dsn
}

func schemaPool(t *testing.T, dsn, schema string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func seed(t *testing.T, pool *pgxpool.Pool, src *Source) {
	t.Helper()
	ctx := t.Context()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	for _, tbl := range protoTables {
		require.NoError(t, insertProtoRows(ctx, tx, "public", tbl, *tbl.src(src)))
	}
	_, err = tx.Exec(ctx, `INSERT INTO public.store (id, name, created_at) VALUES ('01STORE', 'substrate', now())`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO public.tuple (store, object_type, object_id, relation, _user, user_type, ulid, inserted_at)
		VALUES ('01STORE', 'atespace', 'team', 'owner', 'user:alice', 'user', '01TUPLE', now())`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO public.leases (key, token, expires_at, holder) VALUES ('golden', 't', now(), 'ate-api-0')`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
}

func TestConvertPostgres(t *testing.T) {
	ctx := t.Context()
	pool, dsn := startPostgres(t)
	src := testSource(t)
	src.OpenFGA = nil
	seed(t, pool, src)

	source, err := Load(ctx, pool, "public")
	require.NoError(t, err)
	plan := Build(source)
	require.Empty(t, plan.Refusals)

	// The installer creates the schema; ate-convert refuses to run without it.
	exists, _, err := SchemaExists(ctx, pool, targetSchema)
	require.NoError(t, err)
	require.False(t, exists)
	_, err = pool.Exec(ctx, `CREATE SCHEMA `+targetSchema)
	require.NoError(t, err)
	target := schemaPool(t, dsn, targetSchema)
	require.NoError(t, Migrate(ctx, target))

	// A dry run writes nothing.
	require.NoError(t, Write(ctx, target, targetSchema, plan, false))
	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM substrate.actors`).Scan(&n))
	require.Zero(t, n)

	require.NoError(t, Write(ctx, target, targetSchema, plan, true))
	before := snapshotPublic(t, pool)

	// ate-api reads every converted row through the store.
	p, err := atepg.NewPersistence(ctx, target)
	require.NoError(t, err)
	defer p.Close()
	tmpl, err := p.GetActorTemplate(ctx, resources.ActorTemplateRef{Atespace: "team", Name: "agent"})
	require.NoError(t, err)
	require.Equal(t, ateapi.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, tmpl.GetSnapshotConfig().GetPreferredFidelity())
	require.Nil(t, tmpl.GetStatus())
	require.Len(t, tmpl.GetGoldenEgressPolicy().GetRules(), 2)
	tag, err := p.GetTag(ctx, resources.TagRef{Atespace: "team", Name: "release"})
	require.NoError(t, err)
	require.Equal(t, tagUID, tag.GetStatus().GetSnapshot().GetUuid())
	_, err = p.GetTag(ctx, resources.TagRef{Atespace: "ate-golden", Name: templateUID})
	require.Error(t, err, "golden tags are not converted")
	actor, err := p.GetActor(ctx, resources.ActorRef{Atespace: "team", Name: "session"})
	require.NoError(t, err)
	require.Equal(t, ateapi.ActorState_ACTOR_STATE_SUSPENDED, actor.GetStatus().GetState())
	require.Equal(t, actorURI(snapName), actor.GetStatus().GetSnapshots()[0].GetDurableSnapshot().GetObject().GetSnapshotUri())
	require.EqualValues(t, 1, actor.GetStatus().GetLastAssignedGeneration())
	clone, err := p.GetActor(ctx, resources.ActorRef{Atespace: "team", Name: "clone"})
	require.NoError(t, err)
	require.Equal(t, ateapi.SnapshotOwner_SNAPSHOT_OWNER_TAG, clone.GetStatus().GetSnapshots()[0].GetOwner())
	actors, err := p.ListActors(ctx, "team", store.ListOptions{PageSize: 10})
	require.NoError(t, err)
	require.Len(t, actors.Items, 2)
	policy, err := p.GetEgressPolicy(ctx, resources.ActorRef{Atespace: "team", Name: "session"})
	require.NoError(t, err)
	require.Equal(t, []string{"db"}, policy.GetRules()[0].GetTcp().GetHostnames())

	var leases, tuples int
	require.NoError(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM substrate.leases), (SELECT count(*) FROM substrate.tuple)`).Scan(&leases, &tuples))
	require.Zero(t, leases)
	require.Equal(t, 1, tuples)

	var out bytes.Buffer
	require.NoError(t, Verify(ctx, pool, "public", target, targetSchema, &out))
	require.Contains(t, out.String(), "actor team/session ")
	require.NotContains(t, out.String(), "MISMATCH")
	require.NotContains(t, out.String(), "db", "verify prints no stored value")

	// A second run converges.
	require.NoError(t, Write(ctx, target, targetSchema, plan, true))
	require.NoError(t, Verify(ctx, pool, "public", target, targetSchema, &bytes.Buffer{}))
	require.Equal(t, before, snapshotPublic(t, pool), "public is never written")

	// Once 2.x has used the schema, a conversion refuses to replace it.
	_, err = pool.Exec(ctx, `UPDATE substrate.actors SET version = version + 1 WHERE name = 'session'`)
	require.NoError(t, err)
	err = Write(ctx, target, targetSchema, plan, true)
	require.ErrorContains(t, err, "was used after a conversion")
}

// snapshotPublic returns every row of the public schema's tables as text.
func snapshotPublic(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := t.Context()
	rows, err := pool.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY 1`)
	require.NoError(t, err)
	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Err())
	var b strings.Builder
	for _, name := range tables {
		var dump string
		require.NoError(t, pool.QueryRow(ctx, `SELECT coalesce(string_agg(t::text, E'\n' ORDER BY t::text), '') FROM public.`+name+` t`).Scan(&dump))
		b.WriteString(name + "\n" + dump + "\n")
	}
	return b.String()
}
