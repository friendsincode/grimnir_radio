/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/landingpage"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// LandingPageEditor renders the landing page editor
func (h *Handler) LandingPageEditor(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	// Check permission
	if !h.canManageLandingPage(user, station) {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	// Get landing page service from handler (we'll need to add this)
	page, err := h.landingPageSvc.GetOrCreate(r.Context(), station.ID)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to get landing page")
		http.Error(w, "Failed to load landing page", http.StatusInternalServerError)
		return
	}

	// Get the draft config or fall back to published
	config := page.PublishedConfig
	if page.DraftConfig != nil && len(page.DraftConfig) > 0 {
		config = page.DraftConfig
	}

	// Get themes and widgets
	themes := h.landingPageSvc.ListThemes()
	widgets := landingpage.GetWidgetsByCategory()

	// Get assets
	assets, _ := h.landingPageSvc.ListAssets(r.Context(), station.ID)

	h.Render(w, r, "pages/dashboard/landing-editor", PageData{
		Title:    "Landing Page Editor - " + station.Name,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Station":      station,
			"LandingPage":  page,
			"Config":       config,
			"ConfigJSON":   mustMarshalJSON(config),
			"Themes":       themes,
			"Widgets":      widgets,
			"WidgetList":   landingpage.WidgetRegistry,
			"Assets":       assets,
			"HasDraft":     page.HasDraft(),
			"CurrentTheme": h.landingPageSvc.GetTheme(page.Theme),
		},
	})
}

