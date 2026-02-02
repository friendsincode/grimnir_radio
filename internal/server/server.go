/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/analyzer"
	"github.com/friendsincode/grimnir_radio/internal/api"
	"github.com/friendsincode/grimnir_radio/internal/audit"
	"github.com/friendsincode/grimnir_radio/internal/broadcast"
	"github.com/friendsincode/grimnir_radio/internal/cache"
	"github.com/friendsincode/grimnir_radio/internal/clock"
	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/friendsincode/grimnir_radio/internal/db"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/executor"
	"github.com/friendsincode/grimnir_radio/internal/leadership"
	"github.com/friendsincode/grimnir_radio/internal/live"
	"github.com/friendsincode/grimnir_radio/internal/logbuffer"
	"github.com/friendsincode/grimnir_radio/internal/media"
	"github.com/friendsincode/grimnir_radio/internal/notifications"
	"github.com/friendsincode/grimnir_radio/internal/playout"
	"github.com/friendsincode/grimnir_radio/internal/priority"
	"github.com/friendsincode/grimnir_radio/internal/scheduler"
	schedulerstate "github.com/friendsincode/grimnir_radio/internal/scheduler/state"
	"github.com/friendsincode/grimnir_radio/internal/smartblock"
	"github.com/friendsincode/grimnir_radio/internal/telemetry"
	"github.com/friendsincode/grimnir_radio/internal/web"
	"github.com/friendsincode/grimnir_radio/internal/webrtc"
	"github.com/friendsincode/grimnir_radio/internal/webstream"
)

// Server bundles HTTP and supporting services.
type Server struct {
	cfg        *config.Config
	logger     zerolog.Logger
	router     chi.Router
	httpServer *http.Server
	closers    []func() error

	db                   *gorm.DB
	cache                *cache.Cache
	logBuffer            *logbuffer.Buffer
	api                  *api.API
	webHandler           *web.Handler
	scheduler            *scheduler.Service
	leaderAwareScheduler *scheduler.LeaderAwareScheduler
	analyzer             *analyzer.Service
	playout              *playout.Manager
	director             *playout.Director
	bus                  *events.Bus
	auditSvc             *audit.Service
	notificationSvc      *notifications.Service
	webrtcBroadcaster    *webrtc.Broadcaster

	bgCancel context.CancelFunc
	bgWG     sync.WaitGroup
}

// New constructs the server and wires dependencies.
func New(cfg *config.Config, logBuf *logbuffer.Buffer, logger zerolog.Logger) (*Server, error) {
	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(telemetry.TracingMiddleware("grimnir-radio-api")) // Add OpenTelemetry tracing
	router.Use(telemetry.MetricsMiddleware)                      // Add Prometheus metrics
	// Skip timeout for WebSocket and streaming connections
	router.Use(func(next http.Handler) http.Handler {
		timeout := middleware.Timeout(60 * time.Second)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip timeout middleware for WebSocket upgrade requests
			if r.Header.Get("Upgrade") == "websocket" {
				next.ServeHTTP(w, r)
				return
			}
			// Skip timeout for audio stream proxy (long-running connections)
			if len(r.URL.Path) >= 8 && r.URL.Path[:8] == "/stream/" {
				next.ServeHTTP(w, r)
				return
			}
			// Skip timeout for broadcast streams (long-running connections)
			if len(r.URL.Path) >= 6 && r.URL.Path[:6] == "/live/" {
				next.ServeHTTP(w, r)
				return
			}
			timeout(next).ServeHTTP(w, r)
		})
	})

	srv := &Server{
		cfg:       cfg,
		logger:    logger,
		router:    router,
		bus:       events.NewBus(),
		logBuffer: logBuf,
	}

	if err := srv.initDependencies(); err != nil {
		return nil, err
	}

	srv.configureRoutes()
	srv.startBackgroundWorkers()

	addr := fmt.Sprintf("%s:%d", cfg.HTTPBind, cfg.HTTPPort)
	srv.httpServer = &http.Server{
		Addr:        addr,
		Handler:     srv.router,
		ReadTimeout: 15 * time.Second,
		// WriteTimeout set to 0 for streaming support - handlers manage their own deadlines
		// The middleware timeout (60s) handles non-streaming routes
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
	}

	return srv, nil
}

