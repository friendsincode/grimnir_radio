/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/broadcast"
	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/mediaengine"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/smartblock"
	"github.com/friendsincode/grimnir_radio/internal/webstream"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type playoutState struct {
	MediaID   string
	EntryID   string
	StationID string
	Started   time.Time
	Ends      time.Time
	// Enhanced state tracking for playlists and smart blocks
	SourceType string   // "media", "playlist", "smart_block", "clock"
	SourceID   string   // playlist_id, smart_block_id, or clock_id
	Position   int      // current position in sequence
	TotalItems int      // total items in sequence
	Items      []string // pre-loaded media IDs in order
}

type scheduleBoundaryPolicy struct {
	Mode        string
	SoftOverrun time.Duration
}

type cachedScheduleBoundaryPolicy struct {
	policy   scheduleBoundaryPolicy
	loadedAt time.Time
}

type cachedWebRTCPort struct {
	port     int
	loadedAt time.Time
}

type crossfadeConfig struct {
	Enabled bool
	// Duration of overlap. 0 disables crossfade even if Enabled is true.
	Duration time.Duration
}

type cachedCrossfadeConfig struct {
	cfg      crossfadeConfig
	loadedAt time.Time
}

type cachedScheduleSnapshot struct {
	entries  []models.ScheduleEntry
	loadedAt time.Time
	dirty    bool
}

// Director drives schedule execution and emits now playing events.
type Director struct {
	db            *gorm.DB
	cfg           *config.Config
	manager       *Manager
	bus           *events.Bus
	webstreamSvc  *webstream.Service
	broadcast     *broadcast.Server
	smartblockEng *smartblock.Engine
	mediaRoot     string
	logger        zerolog.Logger

	// WebRTC RTP output configuration
	webrtcEnabled bool
	webrtcRTPPort int // base port; per-station port is loaded from DB (Station.WebRTCRTPPort)

	mu     sync.Mutex
	played map[string]time.Time
	active map[string]playoutState

	policyMu    sync.Mutex
	policyCache map[string]cachedScheduleBoundaryPolicy

	webrtcMu    sync.Mutex
	webrtcCache map[string]cachedWebRTCPort

	xfadeMu       sync.Mutex
	xfadeSessions map[string]*pcmCrossfadeSession // mountID -> session

	xfadeCfgMu    sync.Mutex
	xfadeCfgCache map[string]cachedCrossfadeConfig // stationID -> config

	scheduleMu    sync.Mutex
	scheduleCache cachedScheduleSnapshot
}

// NewDirector creates a playout director.
func NewDirector(db *gorm.DB, cfg *config.Config, manager *Manager, bus *events.Bus, webstreamSvc *webstream.Service, broadcastSrv *broadcast.Server, logger zerolog.Logger) *Director {
	return &Director{
		db:            db,
		cfg:           cfg,
		manager:       manager,
		bus:           bus,
		webstreamSvc:  webstreamSvc,
		broadcast:     broadcastSrv,
		smartblockEng: smartblock.New(db, logger),
		mediaRoot:     cfg.MediaRoot,
		logger:        logger,
		webrtcEnabled: cfg.WebRTCEnabled,
		webrtcRTPPort: cfg.WebRTCRTPPort,
		played:        make(map[string]time.Time),
		active:        make(map[string]playoutState),
		policyCache:   make(map[string]cachedScheduleBoundaryPolicy),
		webrtcCache:   make(map[string]cachedWebRTCPort),
		xfadeSessions: make(map[string]*pcmCrossfadeSession),
		xfadeCfgCache: make(map[string]cachedCrossfadeConfig),
		scheduleCache: cachedScheduleSnapshot{dirty: true},
	}
}

// Broadcast returns the broadcast server for registering HTTP handlers.
func (d *Director) Broadcast() *broadcast.Server {
	return d.broadcast
}

// Run executes the director loop until context cancellation.
func (d *Director) Run(ctx context.Context) error {
	d.logger.Info().Msg("playout director started")
	// Best-effort preload: allows resuming deterministic sequences after process restarts.
	d.loadPersistedMountStates(ctx)
	// Tight tick interval reduces schedule boundary jitter.
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	sub := d.bus.Subscribe(events.EventScheduleUpdate)
	defer d.bus.Unsubscribe(events.EventScheduleUpdate, sub)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-sub:
				if !ok {
					return
				}
				d.markScheduleDirty()
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			d.logger.Info().Msg("playout director stopped")
			return ctx.Err()
		case <-ticker.C:
			if err := d.tick(ctx); err != nil {
				d.logger.Error().Err(err).Msg("playout director tick failed")
			}
		}
	}
}

func (d *Director) tick(ctx context.Context) error {
	now := time.Now().UTC()
	d.prunePlayed(now)

	entries, err := d.getScheduleSnapshot(ctx, now)
	if err != nil {
		return err
	}

	for _, rawEntry := range entries {
		entry, playKey, playUntil, ok := resolveEntryForNow(rawEntry, now)
		if !ok {
			continue
		}

		// Soft boundary mode: if something is currently active on this mount, allow it to overrun
		// up to a station-defined limit before starting the next scheduled entry.
		d.mu.Lock()
		active, hasActive := d.active[entry.MountID]
		d.mu.Unlock()
		if hasActive && active.EntryID != entry.ID && active.StationID == entry.StationID {
			policy := d.getScheduleBoundaryPolicy(ctx, entry.StationID)
			if policy.Mode == "soft" && now.Before(active.Ends) {
				continue
			}
		}

		// Crossfade lookahead: allow starting the next entry a little early so the fade completes
		// exactly at the schedule boundary.
		if entry.StartsAt.After(now) {
			if !hasActive || active.EntryID == entry.ID || active.StationID != entry.StationID {
				continue
			}
			stationXFade := d.getCrossfadeConfig(ctx, entry.StationID)
			xfade := effectiveCrossfade(entry, stationXFade)
			if !xfade.Enabled || xfade.Duration <= 0 {
				continue
			}
			if entry.StartsAt.Sub(now) > xfade.Duration {
				continue
			}
		}

		if d.isPlayed(playKey) {
			continue
		}

		if err := d.handleEntry(ctx, entry); err != nil {
			d.logger.Warn().Err(err).Str("entry", entry.ID).Msg("failed to handle schedule entry")
			continue
		}

		d.markPlayed(playKey, playUntil)
	}

	d.emitHealthSnapshot()
	return nil
}

func (d *Director) markScheduleDirty() {
	d.scheduleMu.Lock()
	d.scheduleCache.dirty = true
	d.scheduleMu.Unlock()
}

func (d *Director) getScheduleSnapshot(ctx context.Context, now time.Time) ([]models.ScheduleEntry, error) {
	// Keep the director tick fast/accurate (250ms) without hammering the DB.
	// Refresh on schedule updates and periodically to catch edge cases.
	const refreshMinInterval = 2 * time.Second

	d.scheduleMu.Lock()
	dirty := d.scheduleCache.dirty
	loadedAt := d.scheduleCache.loadedAt
	haveCache := len(d.scheduleCache.entries) > 0
	d.scheduleMu.Unlock()

	needRefresh := dirty || now.Sub(loadedAt) >= refreshMinInterval || !haveCache
	if !needRefresh {
		d.scheduleMu.Lock()
		out := append([]models.ScheduleEntry(nil), d.scheduleCache.entries...)
		d.scheduleMu.Unlock()
		return out, nil
	}

	lookahead := 30 * time.Second
	lookback := 2 * time.Second

	var entries []models.ScheduleEntry
	err := d.db.WithContext(ctx).
		Where("(starts_at <= ? AND ends_at >= ?) OR (recurrence_type != '' AND recurrence_type IS NOT NULL AND is_instance = ? AND starts_at <= ?)",
			now.Add(lookahead), now.Add(-lookback), false, now.Add(lookahead)).
		Where("recurrence_end_date IS NULL OR recurrence_end_date >= ?", now.AddDate(0, 0, -1).Truncate(24*time.Hour)).
		Order("starts_at ASC").
		Find(&entries).Error
	if err != nil {
		// If we have a cache, keep running and try again next tick.
		d.scheduleMu.Lock()
		d.scheduleCache.dirty = true
		out := append([]models.ScheduleEntry(nil), d.scheduleCache.entries...)
		d.scheduleMu.Unlock()
		if len(out) > 0 {
			return out, nil
		}
		return nil, err
	}

	d.scheduleMu.Lock()
	d.scheduleCache.entries = entries
	d.scheduleCache.loadedAt = now
	d.scheduleCache.dirty = false
	out := append([]models.ScheduleEntry(nil), d.scheduleCache.entries...)
	d.scheduleMu.Unlock()
	return out, nil
}

func (d *Director) getScheduleBoundaryPolicy(ctx context.Context, stationID string) scheduleBoundaryPolicy {
	// Cache for a short time to avoid hammering DB at 250ms tick rate.
	const ttl = 30 * time.Second

	now := time.Now()
	d.policyMu.Lock()
	if cached, ok := d.policyCache[stationID]; ok && now.Sub(cached.loadedAt) < ttl {
		p := cached.policy
		d.policyMu.Unlock()
		return p
	}
	d.policyMu.Unlock()

	policy := scheduleBoundaryPolicy{Mode: "hard", SoftOverrun: 0}

	var station models.Station
	if err := d.db.WithContext(ctx).
		Select("id", "schedule_boundary_mode", "schedule_soft_overrun_seconds").
		First(&station, "id = ?", stationID).Error; err == nil {
		if station.ScheduleBoundaryMode == "soft" {
			policy.Mode = "soft"
		}
		if station.ScheduleSoftOverrunSeconds > 0 {
			policy.SoftOverrun = time.Duration(station.ScheduleSoftOverrunSeconds) * time.Second
		}
	}

	d.policyMu.Lock()
	d.policyCache[stationID] = cachedScheduleBoundaryPolicy{policy: policy, loadedAt: now}
	d.policyMu.Unlock()
	return policy
}

func (d *Director) getWebRTCRTPPortForStation(ctx context.Context, stationID string) int {
	if !d.webrtcEnabled {
		return 0
	}

	// Cache for a short time to avoid hammering DB at 250ms tick rate.
	const ttl = 30 * time.Second
	now := time.Now()

	d.webrtcMu.Lock()
	if cached, ok := d.webrtcCache[stationID]; ok && now.Sub(cached.loadedAt) < ttl {
		port := cached.port
		d.webrtcMu.Unlock()
		if port != 0 {
			return port
		}
		return d.webrtcRTPPort
	}
	d.webrtcMu.Unlock()

	type row struct {
		WebRTCRTPPort int `gorm:"column:web_rtc_rtp_port"`
	}
	var r row
	if err := d.db.WithContext(ctx).
		Model(&models.Station{}).
		Select("web_rtc_rtp_port").
		Where("id = ?", stationID).
		Take(&r).Error; err != nil {
		d.logger.Debug().Err(err).Str("station_id", stationID).Msg("failed to load station webrtc port; falling back to base")
		return d.webrtcRTPPort
	}

	port := r.WebRTCRTPPort

	d.webrtcMu.Lock()
	d.webrtcCache[stationID] = cachedWebRTCPort{port: port, loadedAt: now}
	d.webrtcMu.Unlock()

	if port != 0 {
		return port
	}
	return d.webrtcRTPPort
}

