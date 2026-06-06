/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// redactedMarker is the string substituted for any arg value whose key matches
// secretKeyPattern. Square brackets make the marker visually distinct in
// dashboards and impossible to confuse with a real value.
const redactedMarker = "[REDACTED]"

// secretKeyPattern matches argument keys that are presumed to carry secrets.
// Case-insensitive substring match on password / passwd / secret / token / key
// / credential. "key" is intentionally broad: ssh-key, signing-key, api-key,
// and pgpass-key all get caught. False positives (e.g. "primary-key" in a
// schema-management flag) get redacted too; better to over-redact than leak.
var secretKeyPattern = regexp.MustCompile(`(?i)(password|passwd|secret|token|key|credential)`)

// Store writes audit_log rows.
type Store struct {
	db *gorm.DB
}

// NewStore constructs an audit store backed by the given GORM database.
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// WriteStart inserts a "started" row and returns the new entry id. The args
// map is JSON-encoded with secret values redacted in place. A nil args map
// serialises to "{}" so the column always holds valid JSON.
func (s *Store) WriteStart(ctx context.Context, operator, sourceIP, subcommand string, args map[string]any) (uuid.UUID, error) {
	id := uuid.New()
	argsJSON, err := marshalRedacted(args)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal args: %w", err)
	}
	e := Entry{
		ID:         id,
		TS:         time.Now().UTC(),
		Operator:   operator,
		SourceIP:   sourceIP,
		Subcommand: subcommand,
		ArgsJSON:   argsJSON,
		Phase:      PhaseStarted,
	}
	if err := s.db.WithContext(ctx).Create(&e).Error; err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// WriteComplete updates the row to phase="completed" with the given outcome.
func (s *Store) WriteComplete(ctx context.Context, id uuid.UUID, outcome string, duration time.Duration, notes string) error {
	return s.db.WithContext(ctx).Model(&Entry{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"phase":       PhaseCompleted,
			"outcome":     outcome,
			"duration_ms": duration.Milliseconds(),
			"notes":       notes,
		}).Error
}

// WriteFailed updates the row to phase="failed" with the given outcome message.
func (s *Store) WriteFailed(ctx context.Context, id uuid.UUID, outcome string, duration time.Duration) error {
	return s.db.WithContext(ctx).Model(&Entry{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"phase":       PhaseFailed,
			"outcome":     outcome,
			"duration_ms": duration.Milliseconds(),
		}).Error
}

// marshalRedacted JSON-encodes args after replacing values whose keys match
// secretKeyPattern with redactedMarker. nil maps marshal to "{}".
func marshalRedacted(args map[string]any) (string, error) {
	if args == nil {
		return "{}", nil
	}
	clean := make(map[string]any, len(args))
	for k, v := range args {
		if isSecretKey(k) {
			clean[k] = redactedMarker
		} else {
			clean[k] = v
		}
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func isSecretKey(k string) bool {
	return secretKeyPattern.MatchString(k)
}
