/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/landingpage"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// LandingPageAPI handles landing page API endpoints.
type LandingPageAPI struct {
	api     *API
	service *landingpage.Service
}

// NewLandingPageAPI creates a new landing page API handler.
func NewLandingPageAPI(api *API, service *landingpage.Service) *LandingPageAPI {
	return &LandingPageAPI{
		api:     api,
		service: service,
	}
}

// RegisterRoutes registers landing page routes.
func (lp *LandingPageAPI) RegisterRoutes(r chi.Router) {
	r.Route("/landing-page", func(r chi.Router) {
		// Read operations
		r.Get("/", lp.handleGet)
		r.Get("/draft", lp.handleGetDraft)
		r.Get("/preview", lp.handlePreview)

		// Write operations (manager+)
		r.Group(func(r chi.Router) {
			r.Use(lp.api.requireRoles(models.RoleAdmin, models.RoleManager))
			r.Put("/", lp.handleUpdate)
			r.Post("/publish", lp.handlePublish)
			r.Post("/discard-draft", lp.handleDiscardDraft)
			r.Put("/theme", lp.handleUpdateTheme)
			r.Put("/custom-css", lp.handleUpdateCustomCSS)
			r.Put("/custom-head", lp.handleUpdateCustomHead)
		})

		// Assets
		r.Route("/assets", func(r chi.Router) {
			r.Get("/", lp.handleAssetsList)
			r.Group(func(r chi.Router) {
				r.Use(lp.api.requireRoles(models.RoleAdmin, models.RoleManager))
				r.Post("/", lp.handleAssetsUpload)
				r.Delete("/{assetID}", lp.handleAssetsDelete)
			})
		})

		// Versions
		r.Route("/versions", func(r chi.Router) {
			r.Get("/", lp.handleVersionsList)
			r.Get("/{versionID}", lp.handleVersionsGet)
			r.Group(func(r chi.Router) {
				r.Use(lp.api.requireRoles(models.RoleAdmin, models.RoleManager))
				r.Post("/{versionID}/restore", lp.handleVersionsRestore)
			})
		})

		// Themes
		r.Get("/themes", lp.handleThemesList)
		r.Get("/themes/{name}", lp.handleThemesGet)
	})
}