func (d *Director) getCrossfadeConfig(ctx context.Context, stationID string) crossfadeConfig {
	// Cache for a short time to avoid hammering DB at 250ms tick rate.
	const ttl = 30 * time.Second
	now := time.Now()

	d.xfadeCfgMu.Lock()
	if cached, ok := d.xfadeCfgCache[stationID]; ok && now.Sub(cached.loadedAt) < ttl {
		cfg := cached.cfg
		d.xfadeCfgMu.Unlock()
		return cfg
	}
	d.xfadeCfgMu.Unlock()

	cfg := crossfadeConfig{Enabled: false, Duration: 0}
	var station models.Station
	if err := d.db.WithContext(ctx).
		Select("id", "crossfade_enabled", "crossfade_duration_ms").
		First(&station, "id = ?", stationID).Error; err == nil {
		cfg.Enabled = station.CrossfadeEnabled
		if station.CrossfadeDurationMs > 0 {
			cfg.Duration = time.Duration(station.CrossfadeDurationMs) * time.Millisecond
		}
	}

	d.xfadeCfgMu.Lock()
	d.xfadeCfgCache[stationID] = cachedCrossfadeConfig{cfg: cfg, loadedAt: now}
	d.xfadeCfgMu.Unlock()
	return cfg
}

func effectiveCrossfade(entry models.ScheduleEntry, stationCfg crossfadeConfig) crossfadeConfig {
	// Default: station config.
	out := stationCfg

	if entry.Metadata == nil {
		return out
	}
	raw, ok := entry.Metadata["crossfade"]
	if !ok || raw == nil {
		return out
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return out
	}
	override, _ := m["override"].(bool)
	if !override {
		return out
	}

	// enabled: "inherit"|"on"|"off"
	if s, ok := m["enabled"].(string); ok {
		switch s {
		case "on":
			out.Enabled = true
		case "off":
			out.Enabled = false
		default:
			// inherit: keep station setting
		}
	}

	// duration_ms: number
	switch v := m["duration_ms"].(type) {
	case float64:
		if v >= 0 {
			ms := int(v)
			if ms > 30000 {
				ms = 30000
			}
			out.Duration = time.Duration(ms) * time.Millisecond
		}
	case int:
		if v >= 0 {
			ms := v
			if ms > 30000 {
				ms = 30000
			}
			out.Duration = time.Duration(ms) * time.Millisecond
		}
	}

	return out
}

func resolveEntryForNow(entry models.ScheduleEntry, now time.Time) (models.ScheduleEntry, string, time.Time, bool) {
	startWindow := now.Add(-2 * time.Second)
	endWindow := now

	if entry.RecurrenceType == models.RecurrenceNone || entry.IsInstance {
		if entry.StartsAt.After(endWindow) || entry.EndsAt.Before(startWindow) {
			return models.ScheduleEntry{}, "", time.Time{}, false
		}
		return entry, playbackKey(entry.ID, entry.StartsAt), entry.EndsAt, true
	}

	occStart, occEnd, ok := resolveRecurringOccurrenceWindow(entry, now)
	if !ok {
		return models.ScheduleEntry{}, "", time.Time{}, false
	}

	resolved := entry
	resolved.StartsAt = occStart
	resolved.EndsAt = occEnd
	return resolved, playbackKey(entry.ID, occStart), occEnd, true
}

func playbackKey(entryID string, startsAt time.Time) string {
	return fmt.Sprintf("%s@%s", entryID, startsAt.UTC().Format(time.RFC3339Nano))
}

func resolveRecurringOccurrenceWindow(entry models.ScheduleEntry, now time.Time) (time.Time, time.Time, bool) {
	if entry.RecurrenceType == models.RecurrenceNone || entry.IsInstance {
		return time.Time{}, time.Time{}, false
	}

	duration := entry.EndsAt.Sub(entry.StartsAt)
	if duration <= 0 {
		return time.Time{}, time.Time{}, false
	}

	now = now.UTC()
	templateStart := entry.StartsAt.UTC()
	startWindow := now.Add(-2 * time.Second)
	endWindow := now

	candidateDays := []time.Time{
		time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC),
		time.Date(now.AddDate(0, 0, -1).Year(), now.AddDate(0, 0, -1).Month(), now.AddDate(0, 0, -1).Day(), 0, 0, 0, 0, time.UTC),
	}

	var bestStart time.Time
	var bestEnd time.Time
	found := false

	for _, day := range candidateDays {
		occStart := time.Date(day.Year(), day.Month(), day.Day(), templateStart.Hour(), templateStart.Minute(), templateStart.Second(), templateStart.Nanosecond(), time.UTC)
		occEnd := occStart.Add(duration)

		if occStart.Before(templateStart) {
			continue
		}
		if entry.RecurrenceEndDate != nil && occurrenceDateAfter(occStart, *entry.RecurrenceEndDate) {
			continue
		}
		if !matchesRecurringDay(entry, occStart.Weekday()) {
			continue
		}
		if occStart.After(endWindow) || occEnd.Before(startWindow) {
			continue
		}
		if !found || occStart.After(bestStart) {
			bestStart = occStart
			bestEnd = occEnd
			found = true
		}
	}

	return bestStart, bestEnd, found
}

func occurrenceDateAfter(a, b time.Time) bool {
	ay, am, ad := a.UTC().Date()
	by, bm, bd := b.UTC().Date()
	if ay != by {
		return ay > by
	}
	if am != bm {
		return am > bm
	}
	return ad > bd
}

