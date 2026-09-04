// Package config loads the identity service's settings from environment
// variables. Every knob has an IDENTITY_ prefix so several services can
// share one environment (docker compose, a kustomize overlay) without
// colliding.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"
)

// Config is the fully resolved service configuration.
type Config struct {
	HTTPAddr       string
	DatabaseURL    string
	MigrateOnStart bool

	// JWTPrivateKeyFile is a PKCS#8 PEM Ed25519 key. Exactly one of it or
	// JWTDevEphemeralKey must be set: the latter generates a throwaway
	// key at startup, which invalidates every token on restart and is
	// only acceptable for local development.
	JWTPrivateKeyFile  string
	JWTDevEphemeralKey bool
	JWTIssuer          string
	JWTAudience        string
	AccessTokenTTL     time.Duration
	RefreshTokenTTL    time.Duration

	ShutdownTimeout time.Duration
	LogLevel        string
}

// SlogLevel converts LogLevel to a slog.Level; unknown values are info.
func (c Config) SlogLevel() slog.Level {
	switch c.LogLevel {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Load reads configuration through lookup (normally os.LookupEnv) and
// validates it. All problems are reported together rather than one per
// restart.
func Load(lookup func(string) (string, bool)) (Config, error) {
	var errs []error
	env := reader{lookup: lookup, errs: &errs}

	cfg := Config{
		HTTPAddr:           env.string("IDENTITY_HTTP_ADDR", ":8080"),
		DatabaseURL:        env.string("IDENTITY_DATABASE_URL", ""),
		MigrateOnStart:     env.bool("IDENTITY_MIGRATE_ON_START", true),
		JWTPrivateKeyFile:  env.string("IDENTITY_JWT_PRIVATE_KEY_FILE", ""),
		JWTDevEphemeralKey: env.bool("IDENTITY_JWT_DEV_EPHEMERAL_KEY", false),
		JWTIssuer:          env.string("IDENTITY_JWT_ISSUER", "coinly-identity"),
		JWTAudience:        env.string("IDENTITY_JWT_AUDIENCE", "coinly"),
		AccessTokenTTL:     env.duration("IDENTITY_ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL:    env.duration("IDENTITY_REFRESH_TOKEN_TTL", 30*24*time.Hour),
		ShutdownTimeout:    env.duration("IDENTITY_SHUTDOWN_TIMEOUT", 10*time.Second),
		LogLevel:           env.string("IDENTITY_LOG_LEVEL", "info"),
	}

	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("IDENTITY_DATABASE_URL is required"))
	}
	switch {
	case cfg.JWTPrivateKeyFile == "" && !cfg.JWTDevEphemeralKey:
		errs = append(errs, errors.New("IDENTITY_JWT_PRIVATE_KEY_FILE is required (or set IDENTITY_JWT_DEV_EPHEMERAL_KEY=true for local dev)"))
	case cfg.JWTPrivateKeyFile != "" && cfg.JWTDevEphemeralKey:
		errs = append(errs, errors.New("IDENTITY_JWT_PRIVATE_KEY_FILE and IDENTITY_JWT_DEV_EPHEMERAL_KEY are mutually exclusive"))
	}
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("IDENTITY_LOG_LEVEL %q: want debug|info|warn|error", cfg.LogLevel))
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("config: %w", errors.Join(errs...))
	}
	return cfg, nil
}

// reader accumulates parse errors so Load can report them all at once.
type reader struct {
	lookup func(string) (string, bool)
	errs   *[]error
}

func (r reader) string(key, def string) string {
	if v, ok := r.lookup(key); ok {
		return v
	}
	return def
}

func (r reader) bool(key string, def bool) bool {
	v, ok := r.lookup(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		*r.errs = append(*r.errs, fmt.Errorf("%s: %w", key, err))
		return def
	}
	return b
}

func (r reader) duration(key string, def time.Duration) time.Duration {
	v, ok := r.lookup(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		*r.errs = append(*r.errs, fmt.Errorf("%s: %w", key, err))
		return def
	}
	if d <= 0 {
		*r.errs = append(*r.errs, fmt.Errorf("%s: must be positive, got %s", key, d))
		return def
	}
	return d
}
