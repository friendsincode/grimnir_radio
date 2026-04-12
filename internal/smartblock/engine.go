/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package smartblock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// ErrUnresolved indicates the rules could not produce a sequence.
var ErrUnresolved = errors.New("smart block could not satisfy constraints")

// Engine generates playout sequences from smart block rules.
type Engine struct {
	db     *gorm.DB
	logger zerolog.Logger
}

// New creates a smart block engine instance.
func New(db *gorm.DB, logger zerolog.Logger) *Engine {
	return &Engine{db: db, logger: logger}
}

// GenerateRequest describes materialization parameters.
type GenerateRequest struct {
	SmartBlockID string
	Seed         int64
	Duration     int64 // milliseconds
	StationID    string
	MountID      string
	LoopToFill   bool // repeat tracks to reach target duration when candidates are exhausted
}

// SequenceItem is a planned track with cue data.
type SequenceItem struct {
	MediaID        string
	StartsAtMS     int64
	EndsAtMS       int64
	IntroEnd       float64
	OutroIn        float64
	Energy         float64
	IsBumper       bool
	IsInterstitial bool
}

// GenerateResult returns the materialized sequence.
type GenerateResult struct {
	Items              []SequenceItem
	TotalMS            int64
	Exhausted          bool
	Warnings           []string
	BumperCount        int
	BumperLimit        int
	BumperLimitReached bool
}

// Generate materializes a sequence using smart block rules.
// It applies progressive constraint relaxation when strict rules produce no results,
// then tries fallback smart blocks before returning ErrUnresolved.
func (e *Engine) Generate(ctx context.Context, req GenerateRequest) (GenerateResult, error) {
	return e.generateWithDepth(ctx, req, 0)
}

// maxFallbackDepth prevents infinite recursion through fallback chains.
const maxFallbackDepth = 3

func (e *Engine) generateWithDepth(ctx context.Context, req GenerateRequest, depth int) (GenerateResult, error) {
	def, err := e.loadDefinition(ctx, req.SmartBlockID)
	if err != nil {
		return GenerateResult{}, err
	}

	target := req.Duration
	if target <= 0 {
		if def.Duration.TargetMS > 0 {
			target = def.Duration.TargetMS
		} else {
			target = int64(15 * time.Minute / time.Millisecond)
		}
	}

	// Progressive constraint relaxation: try increasingly lenient rule sets.
	// Level 0: strict (current behavior)
	// Level 1: drop separation rules
	// Level 2: drop separation + quotas
	// Level 3: drop separation + quotas + exclude rules
	for level := 0; level <= 3; level++ {
		relaxed := relaxDefinition(def, level)

		recent, err := e.recentPlays(ctx, req.StationID, relaxed.Separation.SeparationDurations())
		if err != nil {
			return GenerateResult{}, err
		}

		candidates, err := e.fetchCandidates(ctx, relaxed, req.StationID, recent)
		if err != nil {
			return GenerateResult{}, err
		}

		if len(candidates) == 0 {
			continue
		}

		// Determine loop-to-fill preference: request-time override OR saved definition.
		loopToFill := req.LoopToFill || def.LoopToFill

		rng := rand.New(rand.NewSource(req.Seed))
		result := e.selectSequence(ctx, rng, candidates, relaxed, target, loopToFill)
		if len(result.Items) == 0 {
			continue
		}

		// Interleave interstitial/ad tracks if configured.
		if def.Interstitials.Enabled {
			if len(def.Interstitials.Sources) == 0 {
				e.logger.Warn().Str("smart_block", req.SmartBlockID).
					Msg("interstitials enabled but no sources configured — ads will not play; check the smart block interstitials settings")
			} else {
				interstitials, err := e.fetchInterstitialCandidates(ctx, def.Interstitials, req.StationID)
				if err != nil {
					e.logger.Warn().Err(err).Str("smart_block", req.SmartBlockID).Msg("failed to fetch interstitial candidates")
				} else if len(interstitials) == 0 {
					e.logger.Warn().
						Str("smart_block", req.SmartBlockID).
						Str("station", req.StationID).
						Int("sources_configured", len(def.Interstitials.Sources)).
						Msg("interstitials enabled but zero candidates found — check source playlist/genre has analyzed tracks")
				} else {
					e.logger.Debug().
						Str("smart_block", req.SmartBlockID).
						Int("candidates", len(interstitials)).
						Int("every_n", def.Interstitials.EveryN).
						Int("per_break", def.Interstitials.PerBreak).
						Msg("interleaving interstitial tracks")
					interleaveInterstitials(&result, interstitials, def.Interstitials.EveryN, def.Interstitials.PerBreak, rng)
				}
			}
		}

		// Tail-fill with bumper tracks if enabled and sequence is underfilled.
		if def.Bumpers.Enabled && result.TotalMS < target {
			result.BumperLimit = def.Bumpers.MaxPerGap
			e.tailFillBumpers(ctx, &result, def.Bumpers, req.StationID, target)
			if result.BumperCount >= def.Bumpers.MaxPerGap && result.TotalMS < target {
				result.BumperLimitReached = true
				result.Warnings = append(result.Warnings, "bumper_limit_reached")
			}
		}

		if level > 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("constraint_relaxed:%d", level))
			e.logger.Info().
				Str("smart_block", req.SmartBlockID).
				Int("relaxation_level", level).
				Msg("smart block resolved with relaxed constraints")
		}
		return result, nil
	}

	// Fallback chain: try alternative smart blocks defined in the definition.
	if depth < maxFallbackDepth {
		for _, fb := range def.Fallbacks {
			if fb.SmartBlockID == "" || fb.SmartBlockID == req.SmartBlockID {
				continue // skip self-references
			}

			fbReq := req
			fbReq.SmartBlockID = fb.SmartBlockID
			result, err := e.generateWithDepth(ctx, fbReq, depth+1)
			if err != nil {
				e.logger.Debug().Err(err).
					Str("fallback_block", fb.SmartBlockID).
					Msg("fallback smart block failed")
				continue
			}

			if fb.Limit > 0 && len(result.Items) > fb.Limit {
				result.Items = result.Items[:fb.Limit]
				// Recalculate TotalMS from truncated items.
				if len(result.Items) > 0 {
					result.TotalMS = result.Items[len(result.Items)-1].EndsAtMS
				} else {
					result.TotalMS = 0
				}
			}

			result.Warnings = append(result.Warnings, "used_fallback:"+fb.SmartBlockID)
			return result, nil
		}
	}

	return GenerateResult{}, ErrUnresolved
}

