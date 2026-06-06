/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelp(t *testing.T) {
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"--help"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("--help should not error: %v", err)
	}
	s := out.String()
	for _, name := range []string{
		"deploy",
		"verify",
		"drain",
		"emergency-pause",
		"emergency-resume",
		"promote-replica",
		"cold-start-region",
		"restore",
		"recover-partition",
		"backup-drill",
	} {
		if !strings.Contains(s, name) {
			t.Errorf("--help output missing subcommand %q\nfull output:\n%s", name, s)
		}
	}
}

func TestVersionFlag(t *testing.T) {
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"--version"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("--version should not error: %v", err)
	}
	if !strings.Contains(out.String(), "grimnir-deploy") {
		t.Errorf("--version output should contain binary name; got %q", out.String())
	}
}