// LandingPageEditorSave saves the landing page draft
func (h *Handler) LandingPageEditorSave(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		writeJSONError(w, http.StatusBadRequest, "no_station")
		return
	}

	if !h.canManageLandingPage(user, station) {
		writeJSONError(w, http.StatusForbidden, "permission_denied")
		return
	}

	var req struct {
		Config map[string]any `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := h.landingPageSvc.SaveDraft(r.Context(), station.ID, req.Config); err != nil {
		h.logger.Error().Err(err).Msg("failed to save draft")
		writeJSONError(w, http.StatusInternalServerError, "save_failed")
		return
	}

	// Emit audit event for draft update
	if h.eventBus != nil {
		h.eventBus.Publish(events.EventAuditLandingPageUpdate, events.Payload{
			"user_id":       user.ID,
			"user_email":    user.Email,
			"station_id":    station.ID,
			"resource_type": "landing_page",
			"resource_id":   station.ID,
			"ip_address":    getClientIP(r),
			"user_agent":    r.UserAgent(),
			"change_type":   "draft_save",
		})
	}

	writeJSONResponse(w, http.StatusOK, map[string]string{"status": "saved"})
}

// LandingPageEditorPublish publishes the landing page
func (h *Handler) LandingPageEditorPublish(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		writeJSONError(w, http.StatusBadRequest, "no_station")
		return
	}

	if !h.canManageLandingPage(user, station) {
		writeJSONError(w, http.StatusForbidden, "permission_denied")
		return
	}

	var req struct {
		Summary string `json:"summary"`
	}
	json.NewDecoder(r.Body).Decode(&req) // Optional summary

	if err := h.landingPageSvc.Publish(r.Context(), station.ID, user.ID, req.Summary); err != nil {
		h.logger.Error().Err(err).Msg("failed to publish")
		writeJSONError(w, http.StatusInternalServerError, "publish_failed")
		return
	}

	h.logger.Info().
		Str("station_id", station.ID).
		Str("user_id", user.ID).
		Msg("landing page published")

	// Emit audit event
	if h.eventBus != nil {
		h.eventBus.Publish(events.EventAuditLandingPagePublish, events.Payload{
			"user_id":       user.ID,
			"user_email":    user.Email,
			"station_id":    station.ID,
			"resource_type": "landing_page",
			"resource_id":   station.ID,
			"ip_address":    getClientIP(r),
			"user_agent":    r.UserAgent(),
			"summary":       req.Summary,
		})
	}

	writeJSONResponse(w, http.StatusOK, map[string]string{"status": "published"})
}

// LandingPageEditorDiscard discards the draft
func (h *Handler) LandingPageEditorDiscard(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		writeJSONError(w, http.StatusBadRequest, "no_station")
		return
	}

	if !h.canManageLandingPage(user, station) {
		writeJSONError(w, http.StatusForbidden, "permission_denied")
		return
	}

	if err := h.landingPageSvc.DiscardDraft(r.Context(), station.ID); err != nil {
		h.logger.Error().Err(err).Msg("failed to discard draft")
		writeJSONError(w, http.StatusInternalServerError, "discard_failed")
		return
	}

	writeJSONResponse(w, http.StatusOK, map[string]string{"status": "discarded"})
}

// LandingPageEditorPreview serves the preview iframe content
func (h *Handler) LandingPageEditorPreview(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	// Get landing page
	page, err := h.landingPageSvc.GetOrCreate(r.Context(), station.ID)
	if err != nil {
		http.Error(w, "Failed to load landing page", http.StatusInternalServerError)
		return
	}

	// Use draft if available
	config := page.PublishedConfig
	if page.DraftConfig != nil && len(page.DraftConfig) > 0 {
		config = page.DraftConfig
	}

	// Get theme from config (not from model field)
	themeID := "daw-dark" // default
	if tid, ok := config["theme"].(string); ok && tid != "" {
		themeID = tid
	}
	theme := h.landingPageSvc.GetTheme(themeID)
	if theme == nil {
		theme = h.landingPageSvc.GetTheme("daw-dark")
	}

	// Create renderer and render preview
	renderer, err := landingpage.NewRenderer(h.db)
	if err != nil {
		http.Error(w, "Failed to initialize renderer", http.StatusInternalServerError)
		return
	}

	html, err := renderer.RenderPage(r.Context(), station, config, theme, page.CustomCSS, page.CustomHead)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to render preview")
		http.Error(w, "Failed to render preview", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// LandingPageVersions renders the version history page
func (h *Handler) LandingPageVersions(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	if !h.canManageLandingPage(user, station) {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	limit := 20
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	versions, total, err := h.landingPageSvc.ListVersions(r.Context(), station.ID, limit, offset)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to list versions")
		http.Error(w, "Failed to load versions", http.StatusInternalServerError)
		return
	}

	h.Render(w, r, "pages/dashboard/landing-versions", PageData{
		Title:    "Landing Page History - " + station.Name,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Station":  station,
			"Versions": versions,
			"Total":    total,
			"Limit":    limit,
			"Offset":   offset,
		},
	})
}

// LandingPageVersionRestore restores a version
func (h *Handler) LandingPageVersionRestore(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		writeJSONError(w, http.StatusBadRequest, "no_station")
		return
	}

	if !h.canManageLandingPage(user, station) {
		writeJSONError(w, http.StatusForbidden, "permission_denied")
		return
	}

	versionID := chi.URLParam(r, "versionID")
	if versionID == "" {
		writeJSONError(w, http.StatusBadRequest, "version_id_required")
		return
	}

	if err := h.landingPageSvc.RestoreVersion(r.Context(), station.ID, versionID, user.ID); err != nil {
		h.logger.Error().Err(err).Msg("failed to restore version")
		writeJSONError(w, http.StatusInternalServerError, "restore_failed")
		return
	}

	h.logger.Info().
		Str("station_id", station.ID).
		Str("version_id", versionID).
		Str("user_id", user.ID).
		Msg("landing page version restored")

	// Emit audit event
	if h.eventBus != nil {
		h.eventBus.Publish(events.EventAuditLandingPageRestore, events.Payload{
			"user_id":       user.ID,
			"user_email":    user.Email,
			"station_id":    station.ID,
			"resource_type": "landing_page_version",
			"resource_id":   versionID,
			"ip_address":    getClientIP(r),
			"user_agent":    r.UserAgent(),
		})
	}

	writeJSONResponse(w, http.StatusOK, map[string]string{"status": "restored"})
}

// LandingPageAssetUpload handles asset uploads
func (h *Handler) LandingPageAssetUpload(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		writeJSONError(w, http.StatusBadRequest, "no_station")
		return
	}

	if !h.canManageLandingPage(user, station) {
		writeJSONError(w, http.StatusForbidden, "permission_denied")
		return
	}

	// Parse multipart form (default 10MB, configurable via GRIMNIR_MAX_UPLOAD_SIZE_MB)
	if err := r.ParseMultipartForm(h.multipartLimit(10 << 20)); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_multipart")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "file_required")
		return
	}
	defer file.Close()

	assetType := r.FormValue("type")
	if assetType == "" {
		assetType = models.AssetTypeImage
	}

	asset, err := h.landingPageSvc.UploadAsset(r.Context(), &station.ID, assetType, header.Filename, file, &user.ID)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to upload asset")
		writeJSONError(w, http.StatusInternalServerError, "upload_failed")
		return
	}

	writeJSONResponse(w, http.StatusCreated, asset)
}

// LandingPageAssetDelete deletes an asset
func (h *Handler) LandingPageAssetDelete(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		writeJSONError(w, http.StatusBadRequest, "no_station")
		return
	}

	if !h.canManageLandingPage(user, station) {
		writeJSONError(w, http.StatusForbidden, "permission_denied")
		return
	}

	assetID := chi.URLParam(r, "assetID")
	if assetID == "" {
		writeJSONError(w, http.StatusBadRequest, "asset_id_required")
		return
	}

	if err := h.landingPageSvc.DeleteAsset(r.Context(), assetID); err != nil {
		h.logger.Error().Err(err).Msg("failed to delete asset")
		writeJSONError(w, http.StatusInternalServerError, "delete_failed")
		return
	}

	writeJSONResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// LandingPageAssetServe serves an asset file
func (h *Handler) LandingPageAssetServe(w http.ResponseWriter, r *http.Request) {
	assetID := chi.URLParam(r, "assetID")
	if assetID == "" {
		http.Error(w, "Asset ID required", http.StatusBadRequest)
		return
	}

	asset, err := h.landingPageSvc.GetAsset(r.Context(), assetID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	path := h.landingPageSvc.GetAssetPath(asset)
	http.ServeFile(w, r, path)
}

// LandingPageAssetByType serves an asset by type (logo, background, etc.)
func (h *Handler) LandingPageAssetByType(w http.ResponseWriter, r *http.Request) {
	assetType := chi.URLParam(r, "assetType")
	if assetType == "" {
		http.Error(w, "Asset type required", http.StatusBadRequest)
		return
	}

	stationID := r.URL.Query().Get("station_id")
	isPlatform := r.URL.Query().Get("platform") == "true"

	var stationIDPtr *string
	if !isPlatform && stationID != "" {
		stationIDPtr = &stationID
	}

	asset, err := h.landingPageSvc.GetAssetByType(r.Context(), stationIDPtr, assetType)
	if err != nil {
		// Return a transparent 1x1 pixel for missing images instead of 404
		w.Header().Set("Content-Type", "image/gif")
		w.Write([]byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x21, 0xf9, 0x04, 0x01, 0x00, 0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3b})
		return
	}

	path := h.landingPageSvc.GetAssetPath(asset)
	http.ServeFile(w, r, path)
}

// LandingPageThemeUpdate updates the theme
func (h *Handler) LandingPageThemeUpdate(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		writeJSONError(w, http.StatusBadRequest, "no_station")
		return
	}

	if !h.canManageLandingPage(user, station) {
		writeJSONError(w, http.StatusForbidden, "permission_denied")
		return
	}

	var req struct {
		Theme string `json:"theme"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := h.landingPageSvc.UpdateTheme(r.Context(), station.ID, req.Theme); err != nil {
		h.logger.Error().Err(err).Msg("failed to update theme")
		writeJSONError(w, http.StatusInternalServerError, "update_failed")
		return
	}

	writeJSONResponse(w, http.StatusOK, map[string]string{"status": "updated"})
}

