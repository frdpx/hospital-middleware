// Package db owns the PostgreSQL connection pool and the migration runner.
package db

import (
	"context"
	"database/sql"
	"fmt"

	// pgx's database/sql driver: keeps the rest of the codebase on the
	// standard *sql.DB interface, which is what sqlmock can stand in for.
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/bambam/hospital-middleware/internal/config"
)

// Connect opens the pool and verifies it with a ping, so a misconfigured
// database fails at startup instead of on the first request.
func Connect(ctx context.Context, cfg config.DBConfig) (*sql.DB, error) {
	pool, err := sql.Open("pgx", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	pool.SetMaxOpenConns(cfg.MaxOpenConns)
	pool.SetMaxIdleConns(cfg.MaxIdleConns)
	pool.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}
