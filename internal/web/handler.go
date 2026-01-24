/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
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
	db        *gorm.DB
	logger    zerolog.Logger
	jwtSecret []byte
	mediaRoot string                         // Root directory for media files
	templates map[string]*template.Template // Each page gets its own template set
	partials  *template.Template            // Shared partials
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
	Data        any
}

// FlashMessage for toast notifications
type FlashMessage struct {
	Type    string // success, error, warning, info
	Message string
}

// NewHandler creates a new web handler.
func NewHandler(db *gorm.DB, jwtSecret []byte, mediaRoot string, logger zerolog.Logger) (*Handler, error) {
	h := &Handler{
		db:        db,
		logger:    logger,
		jwtSecret: jwtSecret,
		mediaRoot: mediaRoot,
	}

	if err := h.loadTemplates(); err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}

	return h, nil
}

func (h *Handler) loadTemplates() error {
	funcMap := template.FuncMap{
		"formatTime":     formatTime,
		"formatDuration": formatDuration,
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
		"isActive":       isActive,
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

	// Get user from context if authenticated
	if user, ok := r.Context().Value(ctxKeyUser).(*models.User); ok {
		data.User = user
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

// StaticHandler returns an http.Handler for static files.
func (h *Handler) StaticHandler() http.Handler {
	fsys, _ := fs.Sub(StaticFS, "static")
	return http.StripPrefix("/static/", http.FileServer(http.FS(fsys)))
}

// Template helper functions

func formatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
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

func add(a, b int) int      { return a + b }
func sub(a, b int) int      { return a - b }
func mul(a, b int) int      { return a * b }
func div(a, b int) int      { return a / b }
func mod(a, b int) int      { return a % b }
func eq(a, b any) bool      { return a == b }
func ne(a, b any) bool      { return a != b }
func lt(a, b int) bool      { return a < b }
func le(a, b int) bool      { return a <= b }
func gt(a, b int) bool      { return a > b }
func ge(a, b int) bool      { return a >= b }
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
	// Simple JSON marshaling for safe inclusion in templates
	// For complex objects, use proper encoding/json
	return template.JS(fmt.Sprintf("%v", v))
}

func roleAtLeast(user *models.User, minRole string) bool {
	if user == nil {
		return false
	}
	roleOrder := map[string]int{
		"admin":   3,
		"manager": 2,
		"dj":      1,
	}
	userLevel := roleOrder[string(user.Role)]
	minLevel := roleOrder[minRole]
	return userLevel >= minLevel
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
