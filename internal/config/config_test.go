package config

import (
	"reflect"
	"testing"
)

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

func TestHAPCMRTPConfig(t *testing.T) {
	// Required base env vars for Load() to succeed.
	setBase := func(t *testing.T) {
		t.Setenv("GRIMNIR_DB_DSN", "host=localhost user=test dbname=test sslmode=disable")
		t.Setenv("GRIMNIR_JWT_SIGNING_KEY", "supersecret")
	}

	t.Run("disabled by default", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_HA_PCM_RTP_ENABLED", "")
		t.Setenv("GRIMNIR_HA_PCM_RTP_TARGETS", "")
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if c.HAPCMRTPEnabled {
			t.Error("HAPCMRTPEnabled = true, want false (default)")
		}
		if len(c.HAPCMRTPTargets) != 0 {
			t.Errorf("HAPCMRTPTargets = %v, want empty", c.HAPCMRTPTargets)
		}
	})

	t.Run("parses target list", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_HA_PCM_RTP_ENABLED", "true")
		t.Setenv("GRIMNIR_HA_PCM_RTP_TARGETS", "<node-a-ip>:5004,<node-b-ip>:5004")
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if !c.HAPCMRTPEnabled {
			t.Error("HAPCMRTPEnabled = false, want true")
		}
		want := []string{"<node-a-ip>:5004", "<node-b-ip>:5004"}
		if !reflect.DeepEqual(c.HAPCMRTPTargets, want) {
			t.Errorf("HAPCMRTPTargets = %v, want %v", c.HAPCMRTPTargets, want)
		}
	})

	t.Run("ignores whitespace in target list", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_HA_PCM_RTP_ENABLED", "true")
		t.Setenv("GRIMNIR_HA_PCM_RTP_TARGETS", " <node-a-ip>:5004 , <node-b-ip>:5004 ")
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"<node-a-ip>:5004", "<node-b-ip>:5004"}
		if !reflect.DeepEqual(c.HAPCMRTPTargets, want) {
			t.Errorf("HAPCMRTPTargets = %v, want %v", c.HAPCMRTPTargets, want)
		}
	})

	t.Run("enabled requires non-empty targets", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_HA_PCM_RTP_ENABLED", "true")
		t.Setenv("GRIMNIR_HA_PCM_RTP_TARGETS", "")
		_, err := Load()
		if err == nil {
			t.Error("Load with enabled=true and empty targets: want error, got nil")
		}
	})
}
