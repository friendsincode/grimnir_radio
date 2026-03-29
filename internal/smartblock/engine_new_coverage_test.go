/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package smartblock

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ---- anyToInt ----

func TestAnyToInt(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected int
	}{
		{"float64", float64(42.9), 42},
		{"int", int(7), 7},
		{"string numeric", "100", 100},
		{"string non-numeric", "abc", 0},
		{"nil", nil, 0},
		{"bool", true, 0},
		{"zero float", float64(0), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := anyToInt(tt.input)
			if got != tt.expected {
				t.Errorf("anyToInt(%v) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

// ---- containsNormalized ----

func TestContainsNormalized(t *testing.T) {
	tests := []struct {
		name      string
		values    []string
		candidate string
		want      bool
	}{
		{"exact match", []string{"The Beatles"}, "The Beatles", true},
		{"normalized punctuation", []string{"AC/DC"}, "acdc", true},
		{"case insensitive", []string{"David Bowie"}, "DAVID BOWIE", true},
		{"spaces stripped", []string{"Pink Floyd"}, "PinkFloyd", true},
		{"no match", []string{"Led Zeppelin"}, "Metallica", false},
		{"empty values", []string{}, "someone", false},
		{"empty candidate", []string{"artist"}, "", false},
		{"hyphen stripped", []string{"AC-DC"}, "acdc", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsNormalized(tt.values, tt.candidate)
			if got != tt.want {
				t.Errorf("containsNormalized(%v, %q) = %v, want %v", tt.values, tt.candidate, got, tt.want)
			}
		})
	}
}

// ---- matchesWeight ----

func TestMatchesWeight(t *testing.T) {
	itemRock := models.MediaItem{Genre: "Rock", Mood: "Energetic"}
	itemWithTag := models.MediaItem{
		Tags: []models.MediaTagLink{{TagID: "t-1"}},
	}
	newItem := models.MediaItem{
		CreatedAt: time.Now().Add(-1 * time.Hour),
	}
	oldItem := models.MediaItem{
		CreatedAt: time.Now().Add(-100 * 24 * time.Hour),
	}

	tests := []struct {
		name   string
		item   models.MediaItem
		weight WeightRule
		want   bool
	}{
		{"genre match", itemRock, WeightRule{Field: "genre", Value: "Rock", Weight: 1.0}, true},
		{"genre no match", itemRock, WeightRule{Field: "genre", Value: "Pop", Weight: 1.0}, false},
		{"mood match", itemRock, WeightRule{Field: "mood", Value: "Energetic", Weight: 0.5}, true},
		{"mood no match", itemRock, WeightRule{Field: "mood", Value: "Chill", Weight: 0.5}, false},
		{"tag match", itemWithTag, WeightRule{Field: "tag", Value: "t-1", Weight: 2.0}, true},
		{"tag no match", itemWithTag, WeightRule{Field: "tag", Value: "t-99", Weight: 2.0}, false},
		{"new_release within window", newItem, WeightRule{Field: "new_release", Value: float64(30), Weight: 1.0}, true},
		{"new_release outside window", oldItem, WeightRule{Field: "new_release", Value: float64(30), Weight: 1.0}, false},
		{"unknown field", itemRock, WeightRule{Field: "unknown", Value: "x", Weight: 1.0}, false},
		{"tag empty value", itemWithTag, WeightRule{Field: "tag", Value: "", Weight: 1.0}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesWeight(tt.item, tt.weight)
			if got != tt.want {
				t.Errorf("matchesWeight() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---- normalizedSQLExprSB ----

func TestNormalizedSQLExprSB(t *testing.T) {
	expr := normalizedSQLExprSB("artist")
	if expr == "" {
		t.Fatal("normalizedSQLExprSB returned empty string")
	}
	// Should contain the column name
	if len(expr) < 10 {
		t.Errorf("normalizedSQLExprSB expression too short: %q", expr)
	}
	// Should contain "artist" somewhere
	found := false
	for i := 0; i < len(expr)-5; i++ {
		if expr[i:i+6] == "artist" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("normalizedSQLExprSB expression does not contain column name: %q", expr)
	}

	// Different columns produce different expressions
	expr2 := normalizedSQLExprSB("title")
	if expr == expr2 {
		t.Error("normalizedSQLExprSB(artist) == normalizedSQLExprSB(title), expected different")
	}
}

// ---- subtractDur ----

func TestSubtractDur(t *testing.T) {
	base := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		n    int
		unit string
		want time.Time
	}{
		{"days default", 7, "", base.AddDate(0, 0, -7)},
		{"days explicit", 3, "days", base.AddDate(0, 0, -3)},
		{"weeks", 2, "weeks", base.AddDate(0, 0, -14)},
		{"months", 1, "months", base.AddDate(0, -1, 0)},
		{"months multiple", 3, "months", base.AddDate(0, -3, 0)},
		{"zero days", 0, "days", base},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := subtractDur(base, tt.n, tt.unit)
			if !got.Equal(tt.want) {
				t.Errorf("subtractDur(%v, %d, %q) = %v, want %v", base, tt.n, tt.unit, got, tt.want)
			}
		})
	}
}

// ---- evaluateFilter ----

func TestEvaluateFilter(t *testing.T) {
	item := models.MediaItem{
		Genre:    "Rock",
		Mood:     "Energetic",
		Artist:   "The Beatles",
		Album:    "Abbey Road",
		Label:    "Apple Records",
		Language: "en",
		Explicit: false,
		BPM:      120.0,
		Year:     "1969",
		Title:    "Come Together",
	}

	tests := []struct {
		name     string
		rule     FilterRule
		positive bool
		want     bool
	}{
		{"genre include match", FilterRule{Field: "genre", Value: "Rock"}, true, true},
		{"genre include no match", FilterRule{Field: "genre", Value: "Pop"}, true, false},
		{"genre exclude match (false because positive=false, item matches)", FilterRule{Field: "genre", Value: "Rock"}, false, false},
		{"genre exclude no match (true because positive=false, item doesn't match)", FilterRule{Field: "genre", Value: "Pop"}, false, true},
		{"mood include", FilterRule{Field: "mood", Value: "Energetic"}, true, true},
		{"artist include match", FilterRule{Field: "artist", Value: "The Beatles"}, true, true},
		{"artist include normalized", FilterRule{Field: "artist", Value: "thebeatles"}, true, true},
		{"album include match", FilterRule{Field: "album", Value: "Abbey Road"}, true, true},
		{"label include match", FilterRule{Field: "label", Value: "Apple Records"}, true, true},
		{"language include match", FilterRule{Field: "language", Value: "en"}, true, true},
		{"explicit include false match", FilterRule{Field: "explicit", Value: false}, true, true},
		{"explicit include true no match", FilterRule{Field: "explicit", Value: true}, true, false},
		{"bpm in range", FilterRule{Field: "bpm", Value: []interface{}{100.0, 140.0}}, true, true},
		{"bpm out of range", FilterRule{Field: "bpm", Value: []interface{}{130.0, 150.0}}, true, false},
		{"year in range", FilterRule{Field: "year", Value: []interface{}{1960.0, 1970.0}}, true, true},
		{"year out of range", FilterRule{Field: "year", Value: []interface{}{1970.0, 1980.0}}, true, false},
		{"source_playlists skip", FilterRule{Field: "source_playlists", Value: []string{"pl-1"}}, true, true},
		{"include_public_archive skip", FilterRule{Field: "include_public_archive", Value: true}, true, true},
		{"unknown field default true", FilterRule{Field: "unknown_field", Value: "x"}, true, true},
		{"tag no match", FilterRule{Field: "tag", Value: []string{"t-missing"}}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateFilter(item, tt.rule, tt.positive)
			if got != tt.want {
				t.Errorf("evaluateFilter(%q, positive=%v) = %v, want %v", tt.rule.Field, tt.positive, got, tt.want)
			}
		})
	}
}

func TestEvaluateFilter_AddedDate(t *testing.T) {
	now := time.Now()
	recentItem := models.MediaItem{CreatedAt: now.Add(-2 * 24 * time.Hour)} // 2 days old
	oldItem := models.MediaItem{CreatedAt: now.Add(-60 * 24 * time.Hour)}   // 60 days old

	// newerThan 7 days → only recentItem passes
	newerRule := FilterRule{
		Field: "added_date",
		Value: map[string]any{
			"newerThan":     float64(7),
			"newerThanUnit": "days",
		},
	}
	if !evaluateFilter(recentItem, newerRule, true) {
		t.Error("recent item should pass newerThan 7 days filter")
	}
	if evaluateFilter(oldItem, newerRule, true) {
		t.Error("old item should not pass newerThan 7 days filter")
	}

	// olderThan 30 days → only oldItem passes
	olderRule := FilterRule{
		Field: "added_date",
		Value: map[string]any{
			"olderThan":     float64(30),
			"olderThanUnit": "days",
		},
	}
	if evaluateFilter(recentItem, olderRule, true) {
		t.Error("recent item should not pass olderThan 30 days filter")
	}
	if !evaluateFilter(oldItem, olderRule, true) {
		t.Error("old item should pass olderThan 30 days filter")
	}
}

func TestEvaluateFilter_Tag(t *testing.T) {
	itemWithTag := models.MediaItem{
		Tags: []models.MediaTagLink{{TagID: "tag-rock"}},
	}
	itemNoTag := models.MediaItem{}

	rule := FilterRule{Field: "tag", Value: []string{"tag-rock"}}

	if !evaluateFilter(itemWithTag, rule, true) {
		t.Error("item with matching tag should pass include tag filter")
	}
	if evaluateFilter(itemNoTag, rule, true) {
		t.Error("item without tag should fail include tag filter")
	}
	// exclude semantics
	if evaluateFilter(itemWithTag, rule, false) {
		t.Error("item with matching tag should fail exclude tag filter")
	}
	if !evaluateFilter(itemNoTag, rule, false) {
		t.Error("item without tag should pass exclude tag filter")
	}
}

// ---- matchesQuota ----

func TestMatchesQuota(t *testing.T) {
	itemRock := models.MediaItem{Genre: "Rock", Mood: "Energetic", Label: "RCA", Artist: "Elvis Presley", Explicit: true}
	tags := map[string]struct{}{"t-rock": {}}

	tests := []struct {
		name string
		rule QuotaRule
		item models.MediaItem
		tags map[string]struct{}
		want bool
	}{
		{"empty values matches all", QuotaRule{Field: "genre", Values: nil}, itemRock, nil, true},
		{"genre match", QuotaRule{Field: "genre", Values: []string{"Rock"}}, itemRock, nil, true},
		{"genre no match", QuotaRule{Field: "genre", Values: []string{"Pop"}}, itemRock, nil, false},
		{"mood match", QuotaRule{Field: "mood", Values: []string{"Energetic"}}, itemRock, nil, true},
		{"mood no match", QuotaRule{Field: "mood", Values: []string{"Chill"}}, itemRock, nil, false},
		{"label match", QuotaRule{Field: "label", Values: []string{"RCA"}}, itemRock, nil, true},
		{"label no match", QuotaRule{Field: "label", Values: []string{"Sony"}}, itemRock, nil, false},
		{"artist match normalized", QuotaRule{Field: "artist", Values: []string{"Elvis Presley"}}, itemRock, nil, true},
		{"artist no match", QuotaRule{Field: "artist", Values: []string{"Beatles"}}, itemRock, nil, false},
		{"explicit true match", QuotaRule{Field: "explicit", Values: []string{"true"}}, itemRock, nil, true},
		{"explicit false no match", QuotaRule{Field: "explicit", Values: []string{"false"}}, itemRock, nil, false},
		{"tag match", QuotaRule{Field: "tag", Values: []string{"t-rock"}}, itemRock, tags, true},
		{"tag no match", QuotaRule{Field: "tag", Values: []string{"t-missing"}}, itemRock, tags, false},
		{"unknown field no match", QuotaRule{Field: "unknown", Values: []string{"x"}}, itemRock, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesQuota(tt.rule, tt.item, tt.tags)
			if got != tt.want {
				t.Errorf("matchesQuota(%q) = %v, want %v", tt.rule.Field, got, tt.want)
			}
		})
	}
}

// ---- baseScore ----

func TestBaseScore(t *testing.T) {
	item := models.MediaItem{Genre: "Rock", Mood: "Energetic"}

	// No weights: score = 1.0
	score := baseScore(item, nil)
	if score != 1.0 {
		t.Errorf("baseScore with no weights = %v, want 1.0", score)
	}

	// Matching weight adds its value
	weights := []WeightRule{
		{Field: "genre", Value: "Rock", Weight: 2.5},
	}
	score = baseScore(item, weights)
	if score != 3.5 {
		t.Errorf("baseScore with matching genre weight = %v, want 3.5", score)
	}

	// Non-matching weight should not add
	weights2 := []WeightRule{
		{Field: "genre", Value: "Pop", Weight: 5.0},
	}
	score2 := baseScore(item, weights2)
	if score2 != 1.0 {
		t.Errorf("baseScore with non-matching weight = %v, want 1.0", score2)
	}

	// Multiple matching weights accumulate
	weights3 := []WeightRule{
		{Field: "genre", Value: "Rock", Weight: 1.0},
		{Field: "mood", Value: "Energetic", Weight: 1.0},
	}
	score3 := baseScore(item, weights3)
	if score3 != 3.0 {
		t.Errorf("baseScore with two matching weights = %v, want 3.0", score3)
	}
}

// ---- definitionIncludesPublicArchive ----

func TestDefinitionIncludesPublicArchive(t *testing.T) {
	tests := []struct {
		name string
		def  Definition
		want bool
	}{
		{
			"no rules",
			Definition{},
			false,
		},
		{
			"include_public_archive true",
			Definition{Include: []FilterRule{{Field: "include_public_archive", Value: true}}},
			true,
		},
		{
			"include_public_archive false",
			Definition{Include: []FilterRule{{Field: "include_public_archive", Value: false}}},
			false,
		},
		{
			"includearchive variant",
			Definition{Include: []FilterRule{{Field: "includearchive", Value: true}}},
			true,
		},
		{
			"source_include_archive variant",
			Definition{Include: []FilterRule{{Field: "source_include_archive", Value: true}}},
			true,
		},
		{
			"other field only",
			Definition{Include: []FilterRule{{Field: "genre", Value: "Rock"}}},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := definitionIncludesPublicArchive(tt.def)
			if got != tt.want {
				t.Errorf("definitionIncludesPublicArchive() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---- lookupRecent ----

func TestLookupRecent(t *testing.T) {
	now := time.Now()
	cache := map[string]map[string]time.Time{
		"artist": {
			"The Beatles": now.Add(-10 * time.Minute),
		},
	}

	// Found entry
	ts := lookupRecent(cache, "artist", "The Beatles")
	if ts.IsZero() {
		t.Error("lookupRecent should find The Beatles in artist cache")
	}

	// Missing value
	ts2 := lookupRecent(cache, "artist", "Rolling Stones")
	if !ts2.IsZero() {
		t.Error("lookupRecent should return zero time for missing artist")
	}

	// Missing key
	ts3 := lookupRecent(cache, "title", "some title")
	if !ts3.IsZero() {
		t.Error("lookupRecent should return zero time for missing key")
	}

	// Empty value
	ts4 := lookupRecent(cache, "artist", "")
	if !ts4.IsZero() {
		t.Error("lookupRecent should return zero time for empty value")
	}

	// Nil cache key
	ts5 := lookupRecent(nil, "artist", "someone")
	if !ts5.IsZero() {
		t.Error("lookupRecent should return zero time for nil cache")
	}
}

// ---- violatesSeparation extended ----

func TestViolatesSeparation_AllFields(t *testing.T) {
	now := time.Now()

	recent := map[string]map[string]time.Time{
		"artist": {"Artist A": now.Add(-10 * time.Minute)},
		"title":  {"Song X": now.Add(-10 * time.Minute)},
		"album":  {"Album Y": now.Add(-10 * time.Minute)},
		"label":  {"Label Z": now.Add(-10 * time.Minute)},
	}
	windows := map[string]time.Duration{
		"artist": 1 * time.Hour,
		"title":  30 * time.Minute,
		"album":  2 * time.Hour,
		"label":  45 * time.Minute,
	}

	// Violates via artist
	if !violatesSeparation(models.MediaItem{Artist: "Artist A"}, recent, windows) {
		t.Error("should violate artist separation")
	}
	// Violates via title
	if !violatesSeparation(models.MediaItem{Title: "Song X"}, recent, windows) {
		t.Error("should violate title separation")
	}
	// Violates via album
	if !violatesSeparation(models.MediaItem{Album: "Album Y"}, recent, windows) {
		t.Error("should violate album separation")
	}
	// Violates via label
	if !violatesSeparation(models.MediaItem{Label: "Label Z"}, recent, windows) {
		t.Error("should violate label separation")
	}
	// No violation for unknown values
	if violatesSeparation(models.MediaItem{Artist: "Unknown Artist", Title: "Unknown", Album: "None", Label: "None"}, recent, windows) {
		t.Error("should not violate separation for unrecognized values")
	}

	// Outside window: artist played >1hr ago → no violation
	old := map[string]map[string]time.Time{
		"artist": {"Artist A": now.Add(-2 * time.Hour)},
	}
	if violatesSeparation(models.MediaItem{Artist: "Artist A"}, old, windows) {
		t.Error("should not violate separation when outside window")
	}
}

// ---- fetchBumperCandidates ----

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.MediaItem{}, &models.Playlist{}, &models.PlaylistItem{}, &models.SmartBlock{}, &models.PlayHistory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestFetchBumperCandidates_Playlist(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-bump"

	media := models.MediaItem{
		ID: "bump-1", StationID: stationID, Title: "Bumper 1",
		Duration: 30 * time.Second, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	pl := models.Playlist{ID: "pl-bump", StationID: stationID, Name: "Bumpers"}
	if err := db.Create(&pl).Error; err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	pi := models.PlaylistItem{ID: "pi-bump-1", PlaylistID: pl.ID, MediaID: media.ID, Position: 0}
	if err := db.Create(&pi).Error; err != nil {
		t.Fatalf("create playlist item: %v", err)
	}

	eng := New(db, zerolog.Nop())
	cfg := BumperConfig{
		SourceType: "playlist",
		PlaylistID: pl.ID,
	}
	items, err := eng.fetchBumperCandidates(context.Background(), cfg, stationID)
	if err != nil {
		t.Fatalf("fetchBumperCandidates: %v", err)
	}
	if len(items) != 1 || items[0].ID != media.ID {
		t.Errorf("expected 1 bumper, got %d", len(items))
	}
}

func TestFetchBumperCandidates_Genre(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-bump2"

	media := models.MediaItem{
		ID: "bump-genre-1", StationID: stationID, Title: "Jingle 1", Genre: "Jingles",
		Duration: 15 * time.Second, AnalysisState: models.AnalysisComplete,
	}
	other := models.MediaItem{
		ID: "bump-genre-2", StationID: stationID, Title: "Regular Track", Genre: "Rock",
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&[]models.MediaItem{media, other}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	eng := New(db, zerolog.Nop())
	cfg := BumperConfig{SourceType: "genre", Genre: "Jingles"}
	items, err := eng.fetchBumperCandidates(context.Background(), cfg, stationID)
	if err != nil {
		t.Fatalf("fetchBumperCandidates: %v", err)
	}
	if len(items) != 1 || items[0].ID != media.ID {
		t.Errorf("expected 1 bumper with genre Jingles, got %d", len(items))
	}
}

func TestFetchBumperCandidates_Title(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-bump3"

	media := models.MediaItem{
		ID: "bump-title-1", StationID: stationID, Title: "Station Bumper ID",
		Duration: 10 * time.Second, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	eng := New(db, zerolog.Nop())
	// default source type = title
	cfg := BumperConfig{SourceType: "", Query: "bumper"}
	items, err := eng.fetchBumperCandidates(context.Background(), cfg, stationID)
	if err != nil {
		t.Fatalf("fetchBumperCandidates: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 bumper by title, got %d", len(items))
	}
}

func TestFetchBumperCandidates_EmptyQuery(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-bump4"

	eng := New(db, zerolog.Nop())

	// Empty playlist ID returns nil,nil
	cfg := BumperConfig{SourceType: "playlist", PlaylistID: ""}
	items, err := eng.fetchBumperCandidates(context.Background(), cfg, stationID)
	if err != nil || items != nil {
		t.Errorf("expected nil,nil for empty playlist ID; got items=%v err=%v", items, err)
	}

	// Empty genre returns nil,nil
	cfg2 := BumperConfig{SourceType: "genre", Genre: ""}
	items2, err2 := eng.fetchBumperCandidates(context.Background(), cfg2, stationID)
	if err2 != nil || items2 != nil {
		t.Errorf("expected nil,nil for empty genre; got items=%v err=%v", items2, err2)
	}

	// Empty artist query returns nil,nil
	cfg3 := BumperConfig{SourceType: "artist", Query: ""}
	items3, err3 := eng.fetchBumperCandidates(context.Background(), cfg3, stationID)
	if err3 != nil || items3 != nil {
		t.Errorf("expected nil,nil for empty artist query; got items=%v err=%v", items3, err3)
	}

	// Empty label query returns nil,nil
	cfg4 := BumperConfig{SourceType: "label", Query: ""}
	items4, err4 := eng.fetchBumperCandidates(context.Background(), cfg4, stationID)
	if err4 != nil || items4 != nil {
		t.Errorf("expected nil,nil for empty label query; got items=%v err=%v", items4, err4)
	}

	// Empty title query returns nil,nil
	cfg5 := BumperConfig{SourceType: "title", Query: ""}
	items5, err5 := eng.fetchBumperCandidates(context.Background(), cfg5, stationID)
	if err5 != nil || items5 != nil {
		t.Errorf("expected nil,nil for empty title query; got items=%v err=%v", items5, err5)
	}
}

func TestFetchBumperCandidates_Artist(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-bump5"

	media := models.MediaItem{
		ID: "bump-artist-1", StationID: stationID, Title: "Artist Track", Artist: "DJ Jingle",
		Duration: 20 * time.Second, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	eng := New(db, zerolog.Nop())
	cfg := BumperConfig{SourceType: "artist", Query: "DJ Jingle"}
	items, err := eng.fetchBumperCandidates(context.Background(), cfg, stationID)
	if err != nil {
		t.Fatalf("fetchBumperCandidates artist: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 bumper by artist, got %d", len(items))
	}
}

func TestFetchBumperCandidates_Label(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-bump6"

	media := models.MediaItem{
		ID: "bump-label-1", StationID: stationID, Title: "Label Track", Label: "MyLabel",
		Duration: 20 * time.Second, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	eng := New(db, zerolog.Nop())
	cfg := BumperConfig{SourceType: "label", Query: "mylabel"}
	items, err := eng.fetchBumperCandidates(context.Background(), cfg, stationID)
	if err != nil {
		t.Fatalf("fetchBumperCandidates label: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 bumper by label, got %d", len(items))
	}
}

// ---- applyFilterRule ----

func TestApplyFilterRule_Genre(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-fr"

	rockItem := models.MediaItem{
		ID: "fr-rock-1", StationID: stationID, Title: "Rock Song", Genre: "Rock",
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	popItem := models.MediaItem{
		ID: "fr-pop-1", StationID: stationID, Title: "Pop Song", Genre: "Pop",
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&[]models.MediaItem{rockItem, popItem}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	// Positive: include only Rock
	rule := FilterRule{Field: "genre", Value: "Rock"}
	q := applyFilterRule(db.Model(&models.MediaItem{}).Where("station_id = ?", stationID), rule, true)
	var items []models.MediaItem
	if err := q.Find(&items).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(items) != 1 || items[0].Genre != "Rock" {
		t.Errorf("expected 1 Rock item, got %d", len(items))
	}

	// Negative: exclude Rock
	q2 := applyFilterRule(db.Model(&models.MediaItem{}).Where("station_id = ?", stationID), rule, false)
	var items2 []models.MediaItem
	if err := q2.Find(&items2).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(items2) != 1 || items2[0].Genre != "Pop" {
		t.Errorf("expected 1 Pop item after excluding Rock, got %d", len(items2))
	}
}

func TestApplyFilterRule_BPM(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-bpm"

	fast := models.MediaItem{
		ID: "bpm-fast", StationID: stationID, Title: "Fast", BPM: 140,
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	slow := models.MediaItem{
		ID: "bpm-slow", StationID: stationID, Title: "Slow", BPM: 80,
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&[]models.MediaItem{fast, slow}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	rule := FilterRule{Field: "bpm", Value: []interface{}{120.0, 160.0}}
	q := applyFilterRule(db.Model(&models.MediaItem{}).Where("station_id = ?", stationID), rule, true)
	var items []models.MediaItem
	if err := q.Find(&items).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(items) != 1 || items[0].ID != fast.ID {
		t.Errorf("expected 1 fast item with BPM filter, got %d", len(items))
	}
}

func TestApplyFilterRule_Explicit(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-explicit"

	clean := models.MediaItem{
		ID: "explicit-clean", StationID: stationID, Title: "Clean", Explicit: false,
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	explicit := models.MediaItem{
		ID: "explicit-dirty", StationID: stationID, Title: "Explicit", Explicit: true,
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&[]models.MediaItem{clean, explicit}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	rule := FilterRule{Field: "explicit", Value: false}
	q := applyFilterRule(db.Model(&models.MediaItem{}).Where("station_id = ?", stationID), rule, true)
	var items []models.MediaItem
	if err := q.Find(&items).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(items) != 1 || items[0].ID != clean.ID {
		t.Errorf("expected 1 clean item, got %d", len(items))
	}
}

func TestApplyFilterRule_IncludePublicArchiveSkip(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-archive"

	media := models.MediaItem{
		ID: "archive-1", StationID: stationID, Title: "Track",
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	// include_public_archive rule should be a no-op (handled elsewhere)
	rule := FilterRule{Field: "include_public_archive", Value: true}
	q := applyFilterRule(db.Model(&models.MediaItem{}).Where("station_id = ?", stationID), rule, true)
	var items []models.MediaItem
	if err := q.Find(&items).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	// Should return the track since the rule is skipped
	if len(items) != 1 {
		t.Errorf("expected 1 item (archive rule skipped), got %d", len(items))
	}
}

func TestApplyFilterRule_SourcePlaylists(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-plfilter"

	inItem := models.MediaItem{
		ID: "pl-in-1", StationID: stationID, Title: "In Playlist",
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	outItem := models.MediaItem{
		ID: "pl-out-1", StationID: stationID, Title: "Not In Playlist",
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&[]models.MediaItem{inItem, outItem}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	pl := models.Playlist{ID: "pl-filter-test", StationID: stationID, Name: "Filter Test"}
	if err := db.Create(&pl).Error; err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	pi := models.PlaylistItem{ID: "pi-filter-1", PlaylistID: pl.ID, MediaID: inItem.ID, Position: 0}
	if err := db.Create(&pi).Error; err != nil {
		t.Fatalf("create playlist item: %v", err)
	}

	rule := FilterRule{Field: "source_playlists", Value: []string{pl.ID}}
	q := applyFilterRule(db.Model(&models.MediaItem{}).Where("station_id = ?", stationID), rule, true)
	var items []models.MediaItem
	if err := q.Find(&items).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(items) != 1 || items[0].ID != inItem.ID {
		t.Errorf("expected 1 item in playlist, got %d", len(items))
	}
}

func TestApplyFilterRule_TextSearch(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-textsearch"

	matching := models.MediaItem{
		ID: "ts-match-1", StationID: stationID, Title: "Hello World Song",
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	notMatching := models.MediaItem{
		ID: "ts-nomatch-1", StationID: stationID, Title: "Something Else",
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&[]models.MediaItem{matching, notMatching}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	rule := FilterRule{Field: "text_search", Value: "hello"}
	q := applyFilterRule(db.Model(&models.MediaItem{}).Where("station_id = ?", stationID), rule, true)
	var items []models.MediaItem
	if err := q.Find(&items).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(items) != 1 || items[0].ID != matching.ID {
		t.Errorf("expected 1 matching item for text_search, got %d", len(items))
	}
}

func TestApplyFilterRule_Year(t *testing.T) {
	db := newTestDB(t)
	stationID := "station-year"

	old := models.MediaItem{
		ID: "year-old", StationID: stationID, Title: "Old Song", Year: "1970",
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	newer := models.MediaItem{
		ID: "year-new", StationID: stationID, Title: "New Song", Year: "2020",
		Duration: 3 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&[]models.MediaItem{old, newer}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	rule := FilterRule{Field: "year", Value: []interface{}{2000.0, 2025.0}}
	q := applyFilterRule(db.Model(&models.MediaItem{}).Where("station_id = ?", stationID), rule, true)
	var items []models.MediaItem
	if err := q.Find(&items).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(items) != 1 || items[0].ID != newer.ID {
		t.Errorf("expected 1 new song for year filter, got %d", len(items))
	}
}

// ---- relaxDefinition ----

func TestRelaxDefinition(t *testing.T) {
	def := Definition{
		Separation: SeparationRules{ArtistSec: 3600},
		Quotas:     []QuotaRule{{Field: "genre", Min: 2, Max: 5}},
		Exclude:    []FilterRule{{Field: "genre", Value: "Pop"}},
	}

	// Level 0: no change
	l0 := relaxDefinition(def, 0)
	if l0.Separation.ArtistSec == 0 {
		t.Error("level 0 should not change separation")
	}
	if len(l0.Quotas) == 0 {
		t.Error("level 0 should not drop quotas")
	}
	if len(l0.Exclude) == 0 {
		t.Error("level 0 should not drop excludes")
	}

	// Level 1: drop separation only
	l1 := relaxDefinition(def, 1)
	if l1.Separation.ArtistSec != 0 {
		t.Error("level 1 should zero out separation")
	}
	if len(l1.Quotas) == 0 {
		t.Error("level 1 should keep quotas")
	}
	if len(l1.Exclude) == 0 {
		t.Error("level 1 should keep excludes")
	}

	// Level 2: drop separation + quotas
	l2 := relaxDefinition(def, 2)
	if l2.Separation.ArtistSec != 0 {
		t.Error("level 2 should zero out separation")
	}
	if l2.Quotas != nil {
		t.Error("level 2 should nil quotas")
	}
	if len(l2.Exclude) == 0 {
		t.Error("level 2 should keep excludes")
	}

	// Level 3: drop separation + quotas + excludes
	l3 := relaxDefinition(def, 3)
	if l3.Separation.ArtistSec != 0 {
		t.Error("level 3 should zero out separation")
	}
	if l3.Quotas != nil {
		t.Error("level 3 should nil quotas")
	}
	if l3.Exclude != nil {
		t.Error("level 3 should nil excludes")
	}
}

// ---- Generate with bumpers ----

func TestGenerate_WithBumpers(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	stationID := "station-bumpers"

	// Main track
	main := models.MediaItem{
		ID: "main-1", StationID: stationID, Title: "Main Track",
		Duration: 4 * time.Minute, AnalysisState: models.AnalysisComplete,
	}
	// Bumper track (short)
	bumper := models.MediaItem{
		ID: "bump-main-1", StationID: stationID, Title: "My Bumper Track",
		Duration: 30 * time.Second, AnalysisState: models.AnalysisComplete,
	}
	if err := db.Create(&[]models.MediaItem{main, bumper}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	sb := models.SmartBlock{
		ID:        "sb-bumpers",
		StationID: stationID,
		Name:      "Bumper Test",
		Rules: map[string]any{
			"targetMinutes": 5,
			"bumpers": map[string]any{
				"enabled":    true,
				"sourceType": "",
				"query":      "bumper",
				"maxPerGap":  float64(3),
			},
		},
	}
	if err := db.Create(&sb).Error; err != nil {
		t.Fatalf("create smartblock: %v", err)
	}

	eng := New(db, zerolog.Nop())
	res, err := eng.Generate(context.Background(), GenerateRequest{
		SmartBlockID: sb.ID,
		Seed:         42,
		Duration:     int64(5 * time.Minute / time.Millisecond),
		StationID:    stationID,
		MountID:      "mount-1",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(res.Items) == 0 {
		t.Fatal("expected items")
	}
}

// ---- Generate with LoopToFill ----

func TestGenerate_LoopToFill(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	stationID := "station-loop"

	// Two tracks - with loop-to-fill the engine will loop through them to fill the target
	tracks := []models.MediaItem{
		{
			ID: "loop-1", StationID: stationID, Title: "Loop Track A",
			Duration: 2 * time.Minute, AnalysisState: models.AnalysisComplete,
		},
		{
			ID: "loop-2", StationID: stationID, Title: "Loop Track B",
			Duration: 2 * time.Minute, AnalysisState: models.AnalysisComplete,
		},
	}
	if err := db.Create(&tracks).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	sb := models.SmartBlock{
		ID: "sb-loop", StationID: stationID, Name: "Loop Test",
		Rules: map[string]any{
			"targetMinutes": 6,
			"loopToFill":    true,
		},
	}
	if err := db.Create(&sb).Error; err != nil {
		t.Fatalf("create smartblock: %v", err)
	}

	eng := New(db, zerolog.Nop())
	res, err := eng.Generate(context.Background(), GenerateRequest{
		SmartBlockID: sb.ID,
		Seed:         1,
		Duration:     int64(6 * time.Minute / time.Millisecond),
		StationID:    stationID,
		MountID:      "mount-1",
		LoopToFill:   true,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	// Should have repeated the track multiple times
	if len(res.Items) < 2 {
		t.Errorf("expected at least 2 items with loop-to-fill, got %d", len(res.Items))
	}
}
