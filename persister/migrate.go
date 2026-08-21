package persister

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migration is a single, ordered, apply-once schema change.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// LoadMigrations reads the embedded migrations and returns them ordered by
// version. A migration file must be named "<version>_<name>.sql".
func LoadMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	migrations := make([]Migration, 0, len(entries))
	seen := make(map[int]string, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		base := strings.TrimSuffix(entry.Name(), ".sql")
		prefix, name, ok := strings.Cut(base, "_")
		if !ok {
			return nil, fmt.Errorf("migration %q: expected <version>_<name>.sql", entry.Name())
		}

		version, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("migration %q: unparseable version %q: %w", entry.Name(), prefix, err)
		}
		if other, dup := seen[version]; dup {
			return nil, fmt.Errorf("migration version %d used twice: %q and %q", version, other, entry.Name())
		}
		seen[version] = entry.Name()

		body, err := migrationFS.ReadFile(path.Join("migrations", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}

		migrations = append(migrations, Migration{Version: version, Name: name, SQL: string(body)})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	return migrations, nil
}

const migrationTableDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version      INTEGER PRIMARY KEY,
    name         TEXT    NOT NULL,
    applied_at_ns INTEGER NOT NULL
);`

// Migrate brings db up to the latest schema version.
//
// It replaces the previous bootstrap, which ran a single schema.sql of
// CREATE TABLE IF NOT EXISTS statements. That approach silently did nothing
// whenever the tables already existed, so any schema change after the first
// deployment was quietly skipped on every existing database.
//
// Each migration runs inside its own transaction together with the insert into
// schema_migrations, so a failed migration leaves no partial schema and no
// bookkeeping row behind.
func Migrate(ctx context.Context, db *sql.DB) (applied []int, err error) {
	migrations, err := LoadMigrations()
	if err != nil {
		return nil, err
	}

	if _, err := db.ExecContext(ctx, migrationTableDDL); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	done := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return nil, err
		}
		done[v] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	for _, m := range migrations {
		if done[m.Version] {
			continue
		}
		if err := applyMigration(ctx, db, m); err != nil {
			return applied, fmt.Errorf("migration %04d_%s: %w", m.Version, m.Name, err)
		}
		slog.Info("Applied migration", slog.Int("version", m.Version), slog.String("name", m.Name))
		applied = append(applied, m.Version)
	}

	return applied, nil
}

func applyMigration(ctx context.Context, db *sql.DB, m Migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, applied_at_ns) VALUES (?, ?, ?)`,
		m.Version, m.Name, time.Now().UnixNano(),
	); err != nil {
		return err
	}

	return tx.Commit()
}

// SchemaVersion returns the highest applied migration version, or 0 if the
// database has never been migrated.
func SchemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var version sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version)
	if err != nil {
		return 0, err
	}
	if !version.Valid {
		return 0, nil
	}
	return int(version.Int64), nil
}
