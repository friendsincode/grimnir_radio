/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/media"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/version"
)

// Theme represents a UI theme
type Theme string

const (
	ThemeDAWDark   Theme = "daw-dark"
	ThemeCleanLight Theme = "clean-light"
	ThemeBroadcast  Theme = "broadcast"
	ThemeClassic    Theme = "classic"
)

// Handler provides web UI endpoints with server-rendered templates.
type Handler struct {
	db               *gorm.DB
	logger           zerolog.Logger
	jwtSecret        []byte
	mediaRoot        string                         // Root directory for media files
	mediaService     *media.Service                 // Media storage service
	icecastURL       string                         // Internal Icecast URL for stream proxy
	icecastPublicURL string                         // Public Icecast URL for browser playback
	templates        map[string]*template.Template // Each page gets its own template set
	partials         *template.Template            // Shared partials
	updateChecker    *version.Checker              // Checks for new versions
	migrationService *migration.Service            // Migration job management
	eventBus         *events.Bus                   // Event bus for real-time updates

	// WebRTC ICE server config (passed to client)
	webrtcSTUNURL      string
	webrtcTURNURL      string
	webrtcTURNUsername string
	webrtcTURNPassword string
}

// PageData holds common data passed to all templates.
type PageData struct {
	Title       string
	Theme       Theme
	User        *models.User
	Station     *models.Station
	Stations    []models.Station
	Flash       *FlashMessage
	CurrentPath string
	CSRFToken   string
	WSToken     string              // Auth token for WebSocket connections (non-HttpOnly)
	Data        any
	Version     string              // Current application version
	UpdateInfo  *version.UpdateInfo // Available update info (nil if no update)

	// WebRTC ICE server config for client
	WebRTCSTUNURL      string
	WebRTCTURNURL      string
	WebRTCTURNUsername string
	WebRTCTURNPassword string
}

// FlashMessage for toast notifications
type FlashMessage struct {
	Type    string // success, error, warning, info
	Message string
}

// WebRTCConfig holds WebRTC ICE server configuration for client-side.
type WebRTCConfig struct {
	STUNURL      string
	TURNURL      string
	TURNUsername string
	TURNPassword string
}

// NewHandler creates a new web handler.
func NewHandler(db *gorm.DB, jwtSecret []byte, mediaRoot string, mediaService *media.Service, icecastURL string, icecastPublicURL string, webrtcCfg WebRTCConfig, eventBus *events.Bus, logger zerolog.Logger) (*Handler, error) {
	// Create migration service
	migrationService := migration.NewService(db, eventBus, logger)

	// Register importers
	migrationService.RegisterImporter(migration.SourceTypeAzuraCast, migration.NewAzuraCastImporter(db, mediaService, logger))
	migrationService.RegisterImporter(migration.SourceTypeLibreTime, migration.NewLibreTimeImporter(db, mediaService, logger))

	h := &Handler{
		db:                 db,
		logger:             logger,
		jwtSecret:          jwtSecret,
		mediaRoot:          mediaRoot,
		mediaService:       mediaService,
		icecastURL:         icecastURL,
		icecastPublicURL:   icecastPublicURL,
		updateChecker:      version.NewChecker(logger),
		migrationService:   migrationService,
		eventBus:           eventBus,
		webrtcSTUNURL:      webrtcCfg.STUNURL,
		webrtcTURNURL:      webrtcCfg.TURNURL,
		webrtcTURNUsername: webrtcCfg.TURNUsername,
		webrtcTURNPassword: webrtcCfg.TURNPassword,
	}

	if err := h.loadTemplates(); err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}

	return h, nil
}

// StartUpdateChecker starts the background version checker.
func (h *Handler) StartUpdateChecker(ctx context.Context) {
	h.updateChecker.Start(ctx)
}

// StopUpdateChecker stops the background version checker.
func (h *Handler) StopUpdateChecker() {
	h.updateChecker.Stop()
}