// canManageLandingPage checks if user can manage the landing page
func (h *Handler) canManageLandingPage(user *models.User, station *models.Station) bool {
	if user == nil || station == nil {
		return false
	}

	// Platform admins can manage all
	if user.IsPlatformAdmin() {
		return true
	}

	// Check station role
	stationUser := h.GetStationRole(user, station.ID)
	if stationUser == nil {
		return false
	}

	// Owner, admin, and manager can manage landing page
	return stationUser.Role == models.StationRoleOwner ||
		stationUser.Role == models.StationRoleAdmin ||
		stationUser.Role == models.StationRoleManager
}

// Helper functions

// writeJSONResponse writes a JSON response with the given status code.
func writeJSONResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func mustMarshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// LandingPageCustomCSS updates custom CSS
func (h *Handler) LandingPageCustomCSS(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	station := h.GetStation(r)

	if station == nil {
		writeJSONError(w, http.StatusBadRequest, "no_station")
		return
	}

	if !h.canManageLandingPage(user, station) {
		writeJSONError(w, http.StatusForbidden, "permission_denied")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read_failed")
		return
	}

	var req struct {
		CSS string `json:"css"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := h.landingPageSvc.UpdateCustomCSS(r.Context(), station.ID, req.CSS); err != nil {
		h.logger.Error().Err(err).Msg("failed to update custom CSS")
		writeJSONError(w, http.StatusInternalServerError, "update_failed")
		return
	}

	writeJSONResponse(w, http.StatusOK, map[string]string{"status": "updated"})
}

// ============================================================================
// Platform Landing Page Editor (Platform Admin Only)
// ============================================================================

// PlatformLandingPageEditor renders the platform landing page editor
func (h *Handler) PlatformLandingPageEditor(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	page, err := h.landingPageSvc.GetOrCreatePlatform(r.Context())
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to get platform landing page")
		http.Error(w, "Failed to load landing page", http.StatusInternalServerError)
		return
	}

	config := page.PublishedConfig
	if page.DraftConfig != nil && len(page.DraftConfig) > 0 {
		config = page.DraftConfig
	}

	themes := h.landingPageSvc.ListThemes()
	widgets := landingpage.GetWidgetsByCategory()
	assets, _ := h.landingPageSvc.ListPlatformAssets(r.Context())

	// Get all public stations for ordering
	var publicStations []models.Station
	h.db.Where("active = ? AND public = ? AND approved = ?", true, true, true).
		Order("sort_order, name").
		Find(&publicStations)

	h.Render(w, r, "pages/dashboard/admin/platform-landing-editor", PageData{
		Title:    "Platform Landing Page Editor",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"LandingPage":  page,
			"Config":       config,
			"ConfigJSON":   mustMarshalJSON(config),
			"Themes":       themes,
			"Widgets":      widgets,
			"WidgetList":   landingpage.WidgetRegistry,
			"Assets":       assets,
			"HasDraft":     page.HasDraft(),
			"CurrentTheme": h.landingPageSvc.GetTheme(page.Theme),
			"IsPlatform":   true,
			"Stations":     publicStations,
		},
	})
}

// PlatformLandingPageSave saves the platform landing page draft
func (h *Handler) PlatformLandingPageSave(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		Config map[string]any `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := h.landingPageSvc.SavePlatformDraft(r.Context(), req.Config); err != nil {
		h.logger.Error().Err(err).Msg("failed to save platform draft")
		writeJSONError(w, http.StatusInternalServerError, "save_failed")
		return
	}

	// Emit audit event for platform draft update
	if h.eventBus != nil {
		h.eventBus.Publish(events.EventAuditLandingPageUpdate, events.Payload{
			"user_id":       user.ID,
			"user_email":    user.Email,
			"resource_type": "platform_landing_page",
			"resource_id":   "platform",
			"ip_address":    getClientIP(r),
			"user_agent":    r.UserAgent(),
			"change_type":   "draft_save",
			"is_platform":   true,
		})
	}

	writeJSONResponse(w, http.StatusOK, map[string]string{"status": "saved"})
}

// PlatformLandingPagePublish publishes the platform landing page
func (h *Handler) PlatformLandingPagePublish(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		Summary string `json:"summary"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if err := h.landingPageSvc.PublishPlatform(r.Context(), user.ID, req.Summary); err != nil {
		h.logger.Error().Err(err).Msg("failed to publish platform landing page")
		writeJSONError(w, http.StatusInternalServerError, "publish_failed")
		return
	}

	h.logger.Info().
		Str("user_id", user.ID).
		Msg("platform landing page published")

	// Emit audit event
	if h.eventBus != nil {
		h.eventBus.Publish(events.EventAuditLandingPagePublish, events.Payload{
			"user_id":       user.ID,
			"user_email":    user.Email,
			"resource_type": "platform_landing_page",
			"resource_id":   "platform",
			"ip_address":    getClientIP(r),
			"user_agent":    r.UserAgent(),
			"summary":       req.Summary,
			"is_platform":   true,
		})
	}

	writeJSONResponse(w, http.StatusOK, map[string]string{"status": "published"})
}

// PlatformLandingPageDiscard discards the platform landing page draft
func (h *Handler) PlatformLandingPageDiscard(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := h.landingPageSvc.DiscardPlatformDraft(r.Context()); err != nil {
		h.logger.Error().Err(err).Msg("failed to discard platform draft")
		writeJSONError(w, http.StatusInternalServerError, "discard_failed")
		return
	}

	writeJSONResponse(w, http.StatusOK, map[string]string{"status": "discarded"})
}

// PlatformLandingPagePreview renders a preview of the platform landing page
func (h *Handler) PlatformLandingPagePreview(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	page, err := h.landingPageSvc.GetOrCreatePlatform(r.Context())
	if err != nil {
		http.Error(w, "Failed to load", http.StatusInternalServerError)
		return
	}

	config := page.PublishedConfig
	if page.DraftConfig != nil && len(page.DraftConfig) > 0 {
		config = page.DraftConfig
	}

	// Get theme from config (not from model field)
	themeID := "daw-dark" // default
	if tid, ok := config["theme"].(string); ok && tid != "" {
		themeID = tid
	}
	theme := h.landingPageSvc.GetTheme(themeID)
	if theme == nil {
		theme = h.landingPageSvc.GetTheme("daw-dark")
	}

	// Get all public stations for the stations grid widget
	var stations []models.Station
	h.db.Where("active = ? AND public = ? AND approved = ?", true, true, true).
		Order("sort_order, name").
		Find(&stations)

	// Order stations based on config
	orderedStations := orderStationsByConfig(stations, config)

	// Prepare stations with their mounts and stream URLs (same as Landing handler)
	type stationWithStream struct {
		Station     models.Station
		StreamURL   string
		StreamURLLQ string
		MountName   string
	}

	var stationsWithStreams []stationWithStream
	for _, s := range orderedStations {
		var mount models.Mount
		h.db.Where("station_id = ?", s.ID).First(&mount)

		sw := stationWithStream{Station: s}
		if mount.ID != "" {
			sw.StreamURL = "/live/" + mount.Name
			sw.StreamURLLQ = "/live/" + mount.Name + "-lq"
			sw.MountName = mount.Name
		}
		stationsWithStreams = append(stationsWithStreams, sw)
	}

	// Get featured station for hero player (if any)
	var featuredStation *stationWithStream
	for i, s := range stationsWithStreams {
		if s.Station.Featured {
			featuredStation = &stationsWithStreams[i]
			break
		}
	}
	if featuredStation == nil && len(stationsWithStreams) > 0 {
		featuredStation = &stationsWithStreams[0]
	}

	h.Render(w, r, "pages/public/platform-landing-preview", PageData{
		Title: "Platform Preview",
		Data: map[string]any{
			"Config":          config,
			"Theme":           theme,
			"Stations":        stations,
			"OrderedStations": stationsWithStreams,
			"FeaturedStation": featuredStation,
			"IsPreview":       true,
			"IsPlatform":      true,
		},
	})
}

// orderStationsByConfig reorders stations based on content.stationOrder config
func orderStationsByConfig(stations []models.Station, config map[string]any) []models.Station {
	content, ok := config["content"].(map[string]any)
	if !ok {
		return stations
	}

	orderList, ok := content["stationOrder"].([]any)
	if !ok || len(orderList) == 0 {
		return stations
	}

	// Build order map
	orderMap := make(map[string]int)
	for i, id := range orderList {
		if idStr, ok := id.(string); ok {
			orderMap[idStr] = i
		}
	}

	// Sort stations
	result := make([]models.Station, len(stations))
	copy(result, stations)

	// Simple insertion sort based on custom order
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			iOrder, iHasOrder := orderMap[result[i].ID]
			jOrder, jHasOrder := orderMap[result[j].ID]

			// Items with custom order come first, in their specified order
			if jHasOrder && !iHasOrder {
				result[i], result[j] = result[j], result[i]
			} else if iHasOrder && jHasOrder && jOrder < iOrder {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
}

// getClientIP extracts the client IP from request, checking proxy headers.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (can be comma-separated list)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// First IP is the client
		if idx := strings.Index(xff, ","); idx > 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to remote addr
	return r.RemoteAddr
}

// PlatformLandingPageAssetUpload uploads an asset for the platform
func (h *Handler) PlatformLandingPageAssetUpload(w http.ResponseWriter, r *http.Request) {
	user := h.GetUser(r)
	if user == nil || !user.IsPlatformAdmin() {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := r.ParseMultipartForm(h.multipartLimit(10 << 20)); err != nil {
		writeJSONError(w, http.StatusBadRequest, "file_too_large")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "no_file")
		return
	}
	defer file.Close()

	assetType := r.FormValue("type")
	if assetType == "" {
		assetType = models.AssetTypeImage
	}

	asset, err := h.landingPageSvc.UploadAsset(r.Context(), nil, assetType, header.Filename, file, &user.ID)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to upload platform asset")
		writeJSONError(w, http.StatusInternalServerError, "upload_failed")
		return
	}

	writeJSONResponse(w, http.StatusOK, map[string]any{
		"id":       asset.ID,
		"filename": asset.FileName,
		"url":      "/landing-assets/" + asset.ID,
		"type":     asset.AssetType,
	})
}
