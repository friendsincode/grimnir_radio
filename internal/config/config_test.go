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
		t.Setenv("GRIMNIR_HA_PCM_RTP_TARGETS", "10.255.255.1:5004,10.255.255.2:5004")
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if !c.HAPCMRTPEnabled {
			t.Error("HAPCMRTPEnabled = false, want true")
		}
		want := []string{"10.255.255.1:5004", "10.255.255.2:5004"}
		if !reflect.DeepEqual(c.HAPCMRTPTargets, want) {
			t.Errorf("HAPCMRTPTargets = %v, want %v", c.HAPCMRTPTargets, want)
		}
	})

	t.Run("ignores whitespace in target list", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_HA_PCM_RTP_ENABLED", "true")
		t.Setenv("GRIMNIR_HA_PCM_RTP_TARGETS", " 10.255.255.1:5004 , 10.255.255.2:5004 ")
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"10.255.255.1:5004", "10.255.255.2:5004"}
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

func TestVRRPVIPsConfig(t *testing.T) {
	setBase := func(t *testing.T) {
		t.Setenv("GRIMNIR_DB_DSN", "host=localhost user=test dbname=test sslmode=disable")
		t.Setenv("GRIMNIR_JWT_SIGNING_KEY", "supersecret")
	}

	t.Run("empty by default", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_VRRP_VIPS", "")
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if len(c.VRRPVIPs) != 0 {
			t.Errorf("VRRPVIPs = %v, want empty", c.VRRPVIPs)
		}
	})

	t.Run("parses comma list with whitespace", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_VRRP_VIPS", " listener , dj ")
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"listener", "dj"}
		if !reflect.DeepEqual(c.VRRPVIPs, want) {
			t.Errorf("VRRPVIPs = %v, want %v", c.VRRPVIPs, want)
		}
	})
}

func TestLiveInputConfig(t *testing.T) {
	setBase := func(t *testing.T) {
		t.Setenv("GRIMNIR_DB_DSN", "host=localhost user=test dbname=test sslmode=disable")
		t.Setenv("GRIMNIR_JWT_SIGNING_KEY", "supersecret")
	}

	t.Run("disabled by default", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_LIVE_INPUT_ENABLED", "")
		t.Setenv("GRIMNIR_LIVE_INPUT_FANOUT_ADDR", "")
		t.Setenv("GRIMNIR_LIVE_INPUT_PORT", "")
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if c.LiveInputEnabled {
			t.Error("LiveInputEnabled = true, want false (default)")
		}
		if c.LiveInputPort != 5008 {
			t.Errorf("LiveInputPort = %d, want 5008 (default)", c.LiveInputPort)
		}
		if c.LiveInputFanoutAddr != "" {
			t.Errorf("LiveInputFanoutAddr = %q, want empty (default)", c.LiveInputFanoutAddr)
		}
	})

	t.Run("enabled with fanout addr succeeds", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_LIVE_INPUT_ENABLED", "true")
		t.Setenv("GRIMNIR_LIVE_INPUT_FANOUT_ADDR", "10.10.0.7:9100")
		t.Setenv("GRIMNIR_LIVE_INPUT_PORT", "5009")
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if !c.LiveInputEnabled {
			t.Error("LiveInputEnabled = false, want true")
		}
		if c.LiveInputFanoutAddr != "10.10.0.7:9100" {
			t.Errorf("LiveInputFanoutAddr = %q, want 10.10.0.7:9100", c.LiveInputFanoutAddr)
		}
		if c.LiveInputPort != 5009 {
			t.Errorf("LiveInputPort = %d, want 5009", c.LiveInputPort)
		}
	})

	t.Run("enabled requires fanout addr", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_LIVE_INPUT_ENABLED", "true")
		t.Setenv("GRIMNIR_LIVE_INPUT_FANOUT_ADDR", "")
		_, err := Load()
		if err == nil {
			t.Error("Load with LIVE_INPUT_ENABLED=true and empty fanout addr: want error, got nil")
		}
	})
}