func matchesRecurringDay(entry models.ScheduleEntry, day time.Weekday) bool {
	switch entry.RecurrenceType {
	case models.RecurrenceDaily:
		return true
	case models.RecurrenceWeekdays:
		return day != time.Saturday && day != time.Sunday
	case models.RecurrenceWeekly:
		return day == entry.StartsAt.UTC().Weekday()
	case models.RecurrenceCustom:
		if len(entry.RecurrenceDays) == 0 {
			return true
		}
		wd := int(day)
		for _, d := range entry.RecurrenceDays {
			if d == wd {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func (d *Director) handleEntry(ctx context.Context, entry models.ScheduleEntry) error {
	// If we have a persisted/resumable state for this mount+entry, prefer it over re-materializing
	// non-deterministic sources (smart blocks, clock slots).
	if d.resumeEntryIfPossible(ctx, entry) {
		return nil
	}
	switch entry.SourceType {
	case "media":
		return d.startMediaEntry(ctx, entry)
	case "playlist":
		return d.startPlaylistEntry(ctx, entry)
	case "smart_block":
		return d.startSmartBlockEntry(ctx, entry)
	case "clock_template":
		return d.startClockEntry(ctx, entry)
	case "webstream":
		return d.startWebstreamEntry(ctx, entry)
	case "live":
		// Live sessions are handled by the live DJ system
		d.publishNowPlaying(entry, map[string]any{"type": "live"})
		return nil
	default:
		d.publishNowPlaying(entry, nil)
		return nil
	}
}

func (d *Director) loadPersistedMountStates(ctx context.Context) {
	// Load last known state for each mount. This runs once at startup and is best-effort.
	now := time.Now().UTC()
	var rows []models.MountPlayoutState
	if err := d.db.WithContext(ctx).Find(&rows).Error; err != nil {
		d.logger.Debug().Err(err).Msg("failed to load persisted mount playout state")
		return
	}

	// Prune obviously stale rows to keep the table small (best effort).
	cutoff := now.Add(-6 * time.Hour)
	var staleMountIDs []string

	d.mu.Lock()
	for _, r := range rows {
		if r.MountID == "" || r.EntryID == "" || r.StationID == "" {
			continue
		}
		if !r.EndsAt.IsZero() && r.EndsAt.Before(cutoff) {
			staleMountIDs = append(staleMountIDs, r.MountID)
			continue
		}
		d.active[r.MountID] = playoutState{
			MediaID:    r.MediaID,
			EntryID:    r.EntryID,
			StationID:  r.StationID,
			Started:    r.StartedAt,
			Ends:       r.EndsAt,
			SourceType: r.SourceType,
			SourceID:   r.SourceID,
			Position:   r.Position,
			TotalItems: r.TotalItems,
			Items:      append([]string(nil), r.Items...),
		}
	}
	d.mu.Unlock()

	if len(staleMountIDs) > 0 {
		_ = d.db.WithContext(ctx).Where("mount_id IN ?", staleMountIDs).Delete(&models.MountPlayoutState{}).Error
	}
}

func (d *Director) persistMountState(ctx context.Context, mountID string, state playoutState) {
	// Skip incomplete rows (we only persist when we have a real "now playing").
	if mountID == "" || state.EntryID == "" || state.StationID == "" || state.MediaID == "" {
		return
	}

	row := models.MountPlayoutState{
		MountID:    mountID,
		StationID:  state.StationID,
		EntryID:    state.EntryID,
		MediaID:    state.MediaID,
		SourceType: state.SourceType,
		SourceID:   state.SourceID,
		Position:   state.Position,
		TotalItems: state.TotalItems,
		Items:      append([]string(nil), state.Items...),
		StartedAt:  state.Started.UTC(),
		EndsAt:     state.Ends.UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	// Upsert by mount_id primary key. This is called on transitions only (not the 250ms tick).
	if err := d.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "mount_id"}},
			UpdateAll: true,
		}).
		Create(&row).Error; err != nil {
		d.logger.Debug().Err(err).Str("mount_id", mountID).Msg("failed to persist mount playout state")
	}
}

func (d *Director) clearPersistedMountState(ctx context.Context, mountID string) {
	if mountID == "" {
		return
	}
	if err := d.db.WithContext(ctx).Delete(&models.MountPlayoutState{}, "mount_id = ?", mountID).Error; err != nil {
		d.logger.Debug().Err(err).Str("mount_id", mountID).Msg("failed to clear persisted mount playout state")
	}
}

func (d *Director) resumeEntryIfPossible(ctx context.Context, entry models.ScheduleEntry) bool {
	// Only resume for sources that are non-deterministic without state.
	switch entry.SourceType {
	case "playlist", "smart_block", "clock_template":
		// ok
	default:
		return false
	}

	now := time.Now().UTC()
	if now.After(entry.EndsAt) {
		return false
	}

	d.mu.Lock()
	state, ok := d.active[entry.MountID]
	d.mu.Unlock()
	if !ok || state.EntryID != entry.ID || state.StationID != entry.StationID || state.MediaID == "" {
		return false
	}

	// Ensure source type matches when schedule entry is explicit.
	if entry.SourceType == "playlist" && state.SourceType != "playlist" {
		return false
	}
	if entry.SourceType == "smart_block" && state.SourceType != "smart_block" {
		return false
	}

	// If we don't have a sequence, there's nothing special to resume.
	if len(state.Items) == 0 || state.Position < 0 || state.Position >= len(state.Items) {
		return false
	}

	// Prefer the sequence media id at current position.
	mediaID := state.Items[state.Position]
	if mediaID == "" {
		mediaID = state.MediaID
	}

	var media models.MediaItem
	if err := d.db.WithContext(ctx).First(&media, "id = ?", mediaID).Error; err != nil {
		return false
	}

	d.logger.Info().
		Str("mount_id", entry.MountID).
		Str("entry_id", entry.ID).
		Str("source_type", state.SourceType).
		Int("position", state.Position).
		Int("total", len(state.Items)).
		Msg("resuming mount playout state after restart")

	_ = d.playMediaWithState(ctx, entry, media, state.SourceType, state.SourceID, state.Position, state.Items, map[string]any{
		"resumed": true,
	})
	return true
}

func (d *Director) startMediaEntry(ctx context.Context, entry models.ScheduleEntry) error {
	var media models.MediaItem
	err := d.db.WithContext(ctx).First(&media, "id = ?", entry.SourceID).Error
	if err != nil {
		return err
	}

	// Use the common playMedia helper
	d.mu.Lock()
	prev, hasPrev := d.active[entry.MountID]
	d.mu.Unlock()

	if err := d.playMedia(ctx, entry, media, nil); err != nil {
		return err
	}

	if hasPrev && prev.MediaID != media.ID {
		d.bus.Publish(events.EventHealth, events.Payload{
			"station_id":        entry.StationID,
			"mount_id":          entry.MountID,
			"previous_media":    prev.MediaID,
			"previous_entry_id": prev.EntryID,
			"current_media":     media.ID,
			"entry_id":          entry.ID,
			"event":             "crossfade",
		})
	}

	return nil
}

func (d *Director) startWebstreamEntry(ctx context.Context, entry models.ScheduleEntry) error {
	// Get webstream ID from metadata or SourceID
	webstreamID := entry.SourceID
	if webstreamID == "" {
		if id, ok := entry.Metadata["webstream_id"].(string); ok {
			webstreamID = id
		}
	}

	if webstreamID == "" {
		return fmt.Errorf("webstream_id not found in entry")
	}

	// Load webstream from database
	ws, err := d.webstreamSvc.GetWebstream(ctx, webstreamID)
	if err != nil {
		return fmt.Errorf("failed to load webstream: %w", err)
	}

	// Get current URL (respects failover state)
	currentURL := ws.GetCurrentURL()
	if currentURL == "" {
		return fmt.Errorf("no URL configured for webstream %s", webstreamID)
	}

	// Get mount configuration from database
	var mount models.Mount
	if err := d.db.WithContext(ctx).First(&mount, "id = ?", entry.MountID).Error; err != nil {
		d.logger.Warn().Err(err).Str("mount_id", entry.MountID).Msg("failed to load mount config, using defaults")
		mount.Format = "mp3"
		mount.Bitrate = 128
		mount.SampleRate = 44100
		mount.Channels = 2
		mount.Name = "stream"
	}

	// Build pipeline: webstream source -> decode -> encode -> icecast
	pipeline, err := d.buildWebstreamIcecastPipeline(currentURL, mount, ws)
	if err != nil {
		return fmt.Errorf("build webstream pipeline: %w", err)
	}

	d.mu.Lock()
	prev, hasPrev := d.active[entry.MountID]
	d.active[entry.MountID] = playoutState{
		MediaID:   webstreamID, // Store webstream ID in MediaID field for tracking
		EntryID:   entry.ID,
		StationID: entry.StationID,
		Started:   entry.StartsAt,
		Ends: func() time.Time {
			stopAt := entry.EndsAt
			policy := d.getScheduleBoundaryPolicy(ctx, entry.StationID)
			if policy.Mode == "soft" && policy.SoftOverrun > 0 {
				stopAt = stopAt.Add(policy.SoftOverrun)
			}
			return stopAt
		}(),
	}
	d.mu.Unlock()
	// Persist on transition.
	d.mu.Lock()
	s := d.active[entry.MountID]
	d.mu.Unlock()
	d.persistMountState(ctx, entry.MountID, s)

	// Stop previous pipeline
	if err := d.manager.StopPipeline(entry.MountID); err != nil {
		d.logger.Debug().Err(err).Str("mount", entry.MountID).Msg("stop pipeline failed")
	}

	d.logger.Info().
		Str("mount", entry.MountID).
		Str("webstream", ws.Name).
		Str("url", currentURL).
		Msg("starting webstream playout")

	// Start webstream pipeline
	if err := d.manager.EnsurePipeline(ctx, entry.MountID, pipeline); err != nil {
		d.logger.Warn().Err(err).Str("mount", entry.MountID).Msg("failed to start webstream pipeline")
		return err
	}

	// Build metadata payload
	payload := map[string]any{
		"webstream_id":   ws.ID,
		"webstream_name": ws.Name,
		"url":            currentURL,
		"health_status":  ws.HealthStatus,
	}

	// Add custom metadata if override is enabled
	if ws.OverrideMetadata && ws.CustomMetadata != nil {
		for k, v := range ws.CustomMetadata {
			payload[k] = v
		}
	}

	if hasPrev && prev.MediaID != webstreamID {
		d.bus.Publish(events.EventHealth, events.Payload{
			"station_id":        entry.StationID,
			"mount_id":          entry.MountID,
			"previous_source":   prev.MediaID,
			"previous_entry_id": prev.EntryID,
			"current_source":    webstreamID,
			"entry_id":          entry.ID,
			"event":             "source_change",
		})
	}

	d.publishNowPlaying(entry, payload)
	d.scheduleStop(ctx, entry.StationID, entry.MountID, entry.EndsAt)

	return nil
}

// buildWebstreamIcecastPipeline creates a GStreamer pipeline that relays a webstream to Icecast
func (d *Director) buildWebstreamIcecastPipeline(sourceURL string, mount models.Mount, ws *models.Webstream) (string, error) {
	// Parse Icecast URL
	icecastURL, err := url.Parse(d.cfg.IcecastURL)
	if err != nil {
		return "", fmt.Errorf("parse icecast URL: %w", err)
	}

	host := icecastURL.Hostname()
	port := icecastURL.Port()
	if port == "" {
		port = "8000"
	}

	// Determine mount point
	mountPoint := mount.Name
	if mountPoint == "" {
		mountPoint = "stream"
	}
	if mountPoint[0] != '/' {
		mountPoint = "/" + mountPoint
	}

	// Determine format
	format := mediaengine.AudioFormat(mount.Format)
	if format == "" {
		format = mediaengine.AudioFormatMP3
	}

	// Set defaults for encoding
	bitrate := mount.Bitrate
	if bitrate == 0 {
		bitrate = 128
	}
	sampleRate := mount.SampleRate
	if sampleRate == 0 {
		sampleRate = 44100
	}
	channels := mount.Channels
	if channels == 0 {
		channels = 2
	}

	// Build encoder configuration
	encoderCfg := mediaengine.EncoderConfig{
		OutputType:  mediaengine.OutputTypeIcecast,
		OutputURL:   d.cfg.IcecastURL,
		Username:    "source",
		Password:    d.cfg.IcecastSourcePassword,
		Mount:       mountPoint,
		StreamName:  ws.Name,
		Description: ws.Description,
		Format:      format,
		Bitrate:     bitrate,
		SampleRate:  sampleRate,
		Channels:    channels,
	}

	builder := mediaengine.NewEncoderBuilder(encoderCfg)
	encoderPipeline, err := builder.Build()
	if err != nil {
		return "", fmt.Errorf("build encoder: %w", err)
	}

	// Build source element
	var sourceElement string
	sourceElement = fmt.Sprintf("souphttpsrc location=%q is-live=true do-timestamp=true", sourceURL)
	if ws.PassthroughMetadata {
		sourceElement += " iradio-mode=true"
	}

	// Add buffer if configured
	bufferElement := ""
	if ws.BufferSizeMS > 0 {
		bufferElement = fmt.Sprintf(" ! queue max-size-time=%d000000", ws.BufferSizeMS)
	}

	// Build complete pipeline: webstream source -> buffer -> decode -> encoder -> icecast
	pipeline := fmt.Sprintf("%s%s ! decodebin ! %s", sourceElement, bufferElement, encoderPipeline)

	d.logger.Debug().
		Str("pipeline", pipeline).
		Str("icecast", fmt.Sprintf("%s:%s%s", host, port, mountPoint)).
		Msg("built webstream icecast pipeline")

	return pipeline, nil
}

func (d *Director) startPlaylistEntry(ctx context.Context, entry models.ScheduleEntry) error {
	// Load playlist with items ordered by position
	var playlist models.Playlist
	if err := d.db.WithContext(ctx).Preload("Items", func(db *gorm.DB) *gorm.DB {
		return db.Order("position ASC")
	}).First(&playlist, "id = ?", entry.SourceID).Error; err != nil {
		return fmt.Errorf("failed to load playlist: %w", err)
	}

	if len(playlist.Items) == 0 {
		d.logger.Warn().Str("playlist", playlist.ID).Msg("playlist is empty")
		d.publishNowPlaying(entry, map[string]any{"playlist_id": playlist.ID, "playlist_name": playlist.Name, "error": "empty playlist"})
		return nil
	}

	// Get position from metadata (survives restarts)
	position := 0
	if p, ok := entry.Metadata["current_position"].(float64); ok {
		position = int(p) % len(playlist.Items)
	}

	// Build items list (media IDs in order)
	items := make([]string, len(playlist.Items))
	for i, item := range playlist.Items {
		items[i] = item.MediaID
	}

	// Load media at current position
	var media models.MediaItem
	if err := d.db.WithContext(ctx).First(&media, "id = ?", items[position]).Error; err != nil {
		return fmt.Errorf("failed to load media item: %w", err)
	}

	// Play with enhanced state tracking
	return d.playMediaWithState(ctx, entry, media, "playlist", playlist.ID, position, items, map[string]any{
		"playlist_id":   playlist.ID,
		"playlist_name": playlist.Name,
		"total_items":   len(playlist.Items),
	})
}

func (d *Director) startSmartBlockEntry(ctx context.Context, entry models.ScheduleEntry) error {
	// Load smart block
	var block models.SmartBlock
	if err := d.db.WithContext(ctx).First(&block, "id = ?", entry.SourceID).Error; err != nil {
		return fmt.Errorf("failed to load smart block: %w", err)
	}

	// If we have persisted state for this entry (from before a restart), resume it instead of re-rolling.
	// This preserves deterministic playback (e.g. contracted underwriting/ads) across reboots.
	if d.resumeEntryIfPossible(ctx, entry) {
		return nil
	}

	// Use SmartBlock Engine to generate a sequence based on rules
	duration := entry.EndsAt.Sub(entry.StartsAt)
	req := smartblock.GenerateRequest{
		SmartBlockID: block.ID,
		Seed:         time.Now().UnixNano(),
		Duration:     duration.Milliseconds(),
		StationID:    entry.StationID,
		MountID:      entry.MountID,
	}

	result, err := d.smartblockEng.Generate(ctx, req)
	if err != nil {
		d.logger.Warn().Err(err).Str("block", block.ID).Msg("smart block generation failed, falling back to random")
		// Fallback to random selection if engine fails
		var media models.MediaItem
		err := d.db.WithContext(ctx).
			Where("station_id = ?", entry.StationID).
			Where("analysis_state = ?", models.AnalysisComplete).
			Order("RANDOM()").
			First(&media).Error
		if err != nil {
			d.logger.Warn().Str("block", block.ID).Msg("no media found for smart block")
			d.publishNowPlaying(entry, map[string]any{"smart_block_id": block.ID, "smart_block_name": block.Name, "error": "no matching media"})
			return nil
		}
		return d.playMedia(ctx, entry, media, map[string]any{
			"smart_block_id":   block.ID,
			"smart_block_name": block.Name,
		})
	}

	if len(result.Items) == 0 {
		d.logger.Warn().Str("block", block.ID).Msg("smart block produced no items")
		d.publishNowPlaying(entry, map[string]any{"smart_block_id": block.ID, "smart_block_name": block.Name, "error": "no matching media"})
		return nil
	}

	// Extract media IDs from sequence
	items := make([]string, len(result.Items))
	for i, item := range result.Items {
		items[i] = item.MediaID
	}

	// Load first media item
	var media models.MediaItem
	if err := d.db.WithContext(ctx).First(&media, "id = ?", items[0]).Error; err != nil {
		return fmt.Errorf("failed to load media item: %w", err)
	}

	// Play with enhanced state tracking
	return d.playMediaWithState(ctx, entry, media, "smart_block", block.ID, 0, items, map[string]any{
		"smart_block_id":   block.ID,
		"smart_block_name": block.Name,
		"total_items":      len(items),
	})
}

func (d *Director) startClockEntry(ctx context.Context, entry models.ScheduleEntry) error {
	// Clock templates can contain smart blocks/playlists that must not re-roll after restarts.
	// If we have an active persisted sequence for this schedule entry, resume it.
	if d.resumeEntryIfPossible(ctx, entry) {
		return nil
	}

	// Load clock template with slots
	var clock models.ClockHour
	if err := d.db.WithContext(ctx).Preload("Slots").First(&clock, "id = ?", entry.SourceID).Error; err != nil {
		return fmt.Errorf("failed to load clock: %w", err)
	}

	if len(clock.Slots) == 0 {
		d.logger.Warn().Str("clock", clock.ID).Msg("clock has no slots")
		d.publishNowPlaying(entry, map[string]any{"clock_id": clock.ID, "clock_name": clock.Name, "error": "no slots"})
		return nil
	}

	// Sort slots by position
	slots := clock.Slots
	sort.Slice(slots, func(i, j int) bool {
		return slots[i].Position < slots[j].Position
	})

	// Find current slot based on time offset within the entry
	slot := d.findCurrentSlot(slots, entry.StartsAt)
	if slot == nil {
		slot = &slots[0] // Default to first slot
	}

	d.logger.Debug().
		Str("clock", clock.ID).
		Str("slot_type", string(slot.Type)).
		Int("position", slot.Position).
		Msg("executing clock slot")

	// Handle slot based on type
	switch slot.Type {
	case models.SlotTypeSmartBlock:
		blockID, ok := slot.Payload["smart_block_id"].(string)
		if !ok {
			return fmt.Errorf("smart_block_id not found in slot payload")
		}
		return d.startSmartBlockByID(ctx, entry, blockID, clock.ID, clock.Name)

	case models.SlotTypeHardItem:
		mediaID, ok := slot.Payload["media_id"].(string)
		if !ok {
			return fmt.Errorf("media_id not found in slot payload")
		}
		return d.playMediaByID(ctx, entry, mediaID, clock.ID, clock.Name)

	case models.SlotTypeWebstream:
		wsID, ok := slot.Payload["webstream_id"].(string)
		if !ok {
			return fmt.Errorf("webstream_id not found in slot payload")
		}
		return d.startWebstreamByID(ctx, entry, wsID, clock.ID, clock.Name)

	case models.SlotTypePlaylist:
		playlistID, ok := slot.Payload["playlist_id"].(string)
		if !ok {
			return fmt.Errorf("playlist_id not found in slot payload")
		}
		return d.startPlaylistByID(ctx, entry, playlistID, clock.ID, clock.Name)

	case models.SlotTypeStopset:
		// Stopset support:
		// 1) Preferred: explicit playlist_id in slot payload.
		// 2) Fallback: explicit media_id hard item.
		// 3) Final fallback: random analyzed media.
		if playlistID, ok := slot.Payload["playlist_id"].(string); ok && playlistID != "" {
			d.logger.Debug().
				Str("clock", clock.ID).
				Str("playlist_id", playlistID).
				Msg("executing stopset slot from playlist")
			return d.startPlaylistByID(ctx, entry, playlistID, clock.ID, clock.Name)
		}
		if mediaID, ok := slot.Payload["media_id"].(string); ok && mediaID != "" {
			d.logger.Debug().
				Str("clock", clock.ID).
				Str("media_id", mediaID).
				Msg("executing stopset slot from media item")
			return d.playMediaByID(ctx, entry, mediaID, clock.ID, clock.Name)
		}

		d.logger.Debug().Str("clock", clock.ID).Msg("stopset slot missing payload, falling back to random")
		var media models.MediaItem
		err := d.db.WithContext(ctx).
			Where("station_id = ?", entry.StationID).
			Where("analysis_state = ?", models.AnalysisComplete).
			Order("RANDOM()").
			First(&media).Error
		if err != nil {
			d.publishNowPlaying(entry, map[string]any{
				"clock_id":   clock.ID,
				"clock_name": clock.Name,
				"slot_type":  "stopset",
				"status":     "no_media",
			})
			return nil
		}
		return d.playMedia(ctx, entry, media, map[string]any{
			"clock_id":   clock.ID,
			"clock_name": clock.Name,
			"slot_type":  "stopset",
		})

	default:
		// Unknown slot type - fall back to random
		d.logger.Warn().Str("slot_type", string(slot.Type)).Msg("unknown slot type, falling back to random")
		var media models.MediaItem
		err := d.db.WithContext(ctx).
			Where("station_id = ?", entry.StationID).
			Where("analysis_state = ?", models.AnalysisComplete).
			Order("RANDOM()").
			First(&media).Error
		if err != nil {
			d.logger.Warn().Str("clock", clock.ID).Msg("no media found for clock")
			d.publishNowPlaying(entry, map[string]any{"clock_id": clock.ID, "clock_name": clock.Name, "error": "no matching media"})
			return nil
		}
		return d.playMedia(ctx, entry, media, map[string]any{
			"clock_id":   clock.ID,
			"clock_name": clock.Name,
			"slot_count": len(clock.Slots),
		})
	}
}

// findCurrentSlot finds the appropriate slot based on time offset within the hour
func (d *Director) findCurrentSlot(slots []models.ClockSlot, startsAt time.Time) *models.ClockSlot {
	// Calculate offset into the hour
	_, minute, second := startsAt.Clock()
	offsetNow := time.Duration(minute)*time.Minute + time.Duration(second)*time.Second

	// Find the slot that covers the current offset
	var currentSlot *models.ClockSlot
	for i := range slots {
		if slots[i].Offset <= offsetNow {
			currentSlot = &slots[i]
		} else {
			break
		}
	}
	return currentSlot
}

// startSmartBlockByID starts a smart block by ID (used by clock slots)
func (d *Director) startSmartBlockByID(ctx context.Context, entry models.ScheduleEntry, blockID, clockID, clockName string) error {
	var block models.SmartBlock
	if err := d.db.WithContext(ctx).First(&block, "id = ?", blockID).Error; err != nil {
		return fmt.Errorf("failed to load smart block: %w", err)
	}

	// Use SmartBlock Engine
	duration := entry.EndsAt.Sub(entry.StartsAt)
	req := smartblock.GenerateRequest{
		SmartBlockID: block.ID,
		Seed:         time.Now().UnixNano(),
		Duration:     duration.Milliseconds(),
		StationID:    entry.StationID,
		MountID:      entry.MountID,
	}

	result, err := d.smartblockEng.Generate(ctx, req)
	if err != nil || len(result.Items) == 0 {
		d.logger.Warn().Err(err).Str("block", block.ID).Msg("smart block generation failed")
		return nil
	}

	items := make([]string, len(result.Items))
	for i, item := range result.Items {
		items[i] = item.MediaID
	}

	var media models.MediaItem
	if err := d.db.WithContext(ctx).First(&media, "id = ?", items[0]).Error; err != nil {
		return fmt.Errorf("failed to load media item: %w", err)
	}

	return d.playMediaWithState(ctx, entry, media, "clock", clockID, 0, items, map[string]any{
		"clock_id":         clockID,
		"clock_name":       clockName,
		"smart_block_id":   block.ID,
		"smart_block_name": block.Name,
		"total_items":      len(items),
	})
}

// playMediaByID plays a specific media item by ID (used by clock hard_item slots)
func (d *Director) playMediaByID(ctx context.Context, entry models.ScheduleEntry, mediaID, clockID, clockName string) error {
	var media models.MediaItem
	if err := d.db.WithContext(ctx).First(&media, "id = ?", mediaID).Error; err != nil {
		return fmt.Errorf("failed to load media item: %w", err)
	}

	return d.playMediaWithState(ctx, entry, media, "clock", clockID, 0, []string{mediaID}, map[string]any{
		"clock_id":   clockID,
		"clock_name": clockName,
	})
}

// startPlaylistByID plays a playlist by ID (used by clock playlist slots)
func (d *Director) startPlaylistByID(ctx context.Context, entry models.ScheduleEntry, playlistID, clockID, clockName string) error {
	// Load playlist with items ordered by position
	var playlist models.Playlist
	if err := d.db.WithContext(ctx).Preload("Items", func(db *gorm.DB) *gorm.DB {
		return db.Order("position ASC")
	}).First(&playlist, "id = ?", playlistID).Error; err != nil {
		return fmt.Errorf("failed to load playlist: %w", err)
	}

	if len(playlist.Items) == 0 {
		d.logger.Warn().Str("playlist", playlist.ID).Str("clock", clockID).Msg("playlist is empty")
		d.publishNowPlaying(entry, map[string]any{
			"clock_id":      clockID,
			"clock_name":    clockName,
			"playlist_id":   playlist.ID,
			"playlist_name": playlist.Name,
			"error":         "empty playlist",
		})
		return nil
	}

	// Build items list (media IDs in order)
	items := make([]string, len(playlist.Items))
	for i, item := range playlist.Items {
		items[i] = item.MediaID
	}

	// Load first media item
	var media models.MediaItem
	if err := d.db.WithContext(ctx).First(&media, "id = ?", items[0]).Error; err != nil {
		return fmt.Errorf("failed to load media item: %w", err)
	}

	d.logger.Info().
		Str("clock", clockID).
		Str("playlist", playlist.Name).
		Int("tracks", len(items)).
		Msg("starting playlist from clock slot")

	return d.playMediaWithState(ctx, entry, media, "clock_playlist", playlist.ID, 0, items, map[string]any{
		"clock_id":      clockID,
		"clock_name":    clockName,
		"playlist_id":   playlist.ID,
		"playlist_name": playlist.Name,
		"total_items":   len(items),
	})
}

// startWebstreamByID starts a webstream by ID (used by clock slots)
func (d *Director) startWebstreamByID(ctx context.Context, entry models.ScheduleEntry, wsID, clockID, clockName string) error {
	// Reuse existing webstream entry logic with modified metadata
	originalSourceID := entry.SourceID
	entry.SourceID = wsID

	if entry.Metadata == nil {
		entry.Metadata = make(map[string]any)
	}
	entry.Metadata["clock_id"] = clockID
	entry.Metadata["clock_name"] = clockName

	err := d.startWebstreamEntry(ctx, entry)

	// Restore original source ID
	entry.SourceID = originalSourceID
	return err
}

// playMedia is a helper to start playing a media item
func (d *Director) playMedia(ctx context.Context, entry models.ScheduleEntry, media models.MediaItem, extraPayload map[string]any) error {
	// Build full path using media root
	// Use path directly if absolute, otherwise join with mediaRoot
	fullPath := media.Path
	if !filepath.IsAbs(media.Path) {
		fullPath = filepath.Join(d.mediaRoot, media.Path)
	}

	// Get mount configuration from database
	var mount models.Mount
	if err := d.db.WithContext(ctx).First(&mount, "id = ?", entry.MountID).Error; err != nil {
		d.logger.Warn().Err(err).Str("mount_id", entry.MountID).Msg("failed to load mount config, using defaults")
		// Use defaults
		mount.Format = "mp3"
		mount.Bitrate = 128
		mount.SampleRate = 44100
		mount.Channels = 2
		mount.Name = "main"
	}

	// Set mount defaults
	mountBitrate := mount.Bitrate
	if mountBitrate == 0 {
		mountBitrate = 128
	}
	lqBitrate := 64

	stationXFade := d.getCrossfadeConfig(ctx, entry.StationID)
	xfade := effectiveCrossfade(entry, stationXFade)

	stopAt := entry.EndsAt
	policy := d.getScheduleBoundaryPolicy(ctx, entry.StationID)
	if policy.Mode == "soft" && policy.SoftOverrun > 0 {
		stopAt = stopAt.Add(policy.SoftOverrun)
	}

	d.mu.Lock()
	d.active[entry.MountID] = playoutState{MediaID: media.ID, EntryID: entry.ID, StationID: entry.StationID, Started: entry.StartsAt, Ends: stopAt}
	d.mu.Unlock()

	// Stop any existing pipeline if needed.
	if xfade.Enabled && xfade.Duration > 0 {
		// For crossfade mode we prefer to keep a persistent PCM-input encoder running.
		// If the mount doesn't have a session yet, stop any existing file-input pipeline
		// so we can start the PCM encoder.
		d.xfadeMu.Lock()
		_, hasSess := d.xfadeSessions[entry.MountID]
		d.xfadeMu.Unlock()
		if !hasSess {
			_ = d.manager.StopPipeline(entry.MountID)
		}
	} else {
		if err := d.manager.StopPipeline(entry.MountID); err != nil {
			d.logger.Debug().Err(err).Str("mount", entry.MountID).Msg("stop pipeline failed")
		}
	}

	// Ensure broadcast mounts exist (HQ and LQ)
	contentType := "audio/mpeg"
	if mount.Format == "aac" {
		contentType = "audio/aac"
	} else if mount.Format == "ogg" || mount.Format == "vorbis" {
		contentType = "audio/ogg"
	}

	// Create high-quality mount
	broadcastMount := d.broadcast.GetMount(mount.Name)
	if broadcastMount == nil {
		broadcastMount = d.broadcast.CreateMount(mount.Name, contentType, mountBitrate)
		d.logger.Info().Str("mount", mount.Name).Int("bitrate", mountBitrate).Msg("created broadcast mount (HQ)")
	}

	// Create low-quality mount
	lqMountName := mount.Name + "-lq"
	lqMount := d.broadcast.GetMount(lqMountName)
	if lqMount == nil {
		lqMount = d.broadcast.CreateMount(lqMountName, contentType, lqBitrate)
		d.logger.Info().Str("mount", lqMountName).Int("bitrate", lqBitrate).Msg("created broadcast mount (LQ)")
	}

	d.logger.Info().
		Str("mount", entry.MountID).
		Str("media", media.Title).
		Str("artist", media.Artist).
		Msg("starting media playout")

	// Clear both buffers synchronously BEFORE starting feeds
	// This ensures HQ and LQ mounts transition to the new track together
	broadcastMount.ClearBuffer()
	lqMount.ClearBuffer()

	// Output handlers for encoder pipeline.
	hqHandler := func(r io.Reader) {
		if err := broadcastMount.FeedFrom(r); err != nil {
			d.logger.Debug().Err(err).Str("mount", mount.Name).Msg("HQ broadcast feed ended")
		}
	}
	lqHandler := func(r io.Reader) {
		if err := lqMount.FeedFrom(r); err != nil {
			d.logger.Debug().Err(err).Str("mount", lqMountName).Msg("LQ broadcast feed ended")
		}
	}

	if xfade.Enabled && xfade.Duration > 0 {
		webrtcPort := d.getWebRTCRTPPortForStation(ctx, entry.StationID)
		launch, err := d.buildPCMEncoderPipeline(mount, mountBitrate, lqBitrate, webrtcPort)
		if err != nil {
			return fmt.Errorf("build pcm encoder pipeline: %w", err)
		}

		stdin, err := d.manager.EnsurePipelineWithDualOutputAndInput(ctx, entry.MountID, launch, hqHandler, lqHandler)
		if err != nil {
			return fmt.Errorf("start pcm encoder pipeline: %w", err)
		}

		d.xfadeMu.Lock()
		sess := d.xfadeSessions[entry.MountID]
		if sess == nil {
			sess = newPCMCrossfadeSession(sessionConfig{
				GStreamerBin: d.cfg.GStreamerBin,
				SampleRate:   mount.SampleRate,
				Channels:     mount.Channels,
			}, stdin, d.logger.With().Str("mount_id", entry.MountID).Logger(), func() {
				d.handleTrackEnded(entry, mount.Name)
			})
			d.xfadeSessions[entry.MountID] = sess
			go func() {
				_ = sess.Pump(ctx)
			}()
		}
		sess.SetEncoderIn(stdin)
		sess.SetOnTrackEnd(func() { d.handleTrackEnded(entry, mount.Name) })
		d.xfadeMu.Unlock()

		if err := sess.Play(ctx, fullPath, xfade.Duration); err != nil {
			return fmt.Errorf("crossfade play: %w", err)
		}
	} else {
		// Build single GStreamer pipeline for both HQ and LQ (using tee for perfect sync)
		webrtcPort := d.getWebRTCRTPPortForStation(ctx, entry.StationID)
		launch, err := d.buildDualBroadcastPipeline(fullPath, mount, mountBitrate, lqBitrate, webrtcPort)
		if err != nil {
			return fmt.Errorf("build pipeline: %w", err)
		}

		// HQ output handler triggers next track when the pipeline ends (EOF).
		hqHandlerWithEnd := func(r io.Reader) {
			if err := broadcastMount.FeedFrom(r); err != nil {
				d.logger.Debug().Err(err).Str("mount", mount.Name).Msg("HQ broadcast feed ended")
			}
			d.handleTrackEnded(entry, mount.Name)
		}
		if err := d.manager.EnsurePipelineWithDualOutput(ctx, entry.MountID, launch, hqHandlerWithEnd, lqHandler); err != nil {
			d.logger.Warn().Err(err).Str("mount", entry.MountID).Msg("failed to start dual pipeline")
		}
	}

	payload := map[string]any{
		"media_id": media.ID,
		"title":    media.Title,
		"artist":   media.Artist,
		"album":    media.Album,
	}
	for k, v := range extraPayload {
		payload[k] = v
	}

	d.publishNowPlaying(entry, payload)
	d.scheduleStop(ctx, entry.StationID, entry.MountID, entry.EndsAt)

	return nil
}

// playMediaWithState plays media with enhanced state tracking for playlists and smart blocks.
// This enables sequential playback and position persistence.
func (d *Director) playMediaWithState(ctx context.Context, entry models.ScheduleEntry, media models.MediaItem, sourceType, sourceID string, position int, items []string, extraPayload map[string]any) error {
	// Build full path using media root
	// Use path directly if absolute, otherwise join with mediaRoot
	fullPath := media.Path
	if !filepath.IsAbs(media.Path) {
		fullPath = filepath.Join(d.mediaRoot, media.Path)
	}

	// Get mount configuration from database
	var mount models.Mount
	if err := d.db.WithContext(ctx).First(&mount, "id = ?", entry.MountID).Error; err != nil {
		d.logger.Warn().Err(err).Str("mount_id", entry.MountID).Msg("failed to load mount config, using defaults")
		mount.Format = "mp3"
		mount.Bitrate = 128
		mount.SampleRate = 44100
		mount.Channels = 2
		mount.Name = "main"
	}

	// Set mount defaults
	mountBitrate := mount.Bitrate
	if mountBitrate == 0 {
		mountBitrate = 128
	}
	lqBitrate := 64

	stationXFade := d.getCrossfadeConfig(ctx, entry.StationID)
	xfade := effectiveCrossfade(entry, stationXFade)

	// Store enhanced state
	d.mu.Lock()
	stopAt := entry.EndsAt
	policy := d.getScheduleBoundaryPolicy(ctx, entry.StationID)
	if policy.Mode == "soft" && policy.SoftOverrun > 0 {
		stopAt = stopAt.Add(policy.SoftOverrun)
	}

	d.active[entry.MountID] = playoutState{
		MediaID:    media.ID,
		EntryID:    entry.ID,
		StationID:  entry.StationID,
		Started:    time.Now(),
		Ends:       stopAt,
		SourceType: sourceType,
		SourceID:   sourceID,
		Position:   position,
		TotalItems: len(items),
		Items:      items,
	}
	d.mu.Unlock()
	// Persist on transition (not on the 250ms director tick).
	d.mu.Lock()
	s := d.active[entry.MountID]
	d.mu.Unlock()
	d.persistMountState(ctx, entry.MountID, s)

	// Stop any existing pipeline if needed.
	if xfade.Enabled && xfade.Duration > 0 {
		d.xfadeMu.Lock()
		_, hasSess := d.xfadeSessions[entry.MountID]
		d.xfadeMu.Unlock()
		if !hasSess {
			_ = d.manager.StopPipeline(entry.MountID)
		}
	} else {
		if err := d.manager.StopPipeline(entry.MountID); err != nil {
			d.logger.Debug().Err(err).Str("mount", entry.MountID).Msg("stop pipeline failed")
		}
	}

	// Ensure broadcast mounts exist
	contentType := "audio/mpeg"
	if mount.Format == "aac" {
		contentType = "audio/aac"
	} else if mount.Format == "ogg" || mount.Format == "vorbis" {
		contentType = "audio/ogg"
	}

	broadcastMount := d.broadcast.GetMount(mount.Name)
	if broadcastMount == nil {
		broadcastMount = d.broadcast.CreateMount(mount.Name, contentType, mountBitrate)
		d.logger.Info().Str("mount", mount.Name).Int("bitrate", mountBitrate).Msg("created broadcast mount (HQ)")
	}

	lqMountName := mount.Name + "-lq"
	lqMount := d.broadcast.GetMount(lqMountName)
	if lqMount == nil {
		lqMount = d.broadcast.CreateMount(lqMountName, contentType, lqBitrate)
		d.logger.Info().Str("mount", lqMountName).Int("bitrate", lqBitrate).Msg("created broadcast mount (LQ)")
	}

	d.logger.Info().
		Str("mount", entry.MountID).
		Str("media", media.Title).
		Str("artist", media.Artist).
		Str("source_type", sourceType).
		Int("position", position).
		Int("total", len(items)).
		Msg("starting media playout with state tracking")

	broadcastMount.ClearBuffer()
	lqMount.ClearBuffer()

	hqHandler := func(r io.Reader) {
		if err := broadcastMount.FeedFrom(r); err != nil {
			d.logger.Debug().Err(err).Str("mount", mount.Name).Msg("HQ broadcast feed ended")
		}
	}
	lqHandler := func(r io.Reader) {
		if err := lqMount.FeedFrom(r); err != nil {
			d.logger.Debug().Err(err).Str("mount", lqMountName).Msg("LQ broadcast feed ended")
		}
	}

	if xfade.Enabled && xfade.Duration > 0 {
		webrtcPort := d.getWebRTCRTPPortForStation(ctx, entry.StationID)
		launch, err := d.buildPCMEncoderPipeline(mount, mountBitrate, lqBitrate, webrtcPort)
		if err != nil {
			return fmt.Errorf("build pcm encoder pipeline: %w", err)
		}
		stdin, err := d.manager.EnsurePipelineWithDualOutputAndInput(ctx, entry.MountID, launch, hqHandler, lqHandler)
		if err != nil {
			return fmt.Errorf("start pcm encoder pipeline: %w", err)
		}

		d.xfadeMu.Lock()
		sess := d.xfadeSessions[entry.MountID]
		if sess == nil {
			sess = newPCMCrossfadeSession(sessionConfig{
				GStreamerBin: d.cfg.GStreamerBin,
				SampleRate:   mount.SampleRate,
				Channels:     mount.Channels,
			}, stdin, d.logger.With().Str("mount_id", entry.MountID).Logger(), func() {
				d.handleTrackEnded(entry, mount.Name)
			})
			d.xfadeSessions[entry.MountID] = sess
			go func() {
				_ = sess.Pump(ctx)
			}()
		}
		sess.SetEncoderIn(stdin)
		sess.SetOnTrackEnd(func() { d.handleTrackEnded(entry, mount.Name) })
		d.xfadeMu.Unlock()

		if err := sess.Play(ctx, fullPath, xfade.Duration); err != nil {
			return fmt.Errorf("crossfade play: %w", err)
		}
	} else {
		webrtcPort := d.getWebRTCRTPPortForStation(ctx, entry.StationID)
		launch, err := d.buildDualBroadcastPipeline(fullPath, mount, mountBitrate, lqBitrate, webrtcPort)
		if err != nil {
			return fmt.Errorf("build pipeline: %w", err)
		}
		hqHandlerWithEnd := func(r io.Reader) {
			if err := broadcastMount.FeedFrom(r); err != nil {
				d.logger.Debug().Err(err).Str("mount", mount.Name).Msg("HQ broadcast feed ended")
			}
			d.handleTrackEnded(entry, mount.Name)
		}
		if err := d.manager.EnsurePipelineWithDualOutput(ctx, entry.MountID, launch, hqHandlerWithEnd, lqHandler); err != nil {
			d.logger.Warn().Err(err).Str("mount", entry.MountID).Msg("failed to start dual pipeline")
		}
	}

	payload := map[string]any{
		"media_id": media.ID,
		"title":    media.Title,
		"artist":   media.Artist,
		"album":    media.Album,
		"position": position,
	}
	for k, v := range extraPayload {
		payload[k] = v
	}

	d.publishNowPlaying(entry, payload)
	d.scheduleStop(ctx, entry.StationID, entry.MountID, entry.EndsAt)

	return nil
}

// buildBroadcastPipeline creates a GStreamer pipeline that outputs to stdout for the broadcast server
func (d *Director) buildBroadcastPipeline(filePath string, mount models.Mount) (string, error) {
	// Determine format
	format := mediaengine.AudioFormat(mount.Format)
	if format == "" {
		format = mediaengine.AudioFormatMP3
	}

	// Set defaults for encoding
	bitrate := mount.Bitrate
	if bitrate == 0 {
		bitrate = 128
	}
	sampleRate := mount.SampleRate
	if sampleRate == 0 {
		sampleRate = 44100
	}
	channels := mount.Channels
	if channels == 0 {
		channels = 2
	}

	// Build encoder configuration - outputs to stdout
	encoderCfg := mediaengine.EncoderConfig{
		OutputType: mediaengine.OutputTypeStdout,
		Format:     format,
		Bitrate:    bitrate,
		SampleRate: sampleRate,
		Channels:   channels,
	}

	builder := mediaengine.NewEncoderBuilder(encoderCfg)
	encoderPipeline, err := builder.Build()
	if err != nil {
		return "", fmt.Errorf("build encoder: %w", err)
	}

	// Build complete pipeline: file source -> decode -> encoder -> stdout
	pipeline := fmt.Sprintf("filesrc location=%q ! decodebin ! %s", filePath, encoderPipeline)

	d.logger.Debug().
		Str("pipeline", pipeline).
		Str("mount", mount.Name).
		Int("bitrate", bitrate).
		Msg("built broadcast pipeline")

	return pipeline, nil
}

// buildDualBroadcastPipeline creates a GStreamer pipeline that outputs both HQ and LQ streams.
// Uses tee to split decoded audio, encodes to HQ (fd=3) and LQ (fd=4) simultaneously.
// If WebRTC is enabled, also outputs RTP/Opus to UDP for low-latency streaming.
// This ensures all streams are perfectly synchronized from the same decode.
func (d *Director) buildDualBroadcastPipeline(filePath string, mount models.Mount, hqBitrate, lqBitrate int, webrtcRTPPort int) (string, error) {
	// Determine format
	format := mount.Format
	if format == "" {
		format = "mp3"
	}

	// Set defaults
	sampleRate := mount.SampleRate
	if sampleRate == 0 {
		sampleRate = 44100
	}
	channels := mount.Channels
	if channels == 0 {
		channels = 2
	}
	if hqBitrate == 0 {
		hqBitrate = 128
	}
	if lqBitrate == 0 {
		lqBitrate = 64
	}

	// Build encoder element based on format
	var hqEncoder, lqEncoder string
	switch format {
	case "aac":
		hqEncoder = fmt.Sprintf("faac bitrate=%d ! audio/mpeg,mpegversion=4", hqBitrate*1000)
		lqEncoder = fmt.Sprintf("faac bitrate=%d ! audio/mpeg,mpegversion=4", lqBitrate*1000)
	case "ogg", "vorbis":
		hqEncoder = fmt.Sprintf("vorbisenc bitrate=%d ! oggmux", hqBitrate*1000)
		lqEncoder = fmt.Sprintf("vorbisenc bitrate=%d ! oggmux", lqBitrate*1000)
	default: // mp3
		hqEncoder = fmt.Sprintf("lamemp3enc target=1 bitrate=%d cbr=true", hqBitrate)
		lqEncoder = fmt.Sprintf("lamemp3enc target=1 bitrate=%d cbr=true", lqBitrate)
	}

	// Build pipeline with tee:
	// filesrc -> decodebin -> audioconvert -> audioresample -> tee
	//   tee.src_0 -> queue -> HQ encoder -> identity sync=true -> fdsink fd=3
	//   tee.src_1 -> queue -> LQ encoder -> identity sync=true -> fdsink fd=4
	//   tee.src_2 -> queue -> Opus encoder -> rtpopuspay -> udpsink (WebRTC)
	// NOTE: identity sync=true is CRITICAL - it throttles output to real-time speed
	// Without it, GStreamer processes the file as fast as possible (sub-second for a 7-min track)

	var webrtcBranch string
	if d.webrtcEnabled && webrtcRTPPort > 0 {
		// WebRTC branch: resample to 48kHz for Opus, encode, packetize, send via UDP
		rtpPort := webrtcRTPPort
		// Opus requires 48kHz, so we resample here since the main tee outputs at mount sampleRate
		webrtcBranch = fmt.Sprintf(
			` t. ! queue ! audioresample ! audio/x-raw,rate=48000 ! opusenc bitrate=128000 ! rtpopuspay pt=111 ! udpsink host=127.0.0.1 port=%d`,
			rtpPort,
		)
	}

	pipeline := fmt.Sprintf(
		`filesrc location=%q ! decodebin ! audioconvert ! audioresample ! audio/x-raw,rate=%d,channels=%d ! tee name=t `+
			`t. ! queue ! %s ! identity sync=true ! fdsink fd=3 `+
			`t. ! queue ! %s ! identity sync=true ! fdsink fd=4%s`,
		filePath, sampleRate, channels, hqEncoder, lqEncoder, webrtcBranch,
	)

	d.logger.Debug().
		Str("pipeline", pipeline).
		Str("mount", mount.Name).
		Int("hq_bitrate", hqBitrate).
		Int("lq_bitrate", lqBitrate).
		Bool("webrtc", d.webrtcEnabled && webrtcRTPPort > 0).
		Int("webrtc_rtp_port", webrtcRTPPort).
		Msg("built dual broadcast pipeline")

	return pipeline, nil
}

func (d *Director) buildPCMEncoderPipeline(mount models.Mount, hqBitrate, lqBitrate int, webrtcRTPPort int) (string, error) {
	format := mount.Format
	if format == "" {
		format = "mp3"
	}
	sampleRate := mount.SampleRate
	if sampleRate == 0 {
		sampleRate = 44100
	}
	channels := mount.Channels
	if channels == 0 {
		channels = 2
	}
	if hqBitrate == 0 {
		hqBitrate = 128
	}
	if lqBitrate == 0 {
		lqBitrate = 64
	}

	var hqEncoder, lqEncoder string
	switch format {
	case "aac":
		hqEncoder = fmt.Sprintf("faac bitrate=%d ! audio/mpeg,mpegversion=4", hqBitrate*1000)
		lqEncoder = fmt.Sprintf("faac bitrate=%d ! audio/mpeg,mpegversion=4", lqBitrate*1000)
	case "ogg", "vorbis":
		hqEncoder = fmt.Sprintf("vorbisenc bitrate=%d ! oggmux", hqBitrate*1000)
		lqEncoder = fmt.Sprintf("vorbisenc bitrate=%d ! oggmux", lqBitrate*1000)
	default:
		hqEncoder = fmt.Sprintf("lamemp3enc target=1 bitrate=%d cbr=true", hqBitrate)
		lqEncoder = fmt.Sprintf("lamemp3enc target=1 bitrate=%d cbr=true", lqBitrate)
	}

	var webrtcBranch string
	if d.webrtcEnabled && webrtcRTPPort > 0 {
		webrtcBranch = fmt.Sprintf(
			` t. ! queue ! audioresample ! audio/x-raw,rate=48000 ! opusenc bitrate=128000 ! rtpopuspay pt=111 ! udpsink host=127.0.0.1 port=%d`,
			webrtcRTPPort,
		)
	}

	// Read raw PCM from stdin, encode to HQ/LQ and optionally RTP/Opus.
	pipeline := fmt.Sprintf(
		`fdsrc fd=0 ! queue ! audio/x-raw,format=S16LE,rate=%d,channels=%d ! audioconvert ! audioresample ! tee name=t `+
			`t. ! queue ! %s ! fdsink fd=3 `+
			`t. ! queue ! %s ! fdsink fd=4%s`,
		sampleRate, channels, hqEncoder, lqEncoder, webrtcBranch,
	)

	return pipeline, nil
}

// handleTrackEnded picks and plays the next track when the current one finishes
func (d *Director) handleTrackEnded(entry models.ScheduleEntry, mountName string) {
	now := time.Now().UTC()

	// Check if we're still within the schedule window
	if now.After(entry.EndsAt) {
		d.logger.Debug().Str("entry", entry.ID).Msg("schedule entry ended, not starting next track")
		return
	}

	// Wait a small delay before starting next track to avoid race conditions
	time.Sleep(100 * time.Millisecond)

	// Get the active state to check if something else started playing
	d.mu.Lock()
	state, ok := d.active[entry.MountID]
	d.mu.Unlock()

	if !ok || state.EntryID != entry.ID {
		// Different entry now active, don't interfere
		return
	}

	// Context-aware track selection based on source type
	switch state.SourceType {
	case "playlist":
		// Playlists wrap around
		nextPos := (state.Position + 1) % state.TotalItems
		if len(state.Items) > nextPos {
			d.updateEntryPosition(entry.ID, nextPos)
			d.playNextFromState(entry, state, nextPos, mountName)
			return
		}

	case "clock_playlist":
		// Playlist slots inside clocks should also wrap around until slot end.
		nextPos := (state.Position + 1) % state.TotalItems
		if len(state.Items) > nextPos {
			d.updateEntryPosition(entry.ID, nextPos)
			d.playNextFromState(entry, state, nextPos, mountName)
			return
		}

	case "smart_block":
		// Smart blocks stop at end
		nextPos := state.Position + 1
		if nextPos >= state.TotalItems {
			d.logger.Debug().
				Str("entry", entry.ID).
				Str("source", state.SourceID).
				Msg("smart block sequence complete")
			return
		}
		if len(state.Items) > nextPos {
			d.playNextFromState(entry, state, nextPos, mountName)
			return
		}

	case "clock":
		// Clock-driven sequences should keep cycling until schedule entry ends.
		// This allows clock templates to back longer blocks (e.g. 2-3 hour shows).
		if state.TotalItems <= 0 {
			break
		}
		nextPos := (state.Position + 1) % state.TotalItems
		if len(state.Items) > nextPos {
			d.playNextFromState(entry, state, nextPos, mountName)
			return
		}
		// Otherwise fall through to random selection
	}

	// Default: random track selection (for plain media entries or exhausted sequences)
	d.playRandomNextTrack(entry, mountName)
}

// updateEntryPosition persists the current playlist position to entry metadata
func (d *Director) updateEntryPosition(entryID string, position int) {
	if err := d.db.Model(&models.ScheduleEntry{}).
		Where("id = ?", entryID).
		Update("metadata", gorm.Expr("jsonb_set(COALESCE(metadata, '{}')::jsonb, '{current_position}', ?::jsonb)", position)).
		Error; err != nil {
		d.logger.Warn().Err(err).Str("entry", entryID).Int("position", position).Msg("failed to update entry position")
	}
}

// playNextFromState plays the next track from the pre-loaded state items
func (d *Director) playNextFromState(entry models.ScheduleEntry, state playoutState, nextPos int, mountName string) {
	if nextPos >= len(state.Items) {
		d.logger.Warn().Int("position", nextPos).Int("total", len(state.Items)).Msg("position out of bounds")
		return
	}

	nextMediaID := state.Items[nextPos]

	var media models.MediaItem
	if err := d.db.First(&media, "id = ?", nextMediaID).Error; err != nil {
		d.logger.Warn().Err(err).Str("media_id", nextMediaID).Msg("failed to load next media item")
		return
	}

	// Get mount config
	var mount models.Mount
	if err := d.db.First(&mount, "id = ?", entry.MountID).Error; err != nil {
		d.logger.Warn().Err(err).Str("mount_id", entry.MountID).Msg("failed to load mount config")
		return
	}

	hqBitrate := mount.Bitrate
	if hqBitrate == 0 {
		hqBitrate = 128
	}
	lqBitrate := 64

	// Use path directly if absolute, otherwise join with mediaRoot
	fullPath := media.Path
	if !filepath.IsAbs(media.Path) {
		fullPath = filepath.Join(d.mediaRoot, media.Path)
	}
	stationXFade := d.getCrossfadeConfig(context.Background(), entry.StationID)
	xfade := effectiveCrossfade(entry, stationXFade)

	// Update active state with new position
	d.mu.Lock()
	d.active[entry.MountID] = playoutState{
		MediaID:    media.ID,
		EntryID:    entry.ID,
		StationID:  entry.StationID,
		Started:    time.Now(),
		Ends:       entry.EndsAt,
		SourceType: state.SourceType,
		SourceID:   state.SourceID,
		Position:   nextPos,
		TotalItems: state.TotalItems,
		Items:      state.Items,
	}
	d.mu.Unlock()
	d.mu.Lock()
	s := d.active[entry.MountID]
	d.mu.Unlock()
	d.persistMountState(context.Background(), entry.MountID, s)

	broadcastMount := d.broadcast.GetMount(mount.Name)
	if broadcastMount == nil {
		d.logger.Warn().Str("mount", mount.Name).Msg("broadcast mount not found")
		return
	}

	lqMountName := mount.Name + "-lq"
	lqMount := d.broadcast.GetMount(lqMountName)
	if lqMount == nil {
		d.logger.Warn().Str("mount", lqMountName).Msg("LQ broadcast mount not found")
		return
	}

	d.logger.Info().
		Str("mount", entry.MountID).
		Str("media", media.Title).
		Str("artist", media.Artist).
		Str("source_type", state.SourceType).
		Int("position", nextPos).
		Int("total", state.TotalItems).
		Msg("starting next track from sequence")

	broadcastMount.ClearBuffer()
	lqMount.ClearBuffer()

	ctx := context.Background()
	hqHandler := func(r io.Reader) {
		if err := broadcastMount.FeedFrom(r); err != nil {
			d.logger.Debug().Err(err).Str("mount", mount.Name).Msg("HQ broadcast feed ended")
		}
	}
	lqHandler := func(r io.Reader) {
		if err := lqMount.FeedFrom(r); err != nil {
			d.logger.Debug().Err(err).Str("mount", lqMountName).Msg("LQ broadcast feed ended")
		}
	}

	if xfade.Enabled && xfade.Duration > 0 {
		webrtcPort := d.getWebRTCRTPPortForStation(ctx, entry.StationID)
		encLaunch, err := d.buildPCMEncoderPipeline(mount, hqBitrate, lqBitrate, webrtcPort)
		if err != nil {
			d.logger.Warn().Err(err).Msg("failed to build pcm encoder pipeline")
			return
		}
		stdin, err := d.manager.EnsurePipelineWithDualOutputAndInput(ctx, entry.MountID, encLaunch, hqHandler, lqHandler)
		if err != nil {
			d.logger.Warn().Err(err).Str("mount", entry.MountID).Msg("failed to start pcm encoder pipeline")
			return
		}

		d.xfadeMu.Lock()
		sess := d.xfadeSessions[entry.MountID]
		if sess == nil {
			sess = newPCMCrossfadeSession(sessionConfig{
				GStreamerBin: d.cfg.GStreamerBin,
				SampleRate:   mount.SampleRate,
				Channels:     mount.Channels,
			}, stdin, d.logger.With().Str("mount_id", entry.MountID).Logger(), func() {
				d.handleTrackEnded(entry, mount.Name)
			})
			d.xfadeSessions[entry.MountID] = sess
			go func() { _ = sess.Pump(ctx) }()
		}
		sess.SetEncoderIn(stdin)
		sess.SetOnTrackEnd(func() { d.handleTrackEnded(entry, mount.Name) })
		d.xfadeMu.Unlock()

		if err := sess.Play(ctx, fullPath, xfade.Duration); err != nil {
			d.logger.Warn().Err(err).Msg("crossfade play failed")
		}
	} else {
		webrtcPort := d.getWebRTCRTPPortForStation(ctx, entry.StationID)
		launch, err := d.buildDualBroadcastPipeline(fullPath, mount, hqBitrate, lqBitrate, webrtcPort)
		if err != nil {
			d.logger.Warn().Err(err).Msg("failed to build pipeline for next track")
			return
		}
		hqHandlerWithEnd := func(r io.Reader) {
			if err := broadcastMount.FeedFrom(r); err != nil {
				d.logger.Debug().Err(err).Str("mount", mount.Name).Msg("HQ broadcast feed ended")
			}
			d.handleTrackEnded(entry, mount.Name)
		}
		if err := d.manager.EnsurePipelineWithDualOutput(ctx, entry.MountID, launch, hqHandlerWithEnd, lqHandler); err != nil {
			d.logger.Warn().Err(err).Str("mount", entry.MountID).Msg("failed to start next track pipeline")
		}
	}

	d.publishNowPlaying(entry, map[string]any{
		"media_id": media.ID,
		"title":    media.Title,
		"artist":   media.Artist,
		"album":    media.Album,
		"position": nextPos,
	})
}

// playRandomNextTrack plays a random track from the station (fallback behavior)
func (d *Director) playRandomNextTrack(entry models.ScheduleEntry, mountName string) {
	var media models.MediaItem
	err := d.db.
		Where("station_id = ?", entry.StationID).
		Where("analysis_state = ?", models.AnalysisComplete).
		Order("RANDOM()").
		First(&media).Error
	if err != nil {
		d.logger.Warn().Err(err).Str("station", entry.StationID).Msg("no media found for next track")
		return
	}

	var mount models.Mount
	if err := d.db.First(&mount, "id = ?", entry.MountID).Error; err != nil {
		d.logger.Warn().Err(err).Str("mount_id", entry.MountID).Msg("failed to load mount config")
		return
	}

	hqBitrate := mount.Bitrate
	if hqBitrate == 0 {
		hqBitrate = 128
	}
	lqBitrate := 64

	// Use path directly if absolute, otherwise join with mediaRoot
	fullPath := media.Path
	if !filepath.IsAbs(media.Path) {
		fullPath = filepath.Join(d.mediaRoot, media.Path)
	}
	webrtcPort := d.getWebRTCRTPPortForStation(context.Background(), entry.StationID)
	launch, err := d.buildDualBroadcastPipeline(fullPath, mount, hqBitrate, lqBitrate, webrtcPort)
	if err != nil {
		d.logger.Warn().Err(err).Msg("failed to build pipeline for next track")
		return
	}

	d.mu.Lock()
	d.active[entry.MountID] = playoutState{
		MediaID:   media.ID,
		EntryID:   entry.ID,
		StationID: entry.StationID,
		Started:   time.Now(),
		Ends:      entry.EndsAt,
	}
	d.mu.Unlock()
	d.mu.Lock()
	s := d.active[entry.MountID]
	d.mu.Unlock()
	d.persistMountState(context.Background(), entry.MountID, s)

	broadcastMount := d.broadcast.GetMount(mount.Name)
	if broadcastMount == nil {
		d.logger.Warn().Str("mount", mount.Name).Msg("broadcast mount not found for next track")
		return
	}

	lqMountName := mount.Name + "-lq"
	lqMount := d.broadcast.GetMount(lqMountName)
	if lqMount == nil {
		d.logger.Warn().Str("mount", lqMountName).Msg("LQ broadcast mount not found for next track")
		return
	}

	d.logger.Info().
		Str("mount", entry.MountID).
		Str("media", media.Title).
		Str("artist", media.Artist).
		Msg("starting random next track")

	broadcastMount.ClearBuffer()
	lqMount.ClearBuffer()

	ctx := context.Background()
	hqHandler := func(r io.Reader) {
		if err := broadcastMount.FeedFrom(r); err != nil {
			d.logger.Debug().Err(err).Str("mount", mount.Name).Msg("HQ broadcast feed ended")
		}
		d.handleTrackEnded(entry, mount.Name)
	}

	lqHandler := func(r io.Reader) {
		if err := lqMount.FeedFrom(r); err != nil {
			d.logger.Debug().Err(err).Str("mount", lqMountName).Msg("LQ broadcast feed ended")
		}
	}

	if err := d.manager.EnsurePipelineWithDualOutput(ctx, entry.MountID, launch, hqHandler, lqHandler); err != nil {
		d.logger.Warn().Err(err).Str("mount", entry.MountID).Msg("failed to start next track dual pipeline")
	}

	d.publishNowPlaying(entry, map[string]any{
		"media_id": media.ID,
		"title":    media.Title,
		"artist":   media.Artist,
		"album":    media.Album,
	})
}

func (d *Director) scheduleStop(ctx context.Context, stationID, mountID string, endsAt time.Time) {
	policy := d.getScheduleBoundaryPolicy(ctx, stationID)
	stopAt := endsAt
	if policy.Mode == "soft" && policy.SoftOverrun > 0 {
		stopAt = stopAt.Add(policy.SoftOverrun)
	}
	xfade := d.getCrossfadeConfig(ctx, stationID)

	delay := time.Until(stopAt)
	if delay < 0 {
		delay = 0
	}
	go func(expected time.Time) {
		timer := time.NewTimer(delay + 200*time.Millisecond)
		defer timer.Stop()
		<-timer.C

		d.mu.Lock()
		state, ok := d.active[mountID]
		if !ok || state.Ends.After(expected.Add(500*time.Millisecond)) {
			d.mu.Unlock()
			return
		}
		delete(d.active, mountID)
		d.mu.Unlock()
		d.clearPersistedMountState(context.Background(), mountID)

		// In crossfade mode we keep a persistent encoder pipeline per-mount so transitions can overlap.
		// We only stop the pipeline when explicitly requested (emergency stop/skip/etc.).
		if !(xfade.Enabled && xfade.Duration > 0) {
			if err := d.manager.StopPipeline(mountID); err != nil {
				d.logger.Debug().Err(err).Str("mount", mountID).Msg("scheduled stop failed")
			}
		}
		d.bus.Publish(events.EventHealth, events.Payload{
			"station_id": state.StationID,
			"mount_id":   mountID,
			"entry_id":   state.EntryID,
			"media_id":   state.MediaID,
			"starts_at":  state.Started,
			"ends_at":    state.Ends,
			"event":      "ended",
			"status":     "ended",
		})
	}(stopAt)
}

func (d *Director) emitHealthSnapshot() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for mountID, state := range d.active {
		d.bus.Publish(events.EventHealth, events.Payload{
			"station_id": state.StationID,
			"mount_id":   mountID,
			"entry_id":   state.EntryID,
			"media_id":   state.MediaID,
			"started_at": state.Started,
			"starts_at":  state.Started,
			"ends_at":    state.Ends,
			"status":     "playing",
		})
	}
}

