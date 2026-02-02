/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"time"
)

// LandingPage stores the landing page configuration for a station or the platform.
// When StationID is empty, this is the platform landing page.
type LandingPage struct {
	ID              string         `gorm:"type:uuid;primaryKey" json:"id"`
	StationID       *string        `gorm:"type:uuid;uniqueIndex" json:"station_id"` // NULL = platform landing page
	Theme           string         `gorm:"type:varchar(64);default:'default'" json:"theme"`
	PublishedConfig map[string]any `gorm:"type:jsonb;serializer:json" json:"published_config"`
	DraftConfig     map[string]any `gorm:"type:jsonb;serializer:json" json:"draft_config"`
	CustomCSS       string         `gorm:"type:text" json:"custom_css"`
	CustomHead      string         `gorm:"type:text" json:"custom_head"`
	PublishedAt     *time.Time     `json:"published_at"`
	PublishedBy     *string        `gorm:"type:uuid" json:"published_by"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// IsPlatformPage returns true if this is the platform landing page (no station).
func (lp *LandingPage) IsPlatformPage() bool {
	return lp.StationID == nil || *lp.StationID == ""
}

// HasDraft returns true if there are unpublished changes.
func (lp *LandingPage) HasDraft() bool {
	return lp.DraftConfig != nil && len(lp.DraftConfig) > 0
}

// LandingPageAsset stores uploaded assets (images, logos) for landing pages.
// When StationID is NULL, this is a platform-level asset.
type LandingPageAsset struct {
	ID         string    `gorm:"type:uuid;primaryKey" json:"id"`
	StationID  *string   `gorm:"type:uuid;index" json:"station_id"` // NULL = platform asset
	AssetType  string    `gorm:"type:varchar(32)" json:"asset_type"` // logo, background, image, favicon
	FilePath   string    `gorm:"type:varchar(512)" json:"file_path"`
	FileName   string    `gorm:"type:varchar(255)" json:"file_name"`
	MimeType   string    `gorm:"type:varchar(64)" json:"mime_type"`
	FileSize   int64     `json:"file_size"`
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	UploadedBy *string   `gorm:"type:uuid" json:"uploaded_by"`
	CreatedAt  time.Time `json:"created_at"`
}

// AssetType constants
const (
	AssetTypeLogo       = "logo"
	AssetTypeBackground = "background"
	AssetTypeImage      = "image"
	AssetTypeFavicon    = "favicon"
	AssetTypeHero       = "hero"
)

// LandingPageVersion stores historical versions for restore functionality.
type LandingPageVersion struct {
	ID            string         `gorm:"type:uuid;primaryKey" json:"id"`
	LandingPageID string         `gorm:"type:uuid;index" json:"landing_page_id"`
	VersionNumber int            `json:"version_number"`
	Config        map[string]any `gorm:"type:jsonb;serializer:json" json:"config"`
	ChangeType    string         `gorm:"type:varchar(32)" json:"change_type"` // publish, auto_save, restore
	ChangeSummary string         `gorm:"type:text" json:"change_summary"`
	CreatedBy     *string        `gorm:"type:uuid" json:"created_by"`
	CreatedAt     time.Time      `json:"created_at"`
}

// ChangeType constants
const (
	ChangeTypePublish  = "publish"
	ChangeTypeAutoSave = "auto_save"
	ChangeTypeRestore  = "restore"
)

// Widget types for landing page configuration
const (
	WidgetTypePlayer       = "player"
	WidgetTypeSchedule     = "schedule"
	WidgetTypeRecentTracks = "recent-tracks"
	WidgetTypeText         = "text"
	WidgetTypeImage        = "image"
	WidgetTypeSpacer       = "spacer"
	WidgetTypeDivider      = "divider"
	WidgetTypeDJGrid       = "dj-grid"
	WidgetTypeUpcoming     = "upcoming-shows"
	WidgetTypeGallery      = "image-gallery"
	WidgetTypeVideo        = "video"
	WidgetTypeCTA          = "cta"
	WidgetTypeContact      = "contact"
	WidgetTypeSocialFeed   = "social-feed"
	WidgetTypeNewsletter   = "newsletter"
	WidgetTypeCustomHTML   = "custom-html"
)
