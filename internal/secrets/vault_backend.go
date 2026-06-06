/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package secrets

import (
	"context"
	"fmt"
)

// VaultConfig holds Vault KV v2 + AppRole connection parameters.
type VaultConfig struct {
	Address    string
	RoleID     string
	SecretID   string
	MountPath  string // "secret" by default
	PathPrefix string // "grimnir" by default; secrets stored under {mount}/data/{prefix}/{name}
}

// VaultBackend is the placeholder Vault KV v2 backend. The Vault substrate
// (Phase 0 operator work: server provisioning, AppRole policy, KV v2 mount)
// has not landed yet, so this backend returns ErrNotConfigured from every
// call. Constructing it via Open also fails fast so misconfigured deployments
// never silently fall back.
//
// Once Vault is provisioned, swap this stub for the hashicorp/vault/api
// implementation per docs/superpowers/plans/2026-06-06-observability-secrets-audit.md
// Task 6.3.
type VaultBackend struct {
	cfg VaultConfig
}

// NewVaultBackend currently refuses to construct: the Vault substrate is not
// provisioned. The error guides the operator to GRIMNIR_SECRETS_BACKEND=env.
func NewVaultBackend(_ context.Context, cfg VaultConfig) (*VaultBackend, error) {
	return nil, fmt.Errorf("%w: Vault backend not yet provisioned; set GRIMNIR_SECRETS_BACKEND=env", ErrNotConfigured)
}

func (v *VaultBackend) Close() error { return nil }

func (v *VaultBackend) Get(_ context.Context, _ string) (string, error) {
	return "", ErrNotConfigured
}

func (v *VaultBackend) Put(_ context.Context, _, _ string) error {
	return ErrNotConfigured
}

func (v *VaultBackend) List(_ context.Context, _ string) ([]string, error) {
	return nil, ErrNotConfigured
}

func (v *VaultBackend) Rotate(_ context.Context, _, _ string, _ func(context.Context, string) error) (string, error) {
	return "", ErrNotConfigured
}
