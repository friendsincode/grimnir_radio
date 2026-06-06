/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package notify

import (
	"context"
	"os"
	"testing"
)

func TestFakeNotifier_RecordsCalls(t *testing.T) {
	f := &FakeNotifier{}
	if err := f.Tier1(context.Background(), "t1", "b1"); err != nil {
		t.Fatal(err)
	}
	if err := f.Tier2(context.Background(), "t2", "b2"); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(f.Calls))
	}
	if f.Calls[0].Tier != 1 || f.Calls[0].Title != "t1" || f.Calls[0].Body != "b1" {
		t.Errorf("call[0] = %+v", f.Calls[0])
	}
	if f.Calls[1].Tier != 2 || f.Calls[1].Title != "t2" || f.Calls[1].Body != "b2" {
		t.Errorf("call[1] = %+v", f.Calls[1])
	}
}

func TestNopNotifier_NeverErrors(t *testing.T) {
	var n Notifier = NopNotifier{}
	if err := n.Tier1(context.Background(), "x", "y"); err != nil {
		t.Errorf("Tier1 = %v", err)
	}
	if err := n.Tier2(context.Background(), "x", "y"); err != nil {
		t.Errorf("Tier2 = %v", err)
	}
}

func TestClient_ImplementsNotifier(t *testing.T) {
	var _ Notifier = (*Client)(nil)
}

func TestFromEnv_UnsetURLReturnsNop(t *testing.T) {
	os.Unsetenv("GRIMNIR_NTFY_URL")
	n := FromEnv()
	if _, ok := n.(NopNotifier); !ok {
		t.Errorf("expected NopNotifier when URL unset, got %T", n)
	}
}

func TestFromEnv_SetURLReturnsClient(t *testing.T) {
	t.Setenv("GRIMNIR_NTFY_URL", "https://ntfy.example")
	t.Setenv("GRIMNIR_NTFY_TOKEN_PAGE", "tk")
	t.Setenv("GRIMNIR_NTFY_TOKEN_AUDIT", "tk")
	t.Setenv("GRIMNIR_NTFY_TOKEN_ROLLBACK", "tk")
	n := FromEnv()
	if _, ok := n.(*Client); !ok {
		t.Errorf("expected *Client when URL set, got %T", n)
	}
}
