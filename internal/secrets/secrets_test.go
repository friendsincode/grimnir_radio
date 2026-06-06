/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package secrets

import (
	"context"
	"errors"
	"sort"
	"testing"
)

// backendFactory returns a fresh backend for each test. Each implementation
// registers a factory here; the contract suite runs against each.
type backendFactory struct {
	name string
	make func(t *testing.T) Backend
}

func allBackends(t *testing.T) []backendFactory {
	t.Helper()
	return []backendFactory{
		{"env", func(t *testing.T) Backend {
			path := t.TempDir() + "/.env"
			b, err := NewEnvBackend(path)
			if err != nil {
				t.Fatal(err)
			}
			return b
		}},
		// vault factory added once Vault is provisioned; the stub backend
		// returns ErrNotConfigured immediately, so it cannot run the contract.
	}
}

func TestContract_PutThenGet(t *testing.T) {
	for _, bf := range allBackends(t) {
		t.Run(bf.name, func(t *testing.T) {
			b := bf.make(t)
			defer b.Close()
			ctx := context.Background()
			if err := b.Put(ctx, "FOO", "bar"); err != nil {
				t.Fatal(err)
			}
			got, err := b.Get(ctx, "FOO")
			if err != nil {
				t.Fatal(err)
			}
			if got != "bar" {
				t.Errorf("got %q, want bar", got)
			}
		})
	}
}

func TestContract_MissingReturnsErrNotFound(t *testing.T) {
	for _, bf := range allBackends(t) {
		t.Run(bf.name, func(t *testing.T) {
			b := bf.make(t)
			defer b.Close()
			_, err := b.Get(context.Background(), "DOES_NOT_EXIST")
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestContract_List(t *testing.T) {
	for _, bf := range allBackends(t) {
		t.Run(bf.name, func(t *testing.T) {
			b := bf.make(t)
			defer b.Close()
			ctx := context.Background()
			if err := b.Put(ctx, "A_ONE", "1"); err != nil {
				t.Fatal(err)
			}
			if err := b.Put(ctx, "A_TWO", "2"); err != nil {
				t.Fatal(err)
			}
			if err := b.Put(ctx, "B_ONE", "3"); err != nil {
				t.Fatal(err)
			}
			got, err := b.List(ctx, "A_")
			if err != nil {
				t.Fatal(err)
			}
			sort.Strings(got)
			if len(got) != 2 || got[0] != "A_ONE" || got[1] != "A_TWO" {
				t.Errorf("list = %v, want [A_ONE A_TWO]", got)
			}
		})
	}
}

func TestContract_RotateSuccess(t *testing.T) {
	for _, bf := range allBackends(t) {
		t.Run(bf.name, func(t *testing.T) {
			b := bf.make(t)
			defer b.Close()
			ctx := context.Background()
			if err := b.Put(ctx, "KEY", "old"); err != nil {
				t.Fatal(err)
			}
			old, err := b.Rotate(ctx, "KEY", "new", func(_ context.Context, v string) error {
				if v != "new" {
					t.Errorf("verifier got %q", v)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if old != "old" {
				t.Errorf("old = %q, want old", old)
			}
			now, err := b.Get(ctx, "KEY")
			if err != nil {
				t.Fatal(err)
			}
			if now != "new" {
				t.Errorf("after rotate, got %q, want new", now)
			}
		})
	}
}

func TestContract_RotateVerifierFailureRollsBack(t *testing.T) {
	for _, bf := range allBackends(t) {
		t.Run(bf.name, func(t *testing.T) {
			b := bf.make(t)
			defer b.Close()
			ctx := context.Background()
			if err := b.Put(ctx, "KEY", "old"); err != nil {
				t.Fatal(err)
			}
			_, err := b.Rotate(ctx, "KEY", "new", func(_ context.Context, _ string) error {
				return errors.New("does not authenticate")
			})
			if err == nil {
				t.Error("expected rotate error")
			}
			got, err := b.Get(ctx, "KEY")
			if err != nil {
				t.Fatal(err)
			}
			if got != "old" {
				t.Errorf("after failed rotate, got %q, want old (rolled back)", got)
			}
		})
	}
}

func TestOpen_UnknownBackend(t *testing.T) {
	t.Setenv("GRIMNIR_SECRETS_BACKEND", "no-such-thing")
	if _, err := Open(context.Background()); err == nil {
		t.Error("expected error for unknown backend")
	}
}

func TestOpen_DefaultsToEnv(t *testing.T) {
	t.Setenv("GRIMNIR_SECRETS_BACKEND", "")
	t.Setenv("GRIMNIR_SECRETS_ENV_FILE", t.TempDir()+"/.env")
	b, err := Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, ok := b.(*EnvBackend); !ok {
		t.Errorf("Open default = %T, want *EnvBackend", b)
	}
}