func (s *Server) initDependencies() error {
	database, err := db.Connect(s.cfg)
	if err != nil {
		return err
	}
	if err := db.Migrate(database); err != nil {
		return err
	}
	s.db = database
	s.DeferClose(func() error { return db.Close(database) })

	// Ensure media directory exists
	if err := os.MkdirAll(s.cfg.MediaRoot, 0755); err != nil {
		return fmt.Errorf("failed to create media directory %s: %w", s.cfg.MediaRoot, err)
	}
	s.logger.Info().Str("path", s.cfg.MediaRoot).Msg("media directory ready")

	// Initialize Redis cache for reducing database load
	cacheCfg := cache.DefaultConfig()
	cacheCfg.RedisAddr = s.cfg.RedisAddr
	cacheCfg.RedisPassword = s.cfg.RedisPassword
	cacheCfg.RedisDB = s.cfg.RedisDB
	entityCache, err := cache.New(cacheCfg, s.logger)
	if err != nil {
		s.logger.Warn().Err(err).Msg("cache initialization failed, continuing without cache")
	} else {
		s.cache = entityCache
		s.DeferClose(func() error { return s.cache.Close() })
	}

	planner := clock.NewPlanner(database, s.logger)
	stateStore := schedulerstate.NewStore()
	blockEngine := smartblock.New(database, s.logger)
	s.scheduler = scheduler.New(database, planner, blockEngine, stateStore, s.cfg.SchedulerLookahead, s.logger)

	// Wire cache into scheduler to reduce database load
	if s.cache != nil {
		s.scheduler.SetCache(s.cache)
	}

	// Setup leader-aware scheduler if leader election is enabled
	if s.cfg.LeaderElectionEnabled {
		electionConfig := leadership.ElectionConfig{
			RedisAddr:       s.cfg.RedisAddr,
			RedisPassword:   s.cfg.RedisPassword,
			RedisDB:         s.cfg.RedisDB,
			ElectionKey:     "grimnir:leader:scheduler",
			LeaseDuration:   15 * time.Second,
			RenewalInterval: 5 * time.Second,
			RetryInterval:   2 * time.Second,
			InstanceID:      s.cfg.InstanceID,
		}

		election, err := leadership.NewElection(electionConfig, s.logger)
		if err != nil {
			return fmt.Errorf("create leader election: %w", err)
		}

		s.leaderAwareScheduler = scheduler.NewLeaderAware(s.scheduler, election, s.logger)
		s.DeferClose(func() error { return s.leaderAwareScheduler.Stop() })

		s.logger.Info().
			Str("redis_addr", s.cfg.RedisAddr).
			Str("instance_id", electionConfig.InstanceID).
			Msg("leader election enabled for scheduler")
	}

	analyzerCfg := analyzer.Config{
		MediaEngineGRPCAddr: s.cfg.MediaEngineGRPCAddr,
	}
	s.analyzer = analyzer.NewWithConfig(database, s.cfg.MediaRoot, s.logger, analyzerCfg)
	s.DeferClose(func() error { return s.analyzer.Close() })
	mediaService, err := media.NewService(s.cfg, s.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize media service: %w", err)
	}

	// Webstream service with health checking (created early for director dependency)
	webstreamService := webstream.NewService(database, s.bus, s.logger)
	s.DeferClose(func() error { return webstreamService.Shutdown() })

	// Broadcast server for streaming without Icecast
	broadcastSrv := broadcast.NewServer(s.logger, s.bus)

	// WebRTC broadcaster for low-latency streaming
	if s.cfg.WebRTCEnabled {
		webrtcCfg := webrtc.Config{
			RTPPort:      s.cfg.WebRTCRTPPort,
			STUNServer:   s.cfg.WebRTCSTUNURL,
			TURNServer:   s.cfg.WebRTCTURNURL,
			TURNUsername: s.cfg.WebRTCTURNUsername,
			TURNPassword: s.cfg.WebRTCTURNPassword,
		}
		var err error
		s.webrtcBroadcaster, err = webrtc.NewBroadcaster(webrtcCfg, s.logger)
		if err != nil {
			return fmt.Errorf("create webrtc broadcaster: %w", err)
		}
		s.DeferClose(func() error { return s.webrtcBroadcaster.Stop() })
		s.logger.Info().
			Int("rtp_port", s.cfg.WebRTCRTPPort).
			Bool("turn_enabled", s.cfg.WebRTCTURNURL != "").
			Msg("WebRTC broadcaster initialized")
	}

	s.playout = playout.NewManager(s.cfg, s.logger)
	s.director = playout.NewDirector(database, s.cfg, s.playout, s.bus, webstreamService, broadcastSrv, s.logger)

	// Priority and executor services (needed by live service)
	priorityService := priority.NewService(database, s.bus, s.logger)
	executorStateMgr := executor.NewStateManager(database, s.logger)

	// Live service depends on priority service
	liveService := live.NewService(database, priorityService, s.bus, s.logger)

	// Audit service for security logging
	s.auditSvc = audit.NewService(database, s.bus, s.logger)

	// Notification service for alerts and reminders
	notifCfg := notifications.ConfigFromEnv()
	s.notificationSvc = notifications.NewService(database, s.bus, notifCfg, s.logger)

	s.DeferClose(func() error { return s.playout.Shutdown() })

	s.api = api.New(s.db, s.scheduler, s.analyzer, mediaService, liveService, webstreamService, s.playout, priorityService, executorStateMgr, s.auditSvc, broadcastSrv, s.bus, s.logBuffer, s.logger)

	// Set notification API
	notificationAPI := api.NewNotificationAPI(s.notificationSvc)
	s.api.SetNotificationAPI(notificationAPI)

	// Web UI handler with WebRTC ICE server config for client
	webrtcCfg := web.WebRTCConfig{
		STUNURL:      s.cfg.WebRTCSTUNURL,
		TURNURL:      s.cfg.WebRTCTURNURL,
		TURNUsername: s.cfg.WebRTCTURNUsername,
		TURNPassword: s.cfg.WebRTCTURNPassword,
	}
	webHandler, err := web.NewHandler(database, []byte(s.cfg.JWTSigningKey), s.cfg.MediaRoot, mediaService, s.cfg.IcecastURL, s.cfg.IcecastPublicURL, webrtcCfg, s.bus, s.director, s.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize web handler: %w", err)
	}
	s.webHandler = webHandler

	return nil
}

