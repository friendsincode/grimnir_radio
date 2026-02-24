/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// SmartBlockList renders the smart blocks page
func (h *Handler) SmartBlockList(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	var blocks []models.SmartBlock
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&blocks)

	h.Render(w, r, "pages/dashboard/smartblocks/list", PageData{
		Title:    "Smart Blocks",
		Stations: h.LoadStations(r),
		Data:     blocks,
	})
}

// SmartBlockNew renders the new smart block form
func (h *Handler) SmartBlockNew(w http.ResponseWriter, r *http.Request) {
	// Get genres and other metadata for rule builder
	station := h.GetStation(r)

	var genres []string
	h.db.Model(&models.MediaItem{}).
		Where("((station_id = ?) OR (show_in_archive = ? AND station_id IN (SELECT id FROM stations WHERE active = ? AND public = ? AND approved = ?))) AND genre != ''",
			station.ID, true, true, true, true).
		Distinct().Pluck("genre", &genres)

	var artists []string
	h.db.Model(&models.MediaItem{}).
		Where("((station_id = ?) OR (show_in_archive = ? AND station_id IN (SELECT id FROM stations WHERE active = ? AND public = ? AND approved = ?))) AND artist != ''",
			station.ID, true, true, true, true).
		Distinct().Pluck("artist", &artists)

	var moods []string
	h.db.Model(&models.MediaItem{}).
		Where("((station_id = ?) OR (show_in_archive = ? AND station_id IN (SELECT id FROM stations WHERE active = ? AND public = ? AND approved = ?))) AND mood != ''",
			station.ID, true, true, true, true).
		Distinct().Pluck("mood", &moods)

	// Get other smart blocks for fallback selection
	var otherBlocks []models.SmartBlock
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&otherBlocks)

	// Get playlists for source and interstitial selection
	var playlists []models.Playlist
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&playlists)

	h.Render(w, r, "pages/dashboard/smartblocks/form", PageData{
		Title:    "New Smart Block",
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Block":           models.SmartBlock{},
			"IsNew":           true,
			"PresetFallbacks": r.URL.Query().Get("mode") == "fallback",
			"Genres":          genres,
			"Artists":         artists,
			"Moods":           moods,
			"OtherBlocks":     otherBlocks,
			"Playlists":       playlists,
		},
	})
}

