/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package pause manages the grimnir-deploy:emergency-pause:<region> Redis key.
// Every grimnir-deploy subcommand that mutates the cluster reads this key
// first and aborts if it is set. The grimnirradio scheduler reads the same
// key before any auto-deploy gate runs.
//
// Key naming: "grimnir-deploy:emergency-pause:<region>"; one key per region.
// Value: JSON {reason, operator, region, ts}. Default TTL is 0 (sticky;
// manual emergency-resume required).
package pause

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrNotPaused is returned by helpers that need to distinguish a missing key
// from a transport-level failure. Check() does not return this; it returns
// (false, "", nil) on absence so callers can short-circuit cleanly.
var ErrNotPaused = errors.New("pause not set")

// keyPrefix is exported through the helper KeyFor so the scheduler in
// grimnirradio can build the same key without importing this package.
const keyPrefix = "grimnir-deploy:emergency-pause:"

// KeyFor returns the Redis key for the given region. Both grimnir-deploy
// and the grimnirradio scheduler MUST use this exact format.
func KeyFor(region string) string {
	if region == "" {
		region = "default"
	}
	return keyPrefix + region
}

// State is the JSON payload stored in the Redis key.
type State struct {
	Reason   string    `json:"reason"`
	Operator string    `json:"operator"`
	Region   string    `json:"region"`
	TS       time.Time `json:"ts"`
}

// Client wraps a *redis.Client for pause-key operations.
type Client struct {
	rdb *redis.Client
}

// NewClient constructs a pause client around the given Redis connection.
func NewClient(rdb *redis.Client) *Client {
	return &Client{rdb: rdb}
}

// Set writes the pause state for the given region. Overwrites any prior
// value. ttl=0 means no expiry (sticky until Clear).
func (c *Client) Set(ctx context.Context, region, reason, operator string, ttl time.Duration) error {
	if reason == "" {
		return errors.New("reason is required")
	}
	if operator == "" {
		return errors.New("operator is required")
	}
	if region == "" {
		region = "default"
	}
	b, err := json.Marshal(State{
		Reason:   reason,
		Operator: operator,
		Region:   region,
		TS:       time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, KeyFor(region), b, ttl).Err()
}

// Read returns the current pause state for the region, or nil if no pause
// is set. A nil State with a nil error means "not paused".
func (c *Client) Read(ctx context.Context, region string) (*State, error) {
	v, err := c.rdb.Get(ctx, KeyFor(region)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal([]byte(v), &s); err != nil {
		return nil, fmt.Errorf("decode pause payload: %w", err)
	}
	return &s, nil
}

// Clear deletes the pause key for the region. Idempotent.
func (c *Client) Clear(ctx context.Context, region string) error {
	return c.rdb.Del(ctx, KeyFor(region)).Err()
}

// Check is the pre-flight gate helper used by every grimnir-deploy
// subcommand and by the future grimnirradio scheduler's auto-deploy path.
// Returns (paused=true, human-readable reason, nil) when the key is set;
// (false, "", nil) when not set; (false, "", err) on transport failure.
//
// The reason string is formatted for direct inclusion in an abort message:
//
//	"<reason text> (set by <operator> at <RFC3339 timestamp>)"
func Check(ctx context.Context, rdb *redis.Client, region string) (bool, string, error) {
	c := NewClient(rdb)
	st, err := c.Read(ctx, region)
	if err != nil {
		return false, "", err
	}
	if st == nil {
		return false, "", nil
	}
	return true, fmt.Sprintf("%s (set by %s at %s)",
		st.Reason, st.Operator, st.TS.Format(time.RFC3339)), nil
}
