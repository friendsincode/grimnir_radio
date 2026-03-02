/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webstream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/rs/zerolog"
)

const (
	defaultICYPollInterval = 15 * time.Second
	icyReadTimeout         = 10 * time.Second
	// Maximum bytes of audio data to read before the metadata block.
	// We read exactly icy-metaint bytes then parse the metadata.
	maxMetaInt = 64 * 1024 // 64 KB sanity cap
)

// ICYPoller periodically connects to a stream URL and extracts inline
// ICY metadata (StreamTitle). Changes are published as now-playing updates.
type ICYPoller struct {
	webstreamID  string
	stationID    string
	mountID      string
	url          string
	bus          *events.Bus
	logger       zerolog.Logger
	pollInterval time.Duration

	cancel context.CancelFunc

	// last seen values — only publish on change
	lastTitle  string
	lastArtist string
}

// NewICYPoller creates a poller that extracts ICY metadata from a stream URL.
func NewICYPoller(webstreamID, stationID, mountID, url string, bus *events.Bus, logger zerolog.Logger) *ICYPoller {
	return &ICYPoller{
		webstreamID:  webstreamID,
		stationID:    stationID,
		mountID:      mountID,
		url:          url,
		bus:          bus,
		logger:       logger.With().Str("component", "icy_poller").Str("webstream_id", webstreamID).Logger(),
		pollInterval: defaultICYPollInterval,
	}
}

// Start begins the polling loop. Blocks until ctx is cancelled or Stop is called.
func (p *ICYPoller) Start(ctx context.Context) {
	ctx, p.cancel = context.WithCancel(ctx)

	p.logger.Info().Str("url", p.url).Dur("interval", p.pollInterval).Msg("ICY metadata poller started")

	// Initial poll
	p.poll(ctx)

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Debug().Msg("ICY metadata poller stopped")
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

// Stop cancels the polling loop.
func (p *ICYPoller) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
}

// SetURL updates the stream URL (e.g. after failover).
func (p *ICYPoller) SetURL(url string) {
	p.url = url
}

func (p *ICYPoller) poll(ctx context.Context) {
	title, artist, err := p.parseICYMetadata(ctx, p.url)
	if err != nil {
		p.logger.Debug().Err(err).Msg("ICY metadata fetch failed")
		return
	}

	if title == p.lastTitle && artist == p.lastArtist {
		return // no change
	}

	p.lastTitle = title
	p.lastArtist = artist

	p.logger.Info().Str("title", title).Str("artist", artist).Msg("ICY metadata changed")

	p.bus.Publish(events.EventNowPlaying, events.Payload{
		"station_id":    p.stationID,
		"mount_id":      p.mountID,
		"webstream_id":  p.webstreamID,
		"source_type":   "webstream",
		"title":         title,
		"artist":        artist,
		"icy_metadata":  true,
		"stream_title":  title,
		"stream_artist": artist,
	})
}

// parseICYMetadata connects to the stream with Icy-MetaData:1, reads
// icy-metaint bytes of audio data, then parses the inline metadata block.
func (p *ICYPoller) parseICYMetadata(ctx context.Context, url string) (title, artist string, err error) {
	ctx, cancel := context.WithTimeout(ctx, icyReadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Icy-MetaData", "1")
	req.Header.Set("User-Agent", "Grimnir-Radio/1.0")

	client := &http.Client{
		Timeout: icyReadTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Get icy-metaint from response headers
	metaIntStr := resp.Header.Get("Icy-Metaint")
	if metaIntStr == "" {
		// Some servers use lowercase
		metaIntStr = resp.Header.Get("icy-metaint")
	}
	if metaIntStr == "" {
		return "", "", fmt.Errorf("server does not support ICY metadata (no icy-metaint header)")
	}

	metaInt, err := strconv.Atoi(metaIntStr)
	if err != nil || metaInt <= 0 {
		return "", "", fmt.Errorf("invalid icy-metaint value: %s", metaIntStr)
	}
	if metaInt > maxMetaInt {
		return "", "", fmt.Errorf("icy-metaint too large: %d", metaInt)
	}

	// Discard exactly metaInt bytes of audio data
	if _, err := io.CopyN(io.Discard, resp.Body, int64(metaInt)); err != nil {
		return "", "", fmt.Errorf("read audio block: %w", err)
	}

	// Read metadata length byte (length * 16 = actual metadata size)
	var lenBuf [1]byte
	if _, err := io.ReadFull(resp.Body, lenBuf[:]); err != nil {
		return "", "", fmt.Errorf("read metadata length: %w", err)
	}

	metaLen := int(lenBuf[0]) * 16
	if metaLen == 0 {
		return "", "", nil // No metadata in this block
	}

	// Read the metadata block
	metaBuf := make([]byte, metaLen)
	if _, err := io.ReadFull(resp.Body, metaBuf); err != nil {
		return "", "", fmt.Errorf("read metadata block: %w", err)
	}

	// Parse: StreamTitle='Artist - Title';StreamUrl='...';
	meta := strings.TrimRight(string(metaBuf), "\x00")
	title, artist = parseStreamTitle(meta)

	return title, artist, nil
}

// parseStreamTitle extracts artist and title from ICY metadata string.
// Standard format: StreamTitle='Artist - Title';
func parseStreamTitle(meta string) (title, artist string) {
	const prefix = "StreamTitle='"
	idx := strings.Index(meta, prefix)
	if idx < 0 {
		return "", ""
	}

	rest := meta[idx+len(prefix):]
	end := strings.Index(rest, "';")
	if end < 0 {
		end = strings.Index(rest, "'")
	}
	if end < 0 {
		return rest, ""
	}

	streamTitle := rest[:end]
	if streamTitle == "" {
		return "", ""
	}

	// Try to split "Artist - Title" format
	parts := strings.SplitN(streamTitle, " - ", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1]), strings.TrimSpace(parts[0])
	}

	// No separator — treat entire string as title
	return streamTitle, ""
}