// handleGet returns the landing page for a station.
func (lp *LandingPageAPI) handleGet(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	page, err := lp.service.GetOrCreate(r.Context(), stationID)
	if err != nil {
		lp.api.logger.Error().Err(err).Msg("get landing page failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, page)
}

// handleGetDraft returns the draft configuration.
func (lp *LandingPageAPI) handleGetDraft(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	config, err := lp.service.GetDraft(r.Context(), stationID)
	if err != nil {
		lp.api.logger.Error().Err(err).Msg("get draft failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"config": config})
}

// handleUpdate saves the draft configuration.
func (lp *LandingPageAPI) handleUpdate(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	var req struct {
		Config map[string]any `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := lp.service.SaveDraft(r.Context(), stationID, req.Config); err != nil {
		lp.api.logger.Error().Err(err).Msg("save draft failed")
		writeError(w, http.StatusInternalServerError, "save_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// handlePublish publishes the draft configuration.
func (lp *LandingPageAPI) handlePublish(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		Summary string `json:"summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Summary is optional
		req.Summary = ""
	}

	if err := lp.service.Publish(r.Context(), stationID, claims.UserID, req.Summary); err != nil {
		lp.api.logger.Error().Err(err).Msg("publish failed")
		writeError(w, http.StatusInternalServerError, "publish_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
}

// handleDiscardDraft discards the draft configuration.
func (lp *LandingPageAPI) handleDiscardDraft(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	if err := lp.service.DiscardDraft(r.Context(), stationID); err != nil {
		lp.api.logger.Error().Err(err).Msg("discard draft failed")
		writeError(w, http.StatusInternalServerError, "discard_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "discarded"})
}

// handleUpdateTheme updates the theme.
func (lp *LandingPageAPI) handleUpdateTheme(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	var req struct {
		Theme string `json:"theme"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := lp.service.UpdateTheme(r.Context(), stationID, req.Theme); err != nil {
		lp.api.logger.Error().Err(err).Msg("update theme failed")
		writeError(w, http.StatusInternalServerError, "update_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleUpdateCustomCSS updates the custom CSS.
func (lp *LandingPageAPI) handleUpdateCustomCSS(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	var req struct {
		CSS string `json:"css"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := lp.service.UpdateCustomCSS(r.Context(), stationID, req.CSS); err != nil {
		lp.api.logger.Error().Err(err).Msg("update custom css failed")
		writeError(w, http.StatusInternalServerError, "update_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleUpdateCustomHead updates the custom head HTML.
func (lp *LandingPageAPI) handleUpdateCustomHead(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	var req struct {
		HTML string `json:"html"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := lp.service.UpdateCustomHead(r.Context(), stationID, req.HTML); err != nil {
		lp.api.logger.Error().Err(err).Msg("update custom head failed")
		writeError(w, http.StatusInternalServerError, "update_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handlePreview returns a preview URL or rendered HTML.
func (lp *LandingPageAPI) handlePreview(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	// Get draft config for preview
	config, err := lp.service.GetDraft(r.Context(), stationID)
	if err != nil {
		lp.api.logger.Error().Err(err).Msg("get draft for preview failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Get landing page for theme and custom CSS
	page, err := lp.service.GetOrCreate(r.Context(), stationID)
	if err != nil {
		lp.api.logger.Error().Err(err).Msg("get landing page failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"config":      config,
		"theme":       page.Theme,
		"custom_css":  page.CustomCSS,
		"custom_head": page.CustomHead,
	})
}

// Assets

// handleAssetsList returns all assets for a station.
func (lp *LandingPageAPI) handleAssetsList(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	assets, err := lp.service.ListAssets(r.Context(), stationID)
	if err != nil {
		lp.api.logger.Error().Err(err).Msg("list assets failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"assets": assets})
}

// handleAssetsUpload uploads a new asset.
func (lp *LandingPageAPI) handleAssetsUpload(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse multipart form (max 10MB)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_multipart")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file_required")
		return
	}
	defer file.Close()

	assetType := r.FormValue("type")
	if assetType == "" {
		assetType = models.AssetTypeImage
	}

	asset, err := lp.service.UploadAsset(r.Context(), stationID, assetType, header.Filename, file, &claims.UserID)
	if err != nil {
		if errors.Is(err, landingpage.ErrInvalidAssetType) {
			writeError(w, http.StatusBadRequest, "invalid_asset_type")
			return
		}
		lp.api.logger.Error().Err(err).Msg("upload asset failed")
		writeError(w, http.StatusInternalServerError, "upload_failed")
		return
	}

	writeJSON(w, http.StatusCreated, asset)
}

// handleAssetsDelete deletes an asset.
func (lp *LandingPageAPI) handleAssetsDelete(w http.ResponseWriter, r *http.Request) {
	assetID := chi.URLParam(r, "assetID")
	if assetID == "" {
		writeError(w, http.StatusBadRequest, "asset_id_required")
		return
	}

	if err := lp.service.DeleteAsset(r.Context(), assetID); err != nil {
		if errors.Is(err, landingpage.ErrAssetNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		lp.api.logger.Error().Err(err).Msg("delete asset failed")
		writeError(w, http.StatusInternalServerError, "delete_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Versions

// handleVersionsList returns version history.
func (lp *LandingPageAPI) handleVersionsList(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
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

	versions, total, err := lp.service.ListVersions(r.Context(), stationID, limit, offset)
	if err != nil {
		lp.api.logger.Error().Err(err).Msg("list versions failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"versions": versions,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// handleVersionsGet returns a specific version.
func (lp *LandingPageAPI) handleVersionsGet(w http.ResponseWriter, r *http.Request) {
	versionID := chi.URLParam(r, "versionID")
	if versionID == "" {
		writeError(w, http.StatusBadRequest, "version_id_required")
		return
	}

	version, err := lp.service.GetVersion(r.Context(), versionID)
	if err != nil {
		if errors.Is(err, landingpage.ErrVersionNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		lp.api.logger.Error().Err(err).Msg("get version failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, version)
}

// handleVersionsRestore restores a version.
func (lp *LandingPageAPI) handleVersionsRestore(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	versionID := chi.URLParam(r, "versionID")
	if versionID == "" {
		writeError(w, http.StatusBadRequest, "version_id_required")
		return
	}

	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := lp.service.RestoreVersion(r.Context(), stationID, versionID, claims.UserID); err != nil {
		if errors.Is(err, landingpage.ErrVersionNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		lp.api.logger.Error().Err(err).Msg("restore version failed")
		writeError(w, http.StatusInternalServerError, "restore_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}

// Themes

// handleThemesList returns all available themes.
func (lp *LandingPageAPI) handleThemesList(w http.ResponseWriter, r *http.Request) {
	themes := lp.service.ListThemes()
	writeJSON(w, http.StatusOK, map[string]any{"themes": themes})
}

// handleThemesGet returns a specific theme.
func (lp *LandingPageAPI) handleThemesGet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name_required")
		return
	}

	theme := lp.service.GetTheme(name)
	if theme == nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}

	writeJSON(w, http.StatusOK, theme)
}
