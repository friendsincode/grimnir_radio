/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// GenerateMountName creates a URL-safe mount name from a station name.
// It converts to lowercase, replaces spaces with hyphens, and removes
// any characters that aren't alphanumeric or hyphens.
func GenerateMountName(name string) string {
	// Convert to lowercase
	name = strings.ToLower(name)
	// Replace spaces with hyphens
	name = strings.ReplaceAll(name, " ", "-")
	// Remove any characters that aren't alphanumeric or hyphens
	reg := regexp.MustCompile(`[^a-z0-9-]`)
	name = reg.ReplaceAllString(name, "")
	// Remove multiple consecutive hyphens
	reg = regexp.MustCompile(`-+`)
	name = reg.ReplaceAllString(name, "-")
	// Trim leading/trailing hyphens
	name = strings.Trim(name, "-")
	// If empty after sanitization, use a default
	if name == "" {
		name = "radio"
	}
	return name
}

// =============================================================================
// PLATFORM-LEVEL ROLES AND PERMISSIONS
// =============================================================================

// PlatformRole enumerates global platform roles.
type PlatformRole string

const (
	PlatformRoleAdmin PlatformRole = "platform_admin" // Full platform control
	PlatformRoleMod   PlatformRole = "platform_mod"   // Moderate content, approve stations
	PlatformRoleUser  PlatformRole = "user"           // Regular user, can own stations
)

// PlatformPermissions defines what a platform group can do.
type PlatformPermissions struct {
	// Station management
	CanApproveStations bool `json:"can_approve_stations"`
	CanSuspendStations bool `json:"can_suspend_stations"`
	CanDeleteStations  bool `json:"can_delete_stations"`
	CanViewAllStations bool `json:"can_view_all_stations"`

	// User management
	CanManageUsers  bool `json:"can_manage_users"`
	CanSuspendUsers bool `json:"can_suspend_users"`
	CanDeleteUsers  bool `json:"can_delete_users"`

	// Platform settings
	CanManageSettings bool `json:"can_manage_settings"`
	CanViewAnalytics  bool `json:"can_view_analytics"`

	// Limits (0 = unlimited for admins, default for users)
	MaxStations     int   `json:"max_stations"`
	MaxStorageBytes int64 `json:"max_storage_bytes"`
}

// Value implements driver.Valuer for database serialization.
func (p PlatformPermissions) Value() (driver.Value, error) {
	return json.Marshal(p)
}

