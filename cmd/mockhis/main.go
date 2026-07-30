// Command mockhis serves a stand-in for the Hospital A HIS during local
// development, because https://hospital-a.api.co.th is not reachable from a
// dev machine. Point the API at it with HIS_BASE_URL_OVERRIDE.
//
//	go run ./cmd/mockhis                       # listens on :9090
//	HIS_BASE_URL_OVERRIDE=http://localhost:9090 go run ./cmd/api
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/bambam/hospital-middleware/internal/hisclient/mockhis"
)

func main() {
	addr := ":" + envOr("MOCK_HIS_PORT", "9090")
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	server := &http.Server{
		Addr:              addr,
		Handler:           mockhis.New().Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("mock Hospital A HIS listening", "addr", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("mock HIS stopped", "error", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
