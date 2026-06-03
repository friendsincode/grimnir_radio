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

func TestRun_CleanDir(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--dir=testdata/migrations_clean"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("clean dir exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("clean dir stdout = %q, want empty", stdout.String())
	}
}

func TestRun_DirtyDir(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--dir=testdata/migrations_dirty"}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("dirty dir exit code = %d, want 1; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "DROP COLUMN") {
		t.Errorf("dirty dir stdout = %q, want to contain 'DROP COLUMN'", stdout.String())
	}
}

func TestRun_MissingDir(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--dir=testdata/does_not_exist"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("missing dir exit code = %d, want 2", code)
	}
}
