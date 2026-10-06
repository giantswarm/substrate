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
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

const (
	migrationTableName = "schema_migrations"
	migrationLockName  = "agent-substrate:atepg:migrations"

	// openFGAMigrationVersion is the Substrate migration that creates the
	// OpenFGA tables (migrations/000002_openfga.sql).
	openFGAMigrationVersion = 2
	// openFGALedgerTableName is the Goose ledger of OpenFGA's own embedded
	// migrations, which created the OpenFGA tables before Substrate managed
	// them in its own migrations.
	openFGALedgerTableName = "goose_db_version"
	// pinnedOpenFGAMigrationVersion is the OpenFGA PostgreSQL migration whose
	// schema migrations/000002_openfga.sql reproduces.
	pinnedOpenFGAMigrationVersion = 6
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// The schema needs PostgreSQL 13+ (xid8, pg_current_xact_id,
	// pg_current_snapshot). Report a clear error before PostgreSQL reports an
	// opaque DDL or function error.
	var version int
	if err := pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&version); err != nil {
		return fmt.Errorf("get PostgreSQL version: %w", err)
	}
	if version < 130000 {
		return fmt.Errorf("atepg requires PostgreSQL 13 or newer for xid8 and pg_current_snapshot. server_version_num is %d", version)
	}
	if err := rejectUnversionedSubstrateSchema(ctx, pool); err != nil {
		return err
	}
	if err := adoptOpenFGASchema(ctx, pool); err != nil {
		return err
	}

	migrations, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("open embedded PostgreSQL migrations: %w", err)
	}
	provider, err := openMigrationProvider(ctx, pool, migrations)
	if err != nil {
		return err
	}
	return errors.Join(migrateToLatest(ctx, provider), provider.Close())
}

func openMigrationProvider(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS) (*goose.Provider, error) {
	lockID, err := migrationLockID(ctx, pool)
	if err != nil {
		return nil, err
	}
	locker, err := lock.NewPostgresSessionLocker(
		lock.WithLockID(lockID),
		lock.WithLockTimeout(1, 300),
	)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL migration locker: %w", err)
	}
	db := stdlib.OpenDBFromPool(pool)
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrations,
		goose.WithTableName(migrationTableName),
		goose.WithSessionLocker(locker),
	)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create PostgreSQL migration provider: %w", err)
	}
	return provider, nil
}

func migrationLockID(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var lockID int64
	if err := pool.QueryRow(ctx, `SELECT hashtextextended($1 || ':' || current_schema(), 0)`, migrationLockName).Scan(&lockID); err != nil {
		return 0, fmt.Errorf("get PostgreSQL migration lock ID: %w", err)
	}
	return lockID, nil
}

// rejectUnversionedSubstrateSchema stops Goose before it creates a migration
// ledger in a database from a pre-migration ateapi version.
// The list contains all tables in the pre-migration schema. Goose creates the
// migration ledger before it creates a later table.
func rejectUnversionedSubstrateSchema(ctx context.Context, pool *pgxpool.Pool) error {
	var hasMetadata, hasSubstrateTables bool
	err := pool.QueryRow(ctx, `
		SELECT
			to_regclass('schema_migrations') IS NOT NULL,
			EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = current_schema()
				AND table_name IN (
					'atespaces', 'actors', 'actor_templates',
					'actor_snapshots', 'actor_snapshot_tags', 'workers',
					'worker_outbox', 'worker_outbox_default',
					'worker_outbox_trim', 'leases'
				)
			)`).Scan(&hasMetadata, &hasSubstrateTables)
	if err != nil {
		return fmt.Errorf("check PostgreSQL migration ledger: %w", err)
	}
	if hasSubstrateTables && !hasMetadata {
		return errors.New("unsupported PostgreSQL schema: Substrate tables exist without a migration ledger")
	}
	return nil
}

