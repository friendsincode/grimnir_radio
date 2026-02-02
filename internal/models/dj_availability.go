/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import "time"

// DJAvailability defines when a DJ is available or unavailable.
type DJAvailability struct {
	ID        string  `gorm:"type:uuid;primaryKey" json:"id"`
	UserID    string  `gorm:"type:uuid;index:idx_dj_availability_user;not null" json:"user_id"`
	StationID *string `gorm:"type:uuid;index:idx_dj_availability_station" json:"station_id,omitempty"` // NULL = all stations

	// Recurring availability (day_of_week set, specific_date null)
	DayOfWeek *int `gorm:"index" json:"day_of_week,omitempty"` // 0=Sunday, 6=Saturday

	// One-off availability (specific_date set, day_of_week null)
	SpecificDate *time.Time `gorm:"type:date" json:"specific_date,omitempty"`

	// Time window
	StartTime string `gorm:"type:varchar(5);not null" json:"start_time"` // HH:MM format
	EndTime   string `gorm:"type:varchar(5);not null" json:"end_time"`   // HH:MM format

	// Available = true means DJ is available during this window
	// Available = false means DJ is NOT available (blocked time)
	Available bool   `gorm:"not null;default:true" json:"available"`
	Note      string `gorm:"type:text" json:"note,omitempty"`

	// Relationships
	User    *User    `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Station *Station `gorm:"foreignKey:StationID" json:"station,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for GORM.
func (DJAvailability) TableName() string {
	return "dj_availability"
}

// RequestType defines the type of schedule change request.
type RequestType string

const (
	RequestTypeNewShow    RequestType = "new_show"   // Request a new show slot
	RequestTypeSwap       RequestType = "swap"       // Swap shift with another DJ
	RequestTypeCancel     RequestType = "cancel"     // Cancel a show instance
	RequestTypeTimeOff    RequestType = "time_off"   // Request time off
	RequestTypeReschedule RequestType = "reschedule" // Move show to different time
)

// RequestStatus defines the status of a schedule request.
type RequestStatus string

const (
	RequestStatusPending   RequestStatus = "pending"
	RequestStatusApproved  RequestStatus = "approved"
	RequestStatusRejected  RequestStatus = "rejected"
	RequestStatusCancelled RequestStatus = "cancelled" // Cancelled by requester
)

// ScheduleRequest represents a DJ's request to change the schedule.
type ScheduleRequest struct {
	ID          string      `gorm:"type:uuid;primaryKey" json:"id"`
	StationID   string      `gorm:"type:uuid;index:idx_schedule_requests_station;not null" json:"station_id"`
	RequestType RequestType `gorm:"type:varchar(32);not null" json:"request_type"`

	// Who is making the request
	RequesterID string `gorm:"type:uuid;index:idx_schedule_requests_requester;not null" json:"requester_id"`

	// Target show instance (for swap, cancel, reschedule)
	TargetInstanceID *string `gorm:"type:uuid" json:"target_instance_id,omitempty"`

	// For swap requests - who to swap with
	SwapWithUserID *string `gorm:"type:uuid" json:"swap_with_user_id,omitempty"`

	// Proposed changes (times, etc.)
	ProposedData map[string]any `gorm:"type:jsonb;serializer:json" json:"proposed_data,omitempty"`

	// Status and review
	Status     RequestStatus `gorm:"type:varchar(32);not null;default:'pending';index:idx_schedule_requests_status" json:"status"`
	ReviewedBy *string       `gorm:"type:uuid" json:"reviewed_by,omitempty"`
	ReviewedAt *time.Time    `json:"reviewed_at,omitempty"`
	ReviewNote string        `gorm:"type:text" json:"review_note,omitempty"`

	// Relationships
	Station        *Station      `gorm:"foreignKey:StationID" json:"station,omitempty"`
	Requester      *User         `gorm:"foreignKey:RequesterID" json:"requester,omitempty"`
	TargetInstance *ShowInstance `gorm:"foreignKey:TargetInstanceID" json:"target_instance,omitempty"`
	SwapWithUser   *User         `gorm:"foreignKey:SwapWithUserID" json:"swap_with_user,omitempty"`
	Reviewer       *User         `gorm:"foreignKey:ReviewedBy" json:"reviewer,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for GORM.
func (ScheduleRequest) TableName() string {
	return "schedule_requests"
}

// ScheduleLock defines when the schedule is locked from DJ edits.
type ScheduleLock struct {
	ID        string `gorm:"type:uuid;primaryKey" json:"id"`
	StationID string `gorm:"type:uuid;uniqueIndex:idx_schedule_locks_station;not null" json:"station_id"`

	// Lock schedule this many days before show time
	// e.g., 7 means schedule is locked 7 days before the show
	LockBeforeDays int `gorm:"not null;default:7" json:"lock_before_days"`

	// Minimum role required to bypass the lock
	MinBypassRole RoleName `gorm:"type:varchar(32);not null;default:'manager'" json:"min_bypass_role"`

	// Optional: specific dates that are always locked regardless of days setting
	LockedDates []time.Time `gorm:"type:jsonb;serializer:json" json:"locked_dates,omitempty"`

	// Relationships
	Station *Station `gorm:"foreignKey:StationID" json:"station,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for GORM.
func (ScheduleLock) TableName() string {
	return "schedule_locks"
}

// IsLocked checks if a given date is locked for DJ edits.
func (sl *ScheduleLock) IsLocked(targetDate time.Time) bool {
	// Check if within lock window
	lockDate := time.Now().AddDate(0, 0, sl.LockBeforeDays)
	if targetDate.Before(lockDate) {
		return true
	}

	// Check specific locked dates
	for _, d := range sl.LockedDates {
		if d.Year() == targetDate.Year() && d.YearDay() == targetDate.YearDay() {
			return true
		}
	}

	return false
}
