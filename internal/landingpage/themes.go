/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package landingpage

// Theme represents a landing page theme with default configuration.
type Theme struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Preview     string          `json:"preview"` // Preview image URL
	Colors      ThemeColors     `json:"colors"`
	Typography  ThemeTypography `json:"typography"`
	Defaults    map[string]any  `json:"defaults"`
}

// ThemeColors defines the color palette for a theme.
type ThemeColors struct {
	Primary    string `json:"primary"`
	Secondary  string `json:"secondary"`
	Background string `json:"background"`
	Surface    string `json:"surface"`
	Text       string `json:"text"`
	TextMuted  string `json:"text_muted"`
	Border     string `json:"border"`
	Accent     string `json:"accent"`
}

// ThemeTypography defines font settings for a theme.
type ThemeTypography struct {
	HeadingFont string `json:"heading_font"`
	BodyFont    string `json:"body_font"`
	BaseSize    string `json:"base_size"`
}

// BuiltInThemes contains all available themes.
var BuiltInThemes = []Theme{
	{
		ID:          "default",
		Name:        "Default",
		Description: "Clean, professional look with balanced colors",
		Preview:     "/static/images/themes/default.png",
		Colors: ThemeColors{
			Primary:    "#3B82F6",
			Secondary:  "#10B981",
			Background: "#FFFFFF",
			Surface:    "#F9FAFB",
			Text:       "#1F2937",
			TextMuted:  "#6B7280",
			Border:     "#E5E7EB",
			Accent:     "#F59E0B",
		},
		Typography: ThemeTypography{
			HeadingFont: "Inter",
			BodyFont:    "Inter",
			BaseSize:    "16px",
		},
		Defaults: defaultThemeConfig("default"),
	},
	{
		ID:          "dark",
		Name:        "Dark Mode",
		Description: "Modern dark theme for a sleek look",
		Preview:     "/static/images/themes/dark.png",
		Colors: ThemeColors{
			Primary:    "#6366F1",
			Secondary:  "#10B981",
			Background: "#0F172A",
			Surface:    "#1E293B",
			Text:       "#F1F5F9",
			TextMuted:  "#94A3B8",
			Border:     "#334155",
			Accent:     "#F59E0B",
		},
		Typography: ThemeTypography{
			HeadingFont: "Inter",
			BodyFont:    "Inter",
			BaseSize:    "16px",
		},
		Defaults: defaultThemeConfig("dark"),
	},
	{
		ID:          "light",
		Name:        "Light & Airy",
		Description: "Bright, minimal design with lots of whitespace",
		Preview:     "/static/images/themes/light.png",
		Colors: ThemeColors{
			Primary:    "#0EA5E9",
			Secondary:  "#14B8A6",
			Background: "#FFFFFF",
			Surface:    "#F0F9FF",
			Text:       "#0C4A6E",
			TextMuted:  "#64748B",
			Border:     "#E0F2FE",
			Accent:     "#EC4899",
		},
		Typography: ThemeTypography{
			HeadingFont: "Poppins",
			BodyFont:    "Open Sans",
			BaseSize:    "16px",
		},
		Defaults: defaultThemeConfig("light"),
	},
	{
		ID:          "bold",
		Name:        "Bold",
		Description: "Strong colors and high contrast for impact",
		Preview:     "/static/images/themes/bold.png",
		Colors: ThemeColors{
			Primary:    "#EF4444",
			Secondary:  "#FBBF24",
			Background: "#18181B",
			Surface:    "#27272A",
			Text:       "#FFFFFF",
			TextMuted:  "#A1A1AA",
			Border:     "#3F3F46",
			Accent:     "#22D3EE",
		},
		Typography: ThemeTypography{
			HeadingFont: "Oswald",
			BodyFont:    "Roboto",
			BaseSize:    "16px",
		},
		Defaults: defaultThemeConfig("bold"),
	},
	{
		ID:          "vintage",
		Name:        "Vintage Radio",
		Description: "Warm tones and retro styling",
		Preview:     "/static/images/themes/vintage.png",
		Colors: ThemeColors{
			Primary:    "#B45309",
			Secondary:  "#78716C",
			Background: "#FEF3C7",
			Surface:    "#FFFBEB",
			Text:       "#451A03",
			TextMuted:  "#78350F",
			Border:     "#FDE68A",
			Accent:     "#DC2626",
		},
		Typography: ThemeTypography{
			HeadingFont: "Playfair Display",
			BodyFont:    "Lora",
			BaseSize:    "17px",
		},
		Defaults: defaultThemeConfig("vintage"),
	},
	{
		ID:          "neon",
		Name:        "Neon Nights",
		Description: "Dark theme with vibrant neon accents",
		Preview:     "/static/images/themes/neon.png",
		Colors: ThemeColors{
			Primary:    "#A855F7",
			Secondary:  "#06B6D4",
			Background: "#020617",
			Surface:    "#0F172A",
			Text:       "#F8FAFC",
			TextMuted:  "#64748B",
			Border:     "#1E293B",
			Accent:     "#F0ABFC",
		},
		Typography: ThemeTypography{
			HeadingFont: "Orbitron",
			BodyFont:    "Exo 2",
			BaseSize:    "16px",
		},
		Defaults: defaultThemeConfig("neon"),
	},
	{
		ID:          "community",
		Name:        "Community Radio",
		Description: "Friendly, approachable design for community stations",
		Preview:     "/static/images/themes/community.png",
		Colors: ThemeColors{
			Primary:    "#059669",
			Secondary:  "#2563EB",
			Background: "#F0FDF4",
			Surface:    "#FFFFFF",
			Text:       "#064E3B",
			TextMuted:  "#047857",
			Border:     "#D1FAE5",
			Accent:     "#F97316",
		},
		Typography: ThemeTypography{
			HeadingFont: "Nunito",
			BodyFont:    "Nunito",
			BaseSize:    "16px",
		},
		Defaults: defaultThemeConfig("community"),
	},
}

