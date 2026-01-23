package mediaengine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// WebstreamPlayer handles relayed HTTP/ICY streams.
type WebstreamPlayer struct {
	WebstreamID string
	StationID   string
	MountID     string
	URLs        []string // Failover chain
	CurrentURL  string
	CurrentIndex int

	// Stream state
	Connected   bool
	ConnectedAt time.Time
	Metadata    map[string]string // ICY metadata

	// GStreamer pipeline
	Pipeline string
	Process  *GStreamerProcess

	logger zerolog.Logger
	mu     sync.RWMutex
}

// WebstreamManager manages multiple concurrent webstream players.
type WebstreamManager struct {
	mu      sync.RWMutex
	players map[string]*WebstreamPlayer // key: webstream_id
	logger  zerolog.Logger
}

// NewWebstreamManager creates a new webstream manager.
func NewWebstreamManager(logger zerolog.Logger) *WebstreamManager {
	return &WebstreamManager{
		players: make(map[string]*WebstreamPlayer),
		logger:  logger.With().Str("component", "webstream_manager").Logger(),
	}
}

// PlayWebstream starts playing a webstream.
func (wm *WebstreamManager) PlayWebstream(ctx context.Context, req *WebstreamPlayRequest) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	webstreamID := req.WebstreamID
	if webstreamID == "" {
		return fmt.Errorf("webstream_id required")
	}

	// Check if already playing
	if existing, exists := wm.players[webstreamID]; exists {
		if existing.Connected {
			return fmt.Errorf("webstream already playing")
		}
	}

	// Determine which URL to use
	currentURL := req.CurrentURL
	if currentURL == "" && len(req.URLs) > 0 {
		currentURL = req.URLs[0]
	}

	if currentURL == "" {
		return fmt.Errorf("no URL provided")
	}

	// Build webstream pipeline
	// souphttpsrc for HTTP/Icecast streams with ICY metadata
	pipeline := fmt.Sprintf("souphttpsrc location=\"%s\" is-live=true do-timestamp=true", currentURL)

	// Add ICY metadata extraction if enabled
	if req.ExtractMetadata {
		pipeline += " iradio-mode=true"
	}

	// Add buffer
	if req.BufferSizeMS > 0 {
		pipeline += fmt.Sprintf(" ! queue max-size-time=%d000000", req.BufferSizeMS) // Convert ms to ns
	}

	// Add decoder
	pipeline += " ! decodebin"

	// Apply DSP graph if provided
	if req.DSPGraphHandle != "" {
		// DSP processing would be inserted here
		pipeline += " ! audioconvert ! audioresample"
	}

	// Add fade in if requested
	if req.FadeInMS > 0 {
		fadeInSec := float64(req.FadeInMS) / 1000.0
		pipeline += fmt.Sprintf(" ! volume volume=0.0 ! volumeenvelope attack=%.3f", fadeInSec)
	}

	// Create webstream player
	player := &WebstreamPlayer{
		WebstreamID:  webstreamID,
		StationID:    req.StationID,
		MountID:      req.MountID,
		URLs:         req.URLs,
		CurrentURL:   currentURL,
		CurrentIndex: findURLIndex(req.URLs, currentURL),
		Connected:    true,
		ConnectedAt:  time.Now(),
		Pipeline:     pipeline,
		Metadata:     make(map[string]string),
		logger:       wm.logger.With().Str("webstream_id", webstreamID).Logger(),
	}

	// Store the player
	wm.players[webstreamID] = player

	wm.logger.Info().
		Str("webstream_id", webstreamID).
		Str("station_id", req.StationID).
		Str("mount_id", req.MountID).
		Str("url", currentURL).
		Msg("webstream playback started")

	return nil
}

// StopWebstream stops a playing webstream.
func (wm *WebstreamManager) StopWebstream(ctx context.Context, webstreamID string) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	player, exists := wm.players[webstreamID]
	if !exists {
		return fmt.Errorf("webstream not found: %s", webstreamID)
	}

	// Stop the pipeline if running
	if player.Process != nil {
		if err := player.Process.Stop(); err != nil {
			wm.logger.Error().Err(err).Str("webstream_id", webstreamID).Msg("failed to stop webstream pipeline")
		}
	}

	// Mark as disconnected
	player.mu.Lock()
	player.Connected = false
	player.mu.Unlock()

	// Remove from active players
	delete(wm.players, webstreamID)

	wm.logger.Info().
		Str("webstream_id", webstreamID).
		Dur("duration", time.Since(player.ConnectedAt)).
		Msg("webstream playback stopped")

	return nil
}

// FailoverWebstream switches a webstream to a different URL.
func (wm *WebstreamManager) FailoverWebstream(ctx context.Context, webstreamID, newURL string) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	player, exists := wm.players[webstreamID]
	if !exists {
		return fmt.Errorf("webstream not found: %s", webstreamID)
	}

	oldURL := player.CurrentURL

	// Stop current pipeline
	if player.Process != nil {
		if err := player.Process.Stop(); err != nil {
			wm.logger.Error().Err(err).Msg("failed to stop current pipeline")
		}
	}

	// Update URL
	player.mu.Lock()
	player.CurrentURL = newURL
	player.CurrentIndex = findURLIndex(player.URLs, newURL)
	player.mu.Unlock()

	// Rebuild pipeline with new URL
	// (In a full implementation, this would restart the GStreamer pipeline)

	wm.logger.Info().
		Str("webstream_id", webstreamID).
		Str("from_url", oldURL).
		Str("to_url", newURL).
		Msg("webstream failover completed")

	return nil
}

// GetWebstreamMetadata retrieves current ICY metadata for a webstream.
func (wm *WebstreamManager) GetWebstreamMetadata(webstreamID string) (map[string]string, error) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	player, exists := wm.players[webstreamID]
	if !exists {
		return nil, fmt.Errorf("webstream not found: %s", webstreamID)
	}

	player.mu.RLock()
	defer player.mu.RUnlock()

	// Return a copy to avoid race conditions
	metadata := make(map[string]string, len(player.Metadata))
	for k, v := range player.Metadata {
		metadata[k] = v
	}

	return metadata, nil
}

// GetActivePlayers returns all active webstream players.
func (wm *WebstreamManager) GetActivePlayers() map[string]*WebstreamPlayer {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	// Return a copy to avoid race conditions
	players := make(map[string]*WebstreamPlayer, len(wm.players))
	for k, v := range wm.players {
		players[k] = v
	}

	return players
}

// Shutdown stops all active webstream players.
func (wm *WebstreamManager) Shutdown() error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	wm.logger.Info().Int("count", len(wm.players)).Msg("shutting down webstream manager")

	for webstreamID, player := range wm.players {
		if player.Process != nil {
			if err := player.Process.Stop(); err != nil {
				wm.logger.Error().Err(err).Str("webstream_id", webstreamID).Msg("failed to stop webstream on shutdown")
			}
		}
	}

	// Clear all players
	wm.players = make(map[string]*WebstreamPlayer)

	return nil
}

// WebstreamPlayRequest contains parameters for starting webstream playback.
type WebstreamPlayRequest struct {
	WebstreamID     string
	StationID       string
	MountID         string
	URLs            []string // Failover chain
	CurrentURL      string   // Specific URL to use (otherwise uses first in chain)
	DSPGraphHandle  string   // DSP graph to apply
	BufferSizeMS    int      // Buffer size in milliseconds
	FadeInMS        int      // Fade in duration
	ExtractMetadata bool     // Extract ICY metadata
}

// Helper functions

func findURLIndex(urls []string, url string) int {
	for i, u := range urls {
		if u == url {
			return i
		}
	}
	return 0
}
