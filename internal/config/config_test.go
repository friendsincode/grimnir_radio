package config

import "testing"

func TestLoadReadsCriticalEnvKeys(t *testing.T) {
	t.Setenv("GRIMNIR_DB_DSN", "host=localhost user=test dbname=test sslmode=disable")
	t.Setenv("GRIMNIR_JWT_SIGNING_KEY", "supersecret")
	t.Setenv("GRIMNIR_ENV", "development")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.DBDSN == "" {
		t.Fatal("expected DB DSN to be set")
	}
	if cfg.JWTSigningKey != "supersecret" {
		t.Fatalf("unexpected jwt signing key: %q", cfg.JWTSigningKey)
	}
}

func TestLoadReportsLegacyEnvWarnings(t *testing.T) {
	t.Setenv("GRIMNIR_DB_DSN", "host=localhost user=test dbname=test sslmode=disable")
	t.Setenv("GRIMNIR_JWT_SIGNING_KEY", "supersecret")
	t.Setenv("JWT_SIGNING_KEY", "legacy")
	t.Setenv("TRACING_ENABLED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.LegacyEnvWarnings) == 0 {
		t.Fatal("expected legacy env warnings")
	}
}

func TestLoadProductionRequiresTurnCredentialsWhenTurnEnabled(t *testing.T) {
	t.Setenv("GRIMNIR_DB_DSN", "host=localhost user=test dbname=test sslmode=disable")
	t.Setenv("GRIMNIR_JWT_SIGNING_KEY", "supersecret")
	t.Setenv("GRIMNIR_ENV", "production")
	t.Setenv("GRIMNIR_WEBRTC_TURN_URL", "turn:turn.example.com:3478")
	t.Setenv("GRIMNIR_WEBRTC_TURN_USERNAME", "")
	t.Setenv("GRIMNIR_WEBRTC_TURN_PASSWORD", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected production config load to fail when TURN credentials are missing")
	}

	t.Setenv("GRIMNIR_WEBRTC_TURN_USERNAME", "user")
	t.Setenv("GRIMNIR_WEBRTC_TURN_PASSWORD", "pass")
	if _, err := Load(); err != nil {
		t.Fatalf("expected production config load with TURN creds to succeed: %v", err)
	}
}
