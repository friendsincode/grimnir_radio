/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later

Real-SSH coverage against a remote host lives in ssh_real_test.go behind the
build tag `//go:build integration && requires_real_cluster`. This file only
exercises the local-exec path; SSH-dial paths are covered by the integration
suite.
*/

package runner

import (
	"context"
	"strings"
	"testing"
)

func TestLocalEcho(t *testing.T) {
	r := NewSSHRunner("noone", 22, "", nil)
	out, _, code, err := r.Run(context.Background(), "local", "echo hello")
	if err != nil || code != 0 {
		t.Fatalf("local echo: err=%v code=%d", err, code)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("stdout = %q", out)
	}
}

func TestLocalExitNonZero(t *testing.T) {
	r := NewSSHRunner("noone", 22, "", nil)
	_, _, code, err := r.Run(context.Background(), "local", "exit 7")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}
