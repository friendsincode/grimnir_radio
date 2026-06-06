/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package runner

import (
	"context"
	"strings"
	"testing"
)

func TestFakeRecordsCalls(t *testing.T) {
	f := NewFake()
	f.SetResponse("hostname", "node-1\n", "", 0, nil)
	out, _, code, err := f.Run(context.Background(), "node-1", "hostname")
	if err != nil || code != 0 {
		t.Fatalf("Run: err=%v code=%d", err, code)
	}
	if strings.TrimSpace(out) != "node-1" {
		t.Errorf("stdout = %q", out)
	}
	if len(f.Calls) != 1 {
		t.Fatalf("Calls = %d, want 1", len(f.Calls))
	}
	if f.Calls[0].Host != "node-1" || f.Calls[0].Cmd != "hostname" {
		t.Errorf("Calls[0] = %+v", f.Calls[0])
	}
}

func TestFakeMatchesPrefixedResponse(t *testing.T) {
	f := NewFake()
	f.SetResponsePrefix("docker compose", "ok\n", "", 0, nil)
	out, _, _, err := f.Run(context.Background(), "local", "docker compose up -d grimnir")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("prefix match did not return configured response; got %q", out)
	}
}

func TestFakeDefaultsToExitZeroEmptyStdout(t *testing.T) {
	f := NewFake()
	out, _, code, err := f.Run(context.Background(), "local", "anything")
	if err != nil || code != 0 || out != "" {
		t.Errorf("default response: err=%v code=%d out=%q", err, code, out)
	}
}