// HTTPServer exposes the underlying net/http server.
func (s *Server) HTTPServer() *http.Server {
	return s.httpServer
}

// LogBuffer returns the server's log buffer for attaching to zerolog.
func (s *Server) LogBuffer() *logbuffer.Buffer {
	return s.logBuffer
}

// Close releases owned resources in reverse order.
func (s *Server) Close() error {
	s.stopBackgroundWorkers()
	var firstErr error
	for i := len(s.closers) - 1; i >= 0; i-- {
		if err := s.closers[i](); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// DeferClose registers a cleanup hook.
func (s *Server) DeferClose(fn func() error) {
	s.closers = append(s.closers, fn)
}

func (s *Server) startBackgroundWorkers() {
	if s.scheduler == nil && s.analyzer == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.bgCancel = cancel

	// Start scheduler (leader-aware if configured, otherwise direct)
	if s.leaderAwareScheduler != nil {
		// Leader-aware scheduler manages its own goroutines
		s.bgWG.Add(1)
		go func() {
			defer s.bgWG.Done()
			if err := s.leaderAwareScheduler.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.logger.Error().Err(err).Msg("leader-aware scheduler exited")
			}
		}()
	} else if s.scheduler != nil {
		// Direct scheduler (single instance mode)
		s.bgWG.Add(1)
		go func() {
			defer s.bgWG.Done()
			if err := s.scheduler.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.logger.Error().Err(err).Msg("scheduler loop exited")
			}
		}()
	}

	if s.analyzer != nil {
		s.bgWG.Add(1)
		go func() {
			defer s.bgWG.Done()
			if err := s.analyzer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.logger.Error().Err(err).Msg("analyzer loop exited")
			}
		}()
	}

	if s.director != nil {
		s.bgWG.Add(1)
		go func() {
			defer s.bgWG.Done()
			if err := s.director.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.logger.Error().Err(err).Msg("director loop exited")
			}
		}()
	}

	// Start database metrics updater
	if s.db != nil {
		s.bgWG.Add(1)
		go func() {
			defer s.bgWG.Done()
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					db.UpdateConnectionMetrics(s.db)
				}
			}
		}()
	}

	// Start WebRTC broadcaster
	if s.webrtcBroadcaster != nil {
		s.bgWG.Add(1)
		go func() {
			defer s.bgWG.Done()
			if err := s.webrtcBroadcaster.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.logger.Error().Err(err).Msg("WebRTC broadcaster failed to start")
			}
		}()
	}

	// Start audit service
	if s.auditSvc != nil {
		s.bgWG.Add(1)
		go func() {
			defer s.bgWG.Done()
			s.auditSvc.Start(ctx)
		}()
	}

	// Start notification service
	if s.notificationSvc != nil {
		s.bgWG.Add(1)
		go func() {
			defer s.bgWG.Done()
			s.notificationSvc.Start(ctx)
		}()
	}

	// Start version update checker
	if s.webHandler != nil {
		s.webHandler.StartUpdateChecker(ctx)
	}

	// Start cache invalidation listener
	if s.cache != nil {
		s.bgWG.Add(1)
		go func() {
			defer s.bgWG.Done()
			s.runCacheInvalidationListener(ctx)
		}()
	}
}