func TestMediaBackendConfig(t *testing.T) {
	setBase := func(t *testing.T) {
		t.Setenv("GRIMNIR_DB_DSN", "host=localhost user=test dbname=test sslmode=disable")
		t.Setenv("GRIMNIR_JWT_SIGNING_KEY", "supersecret")
	}

	t.Run("defaults to fs", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_MEDIA_BACKEND", "")
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.MediaBackend != "fs" {
			t.Errorf("MediaBackend = %q, want fs", c.MediaBackend)
		}
	})

	t.Run("s3 requires bucket", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_MEDIA_BACKEND", "s3")
		t.Setenv("GRIMNIR_S3_BUCKET", "")
		if _, err := Load(); err == nil {
			t.Error("Load with MEDIA_BACKEND=s3 and empty S3_BUCKET: want error, got nil")
		}
	})

	t.Run("s3 with bucket succeeds", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_MEDIA_BACKEND", "s3")
		t.Setenv("GRIMNIR_S3_BUCKET", "grimnir-media-prod")
		t.Setenv("GRIMNIR_S3_ENDPOINT", "https://acc.r2.cloudflarestorage.com")
		t.Setenv("GRIMNIR_S3_ACCESS_KEY", "k")
		t.Setenv("GRIMNIR_S3_SECRET_KEY", "s")
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.MediaBackend != "s3" {
			t.Errorf("MediaBackend = %q, want s3", c.MediaBackend)
		}
		if c.S3Bucket != "grimnir-media-prod" {
			t.Errorf("S3Bucket = %q, want grimnir-media-prod", c.S3Bucket)
		}
		if c.S3AccessKeyID != "k" {
			t.Errorf("S3AccessKeyID = %q, want k (from GRIMNIR_S3_ACCESS_KEY)", c.S3AccessKeyID)
		}
		if c.S3SecretAccessKey != "s" {
			t.Errorf("S3SecretAccessKey = %q, want s (from GRIMNIR_S3_SECRET_KEY)", c.S3SecretAccessKey)
		}
	})

	t.Run("unknown backend rejected", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_MEDIA_BACKEND", "gcs")
		if _, err := Load(); err == nil {
			t.Error("Load with MEDIA_BACKEND=gcs: want error, got nil")
		}
	})

	t.Run("R2-friendly defaults: region=auto path-style=true", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_MEDIA_BACKEND", "fs")
		t.Setenv("GRIMNIR_S3_REGION", "")
		t.Setenv("AWS_REGION", "")
		t.Setenv("GRIMNIR_S3_PATH_STYLE", "")
		t.Setenv("GRIMNIR_S3_USE_PATH_STYLE", "")
		t.Setenv("S3_USE_PATH_STYLE", "")
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.S3Region != "auto" {
			t.Errorf("S3Region default = %q, want auto", c.S3Region)
		}
		if !c.S3UsePathStyle {
			t.Errorf("S3UsePathStyle default = false, want true (R2-friendly)")
		}
	})
}

func TestNetClockConfig(t *testing.T) {
	setBase := func(t *testing.T) {
		t.Setenv("GRIMNIR_DB_DSN", "host=localhost user=test dbname=test sslmode=disable")
		t.Setenv("GRIMNIR_JWT_SIGNING_KEY", "supersecret")
	}

	t.Run("disabled by default", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_NETCLOCK_ENABLED", "")
		t.Setenv("GRIMNIR_NETCLOCK_REGION", "")
		t.Setenv("GRIMNIR_NETCLOCK_PORT", "")
		t.Setenv("GRIMNIR_NETCLOCK_MASTER_ADDR", "")
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if c.NetClockEnabled {
			t.Error("NetClockEnabled = true, want false (default)")
		}
		if c.NetClockPort != 9094 {
			t.Errorf("NetClockPort = %d, want 9094 (default)", c.NetClockPort)
		}
		if c.NetClockRegion != "" {
			t.Errorf("NetClockRegion = %q, want empty (default)", c.NetClockRegion)
		}
		if c.NetClockMasterAddr != "" {
			t.Errorf("NetClockMasterAddr = %q, want empty (default)", c.NetClockMasterAddr)
		}
	})

	t.Run("enabled with region succeeds", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_NETCLOCK_ENABLED", "true")
		t.Setenv("GRIMNIR_NETCLOCK_REGION", "dfw1")
		t.Setenv("GRIMNIR_NETCLOCK_PORT", "9095")
		t.Setenv("GRIMNIR_NETCLOCK_MASTER_ADDR", "10.0.0.5:9095")
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if !c.NetClockEnabled {
			t.Error("NetClockEnabled = false, want true")
		}
		if c.NetClockRegion != "dfw1" {
			t.Errorf("NetClockRegion = %q, want dfw1", c.NetClockRegion)
		}
		if c.NetClockPort != 9095 {
			t.Errorf("NetClockPort = %d, want 9095", c.NetClockPort)
		}
		if c.NetClockMasterAddr != "10.0.0.5:9095" {
			t.Errorf("NetClockMasterAddr = %q, want 10.0.0.5:9095", c.NetClockMasterAddr)
		}
	})

	t.Run("enabled requires region", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_NETCLOCK_ENABLED", "true")
		t.Setenv("GRIMNIR_NETCLOCK_REGION", "")
		_, err := Load()
		if err == nil {
			t.Error("Load with NETCLOCK_ENABLED=true and empty region: want error, got nil")
		}
	})

	t.Run("RLM_ fallback works", func(t *testing.T) {
		setBase(t)
		t.Setenv("GRIMNIR_NETCLOCK_ENABLED", "")
		t.Setenv("RLM_NETCLOCK_ENABLED", "true")
		t.Setenv("GRIMNIR_NETCLOCK_REGION", "")
		t.Setenv("RLM_NETCLOCK_REGION", "ord1")
		t.Setenv("GRIMNIR_NETCLOCK_PORT", "")
		t.Setenv("RLM_NETCLOCK_PORT", "9099")
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if !c.NetClockEnabled {
			t.Error("NetClockEnabled = false, want true (RLM_ fallback)")
		}
		if c.NetClockRegion != "ord1" {
			t.Errorf("NetClockRegion = %q, want ord1 (RLM_ fallback)", c.NetClockRegion)
		}
		if c.NetClockPort != 9099 {
			t.Errorf("NetClockPort = %d, want 9099 (RLM_ fallback)", c.NetClockPort)
		}
	})
}
