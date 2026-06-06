/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis key shape & lifetime for session-state replication.
//
// Per Chunk 8 of docs/superpowers/plans/2026-06-05-live-input-fan-out.md:
// every fan-out node mirrors its live sessions into Redis hashes so a peer
// can resume the DJ on takeover without forcing a re-auth round-trip.
//
//	key:  dj:session:<session-id>
//	type: hash
//	TTL:  60s, refreshed every Replicate() call (call frequency is the
//	      caller's choice; the lifecycle hook fires on Create + every 5s
//	      tick, well inside the 60s expiry budget)
//
// Hash fields:
//
//	id              string  session id (mirrors the hash suffix)
//	protocol        string  one of "harbor"|"rtp"|"srt"|"webrtc"
//	state           string  one of "idle"|"authenticating"|"active"|"ended"
//	started_at      string  RFC3339Nano timestamp
//	last_active_at  string  RFC3339Nano timestamp of this write
//	auth_claims     string  JSON-encoded AuthClaims (omitted when nil)
//
// A node coming up scans dj:session:* keys & for any whose last_active_at
// is within the freshness window (typically 30s) AND that has no local
// session, registers a placeholder Session marked IsRecovering(). The audio
// pipeline only re-attaches once the DJ reconnects.
const (
	sessionKeyPrefix = "dj:session:"
	sessionTTL       = 60 * time.Second
)

// SessionReplicator mirrors Session state into Redis & rehydrates sessions
// on fan-out startup. Safe to share across goroutines; the *redis.Client
// already serializes concurrent commands.
type SessionReplicator struct {
	rdb *redis.Client
}

// NewSessionReplicator returns a replicator backed by the given Redis
// client. A nil client disables replication (every method becomes a no-op
// returning nil) so production wiring stays optional.
func NewSessionReplicator(rdb *redis.Client) *SessionReplicator {
	return &SessionReplicator{rdb: rdb}
}

func sessionKey(id string) string { return sessionKeyPrefix + id }

// Replicate writes the session's metadata to its Redis hash & resets the
// 60s TTL. Called by the session lifecycle hook on Create, on every state
// transition, and on the 5s heartbeat tick.
func (r *SessionReplicator) Replicate(ctx context.Context, s *Session) error {
	if r == nil || r.rdb == nil || s == nil {
		return nil
	}
	now := time.Now().UTC()
	fields := map[string]any{
		"id":             s.ID,
		"protocol":       s.Protocol.String(),
		"state":          s.State().String(),
		"started_at":     s.StartedAt.UTC().Format(time.RFC3339Nano),
		"last_active_at": now.Format(time.RFC3339Nano),
	}
	if claims := s.AuthClaims; claims != nil {
		blob, err := json.Marshal(claims)
		if err != nil {
			return fmt.Errorf("encode auth_claims: %w", err)
		}
		fields["auth_claims"] = string(blob)
	}
	key := sessionKey(s.ID)
	pipe := r.rdb.TxPipeline()
	pipe.HSet(ctx, key, fields)
	pipe.Expire(ctx, key, sessionTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("replicate session %s: %w", s.ID, err)
	}
	return nil
}

// Delete removes a session's Redis hash on graceful end. Idempotent; a
// missing key is not an error.
func (r *SessionReplicator) Delete(ctx context.Context, id string) error {
	if r == nil || r.rdb == nil {
		return nil
	}
	if err := r.rdb.Del(ctx, sessionKey(id)).Err(); err != nil {
		return fmt.Errorf("delete session %s: %w", id, err)
	}
	return nil
}

// Hydrate scans Redis for replicated sessions & registers warm placeholders
// in mgr for any that are recent (last_active_at within freshness) and not
// already in mgr. Returns the number of sessions hydrated.
//
// Placeholders carry IsRecovering()==true. The protocol acceptor checks
// this flag, swaps in the real pipeline, & calls ClearRecovering() on a
// successful DJ reconnect.
func (r *SessionReplicator) Hydrate(ctx context.Context, mgr *SessionMgr, freshness time.Duration) (int, error) {
	if r == nil || r.rdb == nil || mgr == nil {
		return 0, nil
	}
	cutoff := time.Now().Add(-freshness)
	var (
		cursor uint64
		count  int
	)
	for {
		keys, next, err := r.rdb.Scan(ctx, cursor, sessionKeyPrefix+"*", 100).Result()
		if err != nil {
			return count, fmt.Errorf("scan replicated sessions: %w", err)
		}
		for _, key := range keys {
			h, err := r.rdb.HGetAll(ctx, key).Result()
			if err != nil {
				return count, fmt.Errorf("hgetall %s: %w", key, err)
			}
			if len(h) == 0 {
				continue
			}
			id := h["id"]
			if id == "" {
				continue
			}
			if _, exists := mgr.Get(id); exists {
				continue
			}
			lastActive, err := time.Parse(time.RFC3339Nano, h["last_active_at"])
			if err != nil || lastActive.Before(cutoff) {
				continue
			}
			startedAt, err := time.Parse(time.RFC3339Nano, h["started_at"])
			if err != nil {
				startedAt = lastActive
			}
			s := newSessionWithDeps(id, parseProtocol(h["protocol"]), startedAt)
			s.MarkRecovering()
			if claims := h["auth_claims"]; claims != "" {
				var decoded any
				if err := json.Unmarshal([]byte(claims), &decoded); err == nil {
					s.AuthClaims = decoded
				}
			}
			mgr.Add(s)
			count++
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	return count, nil
}

// parseProtocol reverses Protocol.String. Unknown labels collapse to
// ProtocolUnknown rather than failing the whole hydrate; the placeholder
// session still surfaces so an operator can see something was active.
func parseProtocol(s string) Protocol {
	switch s {
	case "harbor":
		return ProtocolHarbor
	case "rtp":
		return ProtocolRTP
	case "srt":
		return ProtocolSRT
	case "webrtc":
		return ProtocolWebRTC
	default:
		return ProtocolUnknown
	}
}
