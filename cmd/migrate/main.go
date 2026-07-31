package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/frdpx/hospital-middleware/internal/config"
	"github.com/frdpx/hospital-middleware/internal/db"
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
