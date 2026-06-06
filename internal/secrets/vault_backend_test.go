/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package secrets

import (
	"context"
	"errors"
	"testing"
)

// The Vault substrate is not provisioned yet. These tests pin the stub
// contract: constructor refuses, every method returns ErrNotConfigured.
// When Task 6.3 lands a real implementation, replace this file with a
// dev-server harness per the plan.

func TestVaultBackend_NewRefusesWithErrNotConfigured(t *testing.T) {
	_, err := NewVaultBackend(context.Background(), VaultConfig{
		Address: "http://127.0.0.1:8200", RoleID: "r", SecretID: "s",
	})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want wraps ErrNotConfigured", err)
	}
}

func TestVaultBackend_StubMethodsReturnErrNotConfigured(t *testing.T) {
	// Use a bare struct (constructor path is gated above). When the real
	// backend lands, delete this test.
	v := &VaultBackend{}
	ctx := context.Background()
	if _, err := v.Get(ctx, "K"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Get err = %v", err)
	}
	if err := v.Put(ctx, "K", "v"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Put err = %v", err)
	}
	if _, err := v.List(ctx, ""); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("List err = %v", err)
	}
	if _, err := v.Rotate(ctx, "K", "v", func(context.Context, string) error { return nil }); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Rotate err = %v", err)
	}
	if err := v.Close(); err != nil {
		t.Errorf("Close err = %v", err)
	}
}

func TestOpen_VaultBackendFailsFast(t *testing.T) {
	// Selecting vault must NOT silently fall back to env.
	t.Setenv("GRIMNIR_SECRETS_BACKEND", "vault")
	t.Setenv("VAULT_ADDR", "http://127.0.0.1:8200")
	t.Setenv("VAULT_ROLE_ID", "r")
	t.Setenv("VAULT_SECRET_ID", "s")
	_, err := Open(context.Background())
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Open with backend=vault err = %v, want wraps ErrNotConfigured", err)
	}
}
