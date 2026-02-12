package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/webrtc"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// webrtcStationManager provides per-station WebRTC signaling and broadcaster instances.
//
// Design goals:
// - Keep the existing Icecast-free broadcast pipeline.
// - Make WebRTC station-scoped so switching stations switches audio.
// - Avoid requiring restarts: lazily create broadcasters on first use.
type webrtcStationManager struct {
	db     *gorm.DB
	cfg    webrtc.Config
	logger zerolog.Logger

	mu           sync.RWMutex
	broadcasters map[string]*webrtc.Broadcaster // stationID -> broadcaster
}

func newWebRTCStationManager(db *gorm.DB, cfg webrtc.Config, logger zerolog.Logger) *webrtcStationManager {
	return &webrtcStationManager{
		db:           db,
		cfg:          cfg,
		logger:       logger.With().Str("component", "webrtc-station-manager").Logger(),
		broadcasters: make(map[string]*webrtc.Broadcaster),
	}
}

func (m *webrtcStationManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for stationID, b := range m.broadcasters {
		if err := b.Stop(); err != nil {
			m.logger.Warn().Err(err).Str("station_id", stationID).Msg("failed to stop webrtc broadcaster")
		}
	}
	m.broadcasters = make(map[string]*webrtc.Broadcaster)
	return nil
}

func (m *webrtcStationManager) HandleSignaling(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Query().Get("station_id")
	if stationID == "" {
		// Backward compatibility: if there is exactly one station broadcaster, use it.
		m.mu.RLock()
		if len(m.broadcasters) == 1 {
			for _, b := range m.broadcasters {
				m.mu.RUnlock()
				b.HandleSignaling(w, r)
				return
			}
		}
		m.mu.RUnlock()
		http.Error(w, "missing station_id", http.StatusBadRequest)
		return
	}

	b, err := m.getOrCreateBroadcaster(r.Context(), stationID)
	if err != nil {
		m.logger.Warn().Err(err).Str("station_id", stationID).Msg("webrtc signaling refused")
		http.Error(w, "webrtc unavailable", http.StatusNotFound)
		return
	}

	b.HandleSignaling(w, r)
}

func (m *webrtcStationManager) getOrCreateBroadcaster(ctx context.Context, stationID string) (*webrtc.Broadcaster, error) {
	// Fast-path: already created.
	m.mu.RLock()
	if b := m.broadcasters[stationID]; b != nil {
		m.mu.RUnlock()
		return b, nil
	}
	m.mu.RUnlock()

	// Ensure station exists and is active.
	var st models.Station
	if err := m.db.WithContext(ctx).First(&st, "id = ? AND active = ?", stationID, true).Error; err != nil {
		return nil, fmt.Errorf("load station: %w", err)
	}

	port, err := m.ensureStationRTPPort(ctx, st.ID)
	if err != nil {
		return nil, err
	}

	// Create outside lock to avoid blocking other calls.
	cfg := m.cfg
	cfg.RTPPort = port

	b, err := webrtc.NewBroadcaster(cfg, m.logger)
	if err != nil {
		return nil, fmt.Errorf("create broadcaster: %w", err)
	}
	if err := b.Start(ctx); err != nil {
		return nil, fmt.Errorf("start broadcaster: %w", err)
	}

	m.mu.Lock()
	// Another goroutine could have created it while we were starting; keep the first.
	if existing := m.broadcasters[stationID]; existing != nil {
		m.mu.Unlock()
		_ = b.Stop()
		return existing, nil
	}
	m.broadcasters[stationID] = b
	m.mu.Unlock()

	m.logger.Info().Str("station_id", stationID).Int("rtp_port", port).Msg("station WebRTC broadcaster started")
	return b, nil
}

func (m *webrtcStationManager) ensureStationRTPPort(ctx context.Context, stationID string) (int, error) {
	// Base port comes from GRIMNIR_WEBRTC_RTP_PORT; for per-station WebRTC, treat this as the base of a range.
	base := m.cfg.RTPPort
	if base == 0 {
		base = 5004
	}

	// Read current.
	var st models.Station
	if err := m.db.WithContext(ctx).Select("id", "web_rtc_rtp_port").First(&st, "id = ?", stationID).Error; err != nil {
		return 0, fmt.Errorf("load station port: %w", err)
	}
	if st.WebRTCRTPPort != 0 {
		return st.WebRTCRTPPort, nil
	}

	// Allocate next free port in [base..base+999].
	type row struct {
		Port int `gorm:"column:web_rtc_rtp_port"`
	}
	var rows []row
	if err := m.db.WithContext(ctx).Model(&models.Station{}).
		Select("web_rtc_rtp_port").
		Where("web_rtc_rtp_port > 0").
		Find(&rows).Error; err != nil {
		return 0, fmt.Errorf("list used ports: %w", err)
	}
	used := make(map[int]struct{}, len(rows))
	for _, r := range rows {
		used[r.Port] = struct{}{}
	}

	port := 0
	for p := base; p <= base+999; p++ {
		if _, ok := used[p]; ok {
			continue
		}
		port = p
		break
	}
	if port == 0 {
		return 0, fmt.Errorf("no free WebRTC RTP ports available in range %d-%d", base, base+999)
	}

	if err := m.db.WithContext(ctx).Model(&models.Station{}).
		Where("id = ? AND web_rtc_rtp_port = 0", stationID).
		Update("web_rtc_rtp_port", port).Error; err != nil {
		return 0, fmt.Errorf("persist station port: %w", err)
	}

	return port, nil
}

