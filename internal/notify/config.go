/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package notify

import (
	"fmt"
	"os"
)

// Config carries every runtime parameter the notify Client needs. The Region
// drives per-region topic naming so a single ntfy server can multiplex alerts
// from every cluster without the operator having to subscribe to wildcard
// topics. Tokens are scoped per topic so a leaked audit token can't fire
// pager-grade alerts.
type Config struct {
	BaseURL       string
	Region        string
	PageToken     string
	AuditToken    string
	RollbackToken string
}

// PageTopic returns the per-region tier-2 topic name. Mirrors the convention
// in HA Section 8.1: operator pages land in grimnir-region-<region>-page.
func (c Config) PageTopic() string { return "grimnir-region-" + c.Region + "-page" }

// AuditTopic returns the per-region tier-1 topic name. Audit events land in
// grimnir-audit-<region> so the audit firehose is separable from pages.
func (c Config) AuditTopic() string { return "grimnir-audit-" + c.Region }

// RollbackTopic returns the per-region tier-3 topic name. Reserved for the
// auto-rollback hook so subscribers can wire a louder ringtone.
func (c Config) RollbackTopic() string { return "grimnir-region-" + c.Region + "-rollback" }

// LoadConfigFromEnv reads the ntfy configuration from GRIMNIR_NTFY_* variables.
// Returns an error when GRIMNIR_NTFY_URL is empty so callers can decide whether
// to abort startup or fall back to a NopNotifier.
func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		BaseURL:       os.Getenv("GRIMNIR_NTFY_URL"),
		Region:        getEnvDefault("GRIMNIR_REGION", "default"),
		PageToken:     os.Getenv("GRIMNIR_NTFY_TOKEN_PAGE"),
		AuditToken:    os.Getenv("GRIMNIR_NTFY_TOKEN_AUDIT"),
		RollbackToken: os.Getenv("GRIMNIR_NTFY_TOKEN_ROLLBACK"),
	}
	if cfg.BaseURL == "" {
		return cfg, fmt.Errorf("notify: GRIMNIR_NTFY_URL is required")
	}
	return cfg, nil
}

func getEnvDefault(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
