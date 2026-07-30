package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bambam/hospital-middleware/internal/config"
)

// validEnv is the minimum set of variables a valid deployment must provide.
func validEnv() map[string]string {
	return map[string]string{
		"POSTGRES_PASSWORD": "hospital_dev_password",
		"JWT_SECRET":        "a-secret-that-is-at-least-32-characters",
	}
}

func withEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for key, value := range env {
		t.Setenv(key, value)
	}
}

func TestLoad_AppliesDefaults(t *testing.T) {
	withEnv(t, validEnv())

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, "development", cfg.App.Env)
	assert.Equal(t, "8080", cfg.App.Port)
	assert.Equal(t, time.Hour, cfg.JWT.TTL)
	assert.Equal(t, 5*time.Second, cfg.HIS.Timeout)
	assert.True(t, cfg.DB.AutoMigrate)
	assert.False(t, cfg.App.IsProduction())
}

func TestLoad_OverridesFromEnvironment(t *testing.T) {
	env := validEnv()
	env["APP_ENV"] = "production"
	env["APP_PORT"] = "9000"
	env["JWT_TTL"] = "15m"
	env["HIS_TIMEOUT"] = "2s"
	env["DB_AUTO_MIGRATE"] = "false"
	env["DB_MAX_OPEN_CONNS"] = "50"
	withEnv(t, env)

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.True(t, cfg.App.IsProduction())
	assert.Equal(t, "9000", cfg.App.Port)
	assert.Equal(t, 15*time.Minute, cfg.JWT.TTL)
	assert.Equal(t, 2*time.Second, cfg.HIS.Timeout)
	assert.False(t, cfg.DB.AutoMigrate)
	assert.Equal(t, 50, cfg.DB.MaxOpenConns)
}

func TestLoad_Rejections(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(env map[string]string)
		wantMsg string
	}{
		{
			name:    "no database password",
			mutate:  func(env map[string]string) { delete(env, "POSTGRES_PASSWORD") },
			wantMsg: "POSTGRES_PASSWORD is required",
		},
		{
			name:    "no JWT secret",
			mutate:  func(env map[string]string) { delete(env, "JWT_SECRET") },
			wantMsg: "JWT_SECRET must be at least 32 characters",
		},
		{
			name:    "JWT secret too short to be safe",
			mutate:  func(env map[string]string) { env["JWT_SECRET"] = "short-secret" },
			wantMsg: "JWT_SECRET must be at least 32 characters",
		},
		{
			name: "the sample secret is refused in production",
			mutate: func(env map[string]string) {
				env["APP_ENV"] = "production"
				env["JWT_SECRET"] = "change-me-to-a-long-random-string-at-least-32-chars"
			},
			wantMsg: "sample value",
		},
		{
			name:    "non-positive token lifetime",
			mutate:  func(env map[string]string) { env["JWT_TTL"] = "0s" },
			wantMsg: "JWT_TTL must be positive",
		},
		{
			name:    "non-positive HIS timeout would mean no timeout at all",
			mutate:  func(env map[string]string) { env["HIS_TIMEOUT"] = "-1s" },
			wantMsg: "HIS_TIMEOUT must be positive",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := validEnv()
			tc.mutate(env)
			// t.Setenv cannot unset, so clear removed keys explicitly.
			for _, key := range []string{"POSTGRES_PASSWORD", "JWT_SECRET"} {
				if _, ok := env[key]; !ok {
					t.Setenv(key, "")
				}
			}
			withEnv(t, env)

			cfg, err := config.Load()

			assert.Nil(t, cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

func TestDBConfig_DSN_EscapesCredentials(t *testing.T) {
	t.Parallel()

	dsn := config.DBConfig{
		Host: "postgres", Port: "5432",
		User: "hospital", Password: "p@ss:word/with?specials",
		Name: "hospital_middleware", SSLMode: "disable",
	}.DSN()

	assert.Contains(t, dsn, "postgres://")
	assert.Contains(t, dsn, "@postgres:5432/hospital_middleware")
	assert.Contains(t, dsn, "sslmode=disable")
	assert.NotContains(t, dsn, "p@ss:word/with?specials", "special characters must be percent-encoded")
}