// relaxDefinition returns a copy of def with constraints removed according to the level.
func relaxDefinition(def Definition, level int) Definition {
	if level <= 0 {
		return def
	}
	relaxed := def
	// Level 1+: drop separation rules
	relaxed.Separation = SeparationRules{}
	if level >= 2 {
		// Level 2+: drop quotas
		relaxed.Quotas = nil
	}
	if level >= 3 {
		// Level 3: drop exclude rules (only includes remain)
		relaxed.Exclude = nil
	}
	return relaxed
}

func (e *Engine) loadDefinition(ctx context.Context, smartBlockID string) (Definition, error) {
	var sb models.SmartBlock
	if err := e.db.WithContext(ctx).First(&sb, "id = ?", smartBlockID).Error; err != nil {
		return Definition{}, err
	}

	bytes, err := json.Marshal(sb.Rules)
	if err != nil {
		return Definition{}, err
	}

	var def Definition
	if err := json.Unmarshal(bytes, &def); err != nil {
		return Definition{}, err
	}

	// Backward compatibility for legacy Smart Block rules used by dashboard form.
	def = applyLegacyRuleCompat(def, sb.Rules)
	return def, nil
}

func applyLegacyRuleCompat(def Definition, rules map[string]any) Definition {
	if rules == nil {
		return def
	}

	hasField := func(field string) bool {
		for _, rule := range def.Include {
			if strings.EqualFold(rule.Field, field) {
				return true
			}
		}
		return false
	}
	addInclude := func(field string, value any) {
		if !hasField(field) {
			def.Include = append(def.Include, FilterRule{Field: field, Value: value})
		}
	}

	if s := strings.TrimSpace(toString(rules["text_search"])); s != "" {
		addInclude("text_search", s)
	}
	if s := strings.TrimSpace(toString(rules["genre"])); s != "" {
		addInclude("genre", s)
	}
	if s := strings.TrimSpace(toString(rules["artist"])); s != "" {
		addInclude("artist", s)
	}
	if s := strings.TrimSpace(toString(rules["mood"])); s != "" {
		addInclude("mood", s)
	}
	if s := strings.TrimSpace(toString(rules["language"])); s != "" {
		addInclude("language", s)
	}

	if bpm, ok := rules["bpmRange"]; ok && !hasField("bpm") {
		def.Include = append(def.Include, FilterRule{Field: "bpm", Value: bpm})
	}
	if year, ok := rules["yearRange"]; ok && !hasField("year") {
		def.Include = append(def.Include, FilterRule{Field: "year", Value: year})
	}
	if minDur := toInt(rules["minTrackDurationSec"]); minDur > 0 && !hasField("min_track_duration_sec") {
		def.Include = append(def.Include, FilterRule{Field: "min_track_duration_sec", Value: minDur})
	}
	if maxDur := toInt(rules["maxTrackDurationSec"]); maxDur > 0 && !hasField("max_track_duration_sec") {
		def.Include = append(def.Include, FilterRule{Field: "max_track_duration_sec", Value: maxDur})
	}
	if adr, ok := rules["addedDateRange"]; ok && !hasField("added_date") {
		def.Include = append(def.Include, FilterRule{Field: "added_date", Value: adr})
	}
	// Source playlists: legacy UI stores playlist IDs as a list. This must be enforced in SQL.
	if v, ok := rules["sourcePlaylists"]; ok && !hasField("source_playlists") {
		def.Include = append(def.Include, FilterRule{Field: "source_playlists", Value: v})
	}
	if v, ok := rules["source_playlists"]; ok && !hasField("source_playlists") {
		def.Include = append(def.Include, FilterRule{Field: "source_playlists", Value: v})
	}
	if includeArchive, ok := rules["includePublicArchive"].(bool); ok && includeArchive && !hasField("include_public_archive") {
		def.Include = append(def.Include, FilterRule{Field: "include_public_archive", Value: true})
	}
	if includeArchive, ok := rules["include_archive"].(bool); ok && includeArchive && !hasField("include_public_archive") {
		def.Include = append(def.Include, FilterRule{Field: "include_public_archive", Value: true})
	}

	if excludeExplicit, ok := rules["excludeExplicit"].(bool); ok && excludeExplicit && !hasField("explicit") {
		// Legacy UI semantics: "exclude explicit" means explicit=false.
		def.Include = append(def.Include, FilterRule{Field: "explicit", Value: false})
	}

	if def.Duration.TargetMS <= 0 {
		if mins := toInt(rules["targetMinutes"]); mins > 0 {
			def.Duration.TargetMS = int64(mins) * int64(time.Minute/time.Millisecond)
		}
	}
	if def.Duration.Tolerance <= 0 {
		if sec := toInt(rules["durationAccuracy"]); sec > 0 {
			def.Duration.Tolerance = int64(sec) * 1000
		}
	}

	// Respect the separationEnabled flag: if explicitly false or absent while
	// separation values exist from legacy data, zero them out so the engine
	// doesn't silently apply constraints the user disabled.
	separationEnabled := true
	if enabled, ok := rules["separationEnabled"].(bool); ok {
		separationEnabled = enabled
	} else if _, hasSep := rules["separation"]; hasSep {
		// Legacy data with separation values but no explicit enabled flag:
		// treat as enabled for backward compatibility.
		separationEnabled = true
	}

	// Bumper config from legacy rules.
	if bumpers, ok := rules["bumpers"].(map[string]any); ok {
		if enabled, ok := bumpers["enabled"].(bool); ok && enabled {
			def.Bumpers.Enabled = true
		}
		if st, ok := bumpers["sourceType"].(string); ok {
			def.Bumpers.SourceType = st
		}
		if pid, ok := bumpers["playlistID"].(string); ok {
			def.Bumpers.PlaylistID = pid
		}
		if genre, ok := bumpers["genre"].(string); ok {
			def.Bumpers.Genre = genre
		}
		if query, ok := bumpers["query"].(string); ok {
			def.Bumpers.Query = strings.TrimSpace(query)
		}
		if includeArchive, ok := bumpers["includePublicArchive"].(bool); ok && includeArchive {
			def.Bumpers.IncludePublicArchive = true
		}
		if maxPerGap, ok := bumpers["maxPerGap"]; ok {
			def.Bumpers.MaxPerGap = toInt(maxPerGap)
		}
		if def.Bumpers.MaxPerGap < 1 {
			def.Bumpers.MaxPerGap = 8
		}
	}

	// Loop-to-fill preference from legacy flat rules key.
	if v, ok := rules["loopToFill"].(bool); ok && v {
		def.LoopToFill = true
	}

	// Interstitials config from legacy rules stored by the dashboard form.
	// The JSON keys in sb.Rules["interstitials"] match the JSON tags on
	// InterstitialsConfig so unmarshal already populated most fields.
	// Handle the legacy single-source case where Sources is empty but the
	// top-level sourceType/playlistID/genre/query keys are present.
	if def.Interstitials.Enabled && len(def.Interstitials.Sources) == 0 {
		if interstitials, ok := rules["interstitials"].(map[string]any); ok {
			st := strings.ToLower(strings.TrimSpace(toString(interstitials["sourceType"])))
			if st != "" {
				src := InterstitialSource{SourceType: st}
				switch st {
				case "playlist":
					src.PlaylistID = strings.TrimSpace(toString(interstitials["playlistID"]))
				case "genre":
					src.Genre = strings.TrimSpace(toString(interstitials["genre"]))
				default:
					src.Query = strings.TrimSpace(toString(interstitials["query"]))
				}
				if src.PlaylistID != "" || src.Genre != "" || src.Query != "" {
					def.Interstitials.Sources = []InterstitialSource{src}
				}
			}
		}
	}
	// Defaults for interstitials EveryN / PerBreak.
	if def.Interstitials.Enabled && def.Interstitials.EveryN < 1 {
		def.Interstitials.EveryN = 4
	}
	if def.Interstitials.Enabled && def.Interstitials.PerBreak < 1 {
		def.Interstitials.PerBreak = 1
	}

	if !separationEnabled {
		def.Separation = SeparationRules{}
	} else if sep, ok := rules["separation"].(map[string]any); ok {
		if def.Separation.ArtistSec == 0 {
			def.Separation.ArtistSec = toInt(sep["artist"]) * 60
		}
		if def.Separation.TitleSec == 0 {
			def.Separation.TitleSec = toInt(sep["title"]) * 60
		}
		if def.Separation.AlbumSec == 0 {
			def.Separation.AlbumSec = toInt(sep["album"]) * 60
		}
		if def.Separation.LabelSec == 0 {
			def.Separation.LabelSec = toInt(sep["label"]) * 60
		}
	}

	return def
}

