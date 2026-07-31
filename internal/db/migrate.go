package db

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"

	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/frdpx/hospital-middleware/internal/config"
	"github.com/frdpx/hospital-middleware/migrations"
)

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

	return m, func() { _, _ = m.Close() }, nil
}