// SmartBlockCreate handles smart block creation
func (h *Handler) SmartBlockCreate(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	// Parse rules and sequence from form fields
	rules, sequence := h.parseSmartBlockForm(r)

	block := models.SmartBlock{
		ID:          uuid.New().String(),
		StationID:   station.ID,
		Name:        r.FormValue("name"),
		Description: r.FormValue("description"),
		Rules:       rules,
		Sequence:    sequence,
	}

	if block.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	if err := h.db.Create(&block).Error; err != nil {
		http.Error(w, "Failed to create smart block", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/smart-blocks/"+block.ID+"/edit")
		return
	}

	http.Redirect(w, r, "/dashboard/smart-blocks/"+block.ID+"/edit", http.StatusSeeOther)
}

// SmartBlockDetail renders the smart block detail page
func (h *Handler) SmartBlockDetail(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	id := chi.URLParam(r, "id")

	var block models.SmartBlock
	if err := h.db.First(&block, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	h.Render(w, r, "pages/dashboard/smartblocks/detail", PageData{
		Title:    block.Name,
		Stations: h.LoadStations(r),
		Data:     block,
	})
}

// SmartBlockEdit renders the smart block edit form
func (h *Handler) SmartBlockEdit(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Redirect(w, r, "/dashboard/stations/select", http.StatusSeeOther)
		return
	}

	id := chi.URLParam(r, "id")

	var block models.SmartBlock
	if err := h.db.First(&block, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	var genres []string
	h.db.Model(&models.MediaItem{}).
		Where("((station_id = ?) OR (show_in_archive = ? AND station_id IN (SELECT id FROM stations WHERE active = ? AND public = ? AND approved = ?))) AND genre != ''",
			station.ID, true, true, true, true).
		Distinct().Pluck("genre", &genres)

	var artists []string
	h.db.Model(&models.MediaItem{}).
		Where("((station_id = ?) OR (show_in_archive = ? AND station_id IN (SELECT id FROM stations WHERE active = ? AND public = ? AND approved = ?))) AND artist != ''",
			station.ID, true, true, true, true).
		Distinct().Pluck("artist", &artists)

	var moods []string
	h.db.Model(&models.MediaItem{}).
		Where("((station_id = ?) OR (show_in_archive = ? AND station_id IN (SELECT id FROM stations WHERE active = ? AND public = ? AND approved = ?))) AND mood != ''",
			station.ID, true, true, true, true).
		Distinct().Pluck("mood", &moods)

	// Get other smart blocks for fallback selection (excluding current)
	var otherBlocks []models.SmartBlock
	h.db.Where("station_id = ? AND id != ?", station.ID, id).Order("name ASC").Find(&otherBlocks)

	// Get playlists for source and interstitial selection
	var playlists []models.Playlist
	h.db.Where("station_id = ?", station.ID).Order("name ASC").Find(&playlists)

	h.Render(w, r, "pages/dashboard/smartblocks/form", PageData{
		Title:    "Edit: " + block.Name,
		Stations: h.LoadStations(r),
		Data: map[string]any{
			"Block":       block,
			"IsNew":       false,
			"Genres":      genres,
			"Artists":     artists,
			"Moods":       moods,
			"OtherBlocks": otherBlocks,
			"Playlists":   playlists,
		},
	})
}

// SmartBlockUpdate handles smart block updates
func (h *Handler) SmartBlockUpdate(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	var block models.SmartBlock
	if err := h.db.First(&block, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	block.Name = r.FormValue("name")
	block.Description = r.FormValue("description")

	// Build rules and sequence from form fields
	block.Rules, block.Sequence = h.parseSmartBlockForm(r)

	h.logger.Info().
		Str("id", id).
		Str("name", block.Name).
		Interface("rules", block.Rules).
		Interface("sequence", block.Sequence).
		Msg("saving smart block")

	if err := h.db.Save(&block).Error; err != nil {
		h.logger.Error().Err(err).Msg("failed to save smart block")
		http.Error(w, "Failed to update smart block", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/smart-blocks/"+id+"/edit")
		return
	}

	http.Redirect(w, r, "/dashboard/smart-blocks/"+id+"/edit", http.StatusSeeOther)
}

// SmartBlockDelete handles smart block deletion
func (h *Handler) SmartBlockDelete(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	// Verify smart block belongs to station
	var block models.SmartBlock
	if err := h.db.First(&block, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := h.db.Delete(&models.SmartBlock{}, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.Error(w, "Failed to delete smart block", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/smart-blocks")
		return
	}

	http.Redirect(w, r, "/dashboard/smart-blocks", http.StatusSeeOther)
}

// SmartBlockDuplicate creates a copy of an existing smart block
func (h *Handler) SmartBlockDuplicate(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	var original models.SmartBlock
	if err := h.db.First(&original, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// Create a copy with new ID and modified name
	duplicate := models.SmartBlock{
		ID:          uuid.New().String(),
		StationID:   original.StationID,
		Name:        original.Name + " (Copy)",
		Description: original.Description,
		Rules:       original.Rules,
		Sequence:    original.Sequence,
	}

	if err := h.db.Create(&duplicate).Error; err != nil {
		http.Error(w, "Failed to duplicate smart block", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("original_id", id).
		Str("duplicate_id", duplicate.ID).
		Str("name", duplicate.Name).
		Msg("smart block duplicated")

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard/smart-blocks/"+duplicate.ID+"/edit")
		return
	}

	http.Redirect(w, r, "/dashboard/smart-blocks/"+duplicate.ID+"/edit", http.StatusSeeOther)
}

// previewItem represents a track in the preview with its type
type previewItem struct {
	Media  models.MediaItem
	IsAd   bool
	Energy float64
}

type previewRun struct {
	Name            string
	Tracks          []models.MediaItem
	TotalDurationMs int64
}

// SmartBlockPreview generates a preview of the smart block with all rules applied
func (h *Handler) SmartBlockPreview(w http.ResponseWriter, r *http.Request) {
	station := h.GetStation(r)
	if station == nil {
		http.Error(w, "No station selected", http.StatusBadRequest)
		return
	}

	id := chi.URLParam(r, "id")

	var block models.SmartBlock
	if err := h.db.First(&block, "id = ? AND station_id = ?", id, station.ID).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	loopEnabled := r.FormValue("loop") == "true"
	previewVariants := 1

	// Prefer in-form values when present so preview reflects unsaved edits.
	rules := block.Rules
	sequence := block.Sequence
	if err := r.ParseForm(); err == nil {
		if raw := strings.TrimSpace(r.FormValue("variants")); raw != "" {
			if parsed := parseInt(raw, 1); parsed > 0 {
				previewVariants = parsed
			}
		}
		if r.FormValue("name") != "" || r.FormValue("filter_text_search") != "" ||
			r.FormValue("filter_genre") != "" || r.FormValue("filter_artist") != "" ||
			r.FormValue("filter_mood") != "" || r.FormValue("duration_value") != "" {
			rules, sequence = h.parseSmartBlockForm(r)
		}
	}
	if previewVariants > 5 {
		previewVariants = 5
	}

	// Extract all settings from rules
	cfg := h.extractPreviewConfig(rules, sequence)

	h.logger.Debug().
		Int("targetMinutes", cfg.targetMinutes).
		Bool("loop", loopEnabled).
		Bool("adsEnabled", cfg.adsEnabled).
		Interface("separation", cfg.separation).
		Msg("smart block preview starting")

	// Get music tracks
	musicTracks := h.fetchMusicTracks(station.ID, rules)
	h.logger.Debug().Int("musicTracksCount", len(musicTracks)).Msg("fetched music tracks")

	// Get ad tracks if enabled
	var adTracks []models.MediaItem
	if cfg.adsEnabled {
		adTracks = h.fetchAdTracks(station.ID, cfg)
		h.logger.Debug().Int("adTracksCount", len(adTracks)).Msg("fetched ad tracks")
	}

	// Get fallback tracks if configured
	var fallbackTracks []models.MediaItem
	if len(cfg.fallbacks) > 0 {
		fallbackTracks = h.fetchFallbackTracks(station.ID, cfg.fallbacks)
		h.logger.Debug().Int("fallbackTracksCount", len(fallbackTracks)).Msg("fetched fallback tracks")
	}

	runs := make([]previewRun, 0, previewVariants)
	for i := 0; i < previewVariants; i++ {
		preview := h.buildPreviewSequence(musicTracks, adTracks, fallbackTracks, cfg, loopEnabled)

		var media []models.MediaItem
		var totalDurationMs int64
		for _, item := range preview {
			media = append(media, item.Media)
			totalDurationMs += item.Media.Duration.Milliseconds()
		}

		runs = append(runs, previewRun{
			Name:            fmt.Sprintf("Variant %d", i+1),
			Tracks:          media,
			TotalDurationMs: totalDurationMs,
		})
	}

	h.logger.Debug().
		Int("variants", len(runs)).
		Msg("preview complete")

	h.RenderPartial(w, r, "partials/smartblock-preview", map[string]any{
		"Runs":          runs,
		"TargetMinutes": cfg.targetMinutes,
		"LoopEnabled":   loopEnabled,
	})
}

// previewConfig holds all extracted settings for preview generation
type previewConfig struct {
	targetMinutes int
	targetMs      int64
	accuracyMs    int64

	// Ads/Interstitials
	adsEnabled    bool
	adsSourceType string
	adsPlaylistID string
	adsGenre      string
	adsQuery      string
	adsEveryN     int
	adsPerBreak   int

	// Section enabled flags
	separationEnabled bool
	boostersEnabled   bool
	quotasEnabled     bool
	energyEnabled     bool
	fallbacksEnabled  bool

	// Separation (in minutes)
	separation map[string]int

	// Energy curve
	energyCurve []int

	// Quotas
	quotas []quotaConfig

	// Fallbacks
	fallbacks []fallbackConfig
}

type quotaConfig struct {
	field  string
	value  string
	minPct int
	maxPct int
}

type fallbackConfig struct {
	blockID string
	limit   int
}

func (h *Handler) extractPreviewConfig(rules, sequence map[string]any) previewConfig {
	cfg := previewConfig{
		targetMinutes: 60,
		accuracyMs:    2000,
		adsEveryN:     4,
		adsPerBreak:   1,
		separation:    make(map[string]int),
	}

	if rules == nil {
		cfg.targetMs = int64(cfg.targetMinutes) * 60 * 1000
		return cfg
	}

	// Target duration
	if tm, ok := rules["targetMinutes"]; ok {
		cfg.targetMinutes = toInt(tm)
	}
	cfg.targetMs = int64(cfg.targetMinutes) * 60 * 1000

	// Accuracy
	if acc, ok := rules["durationAccuracy"]; ok {
		cfg.accuracyMs = int64(toInt(acc)) * 1000
	}

	// Interstitials/Ads
	if interstitials, ok := rules["interstitials"].(map[string]any); ok {
		if enabled, ok := interstitials["enabled"].(bool); ok && enabled {
			cfg.adsEnabled = true
		}
		if st, ok := interstitials["sourceType"].(string); ok {
			cfg.adsSourceType = st
		}
		if pid, ok := interstitials["playlistID"].(string); ok {
			cfg.adsPlaylistID = pid
		}
		if genre, ok := interstitials["genre"].(string); ok {
			cfg.adsGenre = genre
		}
		if query, ok := interstitials["query"].(string); ok {
			cfg.adsQuery = strings.TrimSpace(query)
		}
		if every, ok := interstitials["every"]; ok {
			cfg.adsEveryN = toInt(every)
			if cfg.adsEveryN < 1 {
				cfg.adsEveryN = 4
			}
		}
		if perBreak, ok := interstitials["perBreak"]; ok {
			cfg.adsPerBreak = toInt(perBreak)
			if cfg.adsPerBreak < 1 {
				cfg.adsPerBreak = 1
			}
		}
	}

	// Separation rules (only if enabled)
	if enabled, ok := rules["separationEnabled"].(bool); ok && enabled {
		cfg.separationEnabled = true
		if sep, ok := rules["separation"].(map[string]any); ok {
			for key, val := range sep {
				cfg.separation[key] = toInt(val)
			}
		}
	}

	// Boosters enabled flag
	if enabled, ok := rules["boostersEnabled"].(bool); ok && enabled {
		cfg.boostersEnabled = true
	}

	// Quotas enabled flag
	if enabled, ok := rules["quotasEnabled"].(bool); ok && enabled {
		cfg.quotasEnabled = true
	}

	// Fallbacks enabled flag
	if enabled, ok := rules["fallbacksEnabled"].(bool); ok && enabled {
		cfg.fallbacksEnabled = true
	}

	// Energy curve from sequence (only if enabled)
	if sequence != nil {
		if enabled, ok := sequence["energyEnabled"].(bool); ok && enabled {
			cfg.energyEnabled = true
			if curve, ok := sequence["energyCurve"].([]any); ok {
				for _, v := range curve {
					cfg.energyCurve = append(cfg.energyCurve, toInt(v))
				}
			}
		}
	}

	// Quotas (only load if enabled flag is already set)
	if cfg.quotasEnabled {
		if quotas, ok := rules["quotas"].([]any); ok {
			for _, q := range quotas {
				if qm, ok := q.(map[string]any); ok {
					qc := quotaConfig{
						field:  toString(qm["field"]),
						value:  toString(qm["value"]),
						minPct: toInt(qm["minPct"]),
						maxPct: toInt(qm["maxPct"]),
					}
					if qc.field != "" {
						cfg.quotas = append(cfg.quotas, qc)
					}
				}
			}
		}
	}

	// Fallbacks (only load if enabled flag is already set)
	if cfg.fallbacksEnabled {
		if fallbacks, ok := rules["fallbacks"].([]any); ok {
			for _, f := range fallbacks {
				if fm, ok := f.(map[string]any); ok {
					fc := fallbackConfig{
						blockID: toString(fm["blockID"]),
						limit:   toInt(fm["limit"]),
					}
					if fc.blockID != "" {
						if fc.limit <= 0 {
							fc.limit = 10
						}
						cfg.fallbacks = append(cfg.fallbacks, fc)
					}
				}
			}
		}
	}

	return cfg
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func (h *Handler) fetchMusicTracks(stationID string, rules map[string]any) []models.MediaItem {
	var tracks []models.MediaItem
	query := h.db.Where("station_id = ?", stationID)

	var (
		yearMin         int
		yearMax         int
		eraRangeSet     bool
		excludeExplicit bool
		includeArchive  bool
	)

	if rules != nil {
		if include, ok := rules["includePublicArchive"].(bool); ok && include {
			includeArchive = true
		}
		if include, ok := rules["include_archive"].(bool); ok && include {
			includeArchive = true
		}
		if includeArchive {
			query = h.db.Where(
				"(station_id = ?) OR (show_in_archive = ? AND station_id IN (SELECT id FROM stations WHERE active = ? AND public = ? AND approved = ?))",
				stationID, true, true, true, true,
			)
		}

		// Free text search across title/artist/album
		if textSearch, ok := rules["text_search"].(string); ok && strings.TrimSpace(textSearch) != "" {
			query = applyLooseMediaSearch(query, textSearch)
		}
		// Genre filter
		if genre, ok := rules["genre"].(string); ok && genre != "" {
			query = query.Where("genre = ?", genre)
		}
		// Artist filter
		if artist, ok := rules["artist"].(string); ok && artist != "" {
			query = query.Where(normalizedSQLExpr("artist")+" = ?", normalizeSearchText(artist))
		}
		// Mood filter
		if mood, ok := rules["mood"].(string); ok && mood != "" {
			query = query.Where("mood = ?", mood)
		}
		// Language filter
		if language, ok := rules["language"].(string); ok && strings.TrimSpace(language) != "" {
			query = query.Where("LOWER(language) = ?", strings.ToLower(strings.TrimSpace(language)))
		}
		// Exclude explicit tracks
		if explicit, ok := rules["excludeExplicit"].(bool); ok && explicit {
			excludeExplicit = true
			query = query.Where("explicit = ?", false)
		}
		// BPM range filter
		if bpmRange, ok := rules["bpmRange"].(map[string]any); ok {
			if minBPM := toInt(bpmRange["min"]); minBPM > 0 {
				query = query.Where("bpm >= ?", minBPM)
			}
			if maxBPM := toInt(bpmRange["max"]); maxBPM > 0 {
				query = query.Where("bpm <= ?", maxBPM)
			}
		}
		// Source playlists filter
		playlistIDs := toStringSlice(rules["sourcePlaylists"])
		if len(playlistIDs) > 0 {
			if len(playlistIDs) > 0 {
				query = query.Where("id IN (SELECT media_id FROM playlist_items WHERE playlist_id IN ?)", playlistIDs)
			}
		}
		// Year range filter (applied post-query to avoid cross-database casting issues).
		if yr, ok := rules["yearRange"].(map[string]any); ok {
			yearMin = toInt(yr["min"])
			yearMax = toInt(yr["max"])
		}
		// Era presets map to year ranges.
		if era, ok := rules["era"].(string); ok && strings.TrimSpace(era) != "" {
			if minYear, maxYear, ok := eraToYearRange(era); ok {
				yearMin = minYear
				yearMax = maxYear
				eraRangeSet = true
			}
		}
	}

	query.Order("RANDOM()").Find(&tracks)
	if yearMin == 0 && yearMax == 0 && !eraRangeSet && !excludeExplicit {
		return tracks
	}

	filtered := make([]models.MediaItem, 0, len(tracks))
	for _, track := range tracks {
		if excludeExplicit && track.Explicit {
			continue
		}
		if yearMin > 0 || yearMax > 0 || eraRangeSet {
			year := parseTrackYear(track.Year)
			if year == 0 {
				continue
			}
			if yearMin > 0 && year < yearMin {
				continue
			}
			if yearMax > 0 && year > yearMax {
				continue
			}
		}
		filtered = append(filtered, track)
	}
	return filtered
}

func toStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if strings.TrimSpace(item) != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok && strings.TrimSpace(str) != "" {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

func parseTrackYear(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if len(raw) >= 4 {
		if y, err := strconv.Atoi(raw[:4]); err == nil {
			return y
		}
	}
	if y, err := strconv.Atoi(raw); err == nil {
		return y
	}
	return 0
}

func eraToYearRange(era string) (int, int, bool) {
	now := time.Now().Year()
	switch strings.ToLower(strings.TrimSpace(era)) {
	case "current":
		return now - 2, now, true
	case "2020s":
		return 2020, 2029, true
	case "2010s":
		return 2010, 2019, true
	case "2000s":
		return 2000, 2009, true
	case "90s":
		return 1990, 1999, true
	case "80s":
		return 1980, 1989, true
	case "70s":
		return 1970, 1979, true
	case "classic":
		return 0, 1979, true
	default:
		return 0, 0, false
	}
}

func (h *Handler) fetchAdTracks(stationID string, cfg previewConfig) []models.MediaItem {
	var tracks []models.MediaItem

	if cfg.adsSourceType == "playlist" && cfg.adsPlaylistID != "" {
		h.db.Where("station_id = ?", stationID).
			Where("id IN (SELECT media_id FROM playlist_items WHERE playlist_id = ?)", cfg.adsPlaylistID).
			Order("RANDOM()").
			Find(&tracks)
	} else if cfg.adsSourceType == "genre" && cfg.adsGenre != "" {
		h.db.Where("station_id = ? AND genre = ?", stationID, cfg.adsGenre).
			Order("RANDOM()").
			Find(&tracks)
	} else if cfg.adsSourceType == "title" && cfg.adsQuery != "" {
		h.db.Where("station_id = ? AND LOWER(title) LIKE ?", stationID, "%"+strings.ToLower(cfg.adsQuery)+"%").
			Order("RANDOM()").
			Find(&tracks)
	} else if cfg.adsSourceType == "artist" && cfg.adsQuery != "" {
		h.db.Where(normalizedSQLExpr("artist")+" LIKE ?", "%"+normalizeSearchText(cfg.adsQuery)+"%").
			Where("station_id = ?", stationID).
			Order("RANDOM()").
			Find(&tracks)
	} else if cfg.adsSourceType == "label" && cfg.adsQuery != "" {
		h.db.Where("station_id = ? AND LOWER(label) LIKE ?", stationID, "%"+strings.ToLower(cfg.adsQuery)+"%").
			Order("RANDOM()").
			Find(&tracks)
	}

	return tracks
}

func (h *Handler) fetchFallbackTracks(stationID string, fallbacks []fallbackConfig) []models.MediaItem {
	var allTracks []models.MediaItem

	for _, fb := range fallbacks {
		// Get the fallback smart block
		var fallbackBlock models.SmartBlock
		if err := h.db.First(&fallbackBlock, "id = ?", fb.blockID).Error; err != nil {
			h.logger.Warn().Str("blockID", fb.blockID).Err(err).Msg("fallback block not found")
			continue
		}

		// Fetch tracks using the fallback block's rules
		tracks := h.fetchMusicTracks(stationID, fallbackBlock.Rules)

		// Limit the number of tracks from this fallback
		if len(tracks) > fb.limit {
			tracks = tracks[:fb.limit]
		}

		allTracks = append(allTracks, tracks...)
	}

	return allTracks
}

func (h *Handler) buildPreviewSequence(musicTracks, adTracks, fallbackTracks []models.MediaItem, cfg previewConfig, loopEnabled bool) []previewItem {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	var result []previewItem
	var totalMs int64
	musicCount := 0
	adIndex := 0

	// Track last played times for separation (in milliseconds from start)
	lastPlayed := map[string]map[string]int64{
		"artist": {},
		"title":  {},
		"album":  {},
		"label":  {},
	}

	// Quota tracking
	quotaCounts := make(map[string]int) // "field:value" -> count
	totalForQuota := 0

	// Helper to update last played
	updateLastPlayed := func(track models.MediaItem, currentMs int64) {
		if track.Artist != "" {
			lastPlayed["artist"][track.Artist] = currentMs
		}
		if track.Title != "" {
			lastPlayed["title"][track.Title] = currentMs
		}
		if track.Album != "" {
			lastPlayed["album"][track.Album] = currentMs
		}
		if track.Label != "" {
			lastPlayed["label"][track.Label] = currentMs
		}
	}

	// Helper to check quotas
	canAddForQuota := func(track models.MediaItem) bool {
		if len(cfg.quotas) == 0 || totalForQuota == 0 {
			return true
		}
		for _, q := range cfg.quotas {
			var trackValue string
			switch q.field {
			case "genre":
				trackValue = track.Genre
			case "artist":
				trackValue = track.Artist
			case "mood":
				trackValue = track.Mood
			}
			if trackValue == q.value {
				key := q.field + ":" + q.value
				currentCount := quotaCounts[key]
				currentPct := (currentCount * 100) / totalForQuota
				if q.maxPct > 0 && currentPct >= q.maxPct {
					return false
				}
			}
		}
		return true
	}

	// Helper to check if adding a track would stay within accuracy bounds
	// Returns true only if result is within target ± accuracy
	fitsWithinAccuracy := func(trackMs int64) bool {
		// Always allow the first valid track even if it exceeds target.
		// This matches on-air behavior better for long-form content blocks.
		if len(result) == 0 {
			return trackMs > 0
		}
		if totalMs >= cfg.targetMs+cfg.accuracyMs {
			return false
		}
		newTotal := totalMs + trackMs
		// Must not exceed target + accuracy
		return newTotal <= cfg.targetMs+cfg.accuracyMs
	}

	// Score how close adding this track gets us to the target (higher = better)
	// Returns negative if it would exceed target + accuracy
	durationScore := func(trackMs int64) int64 {
		newTotal := totalMs + trackMs
		if newTotal > cfg.targetMs+cfg.accuracyMs {
			return -1 // Invalid - exceeds accuracy
		}
		// How close to target? Prefer getting as close as possible
		diff := cfg.targetMs - newTotal
		if diff < 0 {
			diff = -diff
		}
		// Invert so closer = higher score (max score when diff = 0)
		return cfg.targetMs - diff
	}

	// Get energy target for current position
	getTargetEnergy := func() float64 {
		if len(cfg.energyCurve) == 0 {
			return 50
		}
		progress := float64(totalMs) / float64(cfg.targetMs)
		idx := int(progress * float64(len(cfg.energyCurve)))
		if idx >= len(cfg.energyCurve) {
			idx = len(cfg.energyCurve) - 1
		}
		if idx < 0 {
			idx = 0
		}
		return float64(cfg.energyCurve[idx])
	}

	// Score track by energy match
	scoreByEnergy := func(track models.MediaItem, targetEnergy float64) float64 {
		trackEnergy := track.BPM // Use BPM as proxy for energy
		if trackEnergy == 0 {
			trackEnergy = 100
		}
		// Normalize to 0-100 scale (assuming BPM 60-180 range)
		normalizedEnergy := ((trackEnergy - 60) / 120) * 100
		if normalizedEnergy < 0 {
			normalizedEnergy = 0
		}
		if normalizedEnergy > 100 {
			normalizedEnergy = 100
		}
		diff := normalizedEnergy - targetEnergy
		if diff < 0 {
			diff = -diff
		}
		return 100 - diff // Higher score = better match
	}

	// Helper to calculate separation violation score (lower = better)
	separationScore := func(track models.MediaItem, currentMs int64) int64 {
		// If separation is not enabled, return 0 (no violation)
		if !cfg.separationEnabled {
			return 0
		}
		var worstViolation int64
		for field, minMinutes := range cfg.separation {
			if minMinutes <= 0 {
				continue
			}
			minMs := int64(minMinutes) * 60 * 1000
			var value string
			switch field {
			case "artist":
				value = track.Artist
			case "title":
				value = track.Title
			case "album":
				value = track.Album
			case "label":
				value = track.Label
			}
			if value == "" {
				continue
			}
			if lastTime, exists := lastPlayed[field][value]; exists {
				timeSince := currentMs - lastTime
				if timeSince < minMs {
					violation := minMs - timeSince
					if violation > worstViolation {
						worstViolation = violation
					}
				}
			}
		}
		return worstViolation
	}

	// Helper to add a track and update all tracking
	addTrack := func(track models.MediaItem, isAd bool) {
		trackMs := track.Duration.Milliseconds()
		result = append(result, previewItem{Media: track, IsAd: isAd})
		totalMs += trackMs

		if !isAd {
			musicCount++
			totalForQuota++
			updateLastPlayed(track, totalMs)

			// Update quota counts
			for _, q := range cfg.quotas {
				var trackValue string
				switch q.field {
				case "genre":
					trackValue = track.Genre
				case "artist":
					trackValue = track.Artist
				case "mood":
					trackValue = track.Mood
				}
				if trackValue == q.value {
					quotaCounts[q.field+":"+q.value]++
				}
			}
		}
	}

	// Main loop
	loopCount := 0
	maxLoops := 1
	if loopEnabled {
		maxLoops = 100
	}

	usedInLoop := make(map[int]bool)       // Track which indices we've used this loop
	lastAddedID := ""                      // Track last added track ID to prevent consecutive repeats
	recentlyPlayed := make(map[string]int) // Track ID -> how many tracks ago it was played

	for loopCount < maxLoops {
		addedThisLoop := false
		usedInLoop = make(map[int]bool) // Reset for each loop

		for totalMs < cfg.targetMs+cfg.accuracyMs {
			// Find the best track to add that stays within accuracy
			bestIdx := -1
			var bestDurScore int64 = -1
			var bestCombinedScore int64 = -1

			targetEnergy := getTargetEnergy()

			for i := 0; i < len(musicTracks); i++ {
				// In non-loop mode, skip already used tracks
				if !loopEnabled && usedInLoop[i] {
					continue
				}

				track := musicTracks[i]

				// Never play the same track twice in a row (check by ID, not index)
				if track.ID == lastAddedID && len(musicTracks) > 1 {
					continue
				}

				// In loop mode, prefer tracks that haven't been played recently
				// Skip if played within last N tracks (where N = min(3, total_tracks/2))
				if loopEnabled && len(musicTracks) > 2 {
					minGap := 3
					if len(musicTracks)/2 < minGap {
						minGap = len(musicTracks) / 2
					}
					if ago, played := recentlyPlayed[track.ID]; played && ago < minGap {
						continue
					}
				}
				trackMs := track.Duration.Milliseconds()

				// Check if this track fits within accuracy bounds (hard constraint)
				if !fitsWithinAccuracy(trackMs) {
					continue
				}

				// Check quotas (hard constraint)
				if !canAddForQuota(track) {
					continue
				}

				// Check separation (hard constraint when enabled)
				sepScore := separationScore(track, totalMs)
				if cfg.separationEnabled && sepScore > 0 {
					// Track violates separation rules - skip it
					continue
				}

				// Score how close this gets us to target duration
				durScore := durationScore(trackMs)

				// Calculate energy match score (only if enabled)
				var energyScore int64
				if cfg.energyEnabled {
					energyScore = int64(scoreByEnergy(track, targetEnergy))
				}

				// Combined score for other factors
				combinedScore := (1000000 - sepScore) + energyScore

				// Prefer tracks that get us closest to target duration
				// Among equal duration scores, use combined score as tiebreaker
				if durScore > bestDurScore ||
					(durScore == bestDurScore && combinedScore > bestCombinedScore) ||
					(durScore == bestDurScore && combinedScore == bestCombinedScore && rng.Intn(2) == 0) {
					bestIdx = i
					bestDurScore = durScore
					bestCombinedScore = combinedScore
				}
			}

			// If still no track found, break
			if bestIdx == -1 {
				break
			}

			// Add the best track
			track := musicTracks[bestIdx]
			addTrack(track, false)
			usedInLoop[bestIdx] = true
			lastAddedID = track.ID
			addedThisLoop = true

			// Update recently played - increment all counters and add this track
			for id := range recentlyPlayed {
				recentlyPlayed[id]++
			}
			recentlyPlayed[track.ID] = 0

			// Insert ads if enabled and it's time
			if cfg.adsEnabled && len(adTracks) > 0 && musicCount%cfg.adsEveryN == 0 {
				for j := 0; j < cfg.adsPerBreak; j++ {
					if adIndex >= len(adTracks) {
						adIndex = 0 // Loop ads
					}
					ad := adTracks[adIndex]
					adMs := ad.Duration.Milliseconds()

					// Check if adding ad would exceed target too much
					if totalMs+adMs <= cfg.targetMs+cfg.accuracyMs {
						addTrack(ad, true)
						adIndex++
					}
				}
			}
		}

		loopCount++

		// Stop if we've reached target or no tracks were added
		if totalMs >= cfg.targetMs-cfg.accuracyMs || !addedThisLoop {
			break
		}
	}

	// If we haven't reached the target and we have fallback tracks, use them
	if totalMs < cfg.targetMs-cfg.accuracyMs && len(fallbackTracks) > 0 {
		usedFallback := make(map[int]bool)

		for totalMs < cfg.targetMs+cfg.accuracyMs {
			bestIdx := -1
			var bestDurScore int64 = -1

			for i := 0; i < len(fallbackTracks); i++ {
				if usedFallback[i] {
					continue
				}

				track := fallbackTracks[i]
				trackMs := track.Duration.Milliseconds()

				// Check if it fits within accuracy
				if !fitsWithinAccuracy(trackMs) {
					continue
				}

				// Check quotas
				if !canAddForQuota(track) {
					continue
				}

				// Check separation (hard constraint when enabled)
				sepScore := separationScore(track, totalMs)
				if cfg.separationEnabled && sepScore > 0 {
					continue
				}

				// Score by duration closeness
				durScore := durationScore(trackMs)

				// Prefer tracks closest to target
				if durScore > bestDurScore || (durScore == bestDurScore && bestIdx == -1) {
					bestIdx = i
					bestDurScore = durScore
				}
			}

			if bestIdx == -1 {
				break
			}

			track := fallbackTracks[bestIdx]
			addTrack(track, false)
			usedFallback[bestIdx] = true

			// Insert ads if enabled and it's time
			if cfg.adsEnabled && len(adTracks) > 0 && musicCount%cfg.adsEveryN == 0 {
				for j := 0; j < cfg.adsPerBreak; j++ {
					if adIndex >= len(adTracks) {
						adIndex = 0
					}
					ad := adTracks[adIndex]
					adMs := ad.Duration.Milliseconds()
					if totalMs+adMs <= cfg.targetMs+cfg.accuracyMs {
						addTrack(ad, true)
						adIndex++
					}
				}
			}
		}
	}

	// Post-processing: if we're over target, check if removing any track gets us closer
	if totalMs > cfg.targetMs {
		currentOvershoot := totalMs - cfg.targetMs

		bestRemoveIdx := -1
		var bestNewDiff int64 = currentOvershoot

		for i, item := range result {
			// Don't remove ads
			if item.IsAd {
				continue
			}

			trackMs := item.Media.Duration.Milliseconds()
			newTotal := totalMs - trackMs
			var newDiff int64
			if newTotal >= cfg.targetMs {
				newDiff = newTotal - cfg.targetMs
			} else {
				newDiff = cfg.targetMs - newTotal
			}

			// If removing this track gets us closer to target, consider it
			if newDiff < bestNewDiff {
				bestRemoveIdx = i
				bestNewDiff = newDiff
			}
		}

		// Remove the track if it improves our accuracy
		if bestRemoveIdx >= 0 {
			result = append(result[:bestRemoveIdx], result[bestRemoveIdx+1:]...)
		}
	}

	return result
}

// parseSmartBlockForm extracts rules and sequence from form fields
func (h *Handler) parseSmartBlockForm(r *http.Request) (map[string]any, map[string]any) {
	rules := make(map[string]any)
	sequence := make(map[string]any)

	// Parse duration → targetMinutes (handle decimal values for hours/days)
	durationValue := parseFloat(r.FormValue("duration_value"), 1.0)
	durationUnit := r.FormValue("duration_unit")
	var targetMinutes int
	switch durationUnit {
	case "hours":
		targetMinutes = int(durationValue * 60)
	case "days":
		targetMinutes = int(durationValue * 60 * 24)
	default: // minutes
		targetMinutes = int(durationValue)
	}
	if targetMinutes < 1 {
		targetMinutes = 1
	}
	rules["targetMinutes"] = targetMinutes

	// Duration accuracy (1-60 seconds, default 2)
	accuracy := parseInt(r.FormValue("duration_accuracy"), 2)
	if accuracy < 1 {
		accuracy = 1
	} else if accuracy > 60 {
		accuracy = 60
	}
	rules["durationAccuracy"] = accuracy

	// Sequence mode
	mode := r.FormValue("sequence_mode")
	if mode == "" {
		mode = "random"
	}
	sequence["mode"] = mode

	// Source playlists filter
	sourcePlaylists := r.Form["source_playlists"]
	if len(sourcePlaylists) > 0 {
		rules["sourcePlaylists"] = sourcePlaylists
	}
	if r.FormValue("source_include_archive") == "on" {
		rules["includePublicArchive"] = true
	}

	// Selection filters
	if textSearch := r.FormValue("filter_text_search"); textSearch != "" {
		rules["text_search"] = textSearch
	}
	if genre := r.FormValue("filter_genre"); genre != "" {
		rules["genre"] = genre
	}
	if artist := r.FormValue("filter_artist"); artist != "" {
		rules["artist"] = artist
	}
	if mood := r.FormValue("filter_mood"); mood != "" {
		rules["mood"] = mood
	}

	// BPM range
	bpmMin := parseInt(r.FormValue("filter_bpm_min"), 0)
	bpmMax := parseInt(r.FormValue("filter_bpm_max"), 0)
	if bpmMin > 0 || bpmMax > 0 {
		rules["bpmRange"] = map[string]int{"min": bpmMin, "max": bpmMax}
	}

	// Year range
	yearMin := parseInt(r.FormValue("filter_year_min"), 0)
	yearMax := parseInt(r.FormValue("filter_year_max"), 0)
	if yearMin > 0 || yearMax > 0 {
		rules["yearRange"] = map[string]int{"min": yearMin, "max": yearMax}
	}

	// Separation rules
	if r.FormValue("separation_enabled") == "on" {
		rules["separationEnabled"] = true
		separation := make(map[string]int)
		if sep := parseSeparationMinutes(r.FormValue("sep_artist"), r.FormValue("sep_artist_unit")); sep > 0 {
			separation["artist"] = sep
		}
		if sep := parseSeparationMinutes(r.FormValue("sep_album"), r.FormValue("sep_album_unit")); sep > 0 {
			separation["album"] = sep
		}
		if sep := parseSeparationMinutes(r.FormValue("sep_title"), r.FormValue("sep_title_unit")); sep > 0 {
			separation["title"] = sep
		}
		if sep := parseSeparationMinutes(r.FormValue("sep_label"), r.FormValue("sep_label_unit")); sep > 0 {
			separation["label"] = sep
		}
		if len(separation) > 0 {
			rules["separation"] = separation
		}
	}

	// Priority boosters (dynamic fields)
	if r.FormValue("boosters_enabled") == "on" {
		rules["boostersEnabled"] = true
		var weights []map[string]any
		for i := 0; i < 10; i++ { // Max 10 boosters
			field := r.FormValue(fmt.Sprintf("booster_%d_field", i))
			value := r.FormValue(fmt.Sprintf("booster_%d_value", i))
			weight := parseFloat(r.FormValue(fmt.Sprintf("booster_%d_weight", i)), 1.0)
			if field != "" && value != "" {
				weights = append(weights, map[string]any{
					"field":  field,
					"value":  value,
					"weight": weight,
				})
			}
		}
		if len(weights) > 0 {
			rules["weights"] = weights
		}
	}

	// Quotas (dynamic fields)
	if r.FormValue("quotas_enabled") == "on" {
		rules["quotasEnabled"] = true
		var quotas []map[string]any
		for i := 0; i < 10; i++ { // Max 10 quotas
			field := r.FormValue(fmt.Sprintf("quota_%d_field", i))
			value := r.FormValue(fmt.Sprintf("quota_%d_value", i))
			minPct := parseInt(r.FormValue(fmt.Sprintf("quota_%d_min", i)), 0)
			maxPct := parseInt(r.FormValue(fmt.Sprintf("quota_%d_max", i)), 100)
			if field != "" && value != "" {
				quotas = append(quotas, map[string]any{
					"field":  field,
					"value":  value,
					"minPct": minPct,
					"maxPct": maxPct,
				})
			}
		}
		if len(quotas) > 0 {
			rules["quotas"] = quotas
		}
	}

	// Interstitials/Ads
	if r.FormValue("ads_enabled") == "on" || r.FormValue("ads_enabled") == "true" {
		interstitials := map[string]any{
			"enabled":    true,
			"sourceType": r.FormValue("ads_source_type"),
			"every":      parseInt(r.FormValue("ads_every_n"), 4),
			"perBreak":   parseInt(r.FormValue("ads_per_break"), 1),
		}
		if playlistID := r.FormValue("ads_playlist"); playlistID != "" {
			interstitials["playlistID"] = playlistID
		}
		if genre := r.FormValue("ads_genre"); genre != "" {
			interstitials["genre"] = genre
		}
		if query := strings.TrimSpace(r.FormValue("ads_query")); query != "" {
			interstitials["query"] = query
		}
		rules["interstitials"] = interstitials
	}

	// Era filter
	if era := r.FormValue("filter_era"); era != "" {
		rules["era"] = era
	}

	// Language filter
	if lang := r.FormValue("filter_language"); lang != "" {
		rules["language"] = lang
	}

	// Exclude explicit
	if r.FormValue("filter_exclude_explicit") == "on" {
		rules["excludeExplicit"] = true
	}

	// Energy curve from comma-separated string
	if r.FormValue("energy_enabled") == "on" {
		sequence["energyEnabled"] = true
		if curveStr := r.FormValue("energy_curve"); curveStr != "" {
			parts := strings.Split(curveStr, ",")
			energyCurve := make([]int, len(parts))
			for i, p := range parts {
				energyCurve[i] = parseInt(strings.TrimSpace(p), 50)
			}
			sequence["energyCurve"] = energyCurve
		}
	}

	// Fallbacks (dynamic fields)
	if r.FormValue("fallbacks_enabled") == "on" {
		rules["fallbacksEnabled"] = true
		var fallbacks []map[string]any
		for i := 0; i < 10; i++ { // Max 10 fallbacks
			blockID := r.FormValue(fmt.Sprintf("fallback_%d_block", i))
			limit := parseInt(r.FormValue(fmt.Sprintf("fallback_%d_limit", i)), 10)
			if blockID != "" {
				fallbacks = append(fallbacks, map[string]any{
					"blockID": blockID,
					"limit":   limit,
				})
			}
		}
		if len(fallbacks) > 0 {
			rules["fallbacks"] = fallbacks
		}
	}

	return rules, sequence
}

// parseInt safely parses an integer with a default value
func parseInt(s string, def int) int {
	if s == "" {
		return def
	}
	var val int
	if _, err := fmt.Sscanf(s, "%d", &val); err != nil {
		return def
	}
	return val
}

// parseFloat safely parses a float with a default value
func parseFloat(s string, def float64) float64 {
	if s == "" {
		return def
	}
	var val float64
	if _, err := fmt.Sscanf(s, "%f", &val); err != nil {
		return def
	}
	return val
}

func parseSeparationMinutes(value, unit string) int {
	n := parseInt(value, 0)
	if n <= 0 {
		return 0
	}

	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "hours":
		return n * 60
	case "days":
		return n * 60 * 24
	case "weeks":
		return n * 60 * 24 * 7
	default:
		return n
	}
}
