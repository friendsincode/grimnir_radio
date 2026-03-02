/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webstream

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

const (
	defaultHLSPollInterval = 15 * time.Second
	hlsReadTimeout         = 10 * time.Second
)

// HLSPoller periodically fetches an HLS manifest and extracts metadata
// from #EXTINF titles and #EXT-X-TITLE tags.
type HLSPoller struct {
	webstreamID  string
	stationID    string
	mountID      string
	url          string
	bus          *events.Bus
	db           *gorm.DB
	logger       zerolog.Logger
	pollInterval time.Duration

	cancel context.CancelFunc

	lastTitle  string
	lastArtist string
}

// NewHLSPoller creates a poller that extracts metadata from HLS manifests.
func NewHLSPoller(webstreamID, stationID, mountID, url string, bus *events.Bus, db *gorm.DB, logger zerolog.Logger) *HLSPoller {
	return &HLSPoller{
		webstreamID:  webstreamID,
		stationID:    stationID,
		mountID:      mountID,
		url:          url,
		bus:          bus,
		db:           db,
		logger:       logger.With().Str("component", "hls_poller").Str("webstream_id", webstreamID).Logger(),
		pollInterval: defaultHLSPollInterval,
	}
}

// Start begins the polling loop. Blocks until ctx is cancelled or Stop is called.
func (p *HLSPoller) Start(ctx context.Context) {
	ctx, p.cancel = context.WithCancel(ctx)

	p.logger.Info().Str("url", p.url).Dur("interval", p.pollInterval).Msg("HLS metadata poller started")

	p.poll(ctx)

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Debug().Msg("HLS metadata poller stopped")
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

// Stop cancels the polling loop.
func (p *HLSPoller) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
}

// SetURL updates the stream URL (e.g. after failover).
func (p *HLSPoller) SetURL(url string) {
	p.url = url
}

func (p *HLSPoller) poll(ctx context.Context) {
	title, artist, err := p.parseHLSMetadata(ctx, p.url)
	if err != nil {
		p.logger.Debug().Err(err).Msg("HLS metadata fetch failed")
		return
	}

	if title == "" && artist == "" {
		return
	}

	if title == p.lastTitle && artist == p.lastArtist {
		return
	}

	p.lastTitle = title
	p.lastArtist = artist

	p.logger.Info().Str("title", title).Str("artist", artist).Msg("HLS metadata changed")

	if p.db != nil {
		var history models.PlayHistory
		err := p.db.
			Where("station_id = ?", p.stationID).
			Order("started_at DESC").
			First(&history).Error
		if err == nil {
			history.Artist = artist
			history.Title = title
			if history.Metadata == nil {
				history.Metadata = make(map[string]any)
			}
			history.Metadata["hls_metadata"] = true
			history.Metadata["stream_title"] = title
			history.Metadata["stream_artist"] = artist
			if saveErr := p.db.Save(&history).Error; saveErr != nil {
				p.logger.Warn().Err(saveErr).Msg("failed to update play history with HLS metadata")
			}
		}
	}

	p.bus.Publish(events.EventNowPlaying, events.Payload{
		"station_id":    p.stationID,
		"mount_id":      p.mountID,
		"webstream_id":  p.webstreamID,
		"source_type":   "webstream",
		"title":         title,
		"artist":        artist,
		"hls_metadata":  true,
		"stream_title":  title,
		"stream_artist": artist,
	})
}

// parseHLSMetadata fetches the m3u8 manifest and extracts metadata from it.
// If the URL points to a master playlist, it resolves to the first media playlist.
func (p *HLSPoller) parseHLSMetadata(ctx context.Context, rawURL string) (title, artist string, err error) {
	lines, err := p.fetchPlaylist(ctx, rawURL)
	if err != nil {
		return "", "", err
	}

	// If this is a master playlist, resolve to the first variant
	if isMasterPlaylist(lines) {
		variantURL := resolveFirstVariant(rawURL, lines)
		if variantURL == "" {
			return "", "", fmt.Errorf("master playlist has no variants")
		}
		lines, err = p.fetchPlaylist(ctx, variantURL)
		if err != nil {
			return "", "", fmt.Errorf("fetch variant playlist: %w", err)
		}
	}

	// Try #EXT-X-TITLE first (non-standard but used by some servers)
	for _, line := range lines {
		if strings.HasPrefix(line, "#EXT-X-TITLE:") {
			raw := strings.TrimPrefix(line, "#EXT-X-TITLE:")
			raw = strings.TrimSpace(raw)
			if raw != "" {
				return splitArtistTitle(raw)
			}
		}
	}

	// Fall back to the last #EXTINF title
	var lastExtinf string
	for _, line := range lines {
		if strings.HasPrefix(line, "#EXTINF:") {
			// Format: #EXTINF:<duration>,<title>
			if idx := strings.IndexByte(line, ','); idx >= 0 {
				t := strings.TrimSpace(line[idx+1:])
				if t != "" {
					lastExtinf = t
				}
			}
		}
	}

	if lastExtinf != "" {
		return splitArtistTitle(lastExtinf)
	}

	return "", "", nil
}

func (p *HLSPoller) fetchPlaylist(ctx context.Context, rawURL string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, hlsReadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Grimnir-Radio/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var lines []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read playlist: %w", err)
	}

	return lines, nil
}

// isMasterPlaylist returns true if the manifest contains #EXT-X-STREAM-INF,
// which indicates a master (multi-variant) playlist.
func isMasterPlaylist(lines []string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			return true
		}
	}
	return false
}

// resolveFirstVariant returns the absolute URL for the first variant
// in a master playlist.
func resolveFirstVariant(masterURL string, lines []string) string {
	for i, line := range lines {
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			// The variant URI is on the next line
			if i+1 < len(lines) {
				uri := strings.TrimSpace(lines[i+1])
				if uri == "" || strings.HasPrefix(uri, "#") {
					continue
				}
				return resolveURL(masterURL, uri)
			}
		}
	}
	return ""
}

// resolveURL resolves a possibly-relative URI against a base URL.
func resolveURL(base, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	// Strip the last path segment from base
	idx := strings.LastIndex(base, "/")
	if idx >= 0 {
		return base[:idx+1] + ref
	}
	return ref
}

// splitArtistTitle splits "Artist - Title" into (title, artist).
// Returns (raw, "") if no separator is found.
func splitArtistTitle(raw string) (title, artist string, err error) {
	parts := strings.SplitN(raw, " - ", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1]), strings.TrimSpace(parts[0]), nil
	}
	return raw, "", nil
}
