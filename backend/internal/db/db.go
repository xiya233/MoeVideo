package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func Open(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(time.Hour)

	if err := applyPragmas(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := SeedCategories(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func applyPragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA busy_timeout=5000;",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("apply pragma %q: %w", pragma, err)
		}
	}
	return nil
}

func runMigrations(db *sql.DB) error {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		files = append(files, filepath.Join("migrations", entry.Name()))
	}
	sort.Strings(files)

	for _, file := range files {
		body, err := migrationFS.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", file, err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			return fmt.Errorf("apply migration %s: %w", file, err)
		}
	}
	return nil
}
