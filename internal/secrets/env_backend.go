/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/joho/godotenv"
)

// EnvBackend persists secrets in a .env file with atomic rewrites. Intended
// for single-instance deployments per the secrets architecture (Q6 in the
// observability plan).
//
// Get reads from the process environment first so an operator-exported
// override (`export FOO=bar`) wins over the file contents. Put only writes
// to the file; process env is never mutated.
//
// Concurrent rewrites are serialized via an in-process mutex and a flock on
// the file (defensive against another grimnir process on the same host).
type EnvBackend struct {
	path string
	mu   sync.Mutex // serializes file rewrites within the process
}

// NewEnvBackend opens (or creates) the .env file at path with 0600 perms.
func NewEnvBackend(path string) (*EnvBackend, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return nil, fmt.Errorf("secrets/env: create %s: %w", path, err)
		}
		f.Close()
	} else if err != nil {
		return nil, fmt.Errorf("secrets/env: stat %s: %w", path, err)
	}
	return &EnvBackend{path: path}, nil
}

// Close is a no-op; the file is reopened per operation.
func (e *EnvBackend) Close() error { return nil }

// Get returns the secret value. Process env wins over the file so operators
// can override locally without rewriting state.
func (e *EnvBackend) Get(_ context.Context, name string) (string, error) {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return v, nil
	}
	kv, err := godotenv.Read(e.path)
	if err != nil {
		return "", fmt.Errorf("secrets/env: read %s: %w", e.path, err)
	}
	v, ok := kv[name]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

// Put writes (or updates) name=value in the file atomically.
func (e *EnvBackend) Put(_ context.Context, name, value string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rewriteUnlocked(func(kv map[string]string) {
		kv[name] = value
	})
}

// List returns every key in the file whose name starts with prefix, sorted.
func (e *EnvBackend) List(_ context.Context, prefix string) ([]string, error) {
	kv, err := godotenv.Read(e.path)
	if err != nil {
		return nil, fmt.Errorf("secrets/env: read %s: %w", e.path, err)
	}
	var out []string
	for k := range kv {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Rotate stages newValue, runs verify, and either commits or rolls back to
// the prior value. Returns the previous value so callers can perform an
// emergency manual restore if the post-rotate flow elsewhere blows up.
func (e *EnvBackend) Rotate(ctx context.Context, name, newValue string, verify func(context.Context, string) error) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	kv, err := godotenv.Read(e.path)
	if err != nil {
		return "", fmt.Errorf("secrets/env: read %s: %w", e.path, err)
	}
	old := kv[name]

	if err := e.rewriteUnlocked(func(m map[string]string) { m[name] = newValue }); err != nil {
		return old, err
	}

	if err := verify(ctx, newValue); err != nil {
		if rbErr := e.rewriteUnlocked(func(m map[string]string) { m[name] = old }); rbErr != nil {
			return old, fmt.Errorf("verify failed (%v) AND rollback failed (%v)", err, rbErr)
		}
		return old, fmt.Errorf("verify failed; rolled back: %w", err)
	}
	return old, nil
}

// rewriteUnlocked rewrites the file atomically (temp + rename) under flock.
// Caller must hold e.mu.
func (e *EnvBackend) rewriteUnlocked(mutate func(map[string]string)) error {
	kv, err := godotenv.Read(e.path)
	if err != nil {
		return fmt.Errorf("secrets/env: read %s: %w", e.path, err)
	}
	mutate(kv)

	// Cross-process serialization via flock on the live file.
	if f, err := os.OpenFile(e.path, os.O_RDONLY, 0600); err == nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		defer func() {
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
		}()
	}

	tmp := e.path + ".tmp"
	if err := godotenv.Write(kv, tmp); err != nil {
		return fmt.Errorf("secrets/env: write %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("secrets/env: chmod %s: %w", tmp, err)
	}

	if tf, err := os.OpenFile(tmp, os.O_RDONLY, 0600); err == nil {
		_ = tf.Sync()
		tf.Close()
	}

	if err := os.Rename(tmp, e.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("secrets/env: rename %s: %w", tmp, err)
	}

	if dir, err := os.Open(filepath.Dir(e.path)); err == nil {
		_ = dir.Sync()
		dir.Close()
	}
	return nil
}