// tailFillBumpers appends short tracks from a bumper source to fill remaining time.
func (e *Engine) tailFillBumpers(ctx context.Context, result *GenerateResult, cfg BumperConfig, stationID string, targetMS int64) {
	bumpers, err := e.fetchBumperCandidates(ctx, cfg, stationID)
	if err != nil || len(bumpers) == 0 {
		return
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	lastID := ""
	prevID := ""
	added := 0

	for added < cfg.MaxPerGap && result.TotalMS < targetMS {
		remaining := targetMS - result.TotalMS
		if remaining <= 0 {
			break
		}

		// Find the best-fitting bumper for the remaining gap.
		bestIdx := -1
		var bestDur int64 = -1
		for i, b := range bumpers {
			dur := b.Duration.Milliseconds()
			if dur <= 0 || dur > remaining {
				continue
			}
			// Avoid immediate repeat.
			if b.ID == lastID && len(bumpers) > 1 {
				continue
			}
			// Avoid ABAB ping-pong.
			if b.ID == prevID && lastID != "" && prevID != "" && len(bumpers) > 2 {
				continue
			}
			if dur > bestDur {
				bestDur = dur
				bestIdx = i
			}
		}
		if bestIdx == -1 {
			break
		}

		chosen := bumpers[bestIdx]
		result.Items = append(result.Items, SequenceItem{
			MediaID:    chosen.ID,
			StartsAtMS: result.TotalMS,
			EndsAtMS:   result.TotalMS + bestDur,
			IntroEnd:   chosen.CuePoints.IntroEnd,
			OutroIn:    chosen.CuePoints.OutroIn,
			Energy:     deriveEnergy(chosen),
			IsBumper:   true,
		})
		result.TotalMS += bestDur
		prevID = lastID
		lastID = chosen.ID
		added++
		result.BumperCount++

		// Shuffle remaining bumpers for variety.
		rng.Shuffle(len(bumpers), func(i, j int) { bumpers[i], bumpers[j] = bumpers[j], bumpers[i] })
	}
}

// fetchBumperCandidates loads media items matching the bumper source config.
func (e *Engine) fetchBumperCandidates(ctx context.Context, cfg BumperConfig, stationID string) ([]models.MediaItem, error) {
	query := e.db.WithContext(ctx).Where("station_id = ?", stationID).
		Where("analysis_state != ? AND duration > 0", models.AnalysisFailed)
	if cfg.IncludePublicArchive {
		query = e.db.WithContext(ctx).
			Where("(station_id = ?) OR (show_in_archive = ? AND station_id IN (SELECT id FROM stations WHERE active = ? AND public = ? AND approved = ?))",
				stationID, true, true, true, true).
			Where("analysis_state != ? AND duration > 0", models.AnalysisFailed)
	}

	var tracks []models.MediaItem
	switch cfg.SourceType {
	case "playlist":
		if cfg.PlaylistID == "" {
			return nil, nil
		}
		query = query.Where("id IN (SELECT media_id FROM playlist_items WHERE playlist_id = ?)", cfg.PlaylistID)
	case "genre":
		if cfg.Genre == "" {
			return nil, nil
		}
		query = query.Where("genre = ?", cfg.Genre)
	case "artist":
		if cfg.Query == "" {
			return nil, nil
		}
		query = query.Where(normalizedSQLExprSB("artist")+" LIKE ?", "%"+normalizeMatchText(cfg.Query)+"%")
	case "label":
		if cfg.Query == "" {
			return nil, nil
		}
		query = query.Where("LOWER(label) LIKE ?", "%"+strings.ToLower(cfg.Query)+"%")
	default: // "title"
		if cfg.Query == "" {
			return nil, nil
		}
		query = query.Where("LOWER(title) LIKE ?", "%"+strings.ToLower(cfg.Query)+"%")
	}

	if err := query.Find(&tracks).Error; err != nil {
		return nil, err
	}
	return tracks, nil
}

// fetchInterstitialCandidates loads ad/interstitial tracks from the configured sources.
func (e *Engine) fetchInterstitialCandidates(ctx context.Context, cfg InterstitialsConfig, stationID string) ([]models.MediaItem, error) {
	if !cfg.Enabled || len(cfg.Sources) == 0 {
		return nil, nil
	}

	baseQuery := e.db.WithContext(ctx).Where("station_id = ?", stationID).
		Where("analysis_state != ? AND duration > 0", models.AnalysisFailed)
	if cfg.IncludePublicArchive {
		baseQuery = e.db.WithContext(ctx).
			Where("(station_id = ?) OR (show_in_archive = ? AND station_id IN (SELECT id FROM stations WHERE active = ? AND public = ? AND approved = ?))",
				stationID, true, true, true, true).
			Where("analysis_state != ? AND duration > 0", models.AnalysisFailed)
	}

	clauses := make([]string, 0, len(cfg.Sources))
	args := make([]any, 0, len(cfg.Sources)*2)
	for _, src := range cfg.Sources {
		st := strings.ToLower(strings.TrimSpace(src.SourceType))
		switch st {
		case "playlist":
			if strings.TrimSpace(src.PlaylistID) == "" {
				continue
			}
			clauses = append(clauses, "id IN (SELECT media_id FROM playlist_items WHERE playlist_id = ?)")
			args = append(args, src.PlaylistID)
		case "genre":
			if strings.TrimSpace(src.Genre) == "" {
				continue
			}
			clauses = append(clauses, "genre = ?")
			args = append(args, src.Genre)
		case "artist":
			if strings.TrimSpace(src.Query) == "" {
				continue
			}
			clauses = append(clauses, normalizedSQLExprSB("artist")+" LIKE ?")
			args = append(args, "%"+normalizeMatchText(src.Query)+"%")
		case "label":
			if strings.TrimSpace(src.Query) == "" {
				continue
			}
			clauses = append(clauses, "LOWER(label) LIKE ?")
			args = append(args, "%"+strings.ToLower(src.Query)+"%")
		default: // "title"
			if strings.TrimSpace(src.Query) == "" {
				continue
			}
			clauses = append(clauses, "LOWER(title) LIKE ?")
			args = append(args, "%"+strings.ToLower(src.Query)+"%")
		}
	}
	if len(clauses) == 0 {
		return nil, nil
	}

	joiner := " OR "
	if strings.ToLower(strings.TrimSpace(cfg.Logic)) == "all" {
		joiner = " AND "
	}
	var tracks []models.MediaItem
	if err := baseQuery.Where("("+strings.Join(clauses, joiner)+")", args...).Find(&tracks).Error; err != nil {
		return nil, err
	}
	return tracks, nil
}

// interleaveInterstitials inserts interstitial tracks into result every everyN music tracks,
// adjusting StartsAtMS/EndsAtMS for all subsequent items. The result is modified in place.
func interleaveInterstitials(result *GenerateResult, interstitials []models.MediaItem, everyN, perBreak int, rng *rand.Rand) {
	if everyN < 1 || perBreak < 1 || len(interstitials) == 0 {
		return
	}

	// Shuffle for variety.
	rng.Shuffle(len(interstitials), func(i, j int) { interstitials[i], interstitials[j] = interstitials[j], interstitials[i] })

	newItems := make([]SequenceItem, 0, len(result.Items)+len(result.Items)/everyN*perBreak)
	musicCount := 0
	cursor := int64(0)
	interIdx := 0

	for _, item := range result.Items {
		dur := item.EndsAtMS - item.StartsAtMS
		item.StartsAtMS = cursor
		item.EndsAtMS = cursor + dur
		cursor = item.EndsAtMS
		newItems = append(newItems, item)

		if !item.IsBumper && !item.IsInterstitial {
			musicCount++
			if musicCount%everyN == 0 {
				for j := 0; j < perBreak; j++ {
					if interIdx >= len(interstitials) {
						interIdx = 0
					}
					ad := interstitials[interIdx]
					adDur := ad.Duration.Milliseconds()
					newItems = append(newItems, SequenceItem{
						MediaID:        ad.ID,
						StartsAtMS:     cursor,
						EndsAtMS:       cursor + adDur,
						IsInterstitial: true,
					})
					cursor += adDur
					interIdx++
				}
			}
		}
	}

	result.Items = newItems
	result.TotalMS = cursor
}

func (e *Engine) recentPlays(ctx context.Context, stationID string, windows map[string]time.Duration) ([]models.PlayHistory, error) {
	maxWindow := time.Duration(0)
	for _, win := range windows {
		if win > maxWindow {
			maxWindow = win
		}
	}

	query := e.db.WithContext(ctx).
		Where("station_id = ?", stationID).
		Order("started_at DESC")

	// When separation windows are configured, keep time-bounded history.
	// Otherwise, still fetch a small recent slice to prevent immediate repeats.
	if maxWindow > 0 {
		cutoff := time.Now().Add(-maxWindow)
		query = query.Where("started_at >= ?", cutoff)
	} else {
		query = query.Limit(25)
	}

	var plays []models.PlayHistory
	err := query.Find(&plays).Error
	return plays, err
}

type candidate struct {
	Item   models.MediaItem
	Score  float64
	Energy float64
	Tags   map[string]struct{}
}

func (e *Engine) fetchCandidates(ctx context.Context, def Definition, stationID string, recent []models.PlayHistory) ([]candidate, error) {
	// Accept tracks that are not failed — pending/complete/blank are all playable.
	// Duration > 0 ensures we only pick tracks with known length for scheduling.
	analysisFilter := "analysis_state != ? AND duration > 0"
	query := e.db.WithContext(ctx).Where("station_id = ?", stationID).Where(analysisFilter, models.AnalysisFailed)
	if definitionIncludesPublicArchive(def) {
		query = e.db.WithContext(ctx).
			Where("(station_id = ?) OR (show_in_archive = ? AND station_id IN (SELECT id FROM stations WHERE active = ? AND public = ? AND approved = ?))",
				stationID, true, true, true, true).
			Where(analysisFilter, models.AnalysisFailed)
	}

	// Apply include filters via SQL when possible
	for _, rule := range def.Include {
		query = applyFilterRule(query, rule, true)
	}
	for _, rule := range def.Exclude {
		query = applyFilterRule(query, rule, false)
	}

	var items []models.MediaItem
	if err := query.Find(&items).Error; err != nil {
		return nil, err
	}

	windows := def.Separation.SeparationDurations()
	recentCache := buildRecentCache(recent)
	avoidMediaID := mostRecentMediaID(recent)

	candidates := make([]candidate, 0, len(items))
	avoidedRecent := make([]candidate, 0, 1)
	for _, item := range items {
		// Keep items that satisfy include rules AND pass exclude rules.
		if !matchesFilters(item, def.Include, true) || !matchesFilters(item, def.Exclude, false) {
			continue
		}
		if violatesSeparation(item, recentCache, windows) {
			continue
		}

		cand := candidate{
			Item:   item,
			Energy: deriveEnergy(item),
			Score:  baseScore(item, def.Weights),
			Tags:   collectTags(item),
		}
		if avoidMediaID != "" && item.ID == avoidMediaID {
			avoidedRecent = append(avoidedRecent, cand)
			continue
		}
		candidates = append(candidates, cand)
	}

	// Fallback: if strict anti-repeat leaves nothing, allow the recent track.
	if len(candidates) == 0 && len(avoidedRecent) > 0 {
		candidates = append(candidates, avoidedRecent...)
	}

	return candidates, nil
}

func mostRecentMediaID(plays []models.PlayHistory) string {
	for _, play := range plays {
		if strings.TrimSpace(play.MediaID) != "" {
			return play.MediaID
		}
	}
	return ""
}

func applyFilterRule(query *gorm.DB, rule FilterRule, positive bool) *gorm.DB {
	field := strings.ToLower(rule.Field)
	value := rule.Value

	cond := func(clause string, arg interface{}) *gorm.DB {
		if positive {
			return query.Where(clause, arg)
		}
		return query.Where("NOT ("+clause+")", arg)
	}

	switch field {
	case "include_public_archive", "includearchive", "source_include_archive":
		// Scope handled before filter loop in fetchCandidates.
		return query
	case "source_playlists", "sourceplaylists", "playlists":
		// Limit candidates to media that exist in one of the selected playlists.
		// This is intentionally SQL-only: MediaItem doesn't preload playlist membership here.
		ids, ok := toStringSlice(value)
		if !ok || len(ids) == 0 {
			return query
		}
		clause := "EXISTS (SELECT 1 FROM playlist_items pi WHERE pi.media_id = media_items.id AND pi.playlist_id IN ?)"
		if positive {
			return query.Where(clause, ids)
		}
		return query.Where("NOT ("+clause+")", ids)
	case "genre", "mood", "language", "album", "title", "label":
		return cond(field+" = ?", value)
	case "artist":
		normalized := normalizeMatchText(toString(value))
		expr := normalizedSQLExprSB("artist")
		if positive {
			return query.Where(expr+" = ?", normalized)
		}
		return query.Where("NOT ("+expr+" = ?)", normalized)
	case "text_search":
		// Search across multiple text fields using ILIKE
		if searchText, ok := value.(string); ok && searchText != "" {
			pattern := "%" + strings.ToLower(searchText) + "%"
			normPattern := "%" + normalizeMatchText(searchText) + "%"
			searchClause := fmt.Sprintf(
				"(LOWER(title) LIKE ? OR LOWER(artist) LIKE ? OR LOWER(album) LIKE ? OR %s LIKE ? OR %s LIKE ? OR %s LIKE ?)",
				normalizedSQLExprSB("title"),
				normalizedSQLExprSB("artist"),
				normalizedSQLExprSB("album"),
			)
			if positive {
				return query.Where(searchClause, pattern, pattern, pattern, normPattern, normPattern, normPattern)
			}
			return query.Where("NOT "+searchClause, pattern, pattern, pattern, normPattern, normPattern, normPattern)
		}
		return query
	case "bpm":
		rangeVals := toFloatRange(value)
		if rangeVals[0] != 0 {
			query = cond("bpm >= ?", rangeVals[0])
		}
		if rangeVals[1] != 0 {
			query = cond("bpm <= ?", rangeVals[1])
		}
		return query
	case "year":
		rangeVals := toFloatRange(value)
		if rangeVals[0] != 0 {
			query = cond("year >= ?", int(rangeVals[0]))
		}
		if rangeVals[1] != 0 {
			query = cond("year <= ?", int(rangeVals[1]))
		}
		return query
	case "added_date":
		if m, ok := value.(map[string]any); ok {
			now := time.Now()
			if newerThan := anyToInt(m["newerThan"]); newerThan > 0 {
				unit, _ := m["newerThanUnit"].(string)
				cutoff := subtractDur(now, newerThan, unit)
				query = cond("created_at >= ?", cutoff)
			}
			if olderThan := anyToInt(m["olderThan"]); olderThan > 0 {
				unit, _ := m["olderThanUnit"].(string)
				cutoff := subtractDur(now, olderThan, unit)
				query = cond("created_at < ?", cutoff)
			}
		}
		return query
	case "explicit":
		return cond("explicit = ?", toBool(value))
	case "tags":
		if vals, ok := toStringSlice(value); ok {
			for _, tag := range vals {
				clause := "EXISTS (SELECT 1 FROM media_tag_links WHERE media_tag_links.media_item_id = media_items.id AND media_tag_links.tag_id = ?)"
				if positive {
					query = query.Where(clause, tag)
				} else {
					query = query.Where("NOT ("+clause+")", tag)
				}
			}
		}
	case "min_track_duration_sec":
		sec := toInt(value)
		if sec > 0 {
			ns := int64(sec) * int64(time.Second)
			if positive {
				query = query.Where("duration >= ?", ns)
			} else {
				query = query.Where("duration < ?", ns)
			}
		}
	case "max_track_duration_sec":
		sec := toInt(value)
		if sec > 0 {
			ns := int64(sec) * int64(time.Second)
			if positive {
				query = query.Where("duration <= ?", ns)
			} else {
				query = query.Where("duration > ?", ns)
			}
		}
	}

	// Fallback to in-memory filtering; load all and filter later.
	return query
}

func collectTags(item models.MediaItem) map[string]struct{} {
	tags := make(map[string]struct{})
	for _, link := range item.Tags {
		tags[link.TagID] = struct{}{}
	}
	return tags
}

func baseScore(item models.MediaItem, weights []WeightRule) float64 {
	score := 1.0
	for _, weight := range weights {
		if matchesWeight(item, weight) {
			score += weight.Weight
		}
	}
	return score
}

func matchesWeight(item models.MediaItem, weight WeightRule) bool {
	switch strings.ToLower(weight.Field) {
	case "genre":
		return strings.EqualFold(item.Genre, toString(weight.Value))
	case "mood":
		return strings.EqualFold(item.Mood, toString(weight.Value))
	case "tag":
		if id := toString(weight.Value); id != "" {
			for _, link := range item.Tags {
				if link.TagID == id {
					return true
				}
			}
		}
	case "new_release":
		if days, ok := weight.Value.(float64); ok {
			return time.Since(item.CreatedAt) <= time.Duration(days*24)*time.Hour
		}
	}
	return false
}

func deriveEnergy(item models.MediaItem) float64 {
	if item.BPM > 0 {
		return item.BPM
	}
	if item.ReplayGain != 0 {
		return 100 + item.ReplayGain
	}
	return 100
}

func (e *Engine) selectSequence(ctx context.Context, rng *rand.Rand, candidates []candidate, def Definition, targetMS int64, loopToFill bool) GenerateResult {
	remaining := make([]candidate, len(candidates))
	copy(remaining, candidates)

	tolerance := def.Duration.Tolerance
	if tolerance <= 0 {
		tolerance = 2000 // default 2-second tolerance
	}

	quotaState := newQuotaState(def.Quotas)
	var result GenerateResult
	var cursor int64

	// loopPassStartCursor tracks cursor position at the start of each loop refill.
	// If a full refill pass makes no progress, we stop to prevent an infinite loop.
	loopPassStartCursor := int64(-1)

	curve := def.Sequence.Curve
	for idx := 0; cursor < targetMS; idx++ {
		if len(remaining) == 0 {
			if !loopToFill {
				break
			}
			// Detect no-progress: if cursor hasn't advanced since the last refill, stop.
			if loopPassStartCursor == cursor {
				break
			}
			loopPassStartCursor = cursor

			// Re-populate from the full candidate set for another pass.
			remaining = make([]candidate, len(candidates))
			copy(remaining, candidates)

			// Avoid back-to-back repeat of the last played track.
			if len(result.Items) > 0 {
				lastID := result.Items[len(result.Items)-1].MediaID
				for i, c := range remaining {
					if c.Item.ID == lastID {
						remaining = append(remaining[:i], remaining[i+1:]...)
						break
					}
				}
			}
			if len(remaining) == 0 {
				break
			}
		}

		targetEnergy := 0.0
		if len(curve) > 0 {
			targetEnergy = curve[idx%len(curve)]
		}

		// Prefer tracks that fit the remaining time window. If none fit, fall
		// back to the full pool — the selected track will be clamped to the slot
		// boundary and the executor will hard-cut it. This eliminates silence
		// gaps caused by all remaining tracks being longer than the residual time.
		pickFrom := remaining
		if cursor > 0 {
			maxDurMS := targetMS + tolerance - cursor
			var fitting []candidate
			for _, c := range remaining {
				d := c.Item.Duration
				if d <= 0 {
					d = 3 * time.Minute
				}
				if d.Milliseconds() <= maxDurMS {
					fitting = append(fitting, c)
				}
			}
			if len(fitting) > 0 {
				pickFrom = fitting
			}
			// If no fitting tracks, pickFrom stays as remaining and the
			// selected track's EndsAtMS will be clamped to targetMS below.
		}

		selectedIdx := selectCandidate(rng, pickFrom, quotaState, targetEnergy)
		if selectedIdx == -1 {
			break
		}

		sel := pickFrom[selectedIdx]
		dur := sel.Item.Duration
		if dur <= 0 {
			dur = 3 * time.Minute
		}

		durMS := dur.Milliseconds()
		endsAtMS := cursor + durMS
		if endsAtMS > targetMS {
			endsAtMS = targetMS // clamp to slot boundary; executor hard-cuts
		}

		item := SequenceItem{
			MediaID:    sel.Item.ID,
			StartsAtMS: cursor,
			EndsAtMS:   endsAtMS,
			IntroEnd:   sel.Item.CuePoints.IntroEnd,
			OutroIn:    sel.Item.CuePoints.OutroIn,
			Energy:     sel.Energy,
		}

		result.Items = append(result.Items, item)
		cursor = endsAtMS
		quotaState.observe(sel.Item, sel.Tags)

		// Remove selected track from remaining (pickFrom may be a filtered subset).
		for i, c := range remaining {
			if c.Item.ID == sel.Item.ID {
				remaining = append(remaining[:i], remaining[i+1:]...)
				break
			}
		}
	}

	result.TotalMS = cursor
	if cursor < targetMS {
		result.Exhausted = true
		result.Warnings = append(result.Warnings, "underfilled_target")
	}

	for _, warn := range quotaState.warnings() {
		result.Warnings = append(result.Warnings, warn)
	}

	return result
}

func selectCandidate(rng *rand.Rand, candidates []candidate, quotaState *quotaState, targetEnergy float64) int {
	type scored struct {
		idx   int
		score float64
	}

	scoredList := make([]scored, 0, len(candidates))
	for idx, cand := range candidates {
		if !quotaState.canSelect(cand.Item, cand.Tags) {
			continue
		}

		score := cand.Score
		if targetEnergy > 0 {
			deviation := math.Abs(targetEnergy - cand.Energy)
			score += 1 / (1 + deviation)
		}
		score += rng.Float64() * 0.1

		scoredList = append(scoredList, scored{idx: idx, score: score})
	}

	if len(scoredList) == 0 {
		return -1
	}

	sort.Slice(scoredList, func(i, j int) bool { return scoredList[i].score > scoredList[j].score })
	return scoredList[0].idx
}

// quotaState tracks quota satisfaction progress.
type quotaState struct {
	rules  []QuotaRule
	counts []int
	alerts []string
}

func newQuotaState(rules []QuotaRule) *quotaState {
	return &quotaState{
		rules:  rules,
		counts: make([]int, len(rules)),
	}
}

func (q *quotaState) canSelect(item models.MediaItem, tags map[string]struct{}) bool {
	for idx, rule := range q.rules {
		if rule.Max > 0 && q.counts[idx] >= rule.Max {
			if matchesQuota(rule, item, tags) {
				return false
			}
		}
	}
	return true
}

func (q *quotaState) observe(item models.MediaItem, tags map[string]struct{}) {
	for idx, rule := range q.rules {
		if matchesQuota(rule, item, tags) {
			q.counts[idx]++
		}
	}
}

func (q *quotaState) warnings() []string {
	for idx, rule := range q.rules {
		if rule.Min > 0 && q.counts[idx] < rule.Min {
			q.alerts = append(q.alerts, "quota_min_unmet:"+rule.Field)
		}
	}
	return q.alerts
}

func matchesQuota(rule QuotaRule, item models.MediaItem, tags map[string]struct{}) bool {
	if len(rule.Values) == 0 {
		return true
	}
	switch strings.ToLower(rule.Field) {
	case "genre":
		return contains(rule.Values, item.Genre)
	case "mood":
		return contains(rule.Values, item.Mood)
	case "tag":
		for _, value := range rule.Values {
			if _, ok := tags[value]; ok {
				return true
			}
		}
	case "label":
		return contains(rule.Values, item.Label)
	case "artist":
		return containsNormalized(rule.Values, item.Artist)
	case "explicit":
		target := strings.EqualFold(rule.Values[0], "true")
		return item.Explicit == target
	}
	return false
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if strings.EqualFold(value, candidate) {
			return true
		}
	}
	return false
}