func (d *Director) publishNowPlaying(entry models.ScheduleEntry, extra map[string]any) {
	payload := events.Payload{
		"entry_id":    entry.ID,
		"station_id":  entry.StationID,
		"mount_id":    entry.MountID,
		"source_type": entry.SourceType,
		"source_id":   entry.SourceID,
		"starts_at":   entry.StartsAt,
		"ends_at":     entry.EndsAt,
	}
	for k, v := range entry.Metadata {
		payload[k] = v
	}
	payload["metadata"] = entry.Metadata
	for k, v := range extra {
		payload[k] = v
	}
	d.bus.Publish(events.EventNowPlaying, payload)

	// Record to play history
	d.recordPlayHistory(entry, extra)
}

func (d *Director) recordPlayHistory(entry models.ScheduleEntry, extra map[string]any) {
	// Extract media info from extra payload
	title, _ := extra["title"].(string)
	artist, _ := extra["artist"].(string)
	album, _ := extra["album"].(string)
	mediaID, _ := extra["media_id"].(string)

	// For webstreams, use webstream name as title
	if entry.SourceType == "webstream" {
		if name, ok := extra["webstream_name"].(string); ok && title == "" {
			title = name
		}
	}

	// For live, set title to "Live DJ"
	if entry.SourceType == "live" {
		if title == "" {
			title = "Live DJ"
		}
	}

	// Don't record if no title
	if title == "" {
		return
	}

	// Use actual current time for started_at, not schedule entry time
	now := time.Now().UTC()
	startedAt := now

	// Try to get media duration to estimate end time
	var endedAt time.Time
	if mediaID != "" {
		var media models.MediaItem
		if err := d.db.First(&media, "id = ?", mediaID).Error; err == nil && media.Duration > 0 {
			endedAt = startedAt.Add(media.Duration)
		}
	}
	// If we couldn't get duration, use a default of 5 minutes
	if endedAt.IsZero() {
		endedAt = startedAt.Add(5 * time.Minute)
	}

	history := models.PlayHistory{
		ID:        uuid.New().String(),
		StationID: entry.StationID,
		MountID:   entry.MountID,
		MediaID:   mediaID,
		Artist:    artist,
		Title:     title,
		Album:     album,
		StartedAt: startedAt,
		EndedAt:   endedAt,
		Metadata:  extra,
	}

	if err := d.db.Create(&history).Error; err != nil {
		d.logger.Warn().Err(err).Msg("failed to record play history")
	}
}

