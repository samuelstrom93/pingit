package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

func Open(ctx context.Context, path string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	conn.SetConnMaxLifetime(0)

	if err := conn.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA busy_timeout=5000;",
	}
	for _, pragma := range pragmas {
		if _, err := conn.ExecContext(ctx, pragma); err != nil {
			return nil, fmt.Errorf("apply sqlite pragma %q: %w", pragma, err)
		}
	}

	return conn, nil
}

func Migrate(conn *sql.DB, migrationsFS fs.FS) error {
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	goose.SetBaseFS(migrationsFS)
	defer goose.SetBaseFS(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := goose.UpContext(ctx, conn, "migrations"); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
