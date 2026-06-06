/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package secrets

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestEnvBackend_ProcessEnvWinsOverFile(t *testing.T) {
	path := t.TempDir() + "/.env"
	b, err := NewEnvBackend(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx := context.Background()
	if err := b.Put(ctx, "GRIMNIR_TEST_ENVWINS", "from-file"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRIMNIR_TEST_ENVWINS", "from-env")
	got, err := b.Get(ctx, "GRIMNIR_TEST_ENVWINS")
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-env" {
		t.Errorf("got %q, want from-env", got)
	}
}

func TestEnvBackend_FilePermissions(t *testing.T) {
	path := t.TempDir() + "/.env"
	b, err := NewEnvBackend(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := b.Put(context.Background(), "FOO", "bar"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("perm = %o, want 0600", mode)
	}
}

func TestEnvBackend_AtomicRewrite_NoTmpLeft(t *testing.T) {
	path := t.TempDir() + "/.env"
	b, err := NewEnvBackend(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := b.Put(context.Background(), "FOO", "bar"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp left behind: %v", err)
	}
}

func TestEnvBackend_PersistsAcrossInstances(t *testing.T) {
	path := t.TempDir() + "/.env"
	b1, err := NewEnvBackend(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := b1.Put(context.Background(), "PERSISTED", "yes"); err != nil {
		t.Fatal(err)
	}
	b1.Close()

	b2, err := NewEnvBackend(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b2.Close()
	got, err := b2.Get(context.Background(), "PERSISTED")
	if err != nil {
		t.Fatal(err)
	}
	if got != "yes" {
		t.Errorf("got %q, want yes", got)
	}
}

func TestEnvBackend_NewBackendCreatesFile(t *testing.T) {
	path := t.TempDir() + "/fresh.env"
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("precondition: file should not exist")
	}
	b, err := NewEnvBackend(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("freshly created perm = %o, want 0600", mode)
	}
}

func TestEnvBackend_PutOverwritesExisting(t *testing.T) {
	path := t.TempDir() + "/.env"
	b, _ := NewEnvBackend(path)
	defer b.Close()
	ctx := context.Background()
	if err := b.Put(ctx, "KEY", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := b.Put(ctx, "KEY", "v2"); err != nil {
		t.Fatal(err)
	}
	got, _ := b.Get(ctx, "KEY")
	if got != "v2" {
		t.Errorf("got %q, want v2", got)
	}
}

func TestEnvBackend_ConcurrentPuts(t *testing.T) {
	// All writers should complete without corrupting the file; final state
	// must be readable and consistent.
	path := t.TempDir() + "/.env"
	b, _ := NewEnvBackend(path)
	defer b.Close()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			key := "KEY_" + string(rune('A'+i%5))
			if err := b.Put(ctx, key, "v"); err != nil {
				t.Errorf("put: %v", err)
			}
		}()
	}
	wg.Wait()

	got, err := b.List(ctx, "KEY_")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Errorf("after concurrent puts, list = %v (want 5 keys)", got)
	}
}

func TestEnvBackend_RotateFailurePreservesFile(t *testing.T) {
	// Sanity: when verifier rejects, the on-disk file ends up holding the
	// old value, not the new one.
	path := t.TempDir() + "/.env"
	b, _ := NewEnvBackend(path)
	defer b.Close()
	ctx := context.Background()
	if err := b.Put(ctx, "TOKEN", "original"); err != nil {
		t.Fatal(err)
	}
	_, err := b.Rotate(ctx, "TOKEN", "candidate", func(_ context.Context, _ string) error {
		return errors.New("reject")
	})
	if err == nil {
		t.Fatal("expected rotate error")
	}

	// Re-read file directly (bypassing process-env override).
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "TOKEN") || !strings.Contains(string(data), "original") {
		t.Errorf("file contents after rollback = %q", string(data))
	}
	if strings.Contains(string(data), "candidate") {
		t.Errorf("file still contains candidate value: %q", string(data))
	}
}

func TestEnvBackend_GetMissingReturnsErrNotFound(t *testing.T) {
	path := t.TempDir() + "/.env"
	b, _ := NewEnvBackend(path)
	defer b.Close()
	_, err := b.Get(context.Background(), "NEVER_SET")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
