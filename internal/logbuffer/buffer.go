/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package logbuffer provides an in-memory ring buffer for capturing logs.
package logbuffer

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

// LogEntry represents a single log entry.
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Component string                 `json:"component,omitempty"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Raw       string                 `json:"raw,omitempty"`
}

// Buffer is a thread-safe ring buffer for log entries.
type Buffer struct {
	mu       sync.RWMutex
	entries  []LogEntry
	capacity int
	head     int
	count    int
}

// New creates a new log buffer with the specified capacity.
func New(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = 10000
	}
	return &Buffer{
		entries:  make([]LogEntry, capacity),
		capacity: capacity,
	}
}

// Add adds a log entry to the buffer.
func (b *Buffer) Add(entry LogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.entries[b.head] = entry
	b.head = (b.head + 1) % b.capacity
	if b.count < b.capacity {
		b.count++
	}
}

// GetAll returns all log entries in chronological order.
func (b *Buffer) GetAll() []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make([]LogEntry, b.count)
	if b.count == 0 {
		return result
	}

	// Calculate start position
	start := 0
	if b.count == b.capacity {
		start = b.head
	}

	for i := 0; i < b.count; i++ {
		idx := (start + i) % b.capacity
		result[i] = b.entries[idx]
	}

	return result
}

// Query returns filtered log entries.
type QueryParams struct {
	Level      string    // Filter by level (debug, info, warn, error)
	Component  string    // Filter by component
	StationID  string    // Filter by station_id field
	Search     string    // Search in message
	Since      time.Time // Only entries after this time
	Limit      int       // Max entries to return (0 = all)
	Descending bool      // Return newest first
}

// Query returns log entries matching the filter criteria.
func (b *Buffer) Query(params QueryParams) []LogEntry {
	all := b.GetAll()

	// Apply filters
	var filtered []LogEntry
	for _, entry := range all {
		// Level filter
		if params.Level != "" && entry.Level != params.Level {
			continue
		}

		// Component filter
		if params.Component != "" && entry.Component != params.Component {
			continue
		}

		// Station ID filter - check in Fields
		if params.StationID != "" {
			stationID, ok := entry.Fields["station_id"].(string)
			if !ok || stationID != params.StationID {
				continue
			}
		}

		// Time filter
		if !params.Since.IsZero() && entry.Timestamp.Before(params.Since) {
			continue
		}

		// Search filter
		if params.Search != "" {
			found := false
			if containsIgnoreCase(entry.Message, params.Search) {
				found = true
			}
			if !found && containsIgnoreCase(entry.Component, params.Search) {
				found = true
			}
			if !found {
				// Search in fields
				for _, v := range entry.Fields {
					if s, ok := v.(string); ok && containsIgnoreCase(s, params.Search) {
						found = true
						break
					}
				}
			}
			if !found {
				continue
			}
		}

		filtered = append(filtered, entry)
	}

	// Reverse if descending (newest first)
	if params.Descending {
		for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
			filtered[i], filtered[j] = filtered[j], filtered[i]
		}
	}

	// Apply limit
	if params.Limit > 0 && len(filtered) > params.Limit {
		filtered = filtered[:params.Limit]
	}

	return filtered
}

// GetComponents returns a list of unique components in the buffer.
func (b *Buffer) GetComponents() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	componentMap := make(map[string]bool)
	for i := 0; i < b.count; i++ {
		idx := i
		if b.count == b.capacity {
			idx = (b.head + i) % b.capacity
		}
		if b.entries[idx].Component != "" {
			componentMap[b.entries[idx].Component] = true
		}
	}

	components := make([]string, 0, len(componentMap))
	for c := range componentMap {
		components = append(components, c)
	}
	return components
}

// Stats returns buffer statistics.
type Stats struct {
	Capacity   int            `json:"capacity"`
	Count      int            `json:"count"`
	LevelCount map[string]int `json:"level_count"`
}

func (b *Buffer) Stats() Stats {
	return b.StatsForStation("")
}

// StatsForStation returns buffer statistics filtered by station_id.
// If stationID is empty, returns stats for all entries.
func (b *Buffer) StatsForStation(stationID string) Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	stats := Stats{
		Capacity:   b.capacity,
		Count:      0,
		LevelCount: make(map[string]int),
	}

	for i := 0; i < b.count; i++ {
		idx := i
		if b.count == b.capacity {
			idx = (b.head + i) % b.capacity
		}
		entry := b.entries[idx]

		// Filter by station_id if specified
		if stationID != "" {
			entryStationID, ok := entry.Fields["station_id"].(string)
			if !ok || entryStationID != stationID {
				continue
			}
		}

		stats.Count++
		stats.LevelCount[entry.Level]++
	}

	return stats
}

// GetComponentsForStation returns unique components for a specific station.
// If stationID is empty, returns all components.
func (b *Buffer) GetComponentsForStation(stationID string) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	componentMap := make(map[string]bool)
	for i := 0; i < b.count; i++ {
		idx := i
		if b.count == b.capacity {
			idx = (b.head + i) % b.capacity
		}
		entry := b.entries[idx]

		// Filter by station_id if specified
		if stationID != "" {
			entryStationID, ok := entry.Fields["station_id"].(string)
			if !ok || entryStationID != stationID {
				continue
			}
		}

		if entry.Component != "" {
			componentMap[entry.Component] = true
		}
	}

	components := make([]string, 0, len(componentMap))
	for c := range componentMap {
		components = append(components, c)
	}
	return components
}

// Clear empties the buffer.
func (b *Buffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.head = 0
	b.count = 0
}

// Writer wraps the buffer to implement io.Writer for zerolog.
type Writer struct {
	buffer   *Buffer
	fallback io.Writer
}

// NewWriter creates a writer that captures logs to the buffer.
func NewWriter(buffer *Buffer, fallback io.Writer) *Writer {
	return &Writer{buffer: buffer, fallback: fallback}
}

// Write implements io.Writer.
func (w *Writer) Write(p []byte) (n int, err error) {
	// Parse the JSON log entry
	var rawEntry map[string]interface{}
	if err := json.Unmarshal(p, &rawEntry); err == nil {
		entry := LogEntry{
			Timestamp: time.Now(),
			Fields:    make(map[string]interface{}),
			Raw:       string(p),
		}

		// Extract standard fields
		if lvl, ok := rawEntry["level"].(string); ok {
			entry.Level = lvl
			delete(rawEntry, "level")
		}
		if msg, ok := rawEntry["message"].(string); ok {
			entry.Message = msg
			delete(rawEntry, "message")
		}
		if comp, ok := rawEntry["component"].(string); ok {
			entry.Component = comp
			delete(rawEntry, "component")
		}
		if ts, ok := rawEntry["time"].(string); ok {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				entry.Timestamp = t
			}
			delete(rawEntry, "time")
		}

		// Store remaining fields
		for k, v := range rawEntry {
			entry.Fields[k] = v
		}

		w.buffer.Add(entry)
	}

	// Always write to fallback
	if w.fallback != nil {
		return w.fallback.Write(p)
	}
	return len(p), nil
}

func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(substr) == 0 ||
			(len(s) > 0 && containsIgnoreCaseImpl(s, substr)))
}

func containsIgnoreCaseImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if equalFoldAt(s, i, substr) {
			return true
		}
	}
	return false
}

func equalFoldAt(s string, start int, substr string) bool {
	for i := 0; i < len(substr); i++ {
		c1 := s[start+i]
		c2 := substr[i]
		if c1 == c2 {
			continue
		}
		// Simple ASCII case folding
		if c1 >= 'A' && c1 <= 'Z' {
			c1 += 'a' - 'A'
		}
		if c2 >= 'A' && c2 <= 'Z' {
			c2 += 'a' - 'A'
		}
		if c1 != c2 {
			return false
		}
	}
	return true
}
