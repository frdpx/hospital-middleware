// Package config loads all runtime configuration from environment variables
// (12-factor), so the same binary runs locally, in docker-compose and in CI
// with nothing but env differences.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const minJWTSecretLength = 32

type Config struct {
	App AppConfig
	DB  DBConfig
	JWT JWTConfig
	HIS HISConfig
}

type AppConfig struct {
	Env      string
	Port     string
	LogLevel string
}

// IsProduction reports whether extra safety checks (e.g. rejecting the sample
// JWT secret) should apply.
func (a AppConfig) IsProduction() bool { return a.Env == "production" }

type DBConfig struct {
	Host            string
	Port            string
	User            string
	Password        string
	Name            string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	AutoMigrate     bool
}

// DSN builds a lib/pq-style connection string. Credentials are URL-escaped so
// passwords containing special characters do not break the DSN.
func (d DBConfig) DSN() string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(d.User, d.Password),
		Host:   fmt.Sprintf("%s:%s", d.Host, d.Port),
		Path:   d.Name,
	}
	q := u.Query()
	q.Set("sslmode", d.SSLMode)
	u.RawQuery = q.Encode()
	return u.String()
}

type JWTConfig struct {
	Secret string
	Issuer string
	TTL    time.Duration
}

type HISConfig struct {
	Timeout time.Duration
	// BaseURLOverride, when set, replaces every hospital's stored HIS base URL.
	// Used to point the whole service at a local mock HIS during development.
	BaseURLOverride string
}

// Load reads configuration from the environment and validates it. It returns
// every validation problem at once rather than failing on the first, so a
// misconfigured deployment can be fixed in one pass.
func Load() (*Config, error) {
	cfg := &Config{
		App: AppConfig{
			Env:      getString("APP_ENV", "development"),
			Port:     getString("APP_PORT", "8080"),
			LogLevel: getString("APP_LOG_LEVEL", "info"),
		},
		DB: DBConfig{
			Host:            getString("POSTGRES_HOST", "localhost"),
			Port:            getString("POSTGRES_PORT", "5432"),
			User:            getString("POSTGRES_USER", "hospital"),
			Password:        getString("POSTGRES_PASSWORD", ""),
			Name:            getString("POSTGRES_DB", "hospital_middleware"),
			SSLMode:         getString("POSTGRES_SSLMODE", "disable"),
			MaxOpenConns:    getInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getInt("DB_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: getDuration("DB_CONN_MAX_LIFETIME", 30*time.Minute),
			AutoMigrate:     getBool("DB_AUTO_MIGRATE", true),
		},
		JWT: JWTConfig{
			Secret: getString("JWT_SECRET", ""),
			Issuer: getString("JWT_ISSUER", "hospital-middleware"),
			TTL:    getDuration("JWT_TTL", time.Hour),
		},
		HIS: HISConfig{
			Timeout:         getDuration("HIS_TIMEOUT", 5*time.Second),
			BaseURLOverride: getString("HIS_BASE_URL_OVERRIDE", ""),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	var problems []string

	if c.DB.Password == "" {
		problems = append(problems, "POSTGRES_PASSWORD is required")
	}
	if len(c.JWT.Secret) < minJWTSecretLength {
		problems = append(problems, fmt.Sprintf("JWT_SECRET must be at least %d characters", minJWTSecretLength))
	}
	if c.App.IsProduction() && strings.HasPrefix(c.JWT.Secret, "change-me") {
		problems = append(problems, "JWT_SECRET still holds the sample value from .env.example")
	}
	if c.JWT.TTL <= 0 {
		problems = append(problems, "JWT_TTL must be positive")
	}
	if c.HIS.Timeout <= 0 {
		problems = append(problems, "HIS_TIMEOUT must be positive")
	}
	if c.DB.MaxOpenConns <= 0 {
		problems = append(problems, "DB_MAX_OPEN_CONNS must be positive")
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid configuration: %s", strings.Join(problems, "; "))
	}
	return nil
}

func getString(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getInt(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func getBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func getDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return parsed
}
