package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/andytrue7/coinly/services/identity/internal/config"
)

func lookup(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := config.Load(lookup(map[string]string{
		"IDENTITY_DATABASE_URL":         "postgres://db/identity",
		"IDENTITY_JWT_PRIVATE_KEY_FILE": "/run/secrets/jwt.pem",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := config.Config{
		HTTPAddr:          ":8080",
		DatabaseURL:       "postgres://db/identity",
		MigrateOnStart:    true,
		JWTPrivateKeyFile: "/run/secrets/jwt.pem",
		JWTIssuer:         "coinly-identity",
		JWTAudience:       "coinly",
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   30 * 24 * time.Hour,
		ShutdownTimeout:   10 * time.Second,
		LogLevel:          "info",
	}
	if cfg != want {
		t.Errorf("Load() =\n%+v\nwant\n%+v", cfg, want)
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := config.Load(lookup(map[string]string{
		"IDENTITY_HTTP_ADDR":            "127.0.0.1:9000",
		"IDENTITY_DATABASE_URL":         "postgres://x",
		"IDENTITY_MIGRATE_ON_START":     "false",
		"IDENTITY_JWT_PRIVATE_KEY_FILE": "k.pem",
		"IDENTITY_JWT_ISSUER":           "iss",
		"IDENTITY_JWT_AUDIENCE":         "aud",
		"IDENTITY_ACCESS_TOKEN_TTL":     "5m",
		"IDENTITY_REFRESH_TOKEN_TTL":    "48h",
		"IDENTITY_SHUTDOWN_TIMEOUT":     "3s",
		"IDENTITY_LOG_LEVEL":            "debug",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9000" || cfg.MigrateOnStart || cfg.JWTIssuer != "iss" ||
		cfg.JWTAudience != "aud" || cfg.AccessTokenTTL != 5*time.Minute ||
		cfg.RefreshTokenTTL != 48*time.Hour || cfg.ShutdownTimeout != 3*time.Second ||
		cfg.LogLevel != "debug" {
		t.Errorf("overrides not applied: %+v", cfg)
	}
}

func TestLoad_DevEphemeralKey(t *testing.T) {
	cfg, err := config.Load(lookup(map[string]string{
		"IDENTITY_DATABASE_URL":          "postgres://x",
		"IDENTITY_JWT_DEV_EPHEMERAL_KEY": "true",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.JWTDevEphemeralKey || cfg.JWTPrivateKeyFile != "" {
		t.Errorf("cfg = %+v; want ephemeral key mode", cfg)
	}
}

func TestLoad_Errors(t *testing.T) {
	base := map[string]string{
		"IDENTITY_DATABASE_URL":         "postgres://x",
		"IDENTITY_JWT_PRIVATE_KEY_FILE": "k.pem",
	}
	tests := []struct {
		name    string
		mutate  func(map[string]string)
		wantMsg string
	}{
		{name: "missing database url", mutate: func(m map[string]string) { delete(m, "IDENTITY_DATABASE_URL") }, wantMsg: "IDENTITY_DATABASE_URL"},
		{name: "no key and no dev flag", mutate: func(m map[string]string) { delete(m, "IDENTITY_JWT_PRIVATE_KEY_FILE") }, wantMsg: "IDENTITY_JWT_PRIVATE_KEY_FILE"},
		{name: "key file and dev flag both set", mutate: func(m map[string]string) { m["IDENTITY_JWT_DEV_EPHEMERAL_KEY"] = "true" }, wantMsg: "IDENTITY_JWT_DEV_EPHEMERAL_KEY"},
		{name: "bad duration", mutate: func(m map[string]string) { m["IDENTITY_ACCESS_TOKEN_TTL"] = "soon" }, wantMsg: "IDENTITY_ACCESS_TOKEN_TTL"},
		{name: "zero ttl", mutate: func(m map[string]string) { m["IDENTITY_REFRESH_TOKEN_TTL"] = "0s" }, wantMsg: "IDENTITY_REFRESH_TOKEN_TTL"},
		{name: "bad bool", mutate: func(m map[string]string) { m["IDENTITY_MIGRATE_ON_START"] = "yes please" }, wantMsg: "IDENTITY_MIGRATE_ON_START"},
		{name: "bad log level", mutate: func(m map[string]string) { m["IDENTITY_LOG_LEVEL"] = "loud" }, wantMsg: "IDENTITY_LOG_LEVEL"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := make(map[string]string, len(base))
			for k, v := range base {
				env[k] = v
			}
			tc.mutate(env)

			_, err := config.Load(lookup(env))
			if err == nil {
				t.Fatal("Load err = nil, want error")
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("err = %q, want mention of %q", err, tc.wantMsg)
			}
		})
	}
}

func TestConfig_SlogLevel(t *testing.T) {
	for in, want := range map[string]string{"debug": "DEBUG", "info": "INFO", "warn": "WARN", "error": "ERROR"} {
		cfg := config.Config{LogLevel: in}
		if got := cfg.SlogLevel().String(); got != want {
			t.Errorf("SlogLevel(%q) = %q, want %q", in, got, want)
		}
	}
}