// Scan implements sql.Scanner for database deserialization.
func (p *PlatformPermissions) Scan(value interface{}) error {
	if value == nil {
		*p = PlatformPermissions{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal PlatformPermissions: %v", value)
	}
	if len(bytes) == 0 {
		*p = PlatformPermissions{}
		return nil
	}
	return json.Unmarshal(bytes, p)
}

// PlatformGroup allows grouping users with shared platform permissions.
type PlatformGroup struct {
	ID          string                `gorm:"type:uuid;primaryKey"`
	Name        string                `gorm:"uniqueIndex"`
	Description string                `gorm:"type:text"`
	Permissions PlatformPermissions   `gorm:"type:jsonb"`
	Members     []PlatformGroupMember `gorm:"foreignKey:GroupID"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// PlatformGroupMember links users to platform groups.
type PlatformGroupMember struct {
	ID        string `gorm:"type:uuid;primaryKey"`
	UserID    string `gorm:"type:uuid;index;not null"`
	GroupID   string `gorm:"type:uuid;index;not null"`
	CreatedAt time.Time
}

// User represents an authenticated account.
type User struct {
	ID                 string `gorm:"type:uuid;primaryKey"`
	Email              string `gorm:"uniqueIndex"`
	Password           string
	PlatformRole       PlatformRole          `gorm:"type:varchar(20);default:'user'"` // Global platform role
	Suspended          bool                  `gorm:"default:false"`                   // Platform-level suspension
	SuspendedReason    string                `gorm:"type:text"`
	CalendarColorTheme string                `gorm:"type:varchar(32);default:'default'"`  // Calendar color theme preset
	Theme              string                `gorm:"type:varchar(32);default:'daw-dark'"` // Dashboard UI theme
	Stations           []StationUser         `gorm:"foreignKey:UserID"`
	PlatformGroups     []PlatformGroupMember `gorm:"foreignKey:UserID"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// RoleName is kept for backward compatibility during migration.
// Deprecated: Use PlatformRole for platform level, StationRole for station level.
type RoleName string

const (
	RoleAdmin   RoleName = "admin"
	RoleManager RoleName = "manager"
	RoleDJ      RoleName = "dj"
)

// Role returns the legacy RoleName for backward compatibility.
func (u *User) Role() RoleName {
	switch u.PlatformRole {
	case PlatformRoleAdmin:
		return RoleAdmin
	case PlatformRoleMod:
		return RoleManager
	default:
		return RoleDJ
	}
}

// IsPlatformAdmin checks if user has platform admin privileges.
func (u *User) IsPlatformAdmin() bool {
	return u.PlatformRole == PlatformRoleAdmin
}

// IsPlatformMod checks if user has platform moderator privileges.
func (u *User) IsPlatformMod() bool {
	return u.PlatformRole == PlatformRoleAdmin || u.PlatformRole == PlatformRoleMod
}

// =============================================================================
// STATION-LEVEL ROLES AND PERMISSIONS
// =============================================================================

// StationRole enumerates per-station roles.
type StationRole string

const (
	StationRoleOwner   StationRole = "owner"   // Full station control, can delete
	StationRoleAdmin   StationRole = "admin"   // Manage settings and users
	StationRoleManager StationRole = "manager" // Manage content
	StationRoleDJ      StationRole = "dj"      // Go live, upload media
	StationRoleViewer  StationRole = "viewer"  // View only (for private stations)
)

// StationPermissions defines granular station-level permissions.
type StationPermissions struct {
	// Media
	CanUploadMedia  bool `json:"can_upload_media"`
	CanDeleteMedia  bool `json:"can_delete_media"`
	CanEditMetadata bool `json:"can_edit_metadata"`

	// Playlists
	CanManagePlaylists   bool `json:"can_manage_playlists"`
	CanManageSmartBlocks bool `json:"can_manage_smart_blocks"`

	// Schedule
	CanManageSchedule bool `json:"can_manage_schedule"`
	CanManageClocks   bool `json:"can_manage_clocks"`

	// Live
	CanGoLive bool `json:"can_go_live"`
	CanKickDJ bool `json:"can_kick_dj"`

	// Admin
	CanManageUsers    bool `json:"can_manage_users"`
	CanManageSettings bool `json:"can_manage_settings"`
	CanViewAnalytics  bool `json:"can_view_analytics"`
	CanManageMounts   bool `json:"can_manage_mounts"`
}

// Value implements driver.Valuer for database serialization.
func (p StationPermissions) Value() (driver.Value, error) {
	return json.Marshal(p)
}

// Scan implements sql.Scanner for database deserialization.
func (p *StationPermissions) Scan(value interface{}) error {
	if value == nil {
		*p = StationPermissions{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal StationPermissions: %v", value)
	}
	if len(bytes) == 0 {
		*p = StationPermissions{}
		return nil
	}
	return json.Unmarshal(bytes, p)
}

// DefaultPermissionsForRole returns the default permissions for a station role.
func DefaultPermissionsForRole(role StationRole) StationPermissions {
	switch role {
	case StationRoleOwner, StationRoleAdmin:
		return StationPermissions{
			CanUploadMedia:       true,
			CanDeleteMedia:       true,
			CanEditMetadata:      true,
			CanManagePlaylists:   true,
			CanManageSmartBlocks: true,
			CanManageSchedule:    true,
			CanManageClocks:      true,
			CanGoLive:            true,
			CanKickDJ:            true,
			CanManageUsers:       role == StationRoleOwner || role == StationRoleAdmin,
			CanManageSettings:    role == StationRoleOwner || role == StationRoleAdmin,
			CanViewAnalytics:     true,
			CanManageMounts:      role == StationRoleOwner || role == StationRoleAdmin,
		}
	case StationRoleManager:
		return StationPermissions{
			CanUploadMedia:       true,
			CanDeleteMedia:       true,
			CanEditMetadata:      true,
			CanManagePlaylists:   true,
			CanManageSmartBlocks: true,
			CanManageSchedule:    true,
			CanManageClocks:      true,
			CanGoLive:            true,
			CanKickDJ:            false,
			CanManageUsers:       false,
			CanManageSettings:    false,
			CanViewAnalytics:     true,
			CanManageMounts:      false,
		}
	case StationRoleDJ:
		return StationPermissions{
			CanUploadMedia:       true,
			CanDeleteMedia:       false,
			CanEditMetadata:      true,
			CanManagePlaylists:   false,
			CanManageSmartBlocks: false,
			CanManageSchedule:    false,
			CanManageClocks:      false,
			CanGoLive:            true,
			CanKickDJ:            false,
			CanManageUsers:       false,
			CanManageSettings:    false,
			CanViewAnalytics:     false,
			CanManageMounts:      false,
		}
	case StationRoleViewer:
		return StationPermissions{} // All false
	default:
		return StationPermissions{}
	}
}

// StationUser associates users with specific stations and their role.
type StationUser struct {
	ID          string             `gorm:"type:uuid;primaryKey"`
	UserID      string             `gorm:"type:uuid;index;not null"`
	StationID   string             `gorm:"type:uuid;index;not null"`
	Role        StationRole        `gorm:"type:varchar(16);not null"` // owner, admin, manager, dj, viewer
	Permissions StationPermissions `gorm:"type:jsonb"`                // Custom permissions (overrides role defaults)
	InvitedBy   *string            `gorm:"type:uuid"`                 // Who invited this user (nil if not invited)
	InvitedAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// GetEffectivePermissions returns the user's effective permissions for this station.
// Custom permissions override role defaults.
func (su *StationUser) GetEffectivePermissions() StationPermissions {
	defaults := DefaultPermissionsForRole(su.Role)

	// If custom permissions are set, use them; otherwise use role defaults
	// For now, we just return role defaults - custom permissions can override later
	return defaults
}

// StationGroup allows grouping users within a station (e.g., "Morning Show Team").
type StationGroup struct {
	ID          string               `gorm:"type:uuid;primaryKey"`
	StationID   string               `gorm:"type:uuid;index;not null"`
	Name        string               `gorm:"index"`
	Description string               `gorm:"type:text"`
	Permissions StationPermissions   `gorm:"type:jsonb"`
	Members     []StationGroupMember `gorm:"foreignKey:GroupID"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// StationGroupMember links users to station groups.
type StationGroupMember struct {
	ID        string `gorm:"type:uuid;primaryKey"`
	UserID    string `gorm:"type:uuid;index;not null"`
	GroupID   string `gorm:"type:uuid;index;not null"`
	CreatedAt time.Time
}

// Station aggregates mounts and scheduling data.
type Station struct {
	ID          string `gorm:"type:uuid;primaryKey"`
	Name        string `gorm:"uniqueIndex"`
	Description string `gorm:"type:text"`
	Timezone    string `gorm:"type:varchar(32)"`

	// Ownership
	OwnerID string `gorm:"type:uuid;index"` // User who created/owns the station

	// Platform admin controls
	Active    bool `gorm:"default:true"`  // Station is enabled (admin can disable)
	Public    bool `gorm:"default:false"` // Public listening allowed (no auth required)
	Approved  bool `gorm:"default:false"` // Approved for broadcast by platform admin
	SortOrder int  `gorm:"default:0"`     // Display order on public pages (lower = first)
	Featured  bool `gorm:"default:false"` // Featured on homepage player

	// Archive defaults for new media
	DefaultShowInArchive bool `gorm:"default:true"` // Default: show new media in public archive
	DefaultAllowDownload bool `gorm:"default:true"` // Default: allow downloads for new media

	// Schedule boundary policy
	// hard: enforce schedule boundaries (default)
	// soft: allow overrun up to ScheduleSoftOverrunSeconds before forcing a cut
	ScheduleBoundaryMode       string `gorm:"type:varchar(8);not null;default:'hard'"`
	ScheduleSoftOverrunSeconds int    `gorm:"not null;default:0"`

	// WebRTC low-latency output (per-station RTP input port for the WebRTC broadcaster).
	// If 0, station will fall back to the global GRIMNIR_WEBRTC_RTP_PORT value.
	WebRTCRTPPort int `gorm:"not null;default:0"`

	// Crossfade defaults for scheduled playout.
	// When enabled, transitions between items will overlap and fade.
	CrossfadeEnabled     bool `gorm:"not null;default:false"`
	CrossfadeDurationMs  int  `gorm:"not null;default:0"` // 0 means "disabled"

	// Branding - imported from source systems (AzuraCast/LibreTime)
	Logo         []byte            `gorm:"type:bytea"` // Station logo (JPEG/PNG)
	LogoMime     string            `gorm:"type:varchar(32)"`
	HeaderImage  []byte            `gorm:"type:bytea"` // Station header/banner image
	HeaderMime   string            `gorm:"type:varchar(32)"`
	Shortcode    string            `gorm:"type:varchar(32);index"` // Short URL-friendly name
	Genre        string            `gorm:"type:varchar(64)"`
	Language     string            `gorm:"type:varchar(32)"`
	Website      string            `gorm:"type:varchar(255)"`          // Station website URL
	SocialLinks  map[string]string `gorm:"type:jsonb;serializer:json"` // Social media links
	ContactEmail string            `gorm:"type:varchar(255)"`
	ListenURL    string            `gorm:"type:varchar(255)"` // Primary public listen URL

	// Relationships
	Users  []StationUser  `gorm:"foreignKey:StationID"`
	Groups []StationGroup `gorm:"foreignKey:StationID"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsOwnedBy checks if the station is owned by the given user.
func (s *Station) IsOwnedBy(userID string) bool {
	return s.OwnerID == userID
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
	EncoderPresetID *string `gorm:"type:uuid"` // Nullable foreign key
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
	ID          string   `gorm:"type:uuid;primaryKey"`
	StationID   string   `gorm:"type:uuid;index"`
	Station     *Station `gorm:"foreignKey:StationID"` // Belongs to station (for cross-station queries)
	Title       string   `gorm:"index"`
	Artist      string   `gorm:"index"`
	Album       string   `gorm:"index"`
	Duration    time.Duration
	Path        string
	StorageKey  string
	ContentHash string `gorm:"type:varchar(64);index"` // SHA-256 hash for deduplication across stations
	ImportPath  string // Original path from import (LibreTime/AzuraCast)

	// Import provenance (nullable for manually created items)
	ImportJobID    *string `gorm:"type:uuid;index"`   // Which import job created this
	ImportSource   string  `gorm:"type:varchar(50)"`  // "libretime", "azuracast"
	ImportSourceID string  `gorm:"type:varchar(255)"` // Original ID in source system
	Genre          string
	Mood           string
	Label          string
	Language       string
	Explicit       bool
	ShowInArchive  bool `gorm:"default:true"` // Whether to show in public archive
	AllowDownload  bool `gorm:"default:true"` // Whether to allow downloads from archive
	LoudnessLUFS   float64
	ReplayGain     float64
	BPM            float64
	Year           string // Changed from int to string for flexibility
	TrackNumber    int
	Bitrate        int
	Samplerate     int
	ISRC           string `gorm:"type:varchar(20);index"` // International Standard Recording Code
	Lyrics         string `gorm:"type:text"`              // Song lyrics
	Composer       string `gorm:"type:varchar(255)"`
	Conductor      string `gorm:"type:varchar(255)"`
	Copyright      string `gorm:"type:varchar(255)"`
	Publisher      string `gorm:"type:varchar(255)"`
	OriginalArtist string `gorm:"type:varchar(255)"`
	AlbumArtist    string `gorm:"type:varchar(255)"`
	DiscNumber     int
	Comment        string            `gorm:"type:text"`
	CustomFields   map[string]string `gorm:"type:jsonb;serializer:json"` // For custom metadata fields
	Tags           []MediaTagLink
	CuePoints      CuePointSet `gorm:"type:jsonb"`
	Waveform       []byte
	Artwork        []byte        // Embedded album art (JPEG/PNG)
	ArtworkMime    string        `gorm:"type:varchar(32)"` // MIME type of artwork
	AnalysisState  AnalysisState `gorm:"type:varchar(32)"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CuePointSet captures intro/outro markers and fades.
type CuePointSet struct {
	IntroEnd float64 `json:"intro_end"`
	OutroIn  float64 `json:"outro_in"`
	FadeIn   float64 `json:"fade_in,omitempty"`  // Fade in duration in seconds
	FadeOut  float64 `json:"fade_out,omitempty"` // Fade out duration in seconds
}

// Value implements driver.Valuer for database serialization.
func (c CuePointSet) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements sql.Scanner for database deserialization.
func (c *CuePointSet) Scan(value interface{}) error {
	if value == nil {
		*c = CuePointSet{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal CuePointSet: %v", value)
	}
	if len(bytes) == 0 {
		*c = CuePointSet{}
		return nil
	}
	return json.Unmarshal(bytes, c)
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
	Rules       map[string]any `gorm:"type:jsonb;serializer:json"`
	Sequence    map[string]any `gorm:"type:jsonb;serializer:json"`

	// Import provenance (nullable for manually created items)
	ImportJobID    *string `gorm:"type:uuid;index"`   // Which import job created this
	ImportSource   string  `gorm:"type:varchar(50)"`  // "libretime", "azuracast"
	ImportSourceID string  `gorm:"type:varchar(255)"` // Original ID in source system

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ClockSlotType enumerates slot types.
type ClockSlotType string

const (
	SlotTypeSmartBlock ClockSlotType = "smart_block"
	SlotTypePlaylist   ClockSlotType = "playlist"
	SlotTypeHardItem   ClockSlotType = "hard_item"
	SlotTypeStopset    ClockSlotType = "stopset"
	SlotTypeWebstream  ClockSlotType = "webstream"
)

// ClockHour describes one hour clock template.
type ClockHour struct {
	ID        string `gorm:"type:uuid;primaryKey"`
	StationID string `gorm:"type:uuid;index"`
	Name      string
	Slots     []ClockSlot `gorm:"foreignKey:ClockHourID"`

	// Import provenance (nullable for manually created items)
	ImportJobID *string `gorm:"type:uuid;index"` // Which import job created this

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
	Payload     map[string]any `gorm:"type:jsonb;serializer:json"`
}

// RecurrenceType defines how a schedule entry repeats
type RecurrenceType string

const (
	RecurrenceNone     RecurrenceType = ""
	RecurrenceDaily    RecurrenceType = "daily"
	RecurrenceWeekdays RecurrenceType = "weekdays"
	RecurrenceWeekly   RecurrenceType = "weekly"
	RecurrenceCustom   RecurrenceType = "custom"
)

// ScheduleEntry materializes a planned item.
type ScheduleEntry struct {
	ID         string         `gorm:"type:uuid;primaryKey"`
	StationID  string         `gorm:"type:uuid;index:idx_schedule_station_time"`
	MountID    string         `gorm:"type:uuid;index"`
	StartsAt   time.Time      `gorm:"index:idx_schedule_station_time;index:idx_schedule_time_range"`
	EndsAt     time.Time      `gorm:"index:idx_schedule_time_range"`
	SourceType string         `gorm:"type:varchar(32)"`
	SourceID   string         `gorm:"type:uuid"`
	Metadata   map[string]any `gorm:"type:jsonb;serializer:json"`

	// Recurrence fields
	RecurrenceType     RecurrenceType `gorm:"type:varchar(16)"`
	RecurrenceDays     []int          `gorm:"type:jsonb;serializer:json"` // 0=Sun, 1=Mon, ..., 6=Sat
	RecurrenceEndDate  *time.Time     // When recurrence stops (nil = forever)
	RecurrenceParentID *string        `gorm:"type:uuid;index"` // Links instance to parent
	IsInstance         bool           `gorm:"default:false"`   // True if this is a generated instance

	// Import provenance (nullable for manually created items)
	ImportJobID    *string `gorm:"type:uuid;index"`   // Which import job created this
	ImportSource   string  `gorm:"type:varchar(50)"`  // "libretime", "azuracast"
	ImportSourceID string  `gorm:"type:varchar(255)"` // Original ID in source system

	CreatedAt time.Time
	UpdatedAt time.Time
}

// PlayHistory stores executed playout events.
type PlayHistory struct {
	ID         string         `gorm:"type:uuid;primaryKey" json:"id"`
	StationID  string         `gorm:"type:uuid;index:idx_history_station_time" json:"station_id"`
	MountID    string         `gorm:"type:uuid;index" json:"mount_id"`
	MediaID    string         `gorm:"type:uuid;index" json:"media_id"`
	Artist     string         `gorm:"index" json:"artist"`
	Title      string         `gorm:"index" json:"title"`
	Album      string         `gorm:"index" json:"album"`
	Label      string         `json:"label"`
	StartedAt  time.Time      `gorm:"index:idx_history_station_time" json:"started_at"`
	EndedAt    time.Time      `json:"ended_at"`
	Transition string         `gorm:"type:varchar(32)" json:"transition"`
	Metadata   map[string]any `gorm:"type:jsonb;serializer:json" json:"metadata"`
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

// Playlist represents a static playlist of media items.
type Playlist struct {
	ID             string         `gorm:"type:uuid;primaryKey"`
	StationID      string         `gorm:"type:uuid;index"`
	Name           string         `gorm:"index"`
	Description    string         `gorm:"type:text"`
	CoverImage     []byte         `gorm:"type:bytea"`
	CoverImageMime string         `gorm:"type:varchar(32)"`
	Items          []PlaylistItem `gorm:"foreignKey:PlaylistID"`

	// Import provenance (nullable for manually created items)
	ImportJobID    *string `gorm:"type:uuid;index"`   // Which import job created this
	ImportSource   string  `gorm:"type:varchar(50)"`  // "libretime", "azuracast"
	ImportSourceID string  `gorm:"type:varchar(255)"` // Original ID in source system

	CreatedAt time.Time
	UpdatedAt time.Time
}

// PlaylistItem represents an item in a playlist.
type PlaylistItem struct {
	ID         string    `gorm:"type:uuid;primaryKey"`
	PlaylistID string    `gorm:"type:uuid;index"`
	MediaID    string    `gorm:"type:uuid;index"`
	Media      MediaItem `gorm:"foreignKey:MediaID"`
	Position   int       `gorm:"index"`
	FadeIn     int       // Fade in duration in milliseconds
	FadeOut    int       // Fade out duration in milliseconds
	CueIn      int       // Cue in point in milliseconds
	CueOut     int       // Cue out point in milliseconds
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Clock represents a show template with flexible duration (not just hourly).
type Clock struct {
	ID          string `gorm:"type:uuid;primaryKey"`
	StationID   string `gorm:"type:uuid;index"`
	Name        string `gorm:"index"`
	Description string `gorm:"type:text"`
	Duration    int    `gorm:"type:integer"` // Duration in seconds

	// Import provenance (nullable for manually created items)
	ImportJobID    *string `gorm:"type:uuid;index"`   // Which import job created this
	ImportSource   string  `gorm:"type:varchar(50)"`  // "libretime", "azuracast"
	ImportSourceID string  `gorm:"type:varchar(255)"` // Original ID in source system

	CreatedAt time.Time
	UpdatedAt time.Time
}