// runCacheInvalidationListener subscribes to cache events and invalidates cache accordingly.
func (s *Server) runCacheInvalidationListener(ctx context.Context) {
	// Subscribe to all cache invalidation events
	stationCreated := s.bus.Subscribe(events.EventStationCreated)
	stationUpdated := s.bus.Subscribe(events.EventStationUpdated)
	stationDeleted := s.bus.Subscribe(events.EventStationDeleted)
	mountCreated := s.bus.Subscribe(events.EventMountCreated)
	mountUpdated := s.bus.Subscribe(events.EventMountUpdated)
	mountDeleted := s.bus.Subscribe(events.EventMountDeleted)

	defer func() {
		s.bus.Unsubscribe(events.EventStationCreated, stationCreated)
		s.bus.Unsubscribe(events.EventStationUpdated, stationUpdated)
		s.bus.Unsubscribe(events.EventStationDeleted, stationDeleted)
		s.bus.Unsubscribe(events.EventMountCreated, mountCreated)
		s.bus.Unsubscribe(events.EventMountUpdated, mountUpdated)
		s.bus.Unsubscribe(events.EventMountDeleted, mountDeleted)
	}()

	s.logger.Info().Msg("cache invalidation listener started")

	for {
		select {
		case <-ctx.Done():
			s.logger.Info().Msg("cache invalidation listener stopped")
			return

		case payload := <-stationCreated:
			s.logger.Debug().Msg("invalidating station list cache (station created)")
			s.cache.InvalidateStationList(ctx)
			if stationID, ok := payload["station_id"].(string); ok {
				s.cache.InvalidateStation(ctx, stationID)
			}

		case payload := <-stationUpdated:
			s.logger.Debug().Msg("invalidating station list cache (station updated)")
			s.cache.InvalidateStationList(ctx)
			if stationID, ok := payload["station_id"].(string); ok {
				s.cache.InvalidateStation(ctx, stationID)
			}

		case payload := <-stationDeleted:
			s.logger.Debug().Msg("invalidating station list cache (station deleted)")
			s.cache.InvalidateStationList(ctx)
			if stationID, ok := payload["station_id"].(string); ok {
				s.cache.InvalidateStation(ctx, stationID)
			}

		case payload := <-mountCreated:
			if stationID, ok := payload["station_id"].(string); ok {
				s.logger.Debug().Str("station_id", stationID).Msg("invalidating mount cache (mount created)")
				s.cache.InvalidateMounts(ctx, stationID)
			}

		case payload := <-mountUpdated:
			mountID, _ := payload["mount_id"].(string)
			stationID, _ := payload["station_id"].(string)
			if mountID != "" && stationID != "" {
				s.logger.Debug().Str("mount_id", mountID).Msg("invalidating mount cache (mount updated)")
				s.cache.InvalidateMount(ctx, mountID, stationID)
			}

		case payload := <-mountDeleted:
			mountID, _ := payload["mount_id"].(string)
			stationID, _ := payload["station_id"].(string)
			if mountID != "" && stationID != "" {
				s.logger.Debug().Str("mount_id", mountID).Msg("invalidating mount cache (mount deleted)")
				s.cache.InvalidateMount(ctx, mountID, stationID)
			}
		}
	}
}

func (s *Server) stopBackgroundWorkers() {
	if s.bgCancel == nil {
		return
	}
	s.bgCancel()
	s.bgWG.Wait()
	s.bgCancel = nil
}

func (s *Server) configureRoutes() {
	s.router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		response := `{"status":"ok"`

		// Add leader status if leader election is enabled
		if s.leaderAwareScheduler != nil {
			isLeader := s.leaderAwareScheduler.IsLeader()
			if isLeader {
				response += `,"leader":true`
			} else {
				response += `,"leader":false`
			}
		}

		response += `}`
		_, _ = w.Write([]byte(response))
	})

	s.router.Handle("/metrics", telemetry.Handler())

	// Audio broadcast streams (served directly by Go, no Icecast needed)
	s.router.HandleFunc("/live/{mount}", func(w http.ResponseWriter, r *http.Request) {
		mountName := chi.URLParam(r, "mount")
		mount := s.director.Broadcast().GetMount(mountName)
		if mount == nil {
			http.Error(w, "Stream not found", http.StatusNotFound)
			return
		}
		mount.ServeHTTP(w, r)
	})

	// WebRTC signaling endpoint for low-latency streaming
	if s.webrtcBroadcaster != nil {
		s.router.HandleFunc("/webrtc/signal", s.webrtcBroadcaster.HandleSignaling)
	}

	s.api.Routes(s.router)

	// Web UI routes
	s.webHandler.Routes(s.router)
}
