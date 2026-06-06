/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	mathrand "math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// Protocol identifies the ingress wire format a Session was constructed for.
// Stable enum; gRPC mirrors map onto this set.
type Protocol int

const (
	ProtocolUnknown Protocol = iota
	ProtocolHarbor           // Icecast/Shoutcast SOURCE push (TCP)
	ProtocolRTP              // Raw RTP audio (UDP)
	ProtocolSRT              // Haivision SRT
	ProtocolWebRTC           // Browser-originated WebRTC
)

// String returns the canonical short name used in metrics & logs.
func (p Protocol) String() string {
	switch p {
	case ProtocolHarbor:
		return "harbor"
	case ProtocolRTP:
		return "rtp"
	case ProtocolSRT:
		return "srt"
	case ProtocolWebRTC:
		return "webrtc"
	default:
		return "unknown"
	}
}

// SessionState is the live lifecycle stage of a Session. The valid transitions
// are linear: Idle -> Authenticating -> Active -> Ended.
type SessionState int

const (
	SessionIdle SessionState = iota
	SessionAuthenticating
	SessionActive
	SessionEnded
)

// String returns a stable, lower-case label suitable for logs & metrics.
func (s SessionState) String() string {
	switch s {
	case SessionIdle:
		return "idle"
	case SessionAuthenticating:
		return "authenticating"
	case SessionActive:
		return "active"
	case SessionEnded:
		return "ended"
	default:
		return "unknown"
	}
}

// SessionTelemetry is a snapshot of a Session's running counters. Returned by
// Session.Telemetry; safe to copy & inspect on any goroutine.
type SessionTelemetry struct {
	BytesIn       uint64
	LastPacketAt  time.Time
	EncoderQueued uint32 // current appsrc backpressure level (Chunk 2.2 wires)
}

// Session represents one live DJ connection. Constructed via SessionMgr.Create
// (which assigns an ID + StartedAt) or via newSessionWithDeps in tests.
//
// The Pipeline field is populated in Task 2.2; Chunks 3-6 push PCM into the
// pipeline's appsrc. AuthClaims is populated in Chunk 7. The current Chunk
// (2.1) only models the lifecycle + telemetry plumbing.
type Session struct {
	ID        string
	Protocol  Protocol
	StartedAt time.Time

	// AuthClaims is opaque until Chunk 7; sessions are unauthenticated until
	// then & the field stays nil.
	AuthClaims any

	// Pipeline is the per-session go-gst pipeline owning audioconvert -> RTP
	// -> multiudpsink. Concrete type is *Pipeline (defined in pipeline.go by
	// Task 2.2); held as any here so this file stays decoupled from go-gst
	// in tests that don't need it. Nil until the protocol terminator
	// (Chunks 3-6) attaches one.
	Pipeline any

	mu    sync.Mutex
	state SessionState

	// Telemetry. Atomics so the recordX/markX paths can run on the audio
	// goroutine without taking the lock.
	bytesIn       atomic.Uint64
	lastPacketNs  atomic.Int64
	encoderQueued atomic.Uint32
}

// newSessionWithDeps constructs a Session with explicit ID / protocol / start
// time. Used directly by tests; the SessionMgr.Create path injects the
// generator + clock for production.
func newSessionWithDeps(id string, proto Protocol, startedAt time.Time) *Session {
	return &Session{
		ID:        id,
		Protocol:  proto,
		StartedAt: startedAt,
		state:     SessionIdle,
	}
}

// State returns the current lifecycle state.
func (s *Session) State() SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// transitionTo advances the session lifecycle. Only forward moves are valid;
// returns an error on any backward or invalid move (including any move out of
// Ended). Callers in chunks 3-6 invoke this from the protocol terminator.
func (s *Session) transitionTo(next SessionState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if next <= s.state {
		return fmt.Errorf("session %s: invalid transition %s -> %s", s.ID, s.state, next)
	}
	s.state = next
	return nil
}

// recordBytesIn adds n to the BytesIn counter. Audio goroutines call this on
// every successful PushBuffer (Chunks 3-6).
func (s *Session) recordBytesIn(n uint64) {
	s.bytesIn.Add(n)
}

// markPacket stores the wall-clock time of the most recent ingress packet.
// Set by the protocol terminator; consumed by the watchdog (future chunk) to
// detect stalled DJ connections.
func (s *Session) markPacket(at time.Time) {
	s.lastPacketNs.Store(at.UnixNano())
}

// setEncoderQueued surfaces the appsrc's current queued-buffer level so the
// telemetry snapshot can show DJ-side backpressure. Wired by the pipeline.
func (s *Session) setEncoderQueued(level uint32) {
	s.encoderQueued.Store(level)
}

// Telemetry returns a point-in-time snapshot of the session's counters.
func (s *Session) Telemetry() SessionTelemetry {
	t := SessionTelemetry{
		BytesIn:       s.bytesIn.Load(),
		EncoderQueued: s.encoderQueued.Load(),
	}
	if ns := s.lastPacketNs.Load(); ns != 0 {
		t.LastPacketAt = time.Unix(0, ns)
	}
	return t
}