// adoptOpenFGASchema records migrations/000002_openfga.sql as applied in a
// database whose OpenFGA tables were created by OpenFGA's own embedded
// migrations, which ateapi ran before Substrate managed the OpenFGA schema.
// Those migrations, at the pinned version, create exactly the schema of
// 000002, which would otherwise fail on the existing tables. OpenFGA's ledger
// stays, so an ateapi that still runs OpenFGA's migrations finds them applied.
func adoptOpenFGASchema(ctx context.Context, pool *pgxpool.Pool) error {
	lockID, err := migrationLockID(ctx, pool)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin OpenFGA schema adoption: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize with Goose's session lock on the same key, so a concurrent
	// startup neither applies 000002 nor adopts the schema twice.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lockID); err != nil {
		return fmt.Errorf("lock PostgreSQL migrations for OpenFGA schema adoption: %w", err)
	}

	var hasLedger, hasOpenFGALedger bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL, to_regclass($2) IS NOT NULL`,
		migrationTableName, openFGALedgerTableName).Scan(&hasLedger, &hasOpenFGALedger); err != nil {
		return fmt.Errorf("check OpenFGA migration ledger: %w", err)
	}
	if !hasLedger || !hasOpenFGALedger {
		return nil
	}
	var current, openFGAVersion int64
	if err := tx.QueryRow(ctx, fmt.Sprintf(`
		SELECT
			(SELECT COALESCE(max(version_id), 0) FROM %s WHERE is_applied),
			(SELECT COALESCE(max(version_id), 0) FROM %s WHERE is_applied)`,
		migrationTableName, openFGALedgerTableName)).Scan(&current, &openFGAVersion); err != nil {
		return fmt.Errorf("read OpenFGA migration versions: %w", err)
	}
	if current != openFGAMigrationVersion-1 {
		return nil
	}
	if openFGAVersion != pinnedOpenFGAMigrationVersion {
		return fmt.Errorf("unsupported PostgreSQL schema: OpenFGA's migrations are at version %d, migration %d adopts version %d only",
			openFGAVersion, openFGAMigrationVersion, pinnedOpenFGAMigrationVersion)
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (version_id, is_applied) VALUES ($1, true)`, migrationTableName),
		openFGAMigrationVersion); err != nil {
		return fmt.Errorf("record OpenFGA schema adoption: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit OpenFGA schema adoption: %w", err)
	}
	slog.InfoContext(ctx, "Adopted the OpenFGA schema created by OpenFGA's migrations",
		slog.Int64("openfga_version", openFGAVersion),
		slog.Int64("migration_version", openFGAMigrationVersion))
	return nil
}

func migrateToLatest(ctx context.Context, provider *goose.Provider) (migrationErr error) {
	started := time.Now()
	current, latest, err := provider.GetVersions(ctx)
	if err != nil {
		return fmt.Errorf("get PostgreSQL migration versions: %w", err)
	}
	starting := current

	applied := 0
	defer func() {
		attributes := []any{
			slog.Int64("starting_version", starting),
			slog.Int64("current_version", current),
			slog.Int64("latest_version", latest),
			slog.Int("applied_migrations", applied),
			slog.Duration("duration", time.Since(started)),
		}
		if migrationErr != nil {
			attributes = append(attributes, slog.Any("err", migrationErr))
			slog.ErrorContext(ctx, "PostgreSQL migrations failed", attributes...)
			return
		}
		slog.InfoContext(ctx, "PostgreSQL migrations ready", attributes...)
	}()

	results, err := provider.Up(ctx)
	if err != nil {
		var partial *goose.PartialError
		if errors.As(err, &partial) {
			applied = len(partial.Applied)
		}
		applyErr := fmt.Errorf("apply PostgreSQL migrations: %w", err)
		failedCurrent, _, versionErr := provider.GetVersions(ctx)
		if versionErr != nil {
			return errors.Join(applyErr, fmt.Errorf("get PostgreSQL migration versions after a failure: %w", versionErr))
		}
		current = failedCurrent
		return applyErr
	}
	applied = len(results)
	current, _, err = provider.GetVersions(ctx)
	if err != nil {
		return fmt.Errorf("get PostgreSQL migration versions after migration: %w", err)
	}
	return nil
}