func (h *Handler) loadTemplates() error {
	funcMap := template.FuncMap{
		"formatTime":     formatTime,
		"timeago":        timeago,
		"formatDuration": formatDuration,
		"formatMs":       formatMs,
		"formatBytes":    formatBytes,
		"truncate":       truncate,
		"lower":          strings.ToLower,
		"upper":          strings.ToUpper,
		"title":          strings.Title,
		"contains":       strings.Contains,
		"hasPrefix":      strings.HasPrefix,
		"hasSuffix":      strings.HasSuffix,
		"join":           strings.Join,
		"split":          strings.Split,
		"dict":           dict,
		"list":           list,
		"safeHTML":       safeHTML,
		"safeJS":         safeJS,
		"safeURL":        safeURL,
		"add":            add,
		"sub":            sub,
		"mul":            mul,
		"div":            div,
		"mod":            mod,
		"eq":             eq,
		"ne":             ne,
		"lt":             lt,
		"le":             le,
		"gt":             gt,
		"ge":             ge,
		"and":            and,
		"or":             or,
		"not":            not,
		"default":        defaultVal,
		"coalesce":       coalesce,
		"ternary":        ternary,
		"jsonMarshal":    jsonMarshal,
		"roleAtLeast":    roleAtLeast,
		"isPlatformAdmin": isPlatformAdmin,
		"isActive":       isActive,
		"iterate":        iterate,
	}

	h.templates = make(map[string]*template.Template)

	// First, collect all layout and partial templates
	var layoutFiles []string
	var partialFiles []string
	var pageFiles []string

	err := fs.WalkDir(TemplateFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}

		if strings.HasPrefix(path, "templates/layouts/") {
			layoutFiles = append(layoutFiles, path)
		} else if strings.HasPrefix(path, "templates/partials/") {
			partialFiles = append(partialFiles, path)
		} else if strings.HasPrefix(path, "templates/pages/") {
			pageFiles = append(pageFiles, path)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Load partials into a shared template set
	h.partials = template.New("").Funcs(funcMap)
	for _, path := range partialFiles {
		content, err := fs.ReadFile(TemplateFS, path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		name := strings.TrimPrefix(path, "templates/")
		name = strings.TrimSuffix(name, ".html")
		if _, err := h.partials.New(name).Parse(string(content)); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		h.logger.Debug().Str("template", name).Msg("loaded partial")
	}

	// For each page template, create its own template set with layouts
	for _, pagePath := range pageFiles {
		// Create a new template set for this page
		tmpl := template.New("").Funcs(funcMap)

		// Parse all layouts first
		for _, layoutPath := range layoutFiles {
			content, err := fs.ReadFile(TemplateFS, layoutPath)
			if err != nil {
				return fmt.Errorf("read %s: %w", layoutPath, err)
			}
			name := strings.TrimPrefix(layoutPath, "templates/")
			name = strings.TrimSuffix(name, ".html")
			if _, err := tmpl.New(name).Parse(string(content)); err != nil {
				return fmt.Errorf("parse %s: %w", layoutPath, err)
			}
		}

		// Parse the page template
		pageContent, err := fs.ReadFile(TemplateFS, pagePath)
		if err != nil {
			return fmt.Errorf("read %s: %w", pagePath, err)
		}
		pageName := strings.TrimPrefix(pagePath, "templates/")
		pageName = strings.TrimSuffix(pageName, ".html")

		if _, err := tmpl.New(pageName).Parse(string(pageContent)); err != nil {
			return fmt.Errorf("parse %s: %w", pagePath, err)
		}

		h.templates[pageName] = tmpl
		h.logger.Debug().Str("template", pageName).Msg("loaded template")
	}

	return nil
}

// Render renders a template with the given data.
func (h *Handler) Render(w http.ResponseWriter, r *http.Request, name string, data PageData) {
	// Set defaults
	if data.Theme == "" {
		data.Theme = ThemeDAWDark
	}
	data.CurrentPath = r.URL.Path
	data.Version = version.Version

	// WebRTC ICE server config for client
	data.WebRTCSTUNURL = h.webrtcSTUNURL
	data.WebRTCTURNURL = h.webrtcTURNURL
	data.WebRTCTURNUsername = h.webrtcTURNUsername
	data.WebRTCTURNPassword = h.webrtcTURNPassword

	// Get user from context if authenticated
	if user, ok := r.Context().Value(ctxKeyUser).(*models.User); ok {
		data.User = user
		// Generate short-lived WS token for JavaScript access
		data.WSToken = h.GenerateWSToken(user)
		if data.WSToken == "" {
			h.logger.Warn().Str("user_id", user.ID).Msg("failed to generate WS token")
		}

		// Only show update info to platform admins
		if user.IsPlatformAdmin() && h.updateChecker != nil {
			info := h.updateChecker.Info()
			if info != nil && info.UpdateAvailable {
				data.UpdateInfo = info
			}
		}
	}

	// Get selected station from context
	if station, ok := r.Context().Value(ctxKeyStation).(*models.Station); ok {
		data.Station = station
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	tmpl, ok := h.templates[name]
	if !ok {
		h.logger.Error().Str("template", name).Msg("template not found")
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}

	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		h.logger.Error().Err(err).Str("template", name).Msg("template render failed")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// RenderPartial renders a partial template (for HTMX responses).
func (h *Handler) RenderPartial(w http.ResponseWriter, r *http.Request, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := h.partials.ExecuteTemplate(w, name, data); err != nil {
		h.logger.Error().Err(err).Str("template", name).Msg("partial render failed")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// staticResponseWriter wraps http.ResponseWriter to force correct MIME types
type staticResponseWriter struct {
	http.ResponseWriter
	contentType string
	wroteHeader bool
}

func (w *staticResponseWriter) WriteHeader(code int) {
	if !w.wroteHeader && w.contentType != "" {
		w.Header().Set("Content-Type", w.contentType)
	}
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *staticResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// StaticHandler returns an http.Handler for static files.
func (h *Handler) StaticHandler() http.Handler {
	fsys, _ := fs.Sub(StaticFS, "static")
	fileServer := http.FileServer(http.FS(fsys))
	return http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Determine correct MIME type for embedded files
		path := r.URL.Path
		var contentType string
		switch {
		case strings.HasSuffix(path, ".css"):
			contentType = "text/css; charset=utf-8"
		case strings.HasSuffix(path, ".js"):
			contentType = "application/javascript; charset=utf-8"
		case strings.HasSuffix(path, ".json"):
			contentType = "application/json; charset=utf-8"
		case strings.HasSuffix(path, ".svg"):
			contentType = "image/svg+xml"
		case strings.HasSuffix(path, ".png"):
			contentType = "image/png"
		case strings.HasSuffix(path, ".ico"):
			contentType = "image/x-icon"
		case strings.HasSuffix(path, ".woff"):
			contentType = "font/woff"
		case strings.HasSuffix(path, ".woff2"):
			contentType = "font/woff2"
		}

		// Wrap response writer to force our Content-Type
		sw := &staticResponseWriter{ResponseWriter: w, contentType: contentType}
		fileServer.ServeHTTP(sw, r)
	}))
}

// Template helper functions

func formatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

func timeago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}

	now := time.Now()
	diff := now.Sub(t)

	if diff < 0 {
		// Future time
		diff = -diff
		if diff < time.Minute {
			return "in a few seconds"
		} else if diff < time.Hour {
			mins := int(diff.Minutes())
			if mins == 1 {
				return "in 1 minute"
			}
			return fmt.Sprintf("in %d minutes", mins)
		} else if diff < 24*time.Hour {
			hours := int(diff.Hours())
			if hours == 1 {
				return "in 1 hour"
			}
			return fmt.Sprintf("in %d hours", hours)
		}
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "in 1 day"
		}
		return fmt.Sprintf("in %d days", days)
	}

	// Past time
	if diff < time.Minute {
		return "just now"
	} else if diff < time.Hour {
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	} else if diff < 24*time.Hour {
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	} else if diff < 7*24*time.Hour {
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	} else if diff < 30*24*time.Hour {
		weeks := int(diff.Hours() / 24 / 7)
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	} else if diff < 365*24*time.Hour {
		months := int(diff.Hours() / 24 / 30)
		if months == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	}

	years := int(diff.Hours() / 24 / 365)
	if years == 1 {
		return "1 year ago"
	}
	return fmt.Sprintf("%d years ago", years)
}

func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func formatMs(ms any) string {
	msVal := toInt(ms)
	if msVal < 0 {
		msVal = -msVal
	}
	totalSec := msVal / 1000
	h := totalSec / 3600
	m := (totalSec % 3600) / 60
	s := totalSec % 60

	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func dict(values ...any) map[string]any {
	if len(values)%2 != 0 {
		return nil
	}
	d := make(map[string]any, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return nil
		}
		d[key] = values[i+1]
	}
	return d
}

func list(values ...any) []any {
	return values
}

func safeHTML(s string) template.HTML {
	return template.HTML(s)
}

func safeJS(s string) template.JS {
	return template.JS(s)
}

func safeURL(s string) template.URL {
	return template.URL(s)
}

func add(a, b any) int      { return toInt(a) + toInt(b) }
func sub(a, b any) int      { return toInt(a) - toInt(b) }

// iterate returns a slice of integers from 0 to n-1 for range loops in templates
func iterate(n int) []int {
	result := make([]int, n)
	for i := 0; i < n; i++ {
		result[i] = i
	}
	return result
}
func mul(a, b any) int      { return toInt(a) * toInt(b) }
func div(a, b any) int      { ai, bi := toInt(a), toInt(b); if bi == 0 { return 0 }; return ai / bi }
func mod(a, b any) int      { ai, bi := toInt(a), toInt(b); if bi == 0 { return 0 }; return ai % bi }
func eq(a, b any) bool      { return a == b }
func ne(a, b any) bool      { return a != b }
func lt(a, b any) bool      { return toInt(a) < toInt(b) }
func le(a, b any) bool      { return toInt(a) <= toInt(b) }
func gt(a, b any) bool      { return toInt(a) > toInt(b) }
func ge(a, b any) bool      { return toInt(a) >= toInt(b) }

// toInt converts various numeric types to int
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	default:
		return 0
	}
}
func and(a, b bool) bool    { return a && b }
func or(a, b bool) bool     { return a || b }
func not(a bool) bool       { return !a }

