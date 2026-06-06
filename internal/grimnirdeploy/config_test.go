/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"testing"
	"time"
)

// TestLoadConfig_AutoRollbackDefaults verifies the auto-rollback envelope:
// the flag defaults true in production, the tick interval falls back to
// 15s, & the Prometheus URL is empty when unset (which the caller treats
// as "disabled even if the flag is on").
func TestLoadConfig_AutoRollbackDefaults(t *testing.T) {
	t.Setenv("GRIMNIR_DEPLOY_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("GRIMNIR_DEPLOY_AUTOROLLBACK_ENABLED", "")
	t.Setenv("GRIMNIR_DEPLOY_AUTOROLLBACK_PROM_URL", "")
	t.Setenv("GRIMNIR_DEPLOY_AUTOROLLBACK_TICK", "")
	t.Setenv("GRIMNIR_PROMETHEUS_URL", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.AutoRollbackEnabled {
		t.Error("AutoRollbackEnabled should default true")
	}
	if cfg.AutoRollbackTickInterval != 15*time.Second {
		t.Errorf("AutoRollbackTickInterval = %v, want 15s", cfg.AutoRollbackTickInterval)
	}
	if cfg.AutoRollbackPromURL != "" {
		t.Errorf("AutoRollbackPromURL = %q, want empty", cfg.AutoRollbackPromURL)
	}
}

func TestLoadConfig_AutoRollbackDisabled(t *testing.T) {
	t.Setenv("GRIMNIR_DEPLOY_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("GRIMNIR_DEPLOY_AUTOROLLBACK_ENABLED", "false")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AutoRollbackEnabled {
		t.Error("AutoRollbackEnabled should be false when env=false")
	}
}

func TestLoadConfig_AutoRollbackPromURLFallback(t *testing.T) {
	t.Setenv("GRIMNIR_DEPLOY_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("GRIMNIR_DEPLOY_AUTOROLLBACK_PROM_URL", "")
	t.Setenv("GRIMNIR_PROMETHEUS_URL", "http://prom:9090")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AutoRollbackPromURL != "http://prom:9090" {
		t.Errorf("expected fallback to GRIMNIR_PROMETHEUS_URL; got %q", cfg.AutoRollbackPromURL)
	}
}

func TestLoadConfig_AutoRollbackTickOverride(t *testing.T) {
	t.Setenv("GRIMNIR_DEPLOY_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("GRIMNIR_DEPLOY_AUTOROLLBACK_TICK", "30s")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AutoRollbackTickInterval != 30*time.Second {
		t.Errorf("tick = %v, want 30s", cfg.AutoRollbackTickInterval)
	}
}