func containsNormalized(values []string, candidate string) bool {
	normCandidate := normalizeMatchText(candidate)
	for _, value := range values {
		if normalizeMatchText(value) == normCandidate {
			return true
		}
	}
	return false
}

var matchNormalizer = strings.NewReplacer(
	" ", "",
	".", "",
	"-", "",
	"_", "",
	"'", "",
	"\"", "",
	"/", "",
	"\\", "",
	"(", "",
	")", "",
	"[", "",
	"]", "",
	",", "",
	";", "",
	":", "",
)

func normalizeMatchText(s string) string {
	return matchNormalizer.Replace(strings.ToLower(strings.TrimSpace(s)))
}

func normalizedSQLExprSB(col string) string {
	return fmt.Sprintf(
		`REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(LOWER(%s), ' ', ''), '.', ''), '-', ''), '_', ''), '''', ''), '"', ''), '/', ''), '\\', ''), '(', ''), ')', ''), '[', ''), ']', ''), ',', ''), ';', '')`,
		col,
	)
}

func buildRecentCache(plays []models.PlayHistory) map[string]map[string]time.Time {
	cache := map[string]map[string]time.Time{}
	for _, play := range plays {
		insertRecent(cache, "artist", play.Artist, play.StartedAt)
		insertRecent(cache, "title", play.MetadataString("title"), play.StartedAt)
		insertRecent(cache, "album", play.MetadataString("album"), play.StartedAt)
		insertRecent(cache, "label", play.MetadataString("label"), play.StartedAt)
	}
	return cache
}

