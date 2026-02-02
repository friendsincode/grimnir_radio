/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"time"

	"github.com/google/uuid"
)

// NetworkShow represents a show that can be syndicated across multiple stations.
type NetworkShow struct {
	ID           string  `gorm:"type:uuid;primaryKey" json:"id"`
	NetworkID    *string `gorm:"type:uuid;index" json:"network_id,omitempty"` // For grouping networks
	SourceShowID *string `gorm:"type:uuid" json:"source_show_id,omitempty"`   // Original show if internal
	Name         string  `gorm:"type:varchar(255);not null" json:"name"`
	Description  string  `gorm:"type:text" json:"description,omitempty"`
	FeedURL      string  `gorm:"type:text" json:"feed_url,omitempty"`         // For external syndicated content
	FeedType     string  `gorm:"type:varchar(32)" json:"feed_type,omitempty"` // "podcast", "live", "recorded"
	DelayMinutes int     `gorm:"default:0" json:"delay_minutes"`              // Delayed broadcast
	Duration     int     `gorm:"not null;default:60" json:"duration_minutes"`
	Active       bool    `gorm:"not null;default:true" json:"active"`

	// Relationships
	SourceShow    *Show                 `gorm:"foreignKey:SourceShowID" json:"source_show,omitempty"`
	Subscriptions []NetworkSubscription `gorm:"foreignKey:NetworkShowID" json:"subscriptions,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for GORM.
func (NetworkShow) TableName() string {
	return "network_shows"
}

// NewNetworkShow creates a new network show.
func NewNetworkShow(name string) *NetworkShow {
	return &NetworkShow{
		ID:     uuid.NewString(),
		Name:   name,
		Active: true,
	}
}

// NetworkSubscription represents a station's subscription to a network show.
type NetworkSubscription struct {
	ID            string `gorm:"type:uuid;primaryKey" json:"id"`
	StationID     string `gorm:"type:uuid;index;not null" json:"station_id"`
	NetworkShowID string `gorm:"type:uuid;index;not null" json:"network_show_id"`
	LocalTime     string `gorm:"type:varchar(8)" json:"local_time,omitempty"`  // "HH:MM:SS" when to air locally
	LocalDays     string `gorm:"type:varchar(32)" json:"local_days,omitempty"` // Comma-separated: "MO,TU,WE"
	Timezone      string `gorm:"type:varchar(64);default:'UTC'" json:"timezone"`
	Active        bool   `gorm:"not null;default:true" json:"active"`
	AutoSchedule  bool   `gorm:"not null;default:false" json:"auto_schedule"` // Auto-create instances

	// Relationships
	Station     *Station     `gorm:"foreignKey:StationID" json:"station,omitempty"`
	NetworkShow *NetworkShow `gorm:"foreignKey:NetworkShowID" json:"network_show,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for GORM.
func (NetworkSubscription) TableName() string {
	return "network_subscriptions"
}

// NewNetworkSubscription creates a new subscription.
func NewNetworkSubscription(stationID, networkShowID string) *NetworkSubscription {
	return &NetworkSubscription{
		ID:            uuid.NewString(),
		StationID:     stationID,
		NetworkShowID: networkShowID,
		Timezone:      "UTC",
		Active:        true,
	}
}

// Network represents a group of stations sharing content.
type Network struct {
	ID          string `gorm:"type:uuid;primaryKey" json:"id"`
	Name        string `gorm:"type:varchar(255);not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	OwnerID     string `gorm:"type:uuid" json:"owner_id,omitempty"` // User who owns the network
	Active      bool   `gorm:"not null;default:true" json:"active"`

	// Relationships
	Shows []NetworkShow `gorm:"foreignKey:NetworkID" json:"shows,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for GORM.
func (Network) TableName() string {
	return "networks"
}

// NewNetwork creates a new network.
func NewNetwork(name string) *Network {
	return &Network{
		ID:     uuid.NewString(),
		Name:   name,
		Active: true,
	}
}
