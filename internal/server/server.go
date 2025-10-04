package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

    "github.com/example/grimnirradio/internal/analyzer"
    "github.com/example/grimnirradio/internal/api"
    "github.com/example/grimnirradio/internal/clock"
    "github.com/example/grimnirradio/internal/config"
    "github.com/example/grimnirradio/internal/db"
    "github.com/example/grimnirradio/internal/events"
    "github.com/example/grimnirradio/internal/live"
    "github.com/example/grimnirradio/internal/media"
    "github.com/example/grimnirradio/internal/playout"
    "github.com/example/grimnirradio/internal/scheduler"
    schedulerstate "github.com/example/grimnirradio/internal/scheduler/state"
    "github.com/example/grimnirradio/internal/smartblock"
    "github.com/example/grimnirradio/internal/telemetry"
)

// Server bundles HTTP and supporting services.
type Server struct {
	cfg        *config.Config
	logger     zerolog.Logger
	router     chi.Router
	httpServer *http.Server
	closers    []func() error

	db        *gorm.DB
	api       *api.API
	scheduler *scheduler.Service
	analyzer  *analyzer.Service
	playout   *playout.Manager
	director  *playout.Director
	bus       *events.Bus

	bgCancel context.CancelFunc
	bgWG     sync.WaitGroup
}

// New constructs the server and wires dependencies.
func New(cfg *config.Config, logger zerolog.Logger) (*Server, error) {
	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Timeout(60 * time.Second))

	srv := &Server{
		cfg:    cfg,
		logger: logger,
		router: router,
		bus:    events.NewBus(),
	}

	if err := srv.initDependencies(); err != nil {
		return nil, err
	}

	srv.configureRoutes()
	srv.startBackgroundWorkers()

	addr := fmt.Sprintf("%s:%d", cfg.HTTPBind, cfg.HTTPPort)
	srv.httpServer = &http.Server{
		Addr:         addr,
		Handler:      srv.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  30 * time.Second,
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

	planner := clock.NewPlanner(database, s.logger)
	stateStore := schedulerstate.NewStore()
	blockEngine := smartblock.New(database, s.logger)
	s.scheduler = scheduler.New(database, planner, blockEngine, stateStore, s.cfg.SchedulerLookahead, s.logger)

	s.analyzer = analyzer.New(database, s.cfg.MediaRoot, s.logger)
	mediaService := media.NewService(s.cfg, s.logger)
	s.playout = playout.NewManager(s.cfg, s.logger)
	s.director = playout.NewDirector(database, s.playout, s.bus, s.logger)
	liveService := live.NewService(s.logger)

	s.DeferClose(func() error { return s.playout.Shutdown() })

	s.api = api.New(s.db, s.scheduler, s.analyzer, mediaService, liveService, s.playout, s.bus, s.logger, []byte(s.cfg.JWTSigningKey))

	return nil
}

// HTTPServer exposes the underlying net/http server.
func (s *Server) HTTPServer() *http.Server {
	return s.httpServer
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

	if s.scheduler != nil {
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
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	s.router.Handle("/metrics", telemetry.Handler())
	s.api.Routes(s.router)
	s.router.Handle("/*", http.FileServer(http.Dir("web")))
}
