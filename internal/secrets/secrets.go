/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package secrets is a pluggable secret-store abstraction.
//
// Two backends are wired in phase 1:
//
//   - env   — .env file (default; matches single-instance + local-disk philosophy)
//   - vault — HashiCorp Vault KV v2 with AppRole auth (stubbed; not yet provisioned)
//
// Backend is selected via GRIMNIR_SECRETS_BACKEND. Both backends honor the
// same Backend interface so callers never branch on the backend type.
package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// ErrNotFound is returned by Get when the named secret does not exist.
var ErrNotFound = errors.New("secrets: not found")

// ErrNotConfigured is returned when a backend is selected but its required
// infrastructure (e.g., Vault server) has not been provisioned yet.
var ErrNotConfigured = errors.New("secrets: backend not configured")

// Backend is the pluggable secret-store interface. Both backends honour the
// same semantics; the contract suite in secrets_test.go is parameterized over
// every implementation.
type Backend interface {
	Get(ctx context.Context, name string) (string, error)
	Put(ctx context.Context, name, value string) error
	List(ctx context.Context, prefix string) ([]string, error)
	// Rotate stages a new value, verifies it via the verifier callback, then
	// commits. On verifier failure the old value is restored. Returns the OLD
	// value for emergency manual restore by the caller.
	Rotate(ctx context.Context, name, newValue string, verify func(ctx context.Context, candidate string) error) (oldValue string, err error)
	// Close releases backend resources (file handles, Vault tokens, etc).
	Close() error
}

// Open instantiates the backend selected by GRIMNIR_SECRETS_BACKEND. Defaults
// to "env".
func Open(ctx context.Context) (Backend, error) {
	backend := os.Getenv("GRIMNIR_SECRETS_BACKEND")
	if backend == "" {
		backend = "env"
	}
	switch backend {
	case "env":
		path := os.Getenv("GRIMNIR_SECRETS_ENV_FILE")
		if path == "" {
			path = ".env"
		}
		return NewEnvBackend(path)
	case "vault":
		return NewVaultBackend(ctx, VaultConfig{
			Address:    os.Getenv("VAULT_ADDR"),
			RoleID:     os.Getenv("VAULT_ROLE_ID"),
			SecretID:   os.Getenv("VAULT_SECRET_ID"),
			MountPath:  getEnvDefault("VAULT_MOUNT", "secret"),
			PathPrefix: getEnvDefault("VAULT_PATH_PREFIX", "grimnir"),
		})
	default:
		return nil, fmt.Errorf("secrets: unknown backend %q", backend)
	}
}

func getEnvDefault(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