func insertRecent(cache map[string]map[string]time.Time, key, value string, ts time.Time) {
	if value == "" {
		return
	}
	if cache[key] == nil {
		cache[key] = map[string]time.Time{}
	}
	if existing, ok := cache[key][value]; !ok || ts.After(existing) {
		cache[key][value] = ts
	}
}

func violatesSeparation(item models.MediaItem, recent map[string]map[string]time.Time, windows map[string]time.Duration) bool {
	now := time.Now()
	if dur := windows["artist"]; dur > 0 {
		if ts := lookupRecent(recent, "artist", item.Artist); !ts.IsZero() && now.Sub(ts) < dur {
			return true
		}
	}
	if dur := windows["title"]; dur > 0 {
		if ts := lookupRecent(recent, "title", item.Title); !ts.IsZero() && now.Sub(ts) < dur {
			return true
		}
	}
	if dur := windows["album"]; dur > 0 {
		if ts := lookupRecent(recent, "album", item.Album); !ts.IsZero() && now.Sub(ts) < dur {
			return true
		}
	}
	if dur := windows["label"]; dur > 0 {
		if ts := lookupRecent(recent, "label", item.Label); !ts.IsZero() && now.Sub(ts) < dur {
			return true
		}
	}
	return false
}

func lookupRecent(cache map[string]map[string]time.Time, key, value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	if inner := cache[key]; inner != nil {
		return inner[value]
	}
	return time.Time{}
}

