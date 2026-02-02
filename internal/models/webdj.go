/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// WebDJSession represents an active WebDJ console session.
type WebDJSession struct {
	ID              string      `gorm:"type:uuid;primaryKey"`
	LiveSessionID   string      `gorm:"type:uuid;index"` // Links to existing LiveSession
	StationID       string      `gorm:"type:uuid;index"`
	UserID          string      `gorm:"type:uuid;index"`
	DeckAState      DeckState   `gorm:"type:jsonb;serializer:json"`
	DeckBState      DeckState   `gorm:"type:jsonb;serializer:json"`
	MixerState      MixerState  `gorm:"type:jsonb;serializer:json"`
	CrossfaderCurve string      `gorm:"type:varchar(32);default:'linear'"`
	Active          bool        `gorm:"default:true;index"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TableName overrides GORM table name.
func (WebDJSession) TableName() string {
	return "webdj_sessions"
}

// DeckState represents the state of a single DJ deck.
type DeckState struct {
	MediaID    string     `json:"media_id,omitempty"`
	Title      string     `json:"title,omitempty"`
	Artist     string     `json:"artist,omitempty"`
	DurationMS int64      `json:"duration_ms"`
	PositionMS int64      `json:"position_ms"`
	State      string     `json:"state"` // idle, loading, cued, playing, paused
	BPM        float64    `json:"bpm,omitempty"`
	Pitch      float64    `json:"pitch"` // -8 to +8 percent
	Volume     float64    `json:"volume"` // 0.0 to 1.0
	HotCues    []CuePoint `json:"hot_cues,omitempty"`
	LoopInMS   *int64     `json:"loop_in_ms,omitempty"`
	LoopOutMS  *int64     `json:"loop_out_ms,omitempty"`
	LoopActive bool       `json:"loop_active"`
	EQHigh     float64    `json:"eq_high"` // -12 to +12 dB
	EQMid      float64    `json:"eq_mid"`
	EQLow      float64    `json:"eq_low"`
}

// Value implements driver.Valuer for DeckState.
func (d DeckState) Value() (driver.Value, error) {
	return json.Marshal(d)
}

// Scan implements sql.Scanner for DeckState.
func (d *DeckState) Scan(value interface{}) error {
	if value == nil {
		*d = DeckState{State: "idle", Volume: 1.0}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return json.Unmarshal([]byte(value.(string)), d)
	}
	return json.Unmarshal(bytes, d)
}

// CuePoint represents a hot cue or loop marker.
type CuePoint struct {
	ID         int    `json:"id"`         // 1-8 for hot cues
	PositionMS int64  `json:"position_ms"`
	Label      string `json:"label,omitempty"`
	Color      string `json:"color,omitempty"` // Hex color
}

// MixerState represents the state of the mixer controls.
type MixerState struct {
	Crossfader    float64 `json:"crossfader"`    // 0.0 = A, 0.5 = center, 1.0 = B
	MasterVolume  float64 `json:"master_volume"` // 0.0 to 1.0
	CueSplit      bool    `json:"cue_split"`     // Monitor cue split mode
	CueMixLevel   float64 `json:"cue_mix_level"` // Monitor cue mix level
	MicActive     bool    `json:"mic_active"`
	MicVolume     float64 `json:"mic_volume"`
	TalkoverDuck  float64 `json:"talkover_duck"` // How much to duck when mic active
}

// Value implements driver.Valuer for MixerState.
func (m MixerState) Value() (driver.Value, error) {
	return json.Marshal(m)
}

// Scan implements sql.Scanner for MixerState.
func (m *MixerState) Scan(value interface{}) error {
	if value == nil {
		*m = MixerState{Crossfader: 0.5, MasterVolume: 1.0, MicVolume: 1.0, TalkoverDuck: 0.7}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return json.Unmarshal([]byte(value.(string)), m)
	}
	return json.Unmarshal(bytes, m)
}

// WaveformCache stores pre-generated waveform data for media items.
type WaveformCache struct {
	MediaID       string `gorm:"type:uuid;primaryKey"`
	SamplesPerSec int    `gorm:"type:int"`
	DurationMS    int64  `gorm:"type:bigint"`
	// PeakData contains compressed waveform peaks (alternating min/max values)
	PeakData    []byte `gorm:"type:bytea"`
	GeneratedAt time.Time
}

// TableName overrides GORM table name.
func (WaveformCache) TableName() string {
	return "waveform_cache"
}

// DeckID represents which deck (A or B).
type DeckID string

const (
	DeckA DeckID = "a"
	DeckB DeckID = "b"
)

// DeckStateType represents the playback state of a deck.
type DeckStateType string

const (
	DeckStateIdle    DeckStateType = "idle"
	DeckStateLoading DeckStateType = "loading"
	DeckStateCued    DeckStateType = "cued"
	DeckStatePlaying DeckStateType = "playing"
	DeckStatePaused  DeckStateType = "paused"
)

// CrossfaderCurveType represents the crossfader curve shape.
type CrossfaderCurveType string

const (
	CrossfaderLinear   CrossfaderCurveType = "linear"
	CrossfaderFastCut  CrossfaderCurveType = "fast_cut"
	CrossfaderSlowCut  CrossfaderCurveType = "slow_cut"
	CrossfaderConstant CrossfaderCurveType = "constant" // Both at full volume
)

// NewDeckState creates a new deck state with default values.
func NewDeckState() DeckState {
	return DeckState{
		State:    string(DeckStateIdle),
		Volume:   1.0,
		Pitch:    0.0,
		EQHigh:   0.0,
		EQMid:    0.0,
		EQLow:    0.0,
		HotCues:  make([]CuePoint, 0),
	}
}

// NewMixerState creates a new mixer state with default values.
func NewMixerState() MixerState {
	return MixerState{
		Crossfader:   0.5,
		MasterVolume: 1.0,
		MicVolume:    1.0,
		TalkoverDuck: 0.7,
	}
}
