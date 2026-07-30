// Command api runs the Hospital Middleware HTTP service.
//
// This file is the composition root: it is the only place that knows about
// concrete implementations. Every layer below depends on interfaces, which is
// what keeps the service and handler packages unit-testable.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bambam/hospital-middleware/internal/auth"
	"github.com/bambam/hospital-middleware/internal/config"
	"github.com/bambam/hospital-middleware/internal/db"
	"github.com/bambam/hospital-middleware/internal/handler"
	"github.com/bambam/hospital-middleware/internal/hisclient"
	"github.com/bambam/hospital-middleware/internal/repository"
	"github.com/bambam/hospital-middleware/internal/service"
)

const (
	startupTimeout  = 30 * time.Second
	shutdownTimeout = 15 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg.App.LogLevel)
	slog.SetDefault(logger)

	// Shut down cleanly on SIGINT/SIGTERM so docker compose down does not kill
	// in-flight requests.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startupCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	pool, err := db.Connect(startupCtx, cfg.DB)
	if err != nil {
		return err
	}
	defer func() { _ = pool.Close() }()
	logger.Info("connected to database", "host", cfg.DB.Host, "database", cfg.DB.Name)

	if cfg.DB.AutoMigrate {
		if err := db.Migrate(cfg.DB); err != nil {
			return err
		}
		logger.Info("migrations applied")
	}

	// --- wiring: repositories -> services -> handlers ---
	hospitalRepo := repository.NewHospitalRepository(pool)
	staffRepo := repository.NewStaffRepository(pool)
	patientRepo := repository.NewPatientRepository(pool)

	tokens := auth.NewTokenManager(cfg.JWT)
	hisFactory := hisclient.NewDefaultFactory(
		&http.Client{Timeout: cfg.HIS.Timeout},
		cfg.HIS.BaseURLOverride,
	)

	staffService := service.NewStaffService(hospitalRepo, staffRepo, tokens)
	patientService := service.NewPatientService(hospitalRepo, patientRepo, hisFactory, logger)

	router := handler.NewRouter(handler.RouterDeps{
		Staff:    handler.NewStaffHandler(staffService, logger),
		Patients: handler.NewPatientHandler(patientService, logger),
		Tokens:   tokens,
		Logger:   logger,
		Ping:     pool.PingContext,
		Debug:    !cfg.App.IsProduction(),
	})

	server := &http.Server{
		Addr:    net.JoinHostPort("", cfg.App.Port),
		Handler: router,
		// Guards against slowloris-style clients holding connections open.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "port", cfg.App.Port, "env", cfg.App.Env)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining connections")
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelShutdown()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("shutdown complete")
	return nil
}

// newLogger emits JSON so container logs are machine-parseable.
func newLogger(level string) *slog.Logger {
	var slogLevel slog.Level
	if err := slogLevel.UnmarshalText([]byte(level)); err != nil {
		slogLevel = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel}))
}
