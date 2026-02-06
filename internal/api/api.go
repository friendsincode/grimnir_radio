/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/analyzer"
	"github.com/friendsincode/grimnir_radio/internal/audit"
	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/broadcast"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/executor"
	"github.com/friendsincode/grimnir_radio/internal/live"
	"github.com/friendsincode/grimnir_radio/internal/logbuffer"
	"github.com/friendsincode/grimnir_radio/internal/media"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/playout"
	"github.com/friendsincode/grimnir_radio/internal/priority"
	"github.com/friendsincode/grimnir_radio/internal/scheduler"
	"github.com/friendsincode/grimnir_radio/internal/smartblock"
	"github.com/friendsincode/grimnir_radio/internal/telemetry"
	"github.com/friendsincode/grimnir_radio/internal/webstream"
	ws "nhooyr.io/websocket"
)

// API exposes HTTP handlers.
type API struct {
	db                   *gorm.DB
	jwtSecret            []byte
	scheduler            *scheduler.Service
	analyzer             *analyzer.Service
	media                *media.Service
	live                 *live.Service
	webstreamSvc         *webstream.Service
	playout              *playout.Manager
	prioritySvc          *priority.Service
	executorStateMgr     *executor.StateManager
	auditSvc             *audit.Service
	notificationAPI      *NotificationAPI
	webhookAPI           *WebhookAPI
	scheduleAnalyticsAPI *ScheduleAnalyticsAPI
	syndicationAPI       *SyndicationAPI
	underwritingAPI      *UnderwritingAPI
	scheduleExportAPI    *ScheduleExportAPI
	landingPageAPI       *LandingPageAPI
	migrationHandler     *MigrationHandler
	webdjAPI             *WebDJAPI
	webdjWS              *WebDJWebSocket
	broadcast            *broadcast.Server
	bus                  *events.Bus
	logBuffer            *logbuffer.Buffer
	logger               zerolog.Logger
}

// New creates the API router wrapper.
func New(db *gorm.DB, jwtSecret []byte, scheduler *scheduler.Service, analyzer *analyzer.Service, media *media.Service, live *live.Service, webstreamSvc *webstream.Service, playout *playout.Manager, prioritySvc *priority.Service, executorStateMgr *executor.StateManager, auditSvc *audit.Service, broadcastSrv *broadcast.Server, bus *events.Bus, logBuf *logbuffer.Buffer, logger zerolog.Logger) *API {
	migrationHandler := NewMigrationHandler(db, media, bus, logger)

	return &API{
		db:               db,
		jwtSecret:        jwtSecret,
		scheduler:        scheduler,
		analyzer:         analyzer,
		media:            media,
		live:             live,
		webstreamSvc:     webstreamSvc,
		playout:          playout,
		prioritySvc:      prioritySvc,
		executorStateMgr: executorStateMgr,
		auditSvc:         auditSvc,
		migrationHandler: migrationHandler,
		logBuffer:        logBuf,
		broadcast:        broadcastSrv,
		bus:              bus,
		logger:           logger,
	}
}

// SetNotificationAPI sets the notification API handler.
func (a *API) SetNotificationAPI(notifAPI *NotificationAPI) {
	a.notificationAPI = notifAPI
}

// SetWebhookAPI sets the webhook API handler.
func (a *API) SetWebhookAPI(webhookAPI *WebhookAPI) {
	a.webhookAPI = webhookAPI
}

// SetScheduleAnalyticsAPI sets the schedule analytics API handler.
func (a *API) SetScheduleAnalyticsAPI(api *ScheduleAnalyticsAPI) {
	a.scheduleAnalyticsAPI = api
}

// SetSyndicationAPI sets the syndication API handler.
func (a *API) SetSyndicationAPI(api *SyndicationAPI) {
	a.syndicationAPI = api
}

// SetUnderwritingAPI sets the underwriting API handler.
func (a *API) SetUnderwritingAPI(api *UnderwritingAPI) {
	a.underwritingAPI = api
}

// SetScheduleExportAPI sets the schedule export API handler.
func (a *API) SetScheduleExportAPI(api *ScheduleExportAPI) {
	a.scheduleExportAPI = api
}

// SetLandingPageAPI sets the landing page API handler.
func (a *API) SetLandingPageAPI(api *LandingPageAPI) {
	a.landingPageAPI = api
}

// SetWebDJAPI sets the WebDJ API handler.
func (a *API) SetWebDJAPI(api *WebDJAPI) {
	a.webdjAPI = api
}

// SetWebDJWebSocket sets the WebDJ WebSocket handler.
func (a *API) SetWebDJWebSocket(ws *WebDJWebSocket) {
	a.webdjWS = ws
}

type mountRequest struct {
	StationID     string         `json:"station_id"`
	Name          string         `json:"name"`
	URL           string         `json:"url"`
	Format        string         `json:"format"`
	Bitrate       int            `json:"bitrate_kbps"`
	Channels      int            `json:"channels"`
	SampleRate    int            `json:"sample_rate"`
	EncoderPreset map[string]any `json:"encoder_preset"`
}

