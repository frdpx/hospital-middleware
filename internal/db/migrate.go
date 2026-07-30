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
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("load embedded migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, cfg.DSN())
	if err != nil {
		return fmt.Errorf("init migrator: %w", err)
	}
	defer func() {
		// Close reports both a source and a database error; neither is
		// actionable once migrations already succeeded.
		_, _ = m.Close()
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
