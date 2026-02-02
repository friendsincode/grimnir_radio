/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"time"

	"github.com/google/uuid"
)

// Sponsor represents an underwriting sponsor.
type Sponsor struct {
	ID           string `gorm:"type:uuid;primaryKey" json:"id"`
	StationID    string `gorm:"type:uuid;index;not null" json:"station_id"`
	Name         string `gorm:"type:varchar(255);not null" json:"name"`
	ContactName  string `gorm:"type:varchar(255)" json:"contact_name,omitempty"`
	ContactEmail string `gorm:"type:varchar(255)" json:"contact_email,omitempty"`
	ContactPhone string `gorm:"type:varchar(32)" json:"contact_phone,omitempty"`
	Address      string `gorm:"type:text" json:"address,omitempty"`
	Notes        string `gorm:"type:text" json:"notes,omitempty"`
	Active       bool   `gorm:"not null;default:true" json:"active"`

	// Relationships
	Station     *Station                 `gorm:"foreignKey:StationID" json:"station,omitempty"`
	Obligations []UnderwritingObligation `gorm:"foreignKey:SponsorID" json:"obligations,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for GORM.
func (Sponsor) TableName() string {
	return "sponsors"
}

// NewSponsor creates a new sponsor.
func NewSponsor(stationID, name string) *Sponsor {
	return &Sponsor{
		ID:        uuid.NewString(),
		StationID: stationID,
		Name:      name,
		Active:    true,
	}
}

// Daypart represents a time-of-day segment.
type Daypart string

const (
	DaypartMorning   Daypart = "morning"   // 6am-10am
	DaypartMidday    Daypart = "midday"    // 10am-3pm
	DaypartAfternoon Daypart = "afternoon" // 3pm-7pm
	DaypartEvening   Daypart = "evening"   // 7pm-12am
	DaypartOvernight Daypart = "overnight" // 12am-6am
)

// UnderwritingObligation represents a sponsorship commitment.
type UnderwritingObligation struct {
	ID                  string     `gorm:"type:uuid;primaryKey" json:"id"`
	SponsorID           string     `gorm:"type:uuid;index;not null" json:"sponsor_id"`
	StationID           string     `gorm:"type:uuid;index;not null" json:"station_id"`
	Name                string     `gorm:"type:varchar(255)" json:"name,omitempty"` // Campaign name
	SpotsPerWeek        int        `gorm:"not null" json:"spots_per_week"`
	SpotDurationSeconds int        `gorm:"not null;default:30" json:"spot_duration_seconds"`
	MediaID             *string    `gorm:"type:uuid" json:"media_id,omitempty"`                   // Pre-recorded spot
	ScriptText          string     `gorm:"type:text" json:"script_text,omitempty"`                // Live-read script
	PreferredDayparts   string     `gorm:"type:varchar(255)" json:"preferred_dayparts,omitempty"` // Comma-separated
	PreferredShows      string     `gorm:"type:text" json:"preferred_shows,omitempty"`            // Comma-separated show IDs
	StartDate           time.Time  `gorm:"type:date;not null" json:"start_date"`
	EndDate             *time.Time `gorm:"type:date" json:"end_date,omitempty"`
	ContractValue       int        `json:"contract_value,omitempty"` // In cents
	Notes               string     `gorm:"type:text" json:"notes,omitempty"`
	Active              bool       `gorm:"not null;default:true" json:"active"`

	// Relationships
	Sponsor *Sponsor           `gorm:"foreignKey:SponsorID" json:"sponsor,omitempty"`
	Station *Station           `gorm:"foreignKey:StationID" json:"station,omitempty"`
	Media   *MediaItem         `gorm:"foreignKey:MediaID" json:"media,omitempty"`
	Spots   []UnderwritingSpot `gorm:"foreignKey:ObligationID" json:"spots,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for GORM.
func (UnderwritingObligation) TableName() string {
	return "underwriting_obligations"
}

// NewUnderwritingObligation creates a new obligation.
func NewUnderwritingObligation(sponsorID, stationID string, spotsPerWeek int) *UnderwritingObligation {
	return &UnderwritingObligation{
		ID:                  uuid.NewString(),
		SponsorID:           sponsorID,
		StationID:           stationID,
		SpotsPerWeek:        spotsPerWeek,
		SpotDurationSeconds: 30,
		StartDate:           time.Now(),
		Active:              true,
	}
}

// SpotStatus represents the status of an underwriting spot.
type SpotStatus string

const (
	SpotStatusScheduled SpotStatus = "scheduled"
	SpotStatusAired     SpotStatus = "aired"
	SpotStatusMissed    SpotStatus = "missed"
	SpotStatusCancelled SpotStatus = "cancelled"
)

// UnderwritingSpot represents a scheduled underwriting spot.
type UnderwritingSpot struct {
	ID           string     `gorm:"type:uuid;primaryKey" json:"id"`
	ObligationID string     `gorm:"type:uuid;index;not null" json:"obligation_id"`
	InstanceID   *string    `gorm:"type:uuid;index" json:"instance_id,omitempty"` // Show instance
	ScheduledAt  time.Time  `gorm:"not null;index" json:"scheduled_at"`
	AiredAt      *time.Time `json:"aired_at,omitempty"`
	Status       SpotStatus `gorm:"type:varchar(32);not null;default:'scheduled'" json:"status"`
	Notes        string     `gorm:"type:text" json:"notes,omitempty"`

	// Relationships
	Obligation *UnderwritingObligation `gorm:"foreignKey:ObligationID" json:"obligation,omitempty"`
	Instance   *ShowInstance           `gorm:"foreignKey:InstanceID" json:"instance,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for GORM.
func (UnderwritingSpot) TableName() string {
	return "underwriting_spots"
}

// NewUnderwritingSpot creates a new spot.
func NewUnderwritingSpot(obligationID string, scheduledAt time.Time) *UnderwritingSpot {
	return &UnderwritingSpot{
		ID:           uuid.NewString(),
		ObligationID: obligationID,
		ScheduledAt:  scheduledAt,
		Status:       SpotStatusScheduled,
	}
}

// FulfillmentReport represents a sponsor fulfillment report.
type FulfillmentReport struct {
	SponsorID       string    `json:"sponsor_id"`
	SponsorName     string    `json:"sponsor_name"`
	ObligationID    string    `json:"obligation_id"`
	ObligationName  string    `json:"obligation_name"`
	PeriodStart     time.Time `json:"period_start"`
	PeriodEnd       time.Time `json:"period_end"`
	SpotsRequired   int       `json:"spots_required"`
	SpotsScheduled  int       `json:"spots_scheduled"`
	SpotsAired      int       `json:"spots_aired"`
	SpotsMissed     int       `json:"spots_missed"`
	FulfillmentRate float64   `json:"fulfillment_rate"` // Percentage
	Status          string    `json:"status"`           // "on_track", "behind", "fulfilled"
}
