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
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.ate.dev/substrate/cmd/ateapi/internal/store/atepg"
)

// protoTable describes a table that holds one proto per row.
type protoTable struct {
	name string
	// keys are the key columns, read into Row.Atespace and Row.Name.
	keys []string
	// get and set address the table's rows in a Source or a Plan.
	src func(*Source) *[]Row
	dst func(*Plan) []Row
}

// protoTables are in the order rows are inserted, parents first.
var protoTables = []protoTable{
	{"atespaces", []string{"name"}, func(s *Source) *[]Row { return &s.Atespaces }, func(p *Plan) []Row { return p.Atespaces }},
	{"global_access_policy", nil, func(s *Source) *[]Row { return &s.GlobalPolicy }, func(p *Plan) []Row { return p.GlobalPolicy }},
	{"atespace_access_policies", []string{"atespace_name"}, func(s *Source) *[]Row { return &s.AtespacePolicies }, func(p *Plan) []Row { return p.AtespacePolicies }},
	{"actor_templates", []string{"atespace", "name"}, func(s *Source) *[]Row { return &s.ActorTemplates }, func(p *Plan) []Row { return p.ActorTemplates }},
	{"tags", []string{"atespace", "name"}, func(s *Source) *[]Row { return &s.Tags }, func(p *Plan) []Row { return p.Tags }},
	{"actors", []string{"atespace", "name"}, func(s *Source) *[]Row { return &s.Actors }, func(p *Plan) []Row { return p.Actors }},
	{"actor_egress_policies", []string{"atespace", "actor_name"}, func(s *Source) *[]Row { return &s.EgressPolicies }, func(p *Plan) []Row { return p.EgressPolicies }},
}

// targetOnlyTables are emptied in the target: nothing in them survives the
// conversion. Workers register again, no assignment or lease can be in
// flight, and the outbox is rebuilt from the workers table.
var targetOnlyTables = []string{"worker_assignments", "workers", "leases", "worker_outbox"}

func ident(schema, table string) string {
	return pgx.Identifier{schema, table}.Sanitize()
}

func keyColumns(t protoTable) []string {
	switch len(t.keys) {
	case 0:
		return []string{"''", "''"}
	case 1:
		return []string{"''", t.keys[0]}
	default:
		return t.keys
	}
}

// Load reads the 1.x rows from schema in one consistent snapshot.
func Load(ctx context.Context, pool *pgxpool.Pool, schema string) (*Source, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only
	src := &Source{OpenFGA: map[string]*Table{}}
	for _, t := range protoTables {
		rows, err := readProtoRows(ctx, tx, schema, t)
		if err != nil {
			return nil, err
		}
		*t.src(src) = rows
	}
	for _, name := range openFGATables {
		t, err := readTable(ctx, tx, schema, name)
		if err != nil {
			return nil, err
		}
		src.OpenFGA[name] = t
	}
	return src, nil
}

func readProtoRows(ctx context.Context, q pgx.Tx, schema string, t protoTable) ([]Row, error) {
	keys := keyColumns(t)
	rows, err := q.Query(ctx, fmt.Sprintf(`SELECT %s, %s, uid, version, proto FROM %s ORDER BY 1, 2`, keys[0], keys[1], ident(schema, t.name)))
	if err != nil {
		return nil, fmt.Errorf("reading %s.%s: %w", schema, t.name, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Row, error) {
		var r Row
		return r, row.Scan(&r.Atespace, &r.Name, &r.UID, &r.Version, &r.Proto)
	})
	if err != nil {
		return nil, fmt.Errorf("reading %s.%s: %w", schema, t.name, err)
	}
	return out, nil
}

func readTable(ctx context.Context, q pgx.Tx, schema, table string) (*Table, error) {
	rows, err := q.Query(ctx, `SELECT * FROM `+ident(schema, table))
	if err != nil {
		return nil, fmt.Errorf("reading %s.%s: %w", schema, table, err)
	}
	defer rows.Close()
	t := &Table{}
	for _, fd := range rows.FieldDescriptions() {
		t.Columns = append(t.Columns, fd.Name)
	}
	for rows.Next() {
		v, err := rows.Values()
		if err != nil {
			return nil, err
		}
		t.Rows = append(t.Rows, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading %s.%s: %w", schema, table, err)
	}
	return t, nil
}

// SchemaExists reports whether schema exists and holds the target tables.
func SchemaExists(ctx context.Context, pool *pgxpool.Pool, schema string) (exists, hasTables bool, err error) {
	err = pool.QueryRow(ctx, `
		SELECT
			EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1),
			EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = 'actors')`,
		schema).Scan(&exists, &hasTables)
	return exists, hasTables, err
}

// Migrate creates the target tables in the schema pool's search_path names,
// with the target's own migrations, as ate-api does at startup.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	p, err := atepg.NewPersistence(ctx, pool)
	if err != nil {
		return fmt.Errorf("applying the target migrations: %w", err)
	}
	p.Close()
	return nil
}

