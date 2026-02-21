/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// StagedImportStatus represents the current state of a staged import.
type StagedImportStatus string

const (
	StagedImportStatusAnalyzing StagedImportStatus = "analyzing"
	StagedImportStatusReady     StagedImportStatus = "ready"     // Analysis complete, ready for review
	StagedImportStatusCommitted StagedImportStatus = "committed" // User approved, items imported
	StagedImportStatusRejected  StagedImportStatus = "rejected"  // User rejected the import
	StagedImportStatusExpired   StagedImportStatus = "expired"   // Staged data expired (cleanup)
)

// StagedImport holds staged import data for review before committing.
type StagedImport struct {
	ID         string             `gorm:"type:uuid;primaryKey" json:"id"`
	JobID      string             `gorm:"type:uuid;index;not null" json:"job_id"`
	SourceType string             `gorm:"type:varchar(50);not null" json:"source_type"` // "libretime", "azuracast"
	Status     StagedImportStatus `gorm:"type:varchar(32);not null;default:'analyzing'" json:"status"`

	// Staged data as JSONB
	StagedMedia       StagedMediaItems      `gorm:"type:jsonb;serializer:json" json:"staged_media"`
	StagedPlaylists   StagedPlaylistItems   `gorm:"type:jsonb;serializer:json" json:"staged_playlists"`
	StagedSmartBlocks StagedSmartBlockItems `gorm:"type:jsonb;serializer:json" json:"staged_smart_blocks"`
	StagedShows       StagedShowItems       `gorm:"type:jsonb;serializer:json" json:"staged_shows"`
	StagedWebstreams  StagedWebstreamItems  `gorm:"type:jsonb;serializer:json" json:"staged_webstreams"`

	// Analysis results
	Warnings    ImportWarnings    `gorm:"type:jsonb;serializer:json" json:"warnings"`
	Suggestions ImportSuggestions `gorm:"type:jsonb;serializer:json" json:"suggestions"`

	// User selections (what to import)
	Selections ImportSelections `gorm:"type:jsonb;serializer:json" json:"selections"`

	// Timestamps
	AnalyzedAt  *time.Time `json:"analyzed_at,omitempty"`
	CommittedAt *time.Time `json:"committed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// TableName returns the table name for GORM.
func (StagedImport) TableName() string {
	return "staged_imports"
}

// StagedMediaItem represents a media file staged for import review.
type StagedMediaItem struct {
	SourceID      string `json:"source_id"` // Original ID in source system
	Title         string `json:"title"`
	Artist        string `json:"artist"`
	Album         string `json:"album"`
	Genre         string `json:"genre,omitempty"`
	DurationMs    int    `json:"duration_ms"`
	FilePath      string `json:"file_path"`
	FileSize      int64  `json:"file_size"`
	ContentHash   string `json:"content_hash,omitempty"`    // For deduplication
	IsDuplicate   bool   `json:"is_duplicate"`              // True if already exists
	DuplicateOfID string `json:"duplicate_of_id,omitempty"` // ID of existing duplicate
	Selected      bool   `json:"selected"`                  // User selection

	// Orphan matching (set during analysis)
	OrphanMatch bool   `json:"orphan_match"`          // True if matched to existing orphan file
	OrphanID    string `json:"orphan_id,omitempty"`   // ID of matched orphan
	OrphanPath  string `json:"orphan_path,omitempty"` // Path of orphan file (for display)
}

// StagedMediaItems is a slice type with GORM scanner/valuer support.
type StagedMediaItems []StagedMediaItem

func (s StagedMediaItems) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *StagedMediaItems) Scan(value interface{}) error {
	if value == nil {
		*s = StagedMediaItems{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal StagedMediaItems: %v", value)
	}
	if len(bytes) == 0 {
		*s = StagedMediaItems{}
		return nil
	}
	return json.Unmarshal(bytes, s)
}

// StagedPlaylistItem represents a playlist staged for import review.
type StagedPlaylistItem struct {
	SourceID    string   `json:"source_id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	ItemCount   int      `json:"item_count"`
	Duration    string   `json:"duration,omitempty"` // Human-readable duration
	ItemIDs     []string `json:"item_ids,omitempty"` // Source IDs of items in playlist
	Selected    bool     `json:"selected"`
}

// StagedPlaylistItems is a slice type with GORM scanner/valuer support.
type StagedPlaylistItems []StagedPlaylistItem

