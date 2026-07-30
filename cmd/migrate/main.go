// Command migrate applies the embedded SQL migrations against the database
// named by the usual POSTGRES_* environment variables.
//
// The API applies migrations itself on startup when DB_AUTO_MIGRATE=true. This
// binary is the documented escape hatch for environments that want schema
// changes gated separately from a deploy:
//
//	DB_AUTO_MIGRATE=false      # in the API's environment
//	go run ./cmd/migrate       # run the change deliberately, then deploy
//
// -direction down rolls the schema back, and CI uses up/down/up to prove that
// both directions of every migration are valid Postgres — something the unit
// tests, which never touch a real database, cannot check.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/bambam/hospital-middleware/internal/config"
	"github.com/bambam/hospital-middleware/internal/db"
)

func main() {
	direction := flag.String("direction", "up", `"up" to apply migrations, "down" to roll them back`)
	flag.Parse()

	if err := run(*direction); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
}

func run(direction string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	switch direction {
	case "up":
		if err := db.Migrate(cfg.DB); err != nil {
			return err
		}
		slog.Info("migrations applied", "database", cfg.DB.Name)
	case "down":
		if err := db.Rollback(cfg.DB); err != nil {
			return err
		}
		slog.Info("migrations rolled back", "database", cfg.DB.Name)
	default:
		return fmt.Errorf("unknown -direction %q: expected \"up\" or \"down\"", direction)
	}
	return nil
}