func toFloatRange(value interface{}) [2]float64 {
	var out [2]float64
	switch v := value.(type) {
	case []interface{}:
		if len(v) > 0 {
			out[0] = toFloat(v[0])
		}
		if len(v) > 1 {
			out[1] = toFloat(v[1])
		}
	case map[string]interface{}:
		out[0] = toFloat(v["min"])
		out[1] = toFloat(v["max"])
	}
	return out
}

func toFloat(value interface{}) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	default:
		return 0
	}
}

func toInt(value interface{}) int {
	return int(toFloat(value))
}

func toBool(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || v == "1"
	case float64:
		return v != 0
	default:
		return false
	}
}

func toString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	}
	return ""
}

func matchesFilters(item models.MediaItem, rules []FilterRule, positive bool) bool {
	for _, rule := range rules {
		if !evaluateFilter(item, rule, positive) {
			return false
		}
	}
	return true
}

func evaluateFilter(item models.MediaItem, rule FilterRule, positive bool) bool {
	// SQL-only filter (handled by applyFilterRule). Skip in-memory evaluation so it doesn't
	// accidentally exclude everything when used as an exclude rule.
	switch strings.ToLower(rule.Field) {
	case "source_playlists", "sourceplaylists", "playlists", "include_public_archive", "includearchive", "source_include_archive":
		return true
	}

	match := false
	switch strings.ToLower(rule.Field) {
	case "genre":
		match = strings.EqualFold(item.Genre, toString(rule.Value))
	case "mood":
		match = strings.EqualFold(item.Mood, toString(rule.Value))
	case "artist":
		match = normalizeMatchText(item.Artist) == normalizeMatchText(toString(rule.Value))
	case "album":
		match = strings.EqualFold(item.Album, toString(rule.Value))
	case "label":
		match = strings.EqualFold(item.Label, toString(rule.Value))
	case "language":
		match = strings.EqualFold(item.Language, toString(rule.Value))
	case "explicit":
		match = item.Explicit == toBool(rule.Value)
	case "bpm":
		rangeVals := toFloatRange(rule.Value)
		min := rangeVals[0]
		max := rangeVals[1]
		if min == 0 && max == 0 {
			match = item.BPM > 0
		} else {
			match = item.BPM >= min || min == 0
			if match && max != 0 {
				match = item.BPM <= max
			}
		}
	case "year":
		rangeVals := toFloatRange(rule.Value)
		match = true
		// Parse year string to float64
		var yearFloat float64
		if item.Year != "" {
			if y, err := strconv.ParseFloat(item.Year, 64); err == nil {
				yearFloat = y
			}
		}
		if rangeVals[0] != 0 && yearFloat < rangeVals[0] {
			match = false
		}
		if rangeVals[1] != 0 && yearFloat > rangeVals[1] {
			match = false
		}
	case "added_date":
		match = true
		if m, ok := rule.Value.(map[string]any); ok {
			now := time.Now()
			if newerThan := anyToInt(m["newerThan"]); newerThan > 0 {
				unit, _ := m["newerThanUnit"].(string)
				cutoff := subtractDur(now, newerThan, unit)
				if item.CreatedAt.Before(cutoff) {
					match = false
				}
			}
			if olderThan := anyToInt(m["olderThan"]); olderThan > 0 {
				unit, _ := m["olderThanUnit"].(string)
				cutoff := subtractDur(now, olderThan, unit)
				if !item.CreatedAt.Before(cutoff) {
					match = false
				}
			}
		}
	case "tag":
		match = false
		if vals, ok := toStringSlice(rule.Value); ok {
			for _, val := range vals {
				for _, link := range item.Tags {
					if strings.EqualFold(link.TagID, val) {
						match = true
						break
					}
				}
				if match {
					break
				}
			}
		}
	default:
		match = true
	}
	if positive {
		return match
	}
	return !match
}

func definitionIncludesPublicArchive(def Definition) bool {
	for _, rule := range def.Include {
		switch strings.ToLower(strings.TrimSpace(rule.Field)) {
		case "include_public_archive", "includearchive", "source_include_archive":
			if toBool(rule.Value) {
				return true
			}
		}
	}
	return false
}

func toStringSlice(value interface{}) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return v, true
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := toString(item); s != "" {
				out = append(out, s)
			}
		}
		return out, true
	}
	return nil, false
}

// anyToInt extracts an int from a JSON-decoded value (float64, int, or string).
func anyToInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

// subtractDur subtracts n units from t. Supported units: "days" (default), "weeks", "months".
func subtractDur(t time.Time, n int, unit string) time.Time {
	switch unit {
	case "weeks":
		return t.AddDate(0, 0, -7*n)
	case "months":
		return t.AddDate(0, -n, 0)
	default:
		return t.AddDate(0, 0, -n)
	}
}