func (d *Director) isPlayed(entryID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.played[entryID]
	return ok
}

func (d *Director) markPlayed(entryID string, endsAt time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.played[entryID] = endsAt
}

func (d *Director) prunePlayed(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, endsAt := range d.played {
		if endsAt.Add(30 * time.Minute).Before(now) {
			delete(d.played, id)
		}
	}
}

// StopStation stops all playout for a specific station.
// This is an emergency stop that clears all active pipelines for the station's mounts.
func (d *Director) StopStation(ctx context.Context, stationID string) (int, error) {
	// Get all mounts for this station
	var mounts []models.Mount
	if err := d.db.WithContext(ctx).Where("station_id = ?", stationID).Find(&mounts).Error; err != nil {
		return 0, fmt.Errorf("failed to load station mounts: %w", err)
	}

	stopped := 0
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, mount := range mounts {
		// Stop crossfade session for this mount (if any).
		d.xfadeMu.Lock()
		if sess := d.xfadeSessions[mount.ID]; sess != nil {
			_ = sess.Close()
			delete(d.xfadeSessions, mount.ID)
		}
		d.xfadeMu.Unlock()

		// Stop pipeline for this mount
		if err := d.manager.StopPipeline(mount.ID); err != nil {
			d.logger.Warn().Err(err).Str("mount", mount.ID).Msg("failed to stop pipeline")
			continue
		}

		// Clear active state for this mount
		if state, ok := d.active[mount.ID]; ok {
			delete(d.active, mount.ID)
			d.clearPersistedMountState(ctx, mount.ID)
			stopped++

			// Publish stop event
			d.bus.Publish(events.EventHealth, events.Payload{
				"station_id": stationID,
				"mount_id":   mount.ID,
				"entry_id":   state.EntryID,
				"media_id":   state.MediaID,
				"event":      "emergency_stop",
				"status":     "stopped",
			})
		}
	}

	d.logger.Info().
		Str("station_id", stationID).
		Int("mounts_stopped", stopped).
		Msg("emergency stop executed for station")

	return stopped, nil
}