// Write replaces the rows of the target schema with plan's in one
// transaction, and commits it only when commit is set. It refuses a target
// that 2.x has already used: one with workers or leases, or rows that are
// not the ones a conversion wrote.
func Write(ctx context.Context, pool *pgxpool.Pool, schema string, plan *Plan, commit bool) error {
	if len(plan.Refusals) > 0 {
		return fmt.Errorf("%d refusals", len(plan.Refusals))
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Commit decides the outcome.
	if err := checkTargetUnused(ctx, tx, schema, plan); err != nil {
		return err
	}
	if err := checkMigrationLedger(ctx, tx, schema); err != nil {
		return err
	}
	for i := len(protoTables) - 1; i >= 0; i-- {
		if _, err := tx.Exec(ctx, `DELETE FROM `+ident(schema, protoTables[i].name)); err != nil {
			return err
		}
	}
	for _, name := range append(slices.Clone(targetOnlyTables), openFGATables...) {
		if _, err := tx.Exec(ctx, `DELETE FROM `+ident(schema, name)); err != nil {
			return err
		}
	}
	for _, t := range protoTables {
		if err := insertProtoRows(ctx, tx, schema, t, t.dst(plan)); err != nil {
			return err
		}
	}
	for _, name := range openFGATables {
		t := plan.OpenFGA[name]
		if t == nil || len(t.Rows) == 0 {
			continue
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{schema, name}, t.Columns, pgx.CopyFromRows(t.Rows)); err != nil {
			return fmt.Errorf("copying %s: %w", name, err)
		}
	}
	// Watchers rebuild from the workers table when the outbox was trimmed
	// past their position.
	if _, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (xid) VALUES (pg_current_xact_id())
		ON CONFLICT (id) DO UPDATE SET xid = EXCLUDED.xid`, ident(schema, "worker_outbox_trim"))); err != nil {
		return err
	}
	if !commit {
		return nil
	}
	return tx.Commit(ctx)
}

func insertProtoRows(ctx context.Context, tx pgx.Tx, schema string, t protoTable, rows []Row) error {
	var cols []string
	switch len(t.keys) {
	case 0:
	case 1:
		cols = []string{t.keys[0]}
	default:
		cols = slices.Clone(t.keys)
	}
	cols = append(cols, "uid", "version", "proto")
	values := make([][]any, 0, len(rows))
	for _, r := range rows {
		var v []any
		switch len(t.keys) {
		case 0:
		case 1:
			v = []any{r.Name}
		default:
			v = []any{r.Atespace, r.Name}
		}
		values = append(values, append(v, r.UID, r.Version, r.Proto))
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{schema, t.name}, cols, pgx.CopyFromRows(values)); err != nil {
		return fmt.Errorf("writing %s: %w", t.name, err)
	}
	return nil
}

// checkTargetUnused refuses a target that holds anything a conversion did
// not write: 2.x ran on it, and replacing its rows would lose that.
func checkTargetUnused(ctx context.Context, tx pgx.Tx, schema string, plan *Plan) error {
	var errs []error
	for _, name := range []string{"workers", "worker_assignments", "leases"} {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+ident(schema, name)).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			errs = append(errs, fmt.Errorf("%s.%s has %d rows", schema, name, n))
		}
	}
	for _, t := range protoTables {
		want := map[string]Row{}
		for _, r := range t.dst(plan) {
			want[r.key()] = r
		}
		have, err := readProtoRows(ctx, tx, schema, t)
		if err != nil {
			return err
		}
		for _, r := range have {
			w, ok := want[r.key()]
			if !ok || w.UID != r.UID || w.Version != r.Version {
				errs = append(errs, fmt.Errorf("%s.%s %s is not the row a conversion writes", schema, t.name, r.key()))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("the target schema was used after a conversion; drop it to convert again: %w", errors.Join(errs...))
	}
	return nil
}

// checkMigrationLedger requires the target's goose ledger to hold exactly
// the target's own migrations, and no ledger of another migration set.
func checkMigrationLedger(ctx context.Context, tx pgx.Tx, schema string) error {
	rows, err := tx.Query(ctx, `SELECT DISTINCT version_id FROM `+ident(schema, "schema_migrations")+` WHERE is_applied ORDER BY 1`)
	if err != nil {
		return err
	}
	versions, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	if !slices.Equal(versions, []int64{0, 1}) {
		return fmt.Errorf("%s.schema_migrations holds versions %v, want [0 1]", schema, versions)
	}
	var extra []string
	rows, err = tx.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = $1 AND table_name IN ('fork_schema_migrations', 'goose_db_version') ORDER BY 1`, schema)
	if err != nil {
		return err
	}
	if extra, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return err
	}
	if len(extra) > 0 {
		return fmt.Errorf("%s holds the ledgers %s", schema, strings.Join(extra, ", "))
	}
	return nil
}
