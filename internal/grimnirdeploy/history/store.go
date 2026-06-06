/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Store reads and writes deploy_history.
type Store struct {
	db            *gorm.DB
	migrationsDir string // for ContractCrossings
}

// NewStore constructs a store. The migrations directory defaults to
// "migrations" relative to CWD; override with WithMigrationsDir for tests
// or when the binary runs from a non-repo CWD.
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db, migrationsDir: "migrations"}
}

// WithMigrationsDir returns a copy of the store with the migration scan
// directory overridden.
func (s *Store) WithMigrationsDir(dir string) *Store {
	cp := *s
	cp.migrationsDir = dir
	return &cp
}

// Start inserts a new in-progress deploy row and returns the new id. The row
// has no outcome / completed_at; callers stamp those with Complete or Fail.
func (s *Store) Start(ctx context.Context, region, tag, prevTag, operator string) (uuid.UUID, error) {
	id := uuid.New()
	e := Entry{
		ID:          id,
		Region:      region,
		Tag:         tag,
		PreviousTag: prevTag,
		StartedAt:   time.Now().UTC(),
		Operator:    operator,
	}
	return id, s.db.WithContext(ctx).Create(&e).Error
}

// Complete stamps completed_at + outcome + soak_outcome on the row.
func (s *Store) Complete(ctx context.Context, id uuid.UUID, outcome, soak string) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Model(&Entry{}).Where("id = ?", id).Updates(map[string]any{
		"completed_at": now,
		"outcome":      outcome,
		"soak_outcome": soak,
	}).Error
}

// Fail stamps completed_at + outcome + failure_log on the row. Use this when
// a deploy aborts before it can record a normal outcome.
func (s *Store) Fail(ctx context.Context, id uuid.UUID, outcome, failureLog string) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Model(&Entry{}).Where("id = ?", id).Updates(map[string]any{
		"completed_at": now,
		"outcome":      outcome,
		"failure_log":  failureLog,
	}).Error
}

// LastSuccessful returns the most recent outcome="success" entry for the
// region, or nil if there is none. The ordering is by started_at DESC, which
// matches the natural "what's running right now" interpretation.
func (s *Store) LastSuccessful(ctx context.Context, region string) (*Entry, error) {
	var e Entry
	err := s.db.WithContext(ctx).
		Where("region = ? AND outcome = ?", region, OutcomeSuccess).
		Order("started_at DESC").
		First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// WithinEligibility returns true if the most recent successful deploy for the
// region completed within the given window. Returns false (no error) when
// there is no successful deploy or the most recent one completed longer ago
// than window. Used by --rollback to refuse stale rollback targets.
func (s *Store) WithinEligibility(ctx context.Context, region string, window time.Duration) (bool, error) {
	last, err := s.LastSuccessful(ctx, region)
	if err != nil {
		return false, err
	}
	if last == nil || last.CompletedAt == nil {
		return false, nil
	}
	return time.Since(*last.CompletedAt) <= window, nil
}

// ContractCrossings returns the migration filenames between fromTag and toTag
// that carry the `migration-contract:` annotation in a comment.
//
// Phase-1 implementation: scans the migrations directory for any file
// containing the annotation. This is intentionally over-conservative: it
// flags every annotated migration in the directory regardless of which tags
// actually introduced them, so --rollback will refuse a few rollbacks that
// would in fact be safe. The trade-off is zero git dependency and zero risk
// of a false "this is safe" answer.
//
// A follow-up will refine this to git-blame-by-tag once we have a stable
// tag-to-commit mapping. The fromTag and toTag parameters are accepted now
// so the call sites do not need to change when that refinement lands.
func (s *Store) ContractCrossings(ctx context.Context, region, fromTag, toTag string) ([]string, error) {
	entries, err := os.ReadDir(s.migrationsDir)
	if err != nil {
		return nil, err
	}
	var hits []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		if name == "TEMPLATE.sql" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.migrationsDir, name))
		if err != nil {
			continue
		}
		if strings.Contains(string(b), "migration-contract:") {
			hits = append(hits, name)
		}
	}
	sort.Strings(hits)
	return hits, nil
}
