/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package config

import (
	"testing"
)

// TestGetEnvBoolAny covers the truthy/falsey parsing and the fallback (#86): a
// typo like GRIMNIR_X=on must fall back to the default, not silently flip.
func TestGetEnvBoolAny(t *testing.T) {
	const key = "GRIMNIR_TEST_BOOL"
	cases := []struct {
		val  string
		def  bool
		want bool
	}{
		{"true", false, true},
		{"1", false, true},
		{"yes", false, true},
		{"false", true, false},
		{"0", true, false},
		{"no", true, false},
		{"TRUE", false, true},   // case-insensitive
		{"garbage", true, true}, // unrecognized -> default
		{"garbage", false, false},
	}
	for _, c := range cases {
		t.Setenv(key, c.val)
		if got := getEnvBoolAny([]string{key}, c.def); got != c.want {
			t.Errorf("getEnvBoolAny(%q, def=%v) = %v, want %v", c.val, c.def, got, c.want)
		}
	}
}

func TestGetEnvIntAny(t *testing.T) {
	const key = "GRIMNIR_TEST_INT"
	t.Setenv(key, "42")
	if got := getEnvIntAny([]string{key}, 7); got != 42 {
		t.Errorf("valid int = %d, want 42", got)
	}
	t.Setenv(key, "notanumber")
	if got := getEnvIntAny([]string{key}, 7); got != 7 {
		t.Errorf("invalid int should fall back to default, got %d", got)
	}
}

func TestGetEnvFloatAny(t *testing.T) {
	const key = "GRIMNIR_TEST_FLOAT"
	t.Setenv(key, "1.5")
	if got := getEnvFloatAny([]string{key}, 0.1); got != 1.5 {
		t.Errorf("valid float = %v, want 1.5", got)
	}
	t.Setenv(key, "x")
	if got := getEnvFloatAny([]string{key}, 0.1); got != 0.1 {
		t.Errorf("invalid float should fall back to default, got %v", got)
	}
}

func TestMaxUploadSizeBytes(t *testing.T) {
	if got := (&Config{MaxUploadSizeMB: 8}).MaxUploadSizeBytes(); got != 8*1024*1024 {
		t.Errorf("8 MB = %d, want %d", got, 8*1024*1024)
	}
	if got := (&Config{MaxUploadSizeMB: 0}).MaxUploadSizeBytes(); got != 0 {
		t.Errorf("0 MB = %d, want 0", got)
	}
	var nilCfg *Config
	if got := nilCfg.MaxUploadSizeBytes(); got != 0 {
		t.Errorf("nil config = %d, want 0", got)
	}
}
