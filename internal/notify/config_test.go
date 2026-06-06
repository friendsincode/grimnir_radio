/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package notify

import (
	"os"
	"testing"
)

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("GRIMNIR_NTFY_URL", "https://ntfy.example")
	t.Setenv("GRIMNIR_NTFY_TOKEN_PAGE", "tk_page")
	t.Setenv("GRIMNIR_NTFY_TOKEN_AUDIT", "tk_audit")
	t.Setenv("GRIMNIR_NTFY_TOKEN_ROLLBACK", "tk_rollback")
	t.Setenv("GRIMNIR_REGION", "us-east")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://ntfy.example" {
		t.Errorf("base url = %q", cfg.BaseURL)
	}
	if cfg.PageTopic() != "grimnir-region-us-east-page" {
		t.Errorf("page topic = %q", cfg.PageTopic())
	}
	if cfg.AuditTopic() != "grimnir-audit-us-east" {
		t.Errorf("audit topic = %q", cfg.AuditTopic())
	}
	if cfg.RollbackTopic() != "grimnir-region-us-east-rollback" {
		t.Errorf("rollback topic = %q", cfg.RollbackTopic())
	}
}

func TestConfigFromEnv_MissingURLIsError(t *testing.T) {
	os.Unsetenv("GRIMNIR_NTFY_URL")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Error("expected error when URL unset")
	}
}

func TestConfigFromEnv_MissingRegionDefaultsToDefault(t *testing.T) {
	t.Setenv("GRIMNIR_NTFY_URL", "https://x")
	t.Setenv("GRIMNIR_NTFY_TOKEN_PAGE", "tk")
	t.Setenv("GRIMNIR_NTFY_TOKEN_AUDIT", "tk")
	t.Setenv("GRIMNIR_NTFY_TOKEN_ROLLBACK", "tk")
	os.Unsetenv("GRIMNIR_REGION")
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Region != "default" {
		t.Errorf("region = %q, want default", cfg.Region)
	}
}