func (s StagedPlaylistItems) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *StagedPlaylistItems) Scan(value interface{}) error {
	if value == nil {
		*s = StagedPlaylistItems{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal StagedPlaylistItems: %v", value)
	}
	if len(bytes) == 0 {
		*s = StagedPlaylistItems{}
		return nil
	}
	return json.Unmarshal(bytes, s)
}

// StagedSmartBlockItem represents a smart block staged for import review.
type StagedSmartBlockItem struct {
	SourceID        string         `json:"source_id"`
	Name            string         `json:"name"`
	Description     string         `json:"description,omitempty"`
	CriteriaCount   int            `json:"criteria_count"`
	CriteriaSummary string         `json:"criteria_summary,omitempty"` // Human-readable summary
	RawCriteria     map[string]any `json:"raw_criteria,omitempty"`     // Original criteria from source
	Selected        bool           `json:"selected"`
}

// StagedSmartBlockItems is a slice type with GORM scanner/valuer support.
type StagedSmartBlockItems []StagedSmartBlockItem

func (s StagedSmartBlockItems) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *StagedSmartBlockItems) Scan(value interface{}) error {
	if value == nil {
		*s = StagedSmartBlockItems{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal StagedSmartBlockItems: %v", value)
	}
	if len(bytes) == 0 {
		*s = StagedSmartBlockItems{}
		return nil
	}
	return json.Unmarshal(bytes, s)
}

// StagedShowItem represents a show staged for import review with recurrence detection.
type StagedShowItem struct {
	SourceID    string `json:"source_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Genre       string `json:"genre,omitempty"`
	Color       string `json:"color,omitempty"` // Hex color code

	// Recurrence detection results
	DetectedRRule     string    `json:"detected_rrule,omitempty"` // Auto-detected RRULE
	DTStart           time.Time `json:"dtstart"`                  // First occurrence
	DurationMinutes   int       `json:"duration_minutes"`
	Timezone          string    `json:"timezone,omitempty"`
	InstanceCount     int       `json:"instance_count"`            // Number of instances found
	PatternConfidence float64   `json:"pattern_confidence"`        // 0.0-1.0 confidence in pattern
	PatternNote       string    `json:"pattern_note,omitempty"`    // Human-readable pattern description
	ExceptionCount    int       `json:"exception_count,omitempty"` // Instances that don't match pattern

	// User choices
	CreateShow  bool   `json:"create_show"`            // Create as Show with RRULE
	CreateClock bool   `json:"create_clock"`           // Create as Clock (template only)
	CustomRRule string `json:"custom_rrule,omitempty"` // User-edited RRULE
	Selected    bool   `json:"selected"`
}

// StagedShowItems is a slice type with GORM scanner/valuer support.
type StagedShowItems []StagedShowItem

func (s StagedShowItems) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *StagedShowItems) Scan(value interface{}) error {
	if value == nil {
		*s = StagedShowItems{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal StagedShowItems: %v", value)
	}
	if len(bytes) == 0 {
		*s = StagedShowItems{}
		return nil
	}
	return json.Unmarshal(bytes, s)
}

// StagedWebstreamItem represents a webstream staged for import review.
type StagedWebstreamItem struct {
	SourceID    string `json:"source_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`
	Selected    bool   `json:"selected"`
}

// StagedWebstreamItems is a slice type with GORM scanner/valuer support.
type StagedWebstreamItems []StagedWebstreamItem

func (s StagedWebstreamItems) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *StagedWebstreamItems) Scan(value interface{}) error {
	if value == nil {
		*s = StagedWebstreamItems{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal StagedWebstreamItems: %v", value)
	}
	if len(bytes) == 0 {
		*s = StagedWebstreamItems{}
		return nil
	}
	return json.Unmarshal(bytes, s)
}

// ImportWarning represents a warning generated during import analysis.
type ImportWarning struct {
	Code     string `json:"code"`                // Machine-readable code (e.g., "duplicate_media")
	Severity string `json:"severity"`            // "info", "warning", "error"
	Message  string `json:"message"`             // Human-readable message
	ItemType string `json:"item_type,omitempty"` // "media", "playlist", "show", etc.
	ItemID   string `json:"item_id,omitempty"`   // Source ID of affected item
	Details  string `json:"details,omitempty"`   // Additional context
}

// ImportWarnings is a slice type with GORM scanner/valuer support.
type ImportWarnings []ImportWarning

func (w ImportWarnings) Value() (driver.Value, error) {
	return json.Marshal(w)
}

func (w *ImportWarnings) Scan(value interface{}) error {
	if value == nil {
		*w = ImportWarnings{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal ImportWarnings: %v", value)
	}
	if len(bytes) == 0 {
		*w = ImportWarnings{}
		return nil
	}
	return json.Unmarshal(bytes, w)
}

// ImportSuggestion represents a suggestion for improving the import.
type ImportSuggestion struct {
	Code     string `json:"code"`    // Machine-readable code
	Message  string `json:"message"` // Human-readable suggestion
	ItemType string `json:"item_type,omitempty"`
	ItemID   string `json:"item_id,omitempty"`
	Action   string `json:"action,omitempty"` // Suggested action (e.g., "skip", "merge", "edit")
}

// ImportSuggestions is a slice type with GORM scanner/valuer support.
type ImportSuggestions []ImportSuggestion

func (s ImportSuggestions) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *ImportSuggestions) Scan(value interface{}) error {
	if value == nil {
		*s = ImportSuggestions{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal ImportSuggestions: %v", value)
	}
	if len(bytes) == 0 {
		*s = ImportSuggestions{}
		return nil
	}
	return json.Unmarshal(bytes, s)
}

// ImportSelections stores user's choices about what to import.
type ImportSelections struct {
	StationIDs    []string `json:"station_ids,omitempty"`     // Selected source station IDs (for scoped imports)
	MediaIDs      []string `json:"media_ids,omitempty"`       // Selected media source IDs
	PlaylistIDs   []string `json:"playlist_ids,omitempty"`    // Selected playlist source IDs
	SmartBlockIDs []string `json:"smart_block_ids,omitempty"` // Selected smart block source IDs
	ShowIDs       []string `json:"show_ids,omitempty"`        // Selected show source IDs
	WebstreamIDs  []string `json:"webstream_ids,omitempty"`   // Selected webstream source IDs

	// Show-specific selections
	ShowsAsShows  []string `json:"shows_as_shows,omitempty"`  // Shows to import as Show with RRULE
	ShowsAsClocks []string `json:"shows_as_clocks,omitempty"` // Shows to import as Clock only

	// Custom RRULEs for shows (source_id -> RRULE)
	CustomRRules map[string]string `json:"custom_rrules,omitempty"`
}

func (s ImportSelections) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *ImportSelections) Scan(value interface{}) error {
	if value == nil {
		*s = ImportSelections{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal ImportSelections: %v", value)
	}
	if len(bytes) == 0 {
		*s = ImportSelections{}
		return nil
	}
	return json.Unmarshal(bytes, s)
}

// ImportedItems tracks what was created by an import job (for rollback/history).
type ImportedItems struct {
	MediaIDs      []string `json:"media_ids,omitempty"`
	SmartBlockIDs []string `json:"smart_block_ids,omitempty"`
	PlaylistIDs   []string `json:"playlist_ids,omitempty"`
	ShowIDs       []string `json:"show_ids,omitempty"`
	ClockIDs      []string `json:"clock_ids,omitempty"`
	WebstreamIDs  []string `json:"webstream_ids,omitempty"`
	ScheduleIDs   []string `json:"schedule_ids,omitempty"`
	UserIDs       []string `json:"user_ids,omitempty"`
}

func (i ImportedItems) Value() (driver.Value, error) {
	return json.Marshal(i)
}

func (i *ImportedItems) Scan(value interface{}) error {
	if value == nil {
		*i = ImportedItems{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal ImportedItems: %v", value)
	}
	if len(bytes) == 0 {
		*i = ImportedItems{}
		return nil
	}
	return json.Unmarshal(bytes, i)
}

// TotalCount returns the total number of items imported.
func (i *ImportedItems) TotalCount() int {
	return len(i.MediaIDs) + len(i.SmartBlockIDs) + len(i.PlaylistIDs) +
		len(i.ShowIDs) + len(i.ClockIDs) + len(i.WebstreamIDs) +
		len(i.ScheduleIDs) + len(i.UserIDs)
}

// SelectedCount returns the count of selected items in a staged import.
func (s *StagedImport) SelectedCount() int {
	count := 0
	for _, m := range s.StagedMedia {
		if m.Selected {
			count++
		}
	}
	for _, p := range s.StagedPlaylists {
		if p.Selected {
			count++
		}
	}
	for _, b := range s.StagedSmartBlocks {
		if b.Selected {
			count++
		}
	}
	for _, sh := range s.StagedShows {
		if sh.Selected {
			count++
		}
	}
	for _, w := range s.StagedWebstreams {
		if w.Selected {
			count++
		}
	}
	return count
}

// TotalCount returns the total number of staged items.
func (s *StagedImport) TotalCount() int {
	return len(s.StagedMedia) + len(s.StagedPlaylists) + len(s.StagedSmartBlocks) +
		len(s.StagedShows) + len(s.StagedWebstreams)
}

// DuplicateCount returns the number of duplicate media items.
func (s *StagedImport) DuplicateCount() int {
	count := 0
	for _, m := range s.StagedMedia {
		if m.IsDuplicate {
			count++
		}
	}
	return count
}

// OrphanMatchCount returns the number of media items that match existing orphan files.
func (s *StagedImport) OrphanMatchCount() int {
	count := 0
	for _, m := range s.StagedMedia {
		if m.OrphanMatch {
			count++
		}
	}
	return count
}
