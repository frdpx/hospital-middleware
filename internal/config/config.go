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
// every problem at once rather than failing on the first, so a misconfigured
// deployment can be fixed in one pass.
func Load() (*Config, error) {
	env := &reader{}

	cfg := &Config{
		App: AppConfig{
			Env:      env.str("APP_ENV", "development"),
			Port:     env.str("APP_PORT", "8080"),
			LogLevel: env.str("APP_LOG_LEVEL", "info"),
		},
		DB: DBConfig{
			Host:            env.str("POSTGRES_HOST", "localhost"),
			Port:            env.str("POSTGRES_PORT", "5432"),
			User:            env.str("POSTGRES_USER", "hospital"),
			Password:        env.str("POSTGRES_PASSWORD", ""),
			Name:            env.str("POSTGRES_DB", "hospital_middleware"),
			SSLMode:         env.str("POSTGRES_SSLMODE", "disable"),
			MaxOpenConns:    env.int("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    env.int("DB_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: env.duration("DB_CONN_MAX_LIFETIME", 30*time.Minute),
			AutoMigrate:     env.boolean("DB_AUTO_MIGRATE", true),
		},
		JWT: JWTConfig{
			Secret: env.str("JWT_SECRET", ""),
			Issuer: env.str("JWT_ISSUER", "hospital-middleware"),
			TTL:    env.duration("JWT_TTL", time.Hour),
		},
		HIS: HISConfig{
			Timeout:         env.duration("HIS_TIMEOUT", 5*time.Second),
			BaseURLOverride: env.str("HIS_BASE_URL_OVERRIDE", ""),
		},
	}

	if err := cfg.validate(env.problems); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate(problems []string) error {

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

// reader pulls typed values out of the environment, accumulating a problem for
// anything it cannot parse.
//
// An unset variable falls back silently — that is the documented default. A
// variable that is *set but malformed* is an operator mistake, and silently
// substituting the default would hide it: `JWT_TTL=1hour` would quietly become
// one hour, and nobody would find out until a token behaved unexpectedly in
// production. Those fail the process at startup instead.
type reader struct {
	problems []string
}

func (r *reader) rawValue(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

func (r *reader) invalid(key, value, expected string) {
	r.problems = append(r.problems, fmt.Sprintf("%s=%q is not a valid %s", key, value, expected))
}

func (r *reader) str(key, fallback string) string {
	if v, ok := r.rawValue(key); ok {
		return v
	}
	return fallback
}

func (r *reader) int(key string, fallback int) int {
	v, ok := r.rawValue(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		r.invalid(key, v, "integer")
		return fallback
	}
	return parsed
}

func (r *reader) boolean(key string, fallback bool) bool {
	v, ok := r.rawValue(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		r.invalid(key, v, "boolean (true/false)")
		return fallback
	}
	return parsed
}

func (r *reader) duration(key string, fallback time.Duration) time.Duration {
	v, ok := r.rawValue(key)
	if !ok {
		return fallback
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		r.invalid(key, v, `duration (e.g. "30s", "1h")`)
		return fallback
	}
	return parsed
}