// SkipStation skips the current track on all active mounts for a station.
// It advances to the next item in the active sequence (playlist/clock/smart block)
// when possible, or falls back to random next-track selection.
func (d *Director) SkipStation(ctx context.Context, stationID string) (int, error) {
	var mounts []models.Mount
	if err := d.db.WithContext(ctx).Where("station_id = ?", stationID).Find(&mounts).Error; err != nil {
		return 0, fmt.Errorf("failed to load station mounts: %w", err)
	}

	skipped := 0
	for _, mount := range mounts {
		// If crossfade session exists, cancel decoders and keep encoder alive;
		// then let handleTrackEnded pick the next item.
		d.xfadeMu.Lock()
		sess := d.xfadeSessions[mount.ID]
		d.xfadeMu.Unlock()
		if sess != nil {
			// Best-effort: stop current decoder by closing the session and recreating on next play.
			_ = sess.Close()
			d.xfadeMu.Lock()
			delete(d.xfadeSessions, mount.ID)
			d.xfadeMu.Unlock()
		}

		d.mu.Lock()
		state, ok := d.active[mount.ID]
		d.mu.Unlock()
		if !ok {
			continue
		}

		if err := d.manager.StopPipeline(mount.ID); err != nil {
			d.logger.Warn().Err(err).Str("mount", mount.ID).Msg("failed to stop pipeline for skip")
		}

		entry := models.ScheduleEntry{
			ID:        state.EntryID,
			StationID: state.StationID,
			MountID:   mount.ID,
			EndsAt:    state.Ends,
		}

		skipped++
		go d.handleTrackEnded(entry, mount.Name)
	}

	d.logger.Info().
		Str("station_id", stationID).
		Int("mounts_skipped", skipped).
		Msg("skip station executed")

	return skipped, nil
}