// GetTheme returns a theme by ID.
func GetTheme(id string) *Theme {
	for _, theme := range BuiltInThemes {
		if theme.ID == id {
			return &theme
		}
	}
	return nil
}

// GetThemeDefaults returns the default configuration for a theme.
func GetThemeDefaults(id string) map[string]any {
	theme := GetTheme(id)
	if theme == nil {
		theme = &BuiltInThemes[0] // Fall back to default
	}
	return theme.Defaults
}

// defaultThemeConfig creates a default landing page configuration.
func defaultThemeConfig(themeID string) map[string]any {
	return map[string]any{
		"version": 1,
		"theme":   themeID,
		"header": map[string]any{
			"showLogo":        true,
			"showStationName": true,
			"showTagline":     true,
			"tagline":         "Your Community Radio Station",
			"socialLinks":     []any{},
		},
		"hero": map[string]any{
			"enabled":         true,
			"height":          "large",
			"overlayOpacity":  0.5,
			"showPlayer":      true,
			"showTitle":       true,
			"showDescription": true,
		},
		"content": map[string]any{
			"layout": "single",
			"widgets": []map[string]any{
				{
					"id":     "w1",
					"type":   "schedule",
					"config": map[string]any{"showCount": 5, "showTime": true, "showHost": true},
				},
				{
					"id":     "w2",
					"type":   "recent-tracks",
					"config": map[string]any{"count": 10, "showAlbum": true, "showTime": true},
				},
			},
		},
		"footer": map[string]any{
			"showCopyright":   true,
			"copyrightText":   "",
			"showSocialLinks": true,
			"links":           []any{},
		},
		"seo": map[string]any{
			"title":       "",
			"description": "",
			"ogImage":     "",
			"noIndex":     false,
		},
	}
}

// MergeConfig merges user config with theme defaults.
func MergeConfig(themeID string, userConfig map[string]any) map[string]any {
	defaults := GetThemeDefaults(themeID)
	if userConfig == nil {
		return defaults
	}

	// Deep merge user config into defaults
	result := deepMerge(defaults, userConfig)
	return result
}

// deepMerge recursively merges two maps.
func deepMerge(base, override map[string]any) map[string]any {
	result := make(map[string]any)

	// Copy all from base
	for k, v := range base {
		result[k] = v
	}

	// Override/merge from override
	for k, v := range override {
		if baseMap, ok := base[k].(map[string]any); ok {
			if overrideMap, ok := v.(map[string]any); ok {
				result[k] = deepMerge(baseMap, overrideMap)
				continue
			}
		}
		result[k] = v
	}

	return result
}
