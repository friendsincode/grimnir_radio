package models

import (
	"strings"
	"time"
)

// RoleName enumerates the RBAC roles.
type RoleName string

const (
	RoleAdmin   RoleName = "admin"
	RoleManager RoleName = "manager"
	RoleDJ      RoleName = "dj"
)

// User represents an authenticated account.
type User struct {
	ID        string `gorm:"type:uuid;primaryKey"`
	Email     string `gorm:"uniqueIndex"`
	Password  string
	Role      RoleName `gorm:"type:varchar(16)"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Station aggregates mounts and scheduling data.
type Station struct {
	ID          string `gorm:"type:uuid;primaryKey"`
	Name        string `gorm:"uniqueIndex"`
	Description string `gorm:"type:text"`
	Timezone    string `gorm:"type:varchar(32)"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Mount describes an output encoder pipeline.
type Mount struct {
	ID              string `gorm:"type:uuid;primaryKey"`
	StationID       string `gorm:"type:uuid;index"`
	Name            string `gorm:"index"`
	URL             string
	Format          string `gorm:"type:varchar(16)"`
	Bitrate         int
	Channels        int
	SampleRate      int
	EncoderPresetID string `gorm:"type:uuid"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// EncoderPreset stores encoder configuration for GStreamer pipelines.
type EncoderPreset struct {
	ID        string `gorm:"type:uuid;primaryKey"`
	Name      string `gorm:"uniqueIndex"`
	Format    string `gorm:"type:varchar(16)"`
	Bitrate   int
	Options   map[string]any `gorm:"type:jsonb"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MediaItem is an audio asset with analysis metadata.
type MediaItem struct {
	ID            string `gorm:"type:uuid;primaryKey"`
	StationID     string `gorm:"type:uuid;index"`
	Title         string `gorm:"index"`
	Artist        string `gorm:"index"`
	Album         string `gorm:"index"`
	Duration      time.Duration
	Path          string
	StorageKey    string
	Genre         string
	Mood          string
	Label         string
	Language      string
	Explicit      bool
	LoudnessLUFS  float64
	ReplayGain    float64
	BPM           float64
	Year          int
	Tags          []MediaTagLink
	CuePoints     CuePointSet `gorm:"type:jsonb"`
	Waveform      []byte
	AnalysisState AnalysisState `gorm:"type:varchar(32)"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CuePointSet captures intro/outro markers.
type CuePointSet struct {
	IntroEnd float64 `json:"intro_end"`
	OutroIn  float64 `json:"outro_in"`
}

// AnalysisState tracks analyzer progress.
type AnalysisState string

const (
	AnalysisPending  AnalysisState = "pending"
	AnalysisRunning  AnalysisState = "running"
	AnalysisComplete AnalysisState = "complete"
	AnalysisFailed   AnalysisState = "failed"
)

// Tag defines a metadata label.
type Tag struct {
	ID        string `gorm:"type:uuid;primaryKey"`
	Name      string `gorm:"uniqueIndex"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MediaTagLink join table between media and tags.
type MediaTagLink struct {
	MediaItemID string `gorm:"type:uuid;primaryKey"`
	TagID       string `gorm:"type:uuid;primaryKey"`
}

// SmartBlock encapsulates rule definitions.
type SmartBlock struct {
	ID          string         `gorm:"type:uuid;primaryKey"`
	StationID   string         `gorm:"type:uuid;index"`
	Name        string         `gorm:"index"`
	Description string         `gorm:"type:text"`
	Rules       map[string]any `gorm:"type:jsonb"`
	Sequence    map[string]any `gorm:"type:jsonb"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ClockSlotType enumerates slot types.
type ClockSlotType string

const (
	SlotTypeSmartBlock ClockSlotType = "smart_block"
	SlotTypeHardItem   ClockSlotType = "hard_item"
	SlotTypeStopset    ClockSlotType = "stopset"
)

// ClockHour describes one hour clock template.
type ClockHour struct {
	ID        string `gorm:"type:uuid;primaryKey"`
	StationID string `gorm:"type:uuid;index"`
	Name      string
	Slots     []ClockSlot `gorm:"foreignKey:ClockHourID"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ClockSlot is an element within the hour.
type ClockSlot struct {
	ID          string `gorm:"type:uuid;primaryKey"`
	ClockHourID string `gorm:"type:uuid;index"`
	Position    int
	Offset      time.Duration
	Type        ClockSlotType  `gorm:"type:varchar(32)"`
	Payload     map[string]any `gorm:"type:jsonb"`
}

// ScheduleEntry materializes a planned item.
type ScheduleEntry struct {
	ID         string `gorm:"type:uuid;primaryKey"`
	StationID  string `gorm:"type:uuid;index"`
	MountID    string `gorm:"type:uuid;index"`
	StartsAt   time.Time
	EndsAt     time.Time
	SourceType string         `gorm:"type:varchar(32)"`
	SourceID   string         `gorm:"type:uuid"`
	Metadata   map[string]any `gorm:"type:jsonb"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// PlayHistory stores executed playout events.
type PlayHistory struct {
	ID         string `gorm:"type:uuid;primaryKey"`
	StationID  string `gorm:"type:uuid;index"`
	MountID    string `gorm:"type:uuid;index"`
	MediaID    string `gorm:"type:uuid"`
	Artist     string `gorm:"index"`
	Title      string `gorm:"index"`
	Album      string `gorm:"index"`
	Label      string
	StartedAt  time.Time
	EndedAt    time.Time
	Transition string         `gorm:"type:varchar(32)"`
	Metadata   map[string]any `gorm:"type:jsonb"`
}

// MetadataString retrieves string metadata with fallback to struct fields.
func (p PlayHistory) MetadataString(key string) string {
	if p.Metadata != nil {
		if val, ok := p.Metadata[key]; ok {
			if str, ok := val.(string); ok {
				return str
			}
		}
	}
	switch strings.ToLower(key) {
	case "artist":
		return p.Artist
	case "title":
		return p.Title
	case "album":
		return p.Album
	case "label":
		return p.Label
	default:
		return ""
	}
}

// AnalysisJob records analyzer work queue.
type AnalysisJob struct {
	ID        string `gorm:"type:uuid;primaryKey"`
	MediaID   string `gorm:"type:uuid;index"`
	Status    string `gorm:"type:varchar(32)"`
	Error     string `gorm:"type:text"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