// ReloadStation stops active pipelines for a station and clears active state so
// the director tick can rebuild playout from current schedule.
func (d *Director) ReloadStation(ctx context.Context, stationID string) (int, error) {
	var mounts []models.Mount
	if err := d.db.WithContext(ctx).Where("station_id = ?", stationID).Find(&mounts).Error; err != nil {
		return 0, fmt.Errorf("failed to load station mounts: %w", err)
	}

	reloaded := 0
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, mount := range mounts {
		if err := d.manager.StopPipeline(mount.ID); err != nil {
			d.logger.Warn().Err(err).Str("mount", mount.ID).Msg("failed to stop pipeline during reload")
		}
		if _, ok := d.active[mount.ID]; ok {
			delete(d.active, mount.ID)
			d.clearPersistedMountState(ctx, mount.ID)
			reloaded++
		}
	}

	d.logger.Info().
		Str("station_id", stationID).
		Int("mounts_reloaded", reloaded).
		Msg("station playout reloaded")

	return reloaded, nil
}

// ListenerCount returns current connected listeners across the station's HQ/LQ mounts.
func (d *Director) ListenerCount(ctx context.Context, stationID string) (int, error) {
	if d.broadcast == nil {
		return 0, nil
	}

	var mounts []models.Mount
	if err := d.db.WithContext(ctx).Where("station_id = ?", stationID).Find(&mounts).Error; err != nil {
		return 0, fmt.Errorf("failed to load station mounts: %w", err)
	}

	total := 0
	for _, mount := range mounts {
		if m := d.broadcast.GetMount(mount.Name); m != nil {
			total += m.ClientCount()
		}
		if lq := d.broadcast.GetMount(mount.Name + "-lq"); lq != nil {
			total += lq.ClientCount()
		}
	}

	return total, nil
}
