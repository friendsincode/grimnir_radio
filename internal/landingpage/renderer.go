/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package landingpage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"gorm.io/gorm"
)

// Renderer handles rendering landing pages and widgets.
type Renderer struct {
	db        *gorm.DB
	templates *template.Template
}

// NewRenderer creates a new landing page renderer.
func NewRenderer(db *gorm.DB) (*Renderer, error) {
	funcMap := template.FuncMap{
		"safeHTML":       func(s string) template.HTML { return template.HTML(s) },
		"safeCSS":        func(s string) template.CSS { return template.CSS(s) },
		"safeJS":         func(s string) template.JS { return template.JS(s) },
		"safeURL":        func(s string) template.URL { return template.URL(s) },
		"jsonMarshal":    jsonMarshal,
		"configVal":      configVal,
		"configBool":     configBool,
		"configInt":      configInt,
		"configString":   configString,
		"formatTime":     formatTime,
		"formatDuration": formatDuration,
		"truncate":       truncate,
		"lower":          strings.ToLower,
		"upper":          strings.ToUpper,
	}

	tmpl := template.New("").Funcs(funcMap)

	// Parse embedded widget templates
	for widgetType, tmplContent := range widgetTemplates {
		_, err := tmpl.New("widget_" + string(widgetType)).Parse(tmplContent)
		if err != nil {
			return nil, fmt.Errorf("parse widget template %s: %w", widgetType, err)
		}
	}

	// Parse page templates
	for name, tmplContent := range pageTemplates {
		_, err := tmpl.New(name).Parse(tmplContent)
		if err != nil {
			return nil, fmt.Errorf("parse page template %s: %w", name, err)
		}
	}

	return &Renderer{
		db:        db,
		templates: tmpl,
	}, nil
}

// RenderWidget renders a single widget to HTML.
func (r *Renderer) RenderWidget(ctx context.Context, station *models.Station, widget WidgetConfig, theme *Theme) (template.HTML, error) {
	// Merge widget config with defaults
	def := GetWidgetDefinition(widget.Type)
	if def == nil {
		return "", ErrInvalidWidgetType
	}

	config := make(map[string]any)
	for k, v := range def.Defaults {
		config[k] = v
	}
	for k, v := range widget.Config {
		config[k] = v
	}

	// Fetch any dynamic data the widget needs
	data := WidgetRenderData{
		Widget:  widget,
		Station: station,
		Config:  config,
		Theme:   theme,
	}

	// Add dynamic data based on widget type
	switch widget.Type {
	case WidgetSchedule, WidgetUpcoming:
		shows, err := r.fetchUpcomingShows(ctx, station.ID, configInt(config, "showCount", 5))
		if err != nil {
			return "", fmt.Errorf("fetch shows: %w", err)
		}
		config["shows"] = shows

	case WidgetRecentTracks:
		tracks, err := r.fetchRecentTracks(ctx, station.ID, configInt(config, "count", 10))
		if err != nil {
			return "", fmt.Errorf("fetch tracks: %w", err)
		}
		config["tracks"] = tracks

	case WidgetDJGrid:
		djs, err := r.fetchStationDJs(ctx, station.ID)
		if err != nil {
			return "", fmt.Errorf("fetch djs: %w", err)
		}
		config["djs"] = djs
	}

	// Render the widget template
	templateName := "widget_" + string(widget.Type)
	var buf bytes.Buffer
	if err := r.templates.ExecuteTemplate(&buf, templateName, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", templateName, err)
	}

	return template.HTML(buf.String()), nil
}

