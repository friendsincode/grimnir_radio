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
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

    "github.com/friendsincode/grimnir_radio/internal/analyzer"
    "github.com/friendsincode/grimnir_radio/internal/auth"
    "github.com/friendsincode/grimnir_radio/internal/events"
    "github.com/friendsincode/grimnir_radio/internal/executor"
    "github.com/friendsincode/grimnir_radio/internal/live"
    "github.com/friendsincode/grimnir_radio/internal/media"
    "github.com/friendsincode/grimnir_radio/internal/models"
    "github.com/friendsincode/grimnir_radio/internal/playout"
    "github.com/friendsincode/grimnir_radio/internal/priority"
    "github.com/friendsincode/grimnir_radio/internal/scheduler"
    "github.com/friendsincode/grimnir_radio/internal/smartblock"
    "github.com/friendsincode/grimnir_radio/internal/webstream"
	ws "nhooyr.io/websocket"
)

// API exposes HTTP handlers.
type API struct {
	db               *gorm.DB
	scheduler        *scheduler.Service
	analyzer         *analyzer.Service
	media            *media.Service
	live             *live.Service
	webstreamSvc     *webstream.Service
	playout          *playout.Manager
	prioritySvc      *priority.Service
	executorStateMgr *executor.StateManager
	migrationHandler *MigrationHandler
	bus              *events.Bus
	logger           zerolog.Logger
	jwtSecret        []byte
}

// New creates the API router wrapper.
func New(db *gorm.DB, scheduler *scheduler.Service, analyzer *analyzer.Service, media *media.Service, live *live.Service, webstreamSvc *webstream.Service, playout *playout.Manager, prioritySvc *priority.Service, executorStateMgr *executor.StateManager, bus *events.Bus, logger zerolog.Logger, jwtSecret []byte) *API {
	migrationHandler := NewMigrationHandler(db, logger)

	return &API{
		db:               db,
		scheduler:        scheduler,
		analyzer:         analyzer,
		media:            media,
		live:             live,
		webstreamSvc:     webstreamSvc,
		playout:          playout,
		prioritySvc:      prioritySvc,
		executorStateMgr: executorStateMgr,
		migrationHandler: migrationHandler,
		bus:              bus,
		logger:           logger,
		jwtSecret:        jwtSecret,
	}
}

type loginRequest struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	StationID string `json:"station_id"`
}

const accessTokenTTL = 15 * time.Minute

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
		r.Post("/auth/login", a.handleAuthLogin)

		r.Group(func(pr chi.Router) {
			pr.Use(a.authMiddleware())

			pr.Post("/auth/refresh", a.handleAuthRefresh)

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
				})
			})

			pr.Route("/media", func(r chi.Router) {
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager, models.RoleDJ)).Post("/upload", a.handleMediaUpload)
				r.Get("/{mediaID}", a.handleMediaGet)
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

				// Legacy handover endpoint (deprecated, kept for compatibility)
				r.With(a.requireRoles(models.RoleAdmin, models.RoleManager)).Post("/handover", a.handleLiveHandover)
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

			pr.Route("/webhooks", func(r chi.Router) {
				r.With(a.requireRoles(models.RoleAdmin)).Post("/track-start", a.handleWebhookTrackStart)
			})

			// Priority management routes
			a.AddPriorityRoutes(pr)

			// Executor state routes
			a.AddExecutorRoutes(pr)

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

func (a *API) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "credentials_required")
		return
	}

	var user models.User
	result := a.db.WithContext(r.Context()).Where("email = ?", strings.ToLower(req.Email)).First(&user)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}

	claims := auth.Claims{
		UserID:    user.ID,
		Roles:     []string{string(user.Role)},
		StationID: req.StationID,
	}
	token, err := auth.Issue(a.jwtSecret, claims, accessTokenTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_issue_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": token,
		"expires_in":   int(accessTokenTTL.Seconds()),
	})
}

func (a *API) handleAuthRefresh(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	token, err := auth.Issue(a.jwtSecret, *claims, accessTokenTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_issue_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": token,
		"expires_in":   int(accessTokenTTL.Seconds()),
	})
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

	if err := a.db.WithContext(r.Context()).Create(&station).Error; err != nil {
		a.logger.Error().Err(err).Msg("create station failed")
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

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
		EncoderPresetID: "",
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

func (a *API) handleSmartBlocksList(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	query := a.db.WithContext(r.Context())
	if stationID != "" {
		query = query.Where("station_id = ?", stationID)
	}
	var blocks []models.SmartBlock
	if err := query.Find(&blocks).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	writeJSON(w, http.StatusOK, blocks)
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
	if stationID == "" {
		writeError(w, http.StatusBadRequest, "station_id_required")
		return
	}

	var clocks []models.ClockHour
	if err := a.db.WithContext(r.Context()).Where("station_id = ?", stationID).Preload("Slots").Find(&clocks).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, clocks)
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
		writeJSON(w, http.StatusOK, map[string]string{})
		return
	}
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, history)
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
	return auth.Middleware(a.jwtSecret)
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

func (a *API) notImplemented(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not_implemented")
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
