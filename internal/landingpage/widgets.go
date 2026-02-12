/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package landingpage

import (
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// WidgetType represents the type of a landing page widget.
type WidgetType string

// Widget types
const (
	WidgetStationsGrid WidgetType = "stations-grid"
	WidgetPlayer       WidgetType = "player"
	WidgetSchedule     WidgetType = "schedule"
	WidgetRecentTracks WidgetType = "recent-tracks"
	WidgetText         WidgetType = "text"
	WidgetImage        WidgetType = "image"
	WidgetSpacer       WidgetType = "spacer"
	WidgetDivider      WidgetType = "divider"
	WidgetDJGrid       WidgetType = "dj-grid"
	WidgetUpcoming     WidgetType = "upcoming-shows"
	WidgetGallery      WidgetType = "image-gallery"
	WidgetVideo        WidgetType = "video"
	WidgetCTA          WidgetType = "cta"
	WidgetContact      WidgetType = "contact"
	WidgetSocialFeed   WidgetType = "social-feed"
	WidgetNewsletter   WidgetType = "newsletter"
	WidgetCustomHTML   WidgetType = "custom-html"
)

// WidgetConfig represents the configuration for a widget instance.
type WidgetConfig struct {
	ID       string         `json:"id"`
	Type     WidgetType     `json:"type"`
	Config   map[string]any `json:"config"`
	Position WidgetPosition `json:"position,omitempty"`
}

// WidgetPosition represents the position of a widget in a grid layout.
type WidgetPosition struct {
	Column int `json:"column"`
	Row    int `json:"row"`
	Width  int `json:"width"`
}

// WidgetDefinition describes a widget type's properties and defaults.
type WidgetDefinition struct {
	Type        WidgetType     `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Icon        string         `json:"icon"`
	Category    string         `json:"category"`
	Defaults    map[string]any `json:"defaults"`
	ConfigSpec  []ConfigField  `json:"config_spec"`
}

// ConfigField describes a widget configuration field.
type ConfigField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"` // text, number, boolean, select, color, image
	Default     any    `json:"default,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Options     []any  `json:"options,omitempty"` // For select fields
	Placeholder string `json:"placeholder,omitempty"`
	Min         *int   `json:"min,omitempty"`
	Max         *int   `json:"max,omitempty"`
}

// WidgetRegistry contains all available widget definitions.
var WidgetRegistry = []WidgetDefinition{
	{
		Type:        WidgetStationsGrid,
		Name:        "Stations Grid",
		Description: "Grid display of all public stations",
		Icon:        "broadcast",
		Category:    "core",
		Defaults: map[string]any{
			"title":       "Our Stations",
			"columns":     3,
			"showLogo":    true,
			"showDesc":    true,
			"centerSingle": true,
		},
		ConfigSpec: []ConfigField{
			{Key: "title", Label: "Section Title", Type: "text", Default: "Our Stations"},
			{Key: "columns", Label: "Columns", Type: "select", Default: 3, Options: []any{2, 3, 4}},
			{Key: "showLogo", Label: "Show Station Logo", Type: "boolean", Default: true},
			{Key: "showDesc", Label: "Show Description", Type: "boolean", Default: true},
			// Station landing pages can optionally place the current station after the platform's first station.
			{Key: "placement", Label: "Station Page Placement", Type: "select", Default: "current_first", Options: []any{"current_first", "after_platform_first"}},
		},
	},
	{
		Type:        WidgetPlayer,
		Name:        "Radio Player",
		Description: "Audio player with now playing info",
		Icon:        "play-circle",
		Category:    "core",
		Defaults: map[string]any{
			"showArtwork":      true,
			"showNowPlaying":   true,
			"showVolumeSlider": true,
			"size":             "large",
			"stationId":        "",
		},
		ConfigSpec: []ConfigField{
			{Key: "title", Label: "Title", Type: "text", Placeholder: "Listen Live"},
			{Key: "showArtwork", Label: "Show Artwork", Type: "boolean", Default: true},
			{Key: "showNowPlaying", Label: "Show Now Playing", Type: "boolean", Default: true},
			{Key: "showVolumeSlider", Label: "Show Volume", Type: "boolean", Default: true},
			{Key: "size", Label: "Size", Type: "select", Default: "large", Options: []any{"small", "medium", "large"}},
		},
	},
	{
		Type:        WidgetSchedule,
		Name:        "Schedule",
		Description: "Today's show schedule",
		Icon:        "calendar",
		Category:    "core",
		Defaults: map[string]any{
			"showCount": 5,
			"showTime":  true,
			"showHost":  true,
			"showImage": true,
		},
		ConfigSpec: []ConfigField{
			{Key: "showCount", Label: "Number of Shows", Type: "number", Default: 5, Min: intPtr(1), Max: intPtr(20)},
			{Key: "showTime", Label: "Show Time", Type: "boolean", Default: true},
			{Key: "showHost", Label: "Show Host", Type: "boolean", Default: true},
			{Key: "showImage", Label: "Show Image", Type: "boolean", Default: true},
		},
	},
	{
		Type:        WidgetRecentTracks,
		Name:        "Recent Tracks",
		Description: "Recently played songs",
		Icon:        "music",
		Category:    "core",
		Defaults: map[string]any{
			"count":     10,
			"showAlbum": true,
			"showTime":  true,
		},
		ConfigSpec: []ConfigField{
			{Key: "count", Label: "Number of Tracks", Type: "number", Default: 10, Min: intPtr(1), Max: intPtr(50)},
			{Key: "showAlbum", Label: "Show Album", Type: "boolean", Default: true},
			{Key: "showTime", Label: "Show Time", Type: "boolean", Default: true},
		},
	},
	{
		Type:        WidgetText,
		Name:        "Text Block",
		Description: "Rich text content",
		Icon:        "type",
		Category:    "content",
		Defaults: map[string]any{
			"title":   "",
			"content": "",
			"align":   "left",
		},
		ConfigSpec: []ConfigField{
			{Key: "title", Label: "Title", Type: "text", Placeholder: "Section title"},
			{Key: "content", Label: "Content", Type: "textarea"},
			{Key: "align", Label: "Alignment", Type: "select", Default: "left", Options: []any{"left", "center", "right"}},
		},
	},
	{
		Type:        WidgetImage,
		Name:        "Image",
		Description: "Single image display",
		Icon:        "image",
		Category:    "content",
		Defaults: map[string]any{
			"src":     "",
			"alt":     "",
			"link":    "",
			"caption": "",
		},
		ConfigSpec: []ConfigField{
			{Key: "src", Label: "Image", Type: "image", Required: true},
			{Key: "alt", Label: "Alt Text", Type: "text"},
			{Key: "link", Label: "Link URL", Type: "text"},
			{Key: "caption", Label: "Caption", Type: "text"},
		},
	},
	{
		Type:        WidgetSpacer,
		Name:        "Spacer",
		Description: "Vertical spacing",
		Icon:        "arrows-vertical",
		Category:    "layout",
		Defaults: map[string]any{
			"height": 40,
		},
		ConfigSpec: []ConfigField{
			{Key: "height", Label: "Height (px)", Type: "number", Default: 40, Min: intPtr(10), Max: intPtr(200)},
		},
	},
	{
		Type:        WidgetDivider,
		Name:        "Divider",
		Description: "Horizontal line separator",
		Icon:        "minus",
		Category:    "layout",
		Defaults: map[string]any{
			"style": "solid",
			"width": "100%",
		},
		ConfigSpec: []ConfigField{
			{Key: "style", Label: "Style", Type: "select", Default: "solid", Options: []any{"solid", "dashed", "dotted"}},
			{Key: "width", Label: "Width", Type: "select", Default: "100%", Options: []any{"50%", "75%", "100%"}},
		},
	},
	{
		Type:        WidgetDJGrid,
		Name:        "DJ Grid",
		Description: "Display station DJs",
		Icon:        "users",
		Category:    "content",
		Defaults: map[string]any{
			"columns":   3,
			"showBio":   true,
			"showOnAir": true,
		},
		ConfigSpec: []ConfigField{
			{Key: "columns", Label: "Columns", Type: "number", Default: 3, Min: intPtr(1), Max: intPtr(6)},
			{Key: "showBio", Label: "Show Bio", Type: "boolean", Default: true},
			{Key: "showOnAir", Label: "Show On-Air Status", Type: "boolean", Default: true},
		},
	},
	{
		Type:        WidgetUpcoming,
		Name:        "Upcoming Shows",
		Description: "Shows coming up soon",
		Icon:        "clock",
		Category:    "core",
		Defaults: map[string]any{
			"count":    5,
			"showHost": true,
			"showTime": true,
		},
		ConfigSpec: []ConfigField{
			{Key: "count", Label: "Number of Shows", Type: "number", Default: 5, Min: intPtr(1), Max: intPtr(20)},
			{Key: "showHost", Label: "Show Host", Type: "boolean", Default: true},
			{Key: "showTime", Label: "Show Time", Type: "boolean", Default: true},
		},
	},
	{
		Type:        WidgetGallery,
		Name:        "Image Gallery",
		Description: "Photo gallery grid",
		Icon:        "grid",
		Category:    "content",
		Defaults: map[string]any{
			"columns":  3,
			"lightbox": true,
			"captions": true,
			"images":   []any{},
		},
		ConfigSpec: []ConfigField{
			{Key: "columns", Label: "Columns", Type: "number", Default: 3, Min: intPtr(2), Max: intPtr(6)},
			{Key: "lightbox", Label: "Enable Lightbox", Type: "boolean", Default: true},
			{Key: "captions", Label: "Show Captions", Type: "boolean", Default: true},
		},
	},
	{
		Type:        WidgetVideo,
		Name:        "Video",
		Description: "Embedded video player",
		Icon:        "video",
		Category:    "content",
		Defaults: map[string]any{
			"url":      "",
			"autoplay": false,
			"muted":    true,
		},
		ConfigSpec: []ConfigField{
			{Key: "url", Label: "Video URL", Type: "text", Required: true, Placeholder: "YouTube or Vimeo URL"},
			{Key: "autoplay", Label: "Autoplay", Type: "boolean", Default: false},
			{Key: "muted", Label: "Muted", Type: "boolean", Default: true},
		},
	},
	{
		Type:        WidgetCTA,
		Name:        "Call to Action",
		Description: "Promotional banner with button",
		Icon:        "megaphone",
		Category:    "content",
		Defaults: map[string]any{
			"headline":   "",
			"subtext":    "",
			"buttonText": "Learn More",
			"buttonUrl":  "",
			"background": "",
		},
		ConfigSpec: []ConfigField{
			{Key: "headline", Label: "Headline", Type: "text", Required: true},
			{Key: "subtext", Label: "Subtext", Type: "text"},
			{Key: "buttonText", Label: "Button Text", Type: "text", Default: "Learn More"},
			{Key: "buttonUrl", Label: "Button URL", Type: "text", Required: true},
			{Key: "background", Label: "Background", Type: "color"},
		},
	},
	{
		Type:        WidgetContact,
		Name:        "Contact",
		Description: "Contact information and form",
		Icon:        "mail",
		Category:    "content",
		Defaults: map[string]any{
			"showForm": true,
			"showMap":  false,
			"email":    "",
			"phone":    "",
			"address":  "",
		},
		ConfigSpec: []ConfigField{
			{Key: "showForm", Label: "Show Contact Form", Type: "boolean", Default: true},
			{Key: "showMap", Label: "Show Map", Type: "boolean", Default: false},
			{Key: "email", Label: "Email", Type: "text"},
			{Key: "phone", Label: "Phone", Type: "text"},
			{Key: "address", Label: "Address", Type: "textarea"},
		},
	},
	{
		Type:        WidgetNewsletter,
		Name:        "Newsletter",
		Description: "Email signup form",
		Icon:        "mail-plus",
		Category:    "content",
		Defaults: map[string]any{
			"title":       "Stay Updated",
			"placeholder": "Enter your email",
			"buttonText":  "Subscribe",
		},
		ConfigSpec: []ConfigField{
			{Key: "title", Label: "Title", Type: "text", Default: "Stay Updated"},
			{Key: "placeholder", Label: "Placeholder", Type: "text", Default: "Enter your email"},
			{Key: "buttonText", Label: "Button Text", Type: "text", Default: "Subscribe"},
		},
	},
	{
		Type:        WidgetCustomHTML,
		Name:        "Custom HTML",
		Description: "Custom HTML content (admin only)",
		Icon:        "code",
		Category:    "advanced",
		Defaults: map[string]any{
			"code": "",
		},
		ConfigSpec: []ConfigField{
			{Key: "code", Label: "HTML Code", Type: "code"},
		},
	},
}

// GetWidgetDefinition returns the definition for a widget type.
func GetWidgetDefinition(widgetType WidgetType) *WidgetDefinition {
	for _, def := range WidgetRegistry {
		if def.Type == widgetType {
			return &def
		}
	}
	return nil
}

// GetWidgetDefaults returns the default config for a widget type.
func GetWidgetDefaults(widgetType WidgetType) map[string]any {
	def := GetWidgetDefinition(widgetType)
	if def == nil {
		return make(map[string]any)
	}
	return def.Defaults
}

// GetWidgetsByCategory returns widgets grouped by category.
func GetWidgetsByCategory() map[string][]WidgetDefinition {
	result := make(map[string][]WidgetDefinition)
	for _, def := range WidgetRegistry {
		result[def.Category] = append(result[def.Category], def)
	}
	return result
}

// ValidateWidgetConfig validates a widget configuration.
func ValidateWidgetConfig(widget WidgetConfig) error {
	def := GetWidgetDefinition(widget.Type)
	if def == nil {
		return ErrInvalidWidgetType
	}
	// Additional validation could be added here
	return nil
}

// ErrInvalidWidgetType is returned when a widget type is not recognized.
var ErrInvalidWidgetType = errInvalidWidgetType{}

type errInvalidWidgetType struct{}

func (errInvalidWidgetType) Error() string { return "invalid widget type" }

// intPtr returns a pointer to an int value.
func intPtr(v int) *int {
	return &v
}

// WidgetRenderData holds data passed to widget templates.
type WidgetRenderData struct {
	Widget  WidgetConfig
	Station *models.Station
	Config  map[string]any
	Theme   *Theme
}