func defaultVal(def, val any) any {
	if val == nil || val == "" || val == 0 || val == false {
		return def
	}
	return val
}

func coalesce(values ...any) any {
	for _, v := range values {
		if v != nil && v != "" && v != 0 && v != false {
			return v
		}
	}
	return nil
}

func ternary(cond bool, a, b any) any {
	if cond {
		return a
	}
	return b
}

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

func roleAtLeast(user *models.User, minRole string) bool {
	if user == nil {
		return false
	}
	// Map platform roles to legacy role levels for backward compatibility
	roleOrder := map[string]int{
		"platform_admin": 3,
		"platform_mod":   2,
		"user":           1,
		// Legacy role names for backward compatibility
		"admin":   3,
		"manager": 2,
		"dj":      1,
	}
	userLevel := roleOrder[string(user.PlatformRole)]
	minLevel := roleOrder[minRole]
	return userLevel >= minLevel
}

func isPlatformAdmin(user *models.User) bool {
	return user != nil && user.IsPlatformAdmin()
}

func isActive(currentPath, linkPath string) bool {
	if linkPath == "/" {
		return currentPath == "/"
	}
	return strings.HasPrefix(currentPath, linkPath)
}

// Context keys
type ctxKey string

const (
	ctxKeyUser    ctxKey = "user"
	ctxKeyStation ctxKey = "station"
)

// GetBasePath returns the file name without directory for template naming
func GetBasePath(path string) string {
	return filepath.Base(path)
}
