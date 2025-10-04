package state

import (
	"sync"
	"time"
)

// RecentPlay stores recent play metadata for separation logic.
type RecentPlay struct {
	MediaID   string
	Artist    string
	Album     string
	Label     string
	PlayedAt  time.Time
	StationID string
	MountID   string
}

// Store keeps in-memory state for quick separation checks.
type Store struct {
	mu     sync.RWMutex
	recent []RecentPlay
}

// NewStore creates a scheduler state store.
func NewStore() *Store {
	return &Store{recent: make([]RecentPlay, 0, 128)}
}

// Add registers a play event.
func (s *Store) Add(play RecentPlay) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recent = append(s.recent, play)
}

// Recent returns snapshot of tracked plays.
func (s *Store) Recent() []RecentPlay {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RecentPlay, len(s.recent))
	copy(out, s.recent)
	return out
}

// Prune removes entries older than cutoff.
func (s *Store) Prune(cutoff time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.recent[:0]
	for _, rp := range s.recent {
		if rp.PlayedAt.After(cutoff) {
			filtered = append(filtered, rp)
		}
	}
	s.recent = filtered
}
