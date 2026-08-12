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

// Prod was found running with GRIMNIR_ENVIRONMENT=production set while the
// loader only read GRIMNIR_ENV, so it silently ran as "development": debug
// logging filled the container log fast enough that rotation kept under four
// hours, and session cookies went out without Secure. The _ENVIRONMENT
// spellings must resolve.
func TestLoadAcceptsEnvironmentSpellings(t *testing.T) {
	for _, key := range []string{"GRIMNIR_ENV", "RLM_ENV", "GRIMNIR_ENVIRONMENT", "RLM_ENVIRONMENT"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv("GRIMNIR_DB_DSN", "host=localhost user=test dbname=test sslmode=disable")
			t.Setenv("GRIMNIR_JWT_SIGNING_KEY", "supersecret")
			for _, k := range EnvironmentEnvKeys {
				t.Setenv(k, "")
			}
			t.Setenv(key, "production")

			cfg, err := Load()
			if err != nil {
				t.Fatalf("load config: %v", err)
			}
			if cfg.Environment != "production" {
				t.Fatalf("%s=production gave Environment=%q, want \"production\"", key, cfg.Environment)
			}
		})
	}
}

// With none of them set the loader still defaults to development, so a machine
// that configures nothing does not silently get production behaviour.
func TestLoadDefaultsToDevelopmentWithNoEnvironmentSet(t *testing.T) {
	t.Setenv("GRIMNIR_DB_DSN", "host=localhost user=test dbname=test sslmode=disable")
	t.Setenv("GRIMNIR_JWT_SIGNING_KEY", "supersecret")
	for _, k := range EnvironmentEnvKeys {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Environment != "development" {
		t.Fatalf("Environment=%q with nothing set, want \"development\"", cfg.Environment)
	}
}