// RenderPage renders a complete landing page to HTML.
func (r *Renderer) RenderPage(ctx context.Context, station *models.Station, config map[string]any, theme *Theme, customCSS, customHead string) (template.HTML, error) {
	// Extract widgets from config
	widgets := []WidgetConfig{}
	if content, ok := config["content"].(map[string]any); ok {
		if widgetList, ok := content["widgets"].([]any); ok {
			for _, w := range widgetList {
				if wMap, ok := w.(map[string]any); ok {
					widget := WidgetConfig{
						ID:     configString(wMap, "id", ""),
						Type:   WidgetType(configString(wMap, "type", "")),
						Config: make(map[string]any),
					}
					if cfg, ok := wMap["config"].(map[string]any); ok {
						widget.Config = cfg
					}
					widgets = append(widgets, widget)
				}
			}
		}
	}

	// Render each widget
	var renderedWidgets []template.HTML
	for _, widget := range widgets {
		html, err := r.RenderWidget(ctx, station, widget, theme)
		if err != nil {
			// Log error but continue with other widgets
			renderedWidgets = append(renderedWidgets, template.HTML(fmt.Sprintf("<!-- Widget error: %s -->", err)))
			continue
		}
		renderedWidgets = append(renderedWidgets, html)
	}

	// Prepare page data
	pageData := map[string]any{
		"station":    station,
		"config":     config,
		"theme":      theme,
		"widgets":    renderedWidgets,
		"customCSS":  template.CSS(customCSS),
		"customHead": template.HTML(customHead),
	}

	// Render the full page
	var buf bytes.Buffer
	if err := r.templates.ExecuteTemplate(&buf, "landing_page", pageData); err != nil {
		return "", fmt.Errorf("execute landing page template: %w", err)
	}

	return template.HTML(buf.String()), nil
}

// fetchUpcomingShows retrieves upcoming shows for a station.
func (r *Renderer) fetchUpcomingShows(ctx context.Context, stationID string, count int) ([]map[string]any, error) {
	var instances []models.ShowInstance
	now := time.Now()
	err := r.db.WithContext(ctx).
		Preload("Show").
		Preload("Show.Host").
		Where("station_id = ? AND starts_at > ? AND status != ?", stationID, now, "cancelled").
		Order("starts_at ASC").
		Limit(count).
		Find(&instances).Error
	if err != nil {
		return nil, err
	}

	result := make([]map[string]any, 0, len(instances))
	for _, inst := range instances {
		hostName := ""
		if inst.Show != nil && inst.Show.Host != nil {
			hostName = inst.Show.Host.Email // Use email as display name for now
		}
		item := map[string]any{
			"id":        inst.ID,
			"title":     inst.Show.Name,
			"host":      hostName,
			"starts_at": inst.StartsAt,
			"ends_at":   inst.EndsAt,
		}
		result = append(result, item)
	}
	return result, nil
}

// fetchRecentTracks retrieves recently played tracks for a station.
func (r *Renderer) fetchRecentTracks(ctx context.Context, stationID string, count int) ([]map[string]any, error) {
	var history []models.PlayHistory
	err := r.db.WithContext(ctx).
		Where("station_id = ?", stationID).
		Order("started_at DESC").
		Limit(count).
		Find(&history).Error
	if err != nil {
		return nil, err
	}

	result := make([]map[string]any, 0, len(history))
	for _, h := range history {
		item := map[string]any{
			"id":         h.ID,
			"artist":     h.Artist,
			"title":      h.Title,
			"album":      h.Album,
			"started_at": h.StartedAt,
		}
		result = append(result, item)
	}
	return result, nil
}

// fetchStationDJs retrieves DJs for a station.
func (r *Renderer) fetchStationDJs(ctx context.Context, stationID string) ([]map[string]any, error) {
	var stationUsers []models.StationUser
	err := r.db.WithContext(ctx).
		Where("station_id = ? AND role IN ?", stationID, []string{"dj", "manager", "admin", "owner"}).
		Find(&stationUsers).Error
	if err != nil {
		return nil, err
	}

	if len(stationUsers) == 0 {
		return []map[string]any{}, nil
	}

	// Get user details
	userIDs := make([]string, len(stationUsers))
	for i, su := range stationUsers {
		userIDs[i] = su.UserID
	}

	var users []models.User
	if err := r.db.WithContext(ctx).Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		return nil, err
	}

	userMap := make(map[string]models.User)
	for _, u := range users {
		userMap[u.ID] = u
	}

	result := make([]map[string]any, 0, len(stationUsers))
	for _, su := range stationUsers {
		if user, ok := userMap[su.UserID]; ok {
			item := map[string]any{
				"id":    user.ID,
				"email": user.Email,
				"role":  su.Role,
			}
			result = append(result, item)
		}
	}
	return result, nil
}

