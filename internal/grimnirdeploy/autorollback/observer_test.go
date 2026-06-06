/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package autorollback

import (
	"testing"
)

func TestEnabledFromEnv_DefaultTrue(t *testing.T) {
	t.Setenv("GRIMNIR_DEPLOY_AUTOROLLBACK_ENABLED", "")
	if !EnabledFromEnv() {
		t.Error("default should be true")
	}
}

func TestEnabledFromEnv_DisabledForms(t *testing.T) {
	for _, v := range []string{"false", "FALSE", "0", "no", "NO", "off"} {
		t.Setenv("GRIMNIR_DEPLOY_AUTOROLLBACK_ENABLED", v)
		if EnabledFromEnv() {
			t.Errorf("value %q should disable", v)
		}
	}
}

func TestEnabledFromEnv_EnabledForms(t *testing.T) {
	for _, v := range []string{"true", "1", "yes", "on", "anything-else"} {
		t.Setenv("GRIMNIR_DEPLOY_AUTOROLLBACK_ENABLED", v)
		if !EnabledFromEnv() {
			t.Errorf("value %q should enable", v)
		}
	}
}

func TestNewMonitorObserver_DefaultsRulesAndTick(t *testing.T) {
	obs, err := NewMonitorObserver("http://example.com:9090", 1, 0, nil)
	if err != nil {
		t.Fatalf("NewMonitorObserver: %v", err)
	}
	m, ok := obs.(*Monitor)
	if !ok {
		t.Fatalf("expected *Monitor, got %T", obs)
	}
	if len(m.Rules) == 0 {
		t.Error("expected default rules")
	}
	if m.TickInterval == 0 {
		t.Error("expected default tick interval")
	}
}
