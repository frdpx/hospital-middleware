package db

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	// registers the "postgres" database driver with golang-migrate
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/bambam/hospital-middleware/internal/config"
	"github.com/bambam/hospital-middleware/migrations"
)

// Migrate applies all pending migrations from the embedded SQL files.
//
// Running migrations from the API process (rather than a separate job) keeps
// docker-compose to three services as the assignment requires. golang-migrate
// takes a Postgres advisory lock for the duration, so several API replicas
// starting at once still apply each migration exactly once.
func Migrate(cfg config.DBConfig) error {
	m, closeMigrator, err := newMigrator(cfg)
	if err != nil {
		return err
	}
	defer closeMigrator()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// Rollback reverses every applied migration. It exists so that the .down.sql
// files are executable rather than decorative — CI runs up/down/up, which is
// the only check that a down migration is valid Postgres at all.
//
// It is deliberately not reachable from the API process: rolling a schema back
// is an operator action, never a side effect of a restart.
func Rollback(cfg config.DBConfig) error {
	m, closeMigrator, err := newMigrator(cfg)
	if err != nil {
		return err
	}
	defer closeMigrator()

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("roll back migrations: %w", err)
	}
	return nil
}

func newMigrator(cfg config.DBConfig) (*migrate.Migrate, func(), error) {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, nil, fmt.Errorf("load embedded migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, cfg.DSN())
	if err != nil {
		return nil, nil, fmt.Errorf("init migrator: %w", err)
	}

	// Close reports both a source and a database error; neither is actionable
	// once the migration itself has already succeeded or failed.
	return m, func() { _, _ = m.Close() }, nil
}