// Helper template functions

func jsonMarshal(v any) template.JS {
	if v == nil {
		return template.JS("null")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return template.JS("null")
	}
	return template.JS(b)
}

func configVal(config map[string]any, key string, def any) any {
	if v, ok := config[key]; ok {
		return v
	}
	return def
}

func configBool(config map[string]any, key string, def bool) bool {
	if v, ok := config[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

func configInt(config map[string]any, key string, def int) int {
	if v, ok := config[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return def
}

func configString(config map[string]any, key string, def string) string {
	if v, ok := config[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

func formatTime(t time.Time) string {
	return t.Format("3:04 PM")
}

func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

// Widget template content
var widgetTemplates = map[WidgetType]string{
	WidgetPlayer: `
<div class="widget widget-player" id="{{.Widget.ID}}">
	<div class="player-container size-{{configString .Config "size" "large"}}">
		{{if configBool .Config "showArtwork" true}}
		<div class="player-artwork">
			<img src="/api/v1/analytics/now-playing/artwork?station_id={{.Station.ID}}" alt="Now Playing" onerror="this.src='/static/images/default-album.png'">
		</div>
		{{end}}
		<div class="player-controls">
			<button class="play-button" onclick="togglePlay()">
				<svg class="icon-play"><use href="#icon-play"></use></svg>
				<svg class="icon-pause" style="display:none"><use href="#icon-pause"></use></svg>
			</button>
			{{if configBool .Config "showVolumeSlider" true}}
			<input type="range" class="volume-slider" min="0" max="100" value="80">
			{{end}}
		</div>
		{{if configBool .Config "showNowPlaying" true}}
		<div class="now-playing">
			<span class="track-title" id="np-title">Loading...</span>
			<span class="track-artist" id="np-artist"></span>
		</div>
		{{end}}
	</div>
</div>`,

	WidgetSchedule: `
<div class="widget widget-schedule" id="{{.Widget.ID}}">
	<h3 class="widget-title">Today's Schedule</h3>
	<ul class="schedule-list">
	{{range .Config.shows}}
		<li class="schedule-item">
			{{if $.Config.showTime}}<time>{{formatTime .starts_at}}</time>{{end}}
			<span class="show-title">{{.title}}</span>
			{{if $.Config.showHost}}<span class="show-host">with {{.host}}</span>{{end}}
		</li>
	{{else}}
		<li class="schedule-item empty">No shows scheduled</li>
	{{end}}
	</ul>
</div>`,

	WidgetRecentTracks: `
<div class="widget widget-recent-tracks" id="{{.Widget.ID}}">
	<h3 class="widget-title">Recently Played</h3>
	<ul class="tracks-list">
	{{range .Config.tracks}}
		<li class="track-item">
			<span class="track-title">{{.title}}</span>
			<span class="track-artist">{{.artist}}</span>
			{{if $.Config.showAlbum}}<span class="track-album">{{.album}}</span>{{end}}
			{{if $.Config.showTime}}<time>{{formatTime .started_at}}</time>{{end}}
		</li>
	{{else}}
		<li class="track-item empty">No tracks played yet</li>
	{{end}}
	</ul>
</div>`,

	WidgetText: `
<div class="widget widget-text align-{{configString .Config "align" "left"}}" id="{{.Widget.ID}}">
	{{if .Config.title}}<h3 class="widget-title">{{.Config.title}}</h3>{{end}}
	<div class="text-content">{{safeHTML (configString .Config "content" "")}}</div>
</div>`,

	WidgetImage: `
<div class="widget widget-image" id="{{.Widget.ID}}">
	{{if .Config.link}}<a href="{{.Config.link}}">{{end}}
	<img src="{{.Config.src}}" alt="{{.Config.alt}}" loading="lazy">
	{{if .Config.link}}</a>{{end}}
	{{if .Config.caption}}<figcaption>{{.Config.caption}}</figcaption>{{end}}
</div>`,

	WidgetSpacer: `
<div class="widget widget-spacer" id="{{.Widget.ID}}" style="height: {{configInt .Config "height" 40}}px"></div>`,

	WidgetDivider: `
<div class="widget widget-divider" id="{{.Widget.ID}}">
	<hr class="divider style-{{configString .Config "style" "solid"}}" style="width: {{configString .Config "width" "100%"}}">
</div>`,

	WidgetDJGrid: `
<div class="widget widget-dj-grid columns-{{configInt .Config "columns" 3}}" id="{{.Widget.ID}}">
	<h3 class="widget-title">Our DJs</h3>
	<div class="dj-grid">
	{{range .Config.djs}}
		<div class="dj-card">
			<div class="dj-avatar"></div>
			<span class="dj-name">{{.email}}</span>
			<span class="dj-role">{{.role}}</span>
		</div>
	{{else}}
		<p class="empty">No DJs to display</p>
	{{end}}
	</div>
</div>`,

	WidgetUpcoming: `
<div class="widget widget-upcoming" id="{{.Widget.ID}}">
	<h3 class="widget-title">Coming Up</h3>
	<ul class="upcoming-list">
	{{range .Config.shows}}
		<li class="upcoming-item">
			{{if $.Config.showTime}}<time>{{formatTime .starts_at}}</time>{{end}}
			<span class="show-title">{{.title}}</span>
			{{if $.Config.showHost}}<span class="show-host">{{.host}}</span>{{end}}
		</li>
	{{else}}
		<li class="upcoming-item empty">No upcoming shows</li>
	{{end}}
	</ul>
</div>`,

	WidgetGallery: `
<div class="widget widget-gallery columns-{{configInt .Config "columns" 3}}" id="{{.Widget.ID}}">
	<div class="gallery-grid">
	{{range .Config.images}}
		<figure class="gallery-item">
			<img src="{{.src}}" alt="{{.alt}}" loading="lazy" {{if $.Config.lightbox}}onclick="openLightbox(this)"{{end}}>
			{{if $.Config.captions}}{{if .caption}}<figcaption>{{.caption}}</figcaption>{{end}}{{end}}
		</figure>
	{{end}}
	</div>
</div>`,

	WidgetVideo: `
<div class="widget widget-video" id="{{.Widget.ID}}">
	<div class="video-wrapper">
		<iframe src="{{.Config.url}}" frameborder="0" allowfullscreen {{if .Config.autoplay}}allow="autoplay"{{end}}></iframe>
	</div>
</div>`,

	WidgetCTA: `
<div class="widget widget-cta" id="{{.Widget.ID}}" {{if .Config.background}}style="background-color: {{.Config.background}}"{{end}}>
	<h2 class="cta-headline">{{.Config.headline}}</h2>
	{{if .Config.subtext}}<p class="cta-subtext">{{.Config.subtext}}</p>{{end}}
	<a href="{{.Config.buttonUrl}}" class="cta-button">{{configString .Config "buttonText" "Learn More"}}</a>
</div>`,

	WidgetContact: `
<div class="widget widget-contact" id="{{.Widget.ID}}">
	<h3 class="widget-title">Contact Us</h3>
	<div class="contact-info">
		{{if .Config.email}}<p><strong>Email:</strong> <a href="mailto:{{.Config.email}}">{{.Config.email}}</a></p>{{end}}
		{{if .Config.phone}}<p><strong>Phone:</strong> {{.Config.phone}}</p>{{end}}
		{{if .Config.address}}<p><strong>Address:</strong> {{.Config.address}}</p>{{end}}
	</div>
	{{if configBool .Config "showForm" true}}
	<form class="contact-form" method="post" action="/contact">
		<input type="text" name="name" placeholder="Your Name" required>
		<input type="email" name="email" placeholder="Your Email" required>
		<textarea name="message" placeholder="Your Message" required></textarea>
		<button type="submit">Send Message</button>
	</form>
	{{end}}
</div>`,

	WidgetNewsletter: `
<div class="widget widget-newsletter" id="{{.Widget.ID}}">
	{{if .Config.title}}<h3 class="widget-title">{{.Config.title}}</h3>{{end}}
	<form class="newsletter-form" method="post" action="/newsletter/subscribe">
		<input type="email" name="email" placeholder="{{configString .Config "placeholder" "Enter your email"}}" required>
		<button type="submit">{{configString .Config "buttonText" "Subscribe"}}</button>
	</form>
</div>`,

	WidgetCustomHTML: `
<div class="widget widget-custom-html" id="{{.Widget.ID}}">
	{{safeHTML (configString .Config "code" "")}}
</div>`,

	WidgetSocialFeed: `
<div class="widget widget-social-feed" id="{{.Widget.ID}}">
	<h3 class="widget-title">Follow Us</h3>
	<div class="social-embed" data-platform="{{.Config.platform}}" data-handle="{{.Config.handle}}">
		<!-- Social feed loaded via JavaScript -->
	</div>
</div>`,
}

// Page template content
var pageTemplates = map[string]string{
	"landing_page": `
<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	{{with .config.seo}}
	<title>{{if .title}}{{.title}}{{else}}{{$.station.Name}}{{end}}</title>
	{{if .description}}<meta name="description" content="{{.description}}">{{end}}
	{{if .ogImage}}<meta property="og:image" content="{{.ogImage}}">{{end}}
	{{if .noIndex}}<meta name="robots" content="noindex">{{end}}
	{{end}}
	<link rel="stylesheet" href="/static/css/landing-page.css">
	{{if .customCSS}}<style>{{.customCSS}}</style>{{end}}
	{{if .customHead}}{{.customHead}}{{end}}
</head>
<body class="theme-{{.theme.ID}}">
	<header class="landing-header">
		{{with .config.header}}
		{{if .showLogo}}<img src="/landing-assets/by-type/logo?station_id={{$.station.ID}}" alt="{{$.station.Name}}" class="station-logo" onerror="this.style.display='none'">{{end}}
		{{if .showStationName}}<h1 class="station-name">{{$.station.Name}}</h1>{{end}}
		{{if .tagline}}<p class="station-tagline">{{.tagline}}</p>{{end}}
		{{end}}
	</header>

	{{with .config.hero}}
	{{if .enabled}}
	<section class="landing-hero hero-{{.height}}">
		{{if .showPlayer}}
		<div class="hero-player">
			<!-- Player rendered here -->
		</div>
		{{end}}
	</section>
	{{end}}
	{{end}}

	<main class="landing-content">
		{{range .widgets}}
		{{.}}
		{{end}}
	</main>

	<footer class="landing-footer">
		{{with .config.footer}}
		{{if .showSocialLinks}}
		<div class="social-links">
			<!-- Social links -->
		</div>
		{{end}}
		{{if .showCopyright}}
		<p class="copyright">{{if .copyrightText}}{{.copyrightText}}{{else}}&copy; {{$.station.Name}}{{end}}</p>
		{{end}}
		{{end}}
	</footer>

	<script src="/static/js/landing-page.js"></script>
</body>
</html>`,
}