type smartBlockRequest struct {
	StationID   string         `json:"station_id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Rules       map[string]any `json:"rules"`
	Sequence    map[string]any `json:"sequence"`
}

type smartBlockMaterializeRequest struct {
	Seed       int64  `json:"seed"`
	DurationMS int64  `json:"duration_ms"`
	MountID    string `json:"mount_id"`
	StationID  string `json:"station_id"`
}

type clockCreateRequest struct {
	StationID string             `json:"station_id"`
	Name      string             `json:"name"`
	Slots     []clockSlotRequest `json:"slots"`
}

type clockSlotRequest struct {
	Position int            `json:"position"`
	OffsetMS int64          `json:"offset_ms"`
	Type     string         `json:"type"`
	Payload  map[string]any `json:"payload"`
}

type scheduleRefreshRequest struct {
	StationID string `json:"station_id"`
}

type scheduleUpdateRequest struct {
	StartsAt string         `json:"starts_at"`
	EndsAt   string         `json:"ends_at"`
	MountID  string         `json:"mount_id"`
	Metadata map[string]any `json:"metadata"`
}

type liveAuthorizeRequest struct {
	StationID string `json:"station_id"`
	MountID   string `json:"mount_id"`
	Token     string `json:"token"`
}

type liveHandoverRequest struct {
	StationID string `json:"station_id"`
	MountID   string `json:"mount_id"`
}

type playoutControlRequest struct {
	StationID string `json:"station_id"`
	MountID   string `json:"mount_id"`
	Launch    string `json:"launch"`
}

type spinsQuery struct {
	StationID string
	Since     time.Time
}

// Routes mounts API routes on provided router.
func (a *API) Routes(r chi.Router) {
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", a.handleHealth)

		// Public endpoints (no auth required)
		r.Get("/analytics/now-playing", a.handleAnalyticsNowPlaying)
		r.Get("/analytics/listeners", a.handleAnalyticsListeners)
		r.Get("/public/stations", a.handlePublicStations)
		r.Get("/stations/{stationID}/logo", a.handleStationLogo)

		// Public schedule endpoints (Phase 8G)
		a.AddPublicScheduleRoutes(r)

		r.Group(func(pr chi.Router) {
			pr.Use(a.authMiddleware())

			pr.Route("/stations", func(r chi.Router) {
				r.Get("/", a.handleStationsList)
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/", a.handleStationsCreate)
				r.Route("/{stationID}", func(r chi.Router) {
					r.Get("/", a.handleStationsGet)
					r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Patch("/", a.notImplemented)
					r.Route("/mounts", func(r chi.Router) {
						r.Get("/", a.handleMountsList)
						r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/", a.handleMountsCreate)
					})
					// Station logs (accessible to all station roles)
					r.Route("/logs", func(lr chi.Router) {
						lr.Get("/", a.handleStationLogs)
						lr.Get("/components", a.handleStationLogComponents)
						lr.Get("/stats", a.handleStationLogStats)
					})

					// Station audit logs (admin/manager only)
					r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Get("/audit", a.handleStationAuditList)
				})
			})

			pr.Route("/media", func(r chi.Router) {
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager, models.RoleDJ)).Post("/upload", a.handleMediaUpload)
				r.Get("/{mediaID}", a.handleMediaGet)
			})

			pr.Route("/playlists", func(r chi.Router) {
				r.Get("/", a.handlePlaylistsList)
			})

			pr.Route("/smart-blocks", func(r chi.Router) {
				r.Get("/", a.handleSmartBlocksList)
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/", a.handleSmartBlocksCreate)
				r.Post("/{blockID}/materialize", a.handleSmartBlockMaterialize)
			})

			pr.Route("/clocks", func(r chi.Router) {
				r.Get("/", a.handleClocksList)
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/", a.handleClocksCreate)
				r.Post("/{clockID}/simulate", a.handleClockSimulate)
			})

			pr.Route("/schedule", func(r chi.Router) {
				r.Get("/", a.handleScheduleList)
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/refresh", a.handleScheduleRefresh)
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Patch("/{entryID}", a.handleScheduleUpdate)
			})

			pr.Route("/live", func(r chi.Router) {
				// Token generation (admin/manager only)
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/tokens", a.handleLiveGenerateToken)

				// Authorization (called by media engine/icecast)
				r.Post("/authorize", a.handleLiveAuthorize)

				// Connect/disconnect
				r.Post("/connect", a.handleLiveConnect)
				r.Delete("/sessions/{session_id}", a.handleLiveDisconnect)

				// Session management
				r.Get("/sessions", a.handleListLiveSessions)
				r.Get("/sessions/{session_id}", a.handleGetLiveSession)

				// Handover management (admin/manager/DJ)
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager, models.RoleDJ)).Post("/sessions/handover", a.handleLiveStartHandover)
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager, models.RoleDJ)).Delete("/sessions/{session_id}/handover", a.handleLiveReleaseHandover)

				// Legacy handover endpoint (deprecated, kept for compatibility)
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/handover", a.handleLiveHandover)
			})

			// WebDJ Console API
			pr.Route("/webdj", func(r chi.Router) {
				// Session management
				r.Post("/sessions", a.handleWebDJStartSession)
				r.Get("/sessions", a.handleWebDJListSessions)
				r.Get("/sessions/{id}", a.handleWebDJGetSession)
				r.Delete("/sessions/{id}", a.handleWebDJEndSession)

				// WebSocket (handled separately)
				r.Get("/sessions/{id}/ws", a.handleWebDJWebSocket)

				// Deck controls
				r.Post("/sessions/{id}/decks/{deck}/load", a.handleWebDJLoadTrack)
				r.Post("/sessions/{id}/decks/{deck}/play", a.handleWebDJPlay)
				r.Post("/sessions/{id}/decks/{deck}/pause", a.handleWebDJPause)
				r.Post("/sessions/{id}/decks/{deck}/seek", a.handleWebDJSeek)
				r.Post("/sessions/{id}/decks/{deck}/cue", a.handleWebDJSetCue)
				r.Delete("/sessions/{id}/decks/{deck}/cue/{cue_id}", a.handleWebDJDeleteCue)
				r.Delete("/sessions/{id}/decks/{deck}", a.handleWebDJEject)
				r.Post("/sessions/{id}/decks/{deck}/volume", a.handleWebDJSetVolume)
				r.Post("/sessions/{id}/decks/{deck}/eq", a.handleWebDJSetEQ)
				r.Post("/sessions/{id}/decks/{deck}/pitch", a.handleWebDJSetPitch)

				// Mixer controls
				r.Post("/sessions/{id}/mixer/crossfader", a.handleWebDJSetCrossfader)
				r.Post("/sessions/{id}/mixer/master-volume", a.handleWebDJSetMasterVolume)

				// Live broadcast
				r.Post("/sessions/{id}/live", a.handleWebDJGoLive)
				r.Delete("/sessions/{id}/live", a.handleWebDJGoOffAir)

				// Library (waveform)
				r.Get("/library/{id}/waveform", a.handleWebDJGetWaveform)
			})
			pr.Route("/webstreams", func(r chi.Router) {
				// List and create webstreams (admin/manager)
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Get("/", a.handleListWebstreams)
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/", a.handleCreateWebstream)

				// Individual webstream operations
				r.Route("/{id}", func(r chi.Router) {
					r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Get("/", a.handleGetWebstream)
					r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Put("/", a.handleUpdateWebstream)
					r.With(a.requireRoles(models.RoleAdmin)).Delete("/", a.handleDeleteWebstream)

					// Failover operations
					r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/failover", a.handleTriggerWebstreamFailover)
					r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/reset", a.handleResetWebstreamToPrimary)
				})
			})

			pr.Route("/playout", func(r chi.Router) {
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/reload", a.handlePlayoutReload)
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager, models.RoleDJ)).Post("/skip", a.handlePlayoutSkip)
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/stop", a.handlePlayoutStop)
			})

			pr.Route("/analytics", func(r chi.Router) {
				r.Get("/now-playing", a.handleAnalyticsNowPlaying)
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Get("/spins", a.handleAnalyticsSpins)
			})

			// User preferences
			pr.Route("/preferences", func(r chi.Router) {
				r.Post("/theme", a.handleSetThemePreference)
			})

			// System status routes (platform admin only)
			pr.Route("/system", func(r chi.Router) {
				r.Use(a.requirePlatformAdmin())
				r.Get("/status", a.handleSystemStatus)
				r.Post("/test-media-engine", a.handleTestMediaEngine)
				r.Post("/reanalyze-missing-artwork", a.handleReanalyzeMissingArtwork)
				r.Get("/logs", a.handleSystemLogs)
				r.Get("/logs/components", a.handleLogComponents)
				r.Get("/logs/stats", a.handleLogStats)
				r.Delete("/logs", a.handleClearLogs)
			})

			// Audit log routes (platform admin only)
			pr.Route("/audit", func(r chi.Router) {
				r.Use(a.requirePlatformAdmin())
				r.Get("/", a.handleAuditList)
			})

			// Priority management routes
			a.AddPriorityRoutes(pr)

			// Executor state routes
			a.AddExecutorRoutes(pr)

			// Shows and scheduling routes
			a.AddShowRoutes(pr)

			// Schedule rules and validation
			a.AddScheduleRuleRoutes(pr)

			// Schedule templates and versions
			a.AddScheduleTemplateRoutes(pr)
			a.AddScheduleVersionRoutes(pr)

			// DJ self-service
			a.AddDJSelfServiceRoutes(pr)

			// Notifications
			if a.notificationAPI != nil {
				a.notificationAPI.RegisterRoutes(pr)
			}

			// Webhooks (station manager+)
			if a.webhookAPI != nil {
				a.webhookAPI.RegisterRoutes(pr)
			}

			// Phase 8H: Advanced Features
			// Schedule Analytics (admin/manager)
			if a.scheduleAnalyticsAPI != nil {
				pr.Group(func(ar chi.Router) {
					ar.Use(a.requireRoles(models.RoleAdmin, models.RoleManager))
					a.scheduleAnalyticsAPI.RegisterRoutes(ar)
				})
			}

			// Syndication (admin/manager)
			if a.syndicationAPI != nil {
				pr.Group(func(sr chi.Router) {
					sr.Use(a.requireRoles(models.RoleAdmin, models.RoleManager))
					a.syndicationAPI.RegisterRoutes(sr)
				})
			}

			// Underwriting (admin/manager)
			if a.underwritingAPI != nil {
				pr.Group(func(ur chi.Router) {
					ur.Use(a.requireRoles(models.RoleAdmin, models.RoleManager))
					a.underwritingAPI.RegisterRoutes(ur)
				})
			}

			// Schedule Import/Export (admin/manager)
			if a.scheduleExportAPI != nil {
				pr.Group(func(er chi.Router) {
					er.Use(a.requireRoles(models.RoleAdmin, models.RoleManager))
					a.scheduleExportAPI.RegisterRoutes(er)
				})
			}

			// Landing Page Editor (admin/manager)
			if a.landingPageAPI != nil {
				a.landingPageAPI.RegisterRoutes(pr)
			}

			// Migration routes (admin only)
			pr.Group(func(mr chi.Router) {
				mr.Use(a.requireRoles(models.RoleAdmin))
				a.migrationHandler.RegisterRoutes(mr)
			})

			pr.Get("/events", a.handleEvents)
		})
	})
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleStationsList(w http.ResponseWriter, r *http.Request) {
	var stations []models.Station
	if err := a.db.WithContext(r.Context()).Find(&stations).Error; err != nil {
		a.logger.Error().Err(err).Msg("list stations failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	writeJSON(w, http.StatusOK, stations)
}

func (a *API) handleStationsCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Timezone    string `json:"timezone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name_required")
		return
	}

	if req.Timezone == "" {
		req.Timezone = "UTC"
	}

	station := models.Station{
		ID:          uuid.NewString(),
		Name:        req.Name,
		Description: req.Description,
		Timezone:    req.Timezone,
	}

	// Use transaction for station + mount creation
	tx := a.db.WithContext(r.Context()).Begin()

	if err := tx.Create(&station).Error; err != nil {
		tx.Rollback()
		a.logger.Error().Err(err).Msg("create station failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Auto-generate default mount point
	mountName := models.GenerateMountName(station.Name)
	mount := models.Mount{
		ID:         uuid.NewString(),
		StationID:  station.ID,
		Name:       mountName,
		Format:     "mp3",
		Bitrate:    128,
		Channels:   2,
		SampleRate: 44100,
	}

	if err := tx.Create(&mount).Error; err != nil {
		tx.Rollback()
		a.logger.Error().Err(err).Msg("create default mount failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	tx.Commit()

	a.logger.Info().
		Str("station_id", station.ID).
		Str("mount", mountName).
		Msg("station created with default mount")

	// Publish audit event
	a.publishAuditEvent(r, events.EventAuditStationCreate, events.Payload{
		"station_id":    station.ID,
		"resource_type": "station",
		"resource_id":   station.ID,
		"name":          station.Name,
	})

	writeJSON(w, http.StatusCreated, station)
}

func (a *API) handleStationsGet(w http.ResponseWriter, r *http.Request) {
	stationID := chi.URLParam(r, "stationID")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	var station models.Station
	result := a.db.WithContext(r.Context()).First(&station, "id = ?", stationID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		a.logger.Error().Err(result.Error).Msg("get station failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, station)
}

// handleStationLogo serves a station's logo image (public, no auth required).
func (a *API) handleStationLogo(w http.ResponseWriter, r *http.Request) {
	stationID := chi.URLParam(r, "stationID")
	if stationID == "" {
		http.NotFound(w, r)
		return
	}

	var station models.Station
	result := a.db.WithContext(r.Context()).Select("id", "logo", "logo_mime").First(&station, "id = ?", stationID)
	if result.Error != nil || len(station.Logo) == 0 {
		http.NotFound(w, r)
		return
	}

	contentType := station.LogoMime
	if contentType == "" {
		contentType = "image/png"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(station.Logo)
}

// handleSetThemePreference saves the user's theme preference.
func (a *API) handleSetThemePreference(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse theme from form or JSON
	theme := r.FormValue("theme")
	if theme == "" {
		var req struct {
			Theme string `json:"theme"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			theme = req.Theme
		}
	}

	// Validate theme
	validThemes := map[string]bool{
		"daw-dark": true, "clean-light": true, "broadcast": true,
		"classic": true, "sm-theme": true,
	}
	if !validThemes[theme] {
		writeError(w, http.StatusBadRequest, "invalid_theme")
		return
	}

	// Update user's theme preference
	result := a.db.WithContext(r.Context()).Model(&models.User{}).
		Where("id = ?", claims.UserID).
		Update("theme", theme)
	if result.Error != nil {
		a.logger.Error().Err(result.Error).Msg("failed to update theme preference")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleMediaUpload(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := r.ParseMultipartForm(128 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_multipart")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file_required")
		return
	}
	defer file.Close()

	stationID := r.FormValue("station_id")
	if stationID == "" {
		stationID = claims.StationID
	}
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	title := r.FormValue("title")
	artist := r.FormValue("artist")
	album := r.FormValue("album")
	durationStr := r.FormValue("duration_seconds")

	var duration time.Duration
	if durationStr != "" {
		val, err := strconv.ParseFloat(durationStr, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_duration")
			return
		}
		duration = time.Duration(val * float64(time.Second))
	}

	mediaID := uuid.NewString()

	storedPath, err := a.media.Store(r.Context(), stationID, mediaID, file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "media_store_failed")
		return
	}
	success := false
	defer func() {
		if !success && storedPath != "" {
			_ = os.Remove(storedPath)
		}
	}()

	item := models.MediaItem{
		ID:            mediaID,
		StationID:     stationID,
		Title:         title,
		Artist:        artist,
		Album:         album,
		Duration:      duration,
		Path:          storedPath,
		AnalysisState: models.AnalysisPending,
	}

	if err := a.db.WithContext(r.Context()).Create(&item).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	jobID, err := a.analyzer.Enqueue(r.Context(), mediaID)
	if err != nil {
		a.db.WithContext(r.Context()).Model(&models.MediaItem{}).Where("id = ?", mediaID).Update("analysis_state", models.AnalysisFailed)
		writeError(w, http.StatusInternalServerError, "analysis_queue_error")
		return
	}

	success = true

	resp := map[string]any{
		"analysis_job_id":  jobID,
		"id":               item.ID,
		"station_id":       item.StationID,
		"title":            item.Title,
		"artist":           item.Artist,
		"album":            item.Album,
		"duration_seconds": item.Duration.Seconds(),
		"analysis_state":   item.AnalysisState,
		"filename":         header.Filename,
	}

	writeJSON(w, http.StatusCreated, resp)
}
func (a *API) handleMountsList(w http.ResponseWriter, r *http.Request) {
	stationID := chi.URLParam(r, "stationID")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	var mounts []models.Mount
	if err := a.db.WithContext(r.Context()).Where("station_id = ?", stationID).Find(&mounts).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, mounts)
}

func (a *API) handleMountsCreate(w http.ResponseWriter, r *http.Request) {
	stationID := chi.URLParam(r, "stationID")
	var req mountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if stationID == "" {
		stationID = req.StationID
	}
	if stationID == "" || req.Name == "" || req.URL == "" || req.Format == "" {
		writeError(w, http.StatusBadRequest, "missing_required_fields")
		return
	}

	mount := models.Mount{
		ID:              uuid.NewString(),
		StationID:       stationID,
		Name:            req.Name,
		URL:             req.URL,
		Format:          req.Format,
		Bitrate:         req.Bitrate,
		Channels:        req.Channels,
		SampleRate:      req.SampleRate,
		EncoderPresetID: nil, // Nullable - no preset selected
	}
	if err := a.db.WithContext(r.Context()).Create(&mount).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusCreated, mount)
}

func (a *API) handleMediaGet(w http.ResponseWriter, r *http.Request) {
	mediaID := chi.URLParam(r, "mediaID")
	if mediaID == "" {
		writeError(w, http.StatusBadRequest, "media_id_required")
		return
	}

	var item models.MediaItem
	result := a.db.WithContext(r.Context()).Preload("Tags").First(&item, "id = ?", mediaID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, item)
}

func (a *API) handlePlaylistsList(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	query := a.db.WithContext(r.Context())
	if stationID != "" {
		query = query.Where("station_id = ?", stationID)
	}
	var playlists []models.Playlist
	if err := query.Order("name ASC").Find(&playlists).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"playlists": playlists})
}

func (a *API) handleSmartBlocksList(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	query := a.db.WithContext(r.Context())
	if stationID != "" {
		query = query.Where("station_id = ?", stationID)
	}
	var blocks []models.SmartBlock
	if err := query.Order("name ASC").Find(&blocks).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"smart_blocks": blocks})
}

func (a *API) handleSmartBlocksCreate(w http.ResponseWriter, r *http.Request) {
	var req smartBlockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.StationID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "missing_required_fields")
		return
	}

	block := models.SmartBlock{
		ID:          uuid.NewString(),
		StationID:   req.StationID,
		Name:        req.Name,
		Description: req.Description,
		Rules:       req.Rules,
		Sequence:    req.Sequence,
	}

	if err := a.db.WithContext(r.Context()).Create(&block).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusCreated, block)
}

func (a *API) handleSmartBlockMaterialize(w http.ResponseWriter, r *http.Request) {
	blockID := chi.URLParam(r, "blockID")
	if blockID == "" {
		writeError(w, http.StatusBadRequest, "block_id_required")
		return
	}

	var req smartBlockMaterializeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.StationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}
	if req.Seed == 0 {
		req.Seed = time.Now().UnixNano()
	}
	if req.DurationMS == 0 {
		req.DurationMS = 15 * 60 * 1000
	}

	result, err := a.scheduler.Materialize(r.Context(), smartblock.GenerateRequest{
		SmartBlockID: blockID,
		Seed:         req.Seed,
		Duration:     req.DurationMS,
		StationID:    req.StationID,
		MountID:      req.MountID,
	})
	if err != nil {
		if errors.Is(err, smartblock.ErrUnresolved) {
			writeError(w, http.StatusConflict, "unresolved")
		} else {
			writeError(w, http.StatusInternalServerError, "materialize_failed")
		}
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (a *API) handleClocksList(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	query := a.db.WithContext(r.Context())
	if stationID != "" {
		query = query.Where("station_id = ?", stationID)
	}

	var clocks []models.ClockHour
	if err := query.Preload("Slots").Order("name ASC").Find(&clocks).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"clocks": clocks})
}

func (a *API) handleClocksCreate(w http.ResponseWriter, r *http.Request) {
	var req clockCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.StationID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "missing_required_fields")
		return
	}

	clockID := uuid.NewString()
	slots := make([]models.ClockSlot, 0, len(req.Slots))
	for _, slotReq := range req.Slots {
		slots = append(slots, models.ClockSlot{
			ID:          uuid.NewString(),
			ClockHourID: clockID,
			Position:    slotReq.Position,
			Offset:      time.Duration(slotReq.OffsetMS) * time.Millisecond,
			Type:        models.ClockSlotType(slotReq.Type),
			Payload:     slotReq.Payload,
		})
	}

	clock := models.ClockHour{
		ID:        clockID,
		StationID: req.StationID,
		Name:      req.Name,
		Slots:     slots,
	}

	if err := a.db.WithContext(r.Context()).Create(&clock).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusCreated, clock)
}

func (a *API) handleClockSimulate(w http.ResponseWriter, r *http.Request) {
	clockID := chi.URLParam(r, "clockID")
	if clockID == "" {
		writeError(w, http.StatusBadRequest, "clock_id_required")
		return
	}

	horizonMinutes := 60
	if v := r.URL.Query().Get("minutes"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			horizonMinutes = parsed
		}
	}

	plans, err := a.scheduler.SimulateClock(r.Context(), clockID, time.Now(), time.Duration(horizonMinutes)*time.Minute)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "simulate_failed")
		return
	}

	writeJSON(w, http.StatusOK, plans)
}

func (a *API) handleScheduleList(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	hours := 6
	if v := r.URL.Query().Get("hours"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			hours = parsed
		}
	}

	entries, err := a.scheduler.Upcoming(r.Context(), stationID, time.Now(), time.Duration(hours)*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, entries)
}

func (a *API) handleScheduleRefresh(w http.ResponseWriter, r *http.Request) {
	var req scheduleRefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.StationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	if err := a.scheduler.RefreshStation(r.Context(), req.StationID); err != nil {
		writeError(w, http.StatusInternalServerError, "refresh_failed")
		return
	}

	// Publish audit event
	a.publishAuditEvent(r, events.EventAuditScheduleRefresh, events.Payload{
		"station_id":    req.StationID,
		"resource_type": "schedule",
	})

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "refresh_queued"})
}
func (a *API) handleScheduleUpdate(w http.ResponseWriter, r *http.Request) {
	entryID := chi.URLParam(r, "entryID")
	if entryID == "" {
		writeError(w, http.StatusBadRequest, "entry_id_required")
		return
	}

	var req scheduleUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	var entry models.ScheduleEntry
	result := a.db.WithContext(r.Context()).First(&entry, "id = ?", entryID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	originalDuration := entry.EndsAt.Sub(entry.StartsAt)

	updates := map[string]any{}

	if req.StartsAt != "" {
		startsAt, err := time.Parse(time.RFC3339, req.StartsAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_starts_at")
			return
		}
		entry.StartsAt = startsAt
		updates["starts_at"] = startsAt

		if req.EndsAt == "" {
			entry.EndsAt = startsAt.Add(originalDuration)
			updates["ends_at"] = entry.EndsAt
		}
	}

	if req.EndsAt != "" {
		endsAt, err := time.Parse(time.RFC3339, req.EndsAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_ends_at")
			return
		}
		entry.EndsAt = endsAt
		updates["ends_at"] = endsAt
	}

	if req.StartsAt == "" && req.EndsAt == "" {
		// no time change, keep as-is
	}

	if req.MountID != "" {
		entry.MountID = req.MountID
		updates["mount_id"] = req.MountID
	}

	if req.Metadata != nil {
		entry.Metadata = req.Metadata
		updates["metadata"] = req.Metadata
	}

	if entry.Metadata == nil {
		entry.Metadata = map[string]any{}
	}

	if len(updates) == 0 {
		writeJSON(w, http.StatusOK, entry)
		return
	}

	if err := a.db.WithContext(r.Context()).Model(&models.ScheduleEntry{}).Where("id = ?", entry.ID).Updates(updates).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed")
		return
	}

	entryMap := events.Payload{
		"entry_id":    entry.ID,
		"station_id":  entry.StationID,
		"mount_id":    entry.MountID,
		"starts_at":   entry.StartsAt,
		"ends_at":     entry.EndsAt,
		"source_type": entry.SourceType,
		"metadata":    entry.Metadata,
	}
	for k, v := range entry.Metadata {
		entryMap[k] = v
	}
	a.bus.Publish(events.EventScheduleUpdate, entryMap)

	writeJSON(w, http.StatusOK, entry)
}

func (a *API) handleLiveHandover(w http.ResponseWriter, r *http.Request) {
	var req liveHandoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.StationID == "" || req.MountID == "" {
		writeError(w, http.StatusBadRequest, "station_and_mount_required")
		return
	}

	a.bus.Publish(events.EventDJConnect, events.Payload{
		"station_id": req.StationID,
		"mount_id":   req.MountID,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "handover_initiated"})
}

func (a *API) handlePlayoutReload(w http.ResponseWriter, r *http.Request) {
	var req playoutControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.MountID == "" || req.Launch == "" {
		writeError(w, http.StatusBadRequest, "mount_and_launch_required")
		return
	}

	if err := a.playout.StopPipeline(req.MountID); err != nil {
		a.logger.Warn().Err(err).Str("mount", req.MountID).Msg("stop pipeline failed")
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if err := a.playout.EnsurePipeline(ctx, req.MountID, req.Launch); err != nil {
		writeError(w, http.StatusInternalServerError, "pipeline_start_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
}

func (a *API) handlePlayoutSkip(w http.ResponseWriter, r *http.Request) {
	var req playoutControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.MountID == "" {
		writeError(w, http.StatusBadRequest, "mount_id_required")
		return
	}

	if err := a.playout.StopPipeline(req.MountID); err != nil {
		a.logger.Warn().Err(err).Str("mount", req.MountID).Msg("skip stop failed")
	}
	a.bus.Publish(events.EventNowPlaying, events.Payload{
		"mount_id":   req.MountID,
		"station_id": req.StationID,
		"skipped":    true,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "skipped"})
}

func (a *API) handlePlayoutStop(w http.ResponseWriter, r *http.Request) {
	var req playoutControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.MountID == "" {
		writeError(w, http.StatusBadRequest, "mount_id_required")
		return
	}

	if err := a.playout.StopPipeline(req.MountID); err != nil {
		writeError(w, http.StatusInternalServerError, "stop_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (a *API) handleAnalyticsNowPlaying(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	var history models.PlayHistory
	result := a.db.WithContext(r.Context()).Where("station_id = ?", stationID).Order("started_at DESC").First(&history)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "idle"})
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Determine if track is currently playing or has ended
	now := time.Now()
	status := "idle"
	if history.EndedAt.IsZero() || history.EndedAt.After(now) {
		status = "playing"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         history.ID,
		"station_id": history.StationID,
		"mount_id":   history.MountID,
		"media_id":   history.MediaID,
		"artist":     history.Artist,
		"title":      history.Title,
		"album":      history.Album,
		"started_at": history.StartedAt,
		"ended_at":   history.EndedAt,
		"status":     status,
	})
}

func (a *API) handleAnalyticsListeners(w http.ResponseWriter, r *http.Request) {
	if a.broadcast == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"total":  0,
			"mounts": []broadcast.MountStats{},
		})
		return
	}

	stats := a.broadcast.GetListenerStats()
	total := a.broadcast.TotalListeners()

	writeJSON(w, http.StatusOK, map[string]any{
		"total":  total,
		"mounts": stats,
	})
}

// handlePublicStations returns the list of public, approved, active stations with their mounts.
// Used for the global player station selector.
func (a *API) handlePublicStations(w http.ResponseWriter, r *http.Request) {
	var stations []models.Station
	a.db.WithContext(r.Context()).
		Where("active = ? AND public = ? AND approved = ?", true, true, true).
		Order("sort_order ASC, name ASC").
		Find(&stations)

	type mountInfo struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Format  string `json:"format"`
		Bitrate int    `json:"bitrate"`
		URL     string `json:"url"`
		LQURL   string `json:"lq_url"`
	}

	type stationInfo struct {
		ID          string      `json:"id"`
		Name        string      `json:"name"`
		Description string      `json:"description,omitempty"`
		Mounts      []mountInfo `json:"mounts"`
	}

	result := make([]stationInfo, 0, len(stations))
	for _, s := range stations {
		var mounts []models.Mount
		a.db.WithContext(r.Context()).Where("station_id = ?", s.ID).Find(&mounts)

		mountList := make([]mountInfo, 0, len(mounts))
		for _, m := range mounts {
			mountList = append(mountList, mountInfo{
				ID:      m.ID,
				Name:    m.Name,
				Format:  m.Format,
				Bitrate: m.Bitrate,
				URL:     "/live/" + m.Name,
				LQURL:   "/live/" + m.Name + "-lq",
			})
		}

		result = append(result, stationInfo{
			ID:          s.ID,
			Name:        s.Name,
			Description: s.Description,
			Mounts:      mountList,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

func (a *API) handleAnalyticsSpins(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	sinceStr := r.URL.Query().Get("since")
	since := time.Now().Add(-30 * 24 * time.Hour)
	if sinceStr != "" {
		if parsed, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			since = parsed
		}
	}

	type spinRow struct {
		Artist string `json:"artist"`
		Title  string `json:"title"`
		Count  int    `json:"count"`
	}

	var rows []spinRow
	if err := a.db.WithContext(r.Context()).Model(&models.PlayHistory{}).
		Select("artist, title, COUNT(*) as count").
		Where("station_id = ?", stationID).
		Where("started_at >= ?", since).
		Group("artist, title").
		Order("count DESC").
		Scan(&rows).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, rows)
}

func (a *API) handleWebhookTrackStart(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	a.bus.Publish(events.EventNowPlaying, payload)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "received"})
}

func (a *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	conn, err := ws.Accept(w, r, &ws.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		a.logger.Error().Err(err).Msg("websocket accept failed")
		return
	}
	defer conn.Close(ws.StatusInternalError, "server error")

	// Track WebSocket connection
	telemetry.APIWebSocketConnections.Inc()
	defer telemetry.APIWebSocketConnections.Dec()

	eventTypes := parseEventTypes(r.URL.Query().Get("types"))
	if len(eventTypes) == 0 {
		eventTypes = []events.EventType{events.EventNowPlaying, events.EventHealth}
	}

	subscribers := make([]events.Subscriber, 0, len(eventTypes))
	for _, eventType := range eventTypes {
		subscribers = append(subscribers, a.bus.Subscribe(eventType))
	}
	defer func() {
		for i, eventType := range eventTypes {
			a.bus.Unsubscribe(eventType, subscribers[i])
		}
	}()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			conn.Close(ws.StatusNormalClosure, "context cancelled")
			return
		case <-ticker.C:
			if err := conn.Write(ctx, ws.MessageText, []byte(`{"type":"ping"}`)); err != nil {
				a.logger.Error().Err(err).Msg("websocket ping failed")
				conn.Close(ws.StatusInternalError, "write failed")
				return
			}
		default:
			sent := false
			for i, sub := range subscribers {
				select {
				case payload := <-sub:
					if err := a.writeEvent(ctx, conn, eventTypes[i], payload); err != nil {
						a.logger.Error().Err(err).Msg("websocket write failed")
						conn.Close(ws.StatusInternalError, "write failed")
						return
					}
					sent = true
				default:
				}
			}
			if !sent {
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
}

func (a *API) writeEvent(ctx context.Context, conn *ws.Conn, eventType events.EventType, payload events.Payload) error {
	data := map[string]any{
		"type":    eventType,
		"payload": payload,
	}
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return conn.Write(ctx, ws.MessageText, bytes)
}

func (a *API) authMiddleware() func(http.Handler) http.Handler {
	return auth.MiddlewareWithJWT(a.db, a.jwtSecret)
}

func (a *API) requireRoles(allowed ...models.RoleName) func(http.Handler) http.Handler {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, role := range allowed {
		allowedSet[string(role)] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			for _, role := range claims.Roles {
				if _, exists := allowedSet[role]; exists {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeError(w, http.StatusForbidden, "insufficient_role")
		})
	}
}

func (a *API) requirePlatformAdmin() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			for _, role := range claims.Roles {
				if role == string(models.PlatformRoleAdmin) {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeError(w, http.StatusForbidden, "platform_admin_required")
		})
	}
}

func (a *API) notImplemented(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not_implemented")
}

// SystemStatus represents the overall system health status.
type SystemStatus struct {
	Database    ComponentStatus `json:"database"`
	MediaEngine ComponentStatus `json:"media_engine"`
	Storage     ComponentStatus `json:"storage"`
	Timestamp   time.Time       `json:"timestamp"`
}

// ComponentStatus represents the status of a single system component.
type ComponentStatus struct {
	Status  string `json:"status"` // "ok", "error", "unavailable"
	Message string `json:"message,omitempty"`
	Address string `json:"address,omitempty"`
}

func (a *API) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	status := SystemStatus{
		Timestamp: time.Now(),
	}

	// Check database connection
	sqlDB, err := a.db.DB()
	if err != nil {
		status.Database = ComponentStatus{Status: "error", Message: err.Error()}
	} else if err := sqlDB.PingContext(ctx); err != nil {
		status.Database = ComponentStatus{Status: "error", Message: err.Error()}
	} else {
		status.Database = ComponentStatus{Status: "ok", Message: "Connected"}
	}

	// Check media engine connection
	if a.analyzer != nil {
		meStatus := a.analyzer.GetMediaEngineStatus(ctx)
		if !meStatus.Configured {
			status.MediaEngine = ComponentStatus{
				Status:  "unavailable",
				Message: "Not configured",
			}
		} else if meStatus.Connected {
			status.MediaEngine = ComponentStatus{
				Status:  "ok",
				Message: "Connected",
				Address: meStatus.Address,
			}
		} else {
			status.MediaEngine = ComponentStatus{
				Status:  "error",
				Message: meStatus.Error,
				Address: meStatus.Address,
			}
		}
	} else {
		status.MediaEngine = ComponentStatus{
			Status:  "unavailable",
			Message: "Analyzer service not available",
		}
	}

	// Check storage access
	if a.media != nil {
		if err := a.media.CheckStorageAccess(); err != nil {
			status.Storage = ComponentStatus{
				Status:  "error",
				Message: err.Error(),
			}
		} else {
			status.Storage = ComponentStatus{
				Status:  "ok",
				Message: "Accessible",
			}
		}
	} else {
		status.Storage = ComponentStatus{
			Status:  "unavailable",
			Message: "Media service not available",
		}
	}

	writeJSON(w, http.StatusOK, status)
}

func (a *API) handleTestMediaEngine(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if a.analyzer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"success": false,
			"error":   "Analyzer service not available",
		})
		return
	}

	err := a.analyzer.TestMediaEngine(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Media engine connection successful",
	})
}

func (a *API) handleReanalyzeMissingArtwork(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if a.analyzer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"success": false,
			"error":   "Analyzer service not available",
		})
		return
	}

	// Find media items without artwork that haven't been analyzed or need re-analysis
	var mediaIDs []string
	err := a.db.WithContext(ctx).
		Model(&models.MediaItem{}).
		Select("id").
		Where("(artwork_mime IS NULL OR artwork_mime = '') AND (analysis_state IS NULL OR analysis_state != ?)", models.AnalysisPending).
		Pluck("id", &mediaIDs).Error
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Queue each item for analysis
	queued := 0
	for _, mediaID := range mediaIDs {
		// Check if already in queue
		var existingJob models.AnalysisJob
		err := a.db.WithContext(ctx).Where("media_id = ? AND status IN ?", mediaID, []string{"pending", "running"}).First(&existingJob).Error
		if err == nil {
			continue // Already queued
		}

		// Update analysis state and queue
		a.db.WithContext(ctx).Model(&models.MediaItem{}).Where("id = ?", mediaID).Update("analysis_state", models.AnalysisPending)
		if _, err := a.analyzer.Enqueue(ctx, mediaID); err != nil {
			a.logger.Warn().Err(err).Str("media_id", mediaID).Msg("failed to queue media for re-analysis")
			continue
		}
		queued++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"total_found":  len(mediaIDs),
		"queued":       queued,
		"already_done": len(mediaIDs) - queued,
	})
}

func (a *API) handleSystemLogs(w http.ResponseWriter, r *http.Request) {
	if a.logBuffer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "Log buffer not available",
		})
		return
	}

	// Parse query parameters
	params := logbuffer.QueryParams{
		Level:      r.URL.Query().Get("level"),
		Component:  r.URL.Query().Get("component"),
		Search:     r.URL.Query().Get("search"),
		Descending: true, // Default to newest first
	}

	if since := r.URL.Query().Get("since"); since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			params.Since = t
		}
	}

	if limit := r.URL.Query().Get("limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil && n > 0 {
			params.Limit = n
		}
	} else {
		params.Limit = 500 // Default limit
	}

	if order := r.URL.Query().Get("order"); order == "asc" {
		params.Descending = false
	}

	entries := a.logBuffer.Query(params)

	// Collect unique station IDs and fetch their names
	stationIDs := make(map[string]bool)
	for _, entry := range entries {
		if sid, ok := entry.Fields["station_id"].(string); ok && sid != "" {
			stationIDs[sid] = true
		}
	}

	stationNames := make(map[string]string)
	if len(stationIDs) > 0 {
		ids := make([]string, 0, len(stationIDs))
		for id := range stationIDs {
			ids = append(ids, id)
		}
		var stations []models.Station
		a.db.Select("id", "name").Where("id IN ?", ids).Find(&stations)
		for _, s := range stations {
			stationNames[s.ID] = s.Name
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entries":       entries,
		"count":         len(entries),
		"station_names": stationNames,
	})
}

func (a *API) handleLogComponents(w http.ResponseWriter, r *http.Request) {
	if a.logBuffer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "Log buffer not available",
		})
		return
	}

	components := a.logBuffer.GetComponents()
	writeJSON(w, http.StatusOK, map[string]any{
		"components": components,
	})
}

func (a *API) handleLogStats(w http.ResponseWriter, r *http.Request) {
	if a.logBuffer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "Log buffer not available",
		})
		return
	}

	stats := a.logBuffer.Stats()
	writeJSON(w, http.StatusOK, stats)
}

func (a *API) handleClearLogs(w http.ResponseWriter, r *http.Request) {
	if a.logBuffer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "Log buffer not available",
		})
		return
	}

	a.logBuffer.Clear()
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Log buffer cleared",
	})
}

// Station-scoped log handlers

func (a *API) handleStationLogs(w http.ResponseWriter, r *http.Request) {
	if a.logBuffer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "Log buffer not available",
		})
		return
	}

	stationID := chi.URLParam(r, "stationID")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	// Parse query parameters
	params := logbuffer.QueryParams{
		StationID:  stationID,
		Level:      r.URL.Query().Get("level"),
		Component:  r.URL.Query().Get("component"),
		Search:     r.URL.Query().Get("search"),
		Descending: true, // Default to newest first
	}

	if since := r.URL.Query().Get("since"); since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			params.Since = t
		}
	}

	if limit := r.URL.Query().Get("limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil && n > 0 {
			params.Limit = n
		}
	} else {
		params.Limit = 500 // Default limit
	}

	if order := r.URL.Query().Get("order"); order == "asc" {
		params.Descending = false
	}

	entries := a.logBuffer.Query(params)
	writeJSON(w, http.StatusOK, map[string]any{
		"entries":    entries,
		"count":      len(entries),
		"station_id": stationID,
	})
}

func (a *API) handleStationLogComponents(w http.ResponseWriter, r *http.Request) {
	if a.logBuffer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "Log buffer not available",
		})
		return
	}

	stationID := chi.URLParam(r, "stationID")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	components := a.logBuffer.GetComponentsForStation(stationID)
	writeJSON(w, http.StatusOK, map[string]any{
		"components": components,
		"station_id": stationID,
	})
}

func (a *API) handleStationLogStats(w http.ResponseWriter, r *http.Request) {
	if a.logBuffer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "Log buffer not available",
		})
		return
	}

	stationID := chi.URLParam(r, "stationID")
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id required")
		return
	}

	stats := a.logBuffer.StatsForStation(stationID)
	writeJSON(w, http.StatusOK, stats)
}

func parseEventTypes(raw string) []events.EventType {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]events.EventType, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, events.EventType(strings.TrimSpace(part)))
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

// auditContext extracts user and request info for audit logging.
func (a *API) auditContext(r *http.Request) events.Payload {
	payload := events.Payload{
		"ip_address": r.RemoteAddr,
		"user_agent": r.UserAgent(),
	}

	// Extract user info from JWT claims
	if claims, ok := auth.ClaimsFromContext(r.Context()); ok && claims != nil {
		payload["user_id"] = claims.UserID

		// Try to get user email from database
		var user models.User
		if err := a.db.Select("email").First(&user, "id = ?", claims.UserID).Error; err == nil {
			payload["user_email"] = user.Email
		}
	}

	return payload
}

// publishAuditEvent publishes an audit event with user and request context.
func (a *API) publishAuditEvent(r *http.Request, eventType events.EventType, data events.Payload) {
	payload := a.auditContext(r)
	for k, v := range data {
		payload[k] = v
	}
	a.bus.Publish(eventType, payload)
}

// WebDJ API handler delegates

func (a *API) handleWebDJStartSession(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleStartSession(w, r)
}

func (a *API) handleWebDJListSessions(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleListSessions(w, r)
}

func (a *API) handleWebDJGetSession(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleGetSession(w, r)
}

func (a *API) handleWebDJEndSession(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleEndSession(w, r)
}

func (a *API) handleWebDJWebSocket(w http.ResponseWriter, r *http.Request) {
	if a.webdjWS == nil {
		http.Error(w, "webdj_not_available", http.StatusServiceUnavailable)
		return
	}
	a.webdjWS.HandleWebSocket(w, r)
}

func (a *API) handleWebDJLoadTrack(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleLoadTrack(w, r)
}

func (a *API) handleWebDJPlay(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handlePlay(w, r)
}

func (a *API) handleWebDJPause(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handlePause(w, r)
}

func (a *API) handleWebDJSeek(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleSeek(w, r)
}

func (a *API) handleWebDJSetCue(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleSetCue(w, r)
}

func (a *API) handleWebDJDeleteCue(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleDeleteCue(w, r)
}

func (a *API) handleWebDJEject(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleEject(w, r)
}

func (a *API) handleWebDJSetVolume(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleSetVolume(w, r)
}

func (a *API) handleWebDJSetEQ(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleSetEQ(w, r)
}

func (a *API) handleWebDJSetPitch(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleSetPitch(w, r)
}

func (a *API) handleWebDJSetCrossfader(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleSetCrossfader(w, r)
}

func (a *API) handleWebDJSetMasterVolume(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleSetMasterVolume(w, r)
}

func (a *API) handleWebDJGoLive(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleGoLive(w, r)
}

func (a *API) handleWebDJGoOffAir(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleGoOffAir(w, r)
}

func (a *API) handleWebDJGetWaveform(w http.ResponseWriter, r *http.Request) {
	if a.webdjAPI == nil {
		writeError(w, http.StatusServiceUnavailable, "webdj_not_available")
		return
	}
	a.webdjAPI.handleGetWaveform(w, r)
}