// IDGenerator produces session IDs. Random UUIDs in production; seeded
// deterministic strings in tests. SessionMgr depends only on the interface so
// the production wiring stays decoupled from the test wiring.
type IDGenerator interface {
	NewID() string
}

// NewUUIDGenerator returns an IDGenerator backed by github.com/google/uuid's
// random UUIDv4. The standard production choice.
func NewUUIDGenerator() IDGenerator {
	return uuidGen{}
}

type uuidGen struct{}

func (uuidGen) NewID() string { return uuid.NewString() }

// NewSeededIDGenerator returns an IDGenerator backed by math/rand seeded with
// the given seed. Two generators with the same seed emit the same sequence of
// IDs; used in tests to make session IDs deterministic.
func NewSeededIDGenerator(seed int64) IDGenerator {
	return &seededIDGen{rng: mathrand.New(mathrand.NewSource(seed))}
}

type seededIDGen struct {
	mu  sync.Mutex
	rng *mathrand.Rand
}

func (g *seededIDGen) NewID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	b := make([]byte, 16)
	_, _ = g.rng.Read(b)
	return hex.EncodeToString(b)
}

// Clock is the small abstraction we need over time.Now so tests can pin
// StartedAt without time.Now's drift.
type Clock interface {
	Now() time.Time
}

// SystemClock is the production Clock; delegates to time.Now.
type SystemClock struct{}

// Now returns the current wall-clock time.
func (SystemClock) Now() time.Time { return time.Now() }

// fixedClock is the test Clock; always returns the same time.
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// secureRandomID is an internal fallback when no IDGenerator is provided; we
// don't expose it because the SessionMgr always carries one. Kept here to
// avoid a separate uuid dependency in production paths that might want a
// non-uuid ID format.
func secureRandomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is fatal-class; the OS is broken. The caller
		// will see a panic from the empty string downstream, so make that
		// failure explicit here instead.
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// SessionMgr owns the map of live sessions and the gRPC StatusProvider's
// per-protocol counters. Methods are safe to call from any goroutine.
type SessionMgr struct {
	idgen IDGenerator
	clock Clock

	mu       sync.RWMutex
	sessions map[string]*Session

	totalServed atomic.Int64
}

// NewSessionMgr returns a manager using the system clock and a random-UUID
// generator. Tests that want determinism should call NewSessionMgrWithDeps.
func NewSessionMgr() *SessionMgr {
	return NewSessionMgrWithDeps(NewUUIDGenerator(), SystemClock{})
}

// NewSessionMgrWithDeps wires a manager with explicit ID generator + clock.
// Used by tests and by the production main once Chunk 2.3 lands.
func NewSessionMgrWithDeps(idgen IDGenerator, clock Clock) *SessionMgr {
	if idgen == nil {
		idgen = NewUUIDGenerator()
	}
	if clock == nil {
		clock = SystemClock{}
	}
	return &SessionMgr{
		idgen:    idgen,
		clock:    clock,
		sessions: make(map[string]*Session),
	}
}

// Create constructs a new Session with a fresh ID + StartedAt and registers it
// with the manager. Protocol identifies which ingress terminator built it.
func (m *SessionMgr) Create(proto Protocol) *Session {
	s := newSessionWithDeps(m.idgen.NewID(), proto, m.clock.Now())
	m.Add(s)
	return s
}

// Add registers an externally-constructed Session. Used by tests; production
// callers should prefer Create so the ID + StartedAt come from the manager's
// generator + clock.
func (m *SessionMgr) Add(s *Session) {
	if s == nil {
		return
	}
	m.mu.Lock()
	if _, dup := m.sessions[s.ID]; !dup {
		m.totalServed.Add(1)
	}
	m.sessions[s.ID] = s
	m.mu.Unlock()
}

// Get looks up a session by ID. The boolean is false when the ID is absent.
func (m *SessionMgr) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

// Remove tears down a session by ID. Idempotent: removing an unknown ID is a
// no-op (and doesn't decrement the totalServed counter).
func (m *SessionMgr) Remove(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

// Count returns the number of currently-active sessions.
func (m *SessionMgr) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// CountByProtocol returns the number of currently-active sessions for one
// protocol. Used by the StatusProvider to fill the per-protocol counters on
// the gRPC status response.
func (m *SessionMgr) CountByProtocol(proto Protocol) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, s := range m.sessions {
		if s.Protocol == proto {
			n++
		}
	}
	return n
}

// TotalSessionsServed returns the lifetime counter of all sessions ever
// registered. Survives Remove; useful for capacity-planning dashboards.
func (m *SessionMgr) TotalSessionsServed() int64 {
	return m.totalServed.Load()
}

// List returns a snapshot of all live sessions. The slice is freshly allocated
// so callers may mutate it without affecting the manager.
func (m *SessionMgr) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}
