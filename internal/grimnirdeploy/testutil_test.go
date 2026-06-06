/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/pause"
)

// fakeNtfy is an in-memory audit.Poster used by tests.
type fakeNtfy struct {
	mu    sync.Mutex
	posts []ntfyCall
}

type ntfyCall struct {
	Topic    string
	Title    string
	Message  string
	Priority audit.Priority
}

func (f *fakeNtfy) Post(_ context.Context, topic, title, message string, priority audit.Priority, _ ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts = append(f.posts, ntfyCall{Topic: topic, Title: title, Message: message, Priority: priority})
	return nil
}

// testRecorder pairs an audit Store with an in-memory ntfy poster so the
// audit Wrapper can be exercised end-to-end without external services.
type testRecorder struct {
	store *audit.Store
	ntfy  *fakeNtfy
}

func (r testRecorder) WriteStart(ctx context.Context, op, ip, sub string, args map[string]any) (uuid.UUID, error) {
	return r.store.WriteStart(ctx, op, ip, sub, args)
}
func (r testRecorder) WriteComplete(ctx context.Context, id uuid.UUID, outcome string, dur time.Duration, notes string) error {
	return r.store.WriteComplete(ctx, id, outcome, dur, notes)
}
func (r testRecorder) WriteFailed(ctx context.Context, id uuid.UUID, outcome string, dur time.Duration) error {
	return r.store.WriteFailed(ctx, id, outcome, dur)
}
func (r testRecorder) PostNtfy(ctx context.Context, title, message string, priority audit.Priority) error {
	if r.ntfy == nil {
		return nil
	}
	return r.ntfy.Post(ctx, "test-topic", title, message, priority)
}

// testEnv bundles the deps every subcommand test needs. A separate struct
// (vs returning four values) lets future tests grow what they need without
// breaking every call site.
type testEnv struct {
	Pause *pause.Client
	Store *audit.Store
	Ntfy  *fakeNtfy
	DB    *gorm.DB
}

// setupTestEnv builds the shared deps every subcommand test needs: a Redis
// (miniredis) backed pause.Client, an in-memory audit Store backed by sqlite,
// and a fake ntfy poster.
func setupTestEnv(t *testing.T) (*pause.Client, *audit.Store, *fakeNtfy) {
	t.Helper()
	env := newTestEnv(t)
	return env.Pause, env.Store, env.Ntfy
}

// newTestEnv is the full-bundle variant. Use it when the test needs direct
// access to the underlying *gorm.DB (e.g. to query audit_log rows).
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	pc := pause.NewClient(rdb)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	if err := db.AutoMigrate(&audit.Entry{}); err != nil {
		t.Fatalf("audit migrate: %v", err)
	}
	return &testEnv{Pause: pc, Store: audit.NewStore(db), Ntfy: &fakeNtfy{}, DB: db}
}
