/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webdj

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/live"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// newSvc builds a WebDJ service backed by in-memory sqlite. meClient stays nil,
// so every media-engine branch takes the "not connected" path and only the
// DB/in-memory state machine is exercised.
func newSvc(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.WebDJSession{},
		&models.MediaItem{},
		&models.WaveformCache{},
		&models.Mount{},
		&models.LiveSession{},
		&models.Station{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	bus := events.NewBus()
	liveSvc := live.NewService(db, nil, bus, zerolog.Nop())
	return NewService(db, liveSvc, nil, nil, bus, zerolog.Nop()), db
}

func bg() context.Context { return context.Background() }

// startSession creates a session through the service so both the DB row and the
// in-memory Session exist, which is what the deck/mixer setters assume.
func startSession(t *testing.T, s *Service, stationID string) *models.WebDJSession {
	t.Helper()
	sess, err := s.StartSession(bg(), StartSessionRequest{
		StationID: stationID,
		UserID:    "user-1",
		Username:  "dj",
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	return sess
}

func seedMedia(t *testing.T, db *gorm.DB, id, stationID string) *models.MediaItem {
	t.Helper()
	m := &models.MediaItem{
		ID:        id,
		StationID: stationID,
		Path:      id + ".mp3",
		Title:     "Blue Monday",
		Artist:    "New Order",
		Duration:  7*time.Minute + 29*time.Second,
	}
	if err := db.Create(m).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}
	return m
}

// ---------------------------------------------------------------------------
// session lifecycle
// ---------------------------------------------------------------------------

func TestStartSession_CreatesActiveSessionAndLiveToken(t *testing.T) {
	s, db := newSvc(t)

	sess := startSession(t, s, "st1")
	if sess.ID == "" {
		t.Fatal("session ID empty")
	}
	if !sess.Active {
		t.Fatal("new session should be active")
	}
	if sess.CrossfaderCurve != string(models.CrossfaderLinear) {
		t.Fatalf("crossfader curve = %q, want linear", sess.CrossfaderCurve)
	}
	if sess.LiveSessionID == nil || *sess.LiveSessionID == "" {
		t.Fatal("expected a live handover token on the session")
	}

	var count int64
	db.Model(&models.WebDJSession{}).Where("id = ?", sess.ID).Count(&count)
	if count != 1 {
		t.Fatalf("persisted rows = %d, want 1", count)
	}
}

// A second StartSession for the same station+user must reuse the existing row
// rather than stacking sessions, otherwise the console opens a fresh set of
// decks every time the browser reconnects.
func TestStartSession_ReusesExistingSession(t *testing.T) {
	s, db := newSvc(t)

	first := startSession(t, s, "st1")
	second := startSession(t, s, "st1")

	if first.ID != second.ID {
		t.Fatalf("second session ID = %s, want %s", second.ID, first.ID)
	}
	var count int64
	db.Model(&models.WebDJSession{}).Count(&count)
	if count != 1 {
		t.Fatalf("session rows = %d, want 1", count)
	}
}

func TestEndSession_MarksInactiveAndDropsMemory(t *testing.T) {
	s, db := newSvc(t)
	sess := startSession(t, s, "st1")

	if err := s.EndSession(bg(), sess.ID); err != nil {
		t.Fatalf("end session: %v", err)
	}

	var row models.WebDJSession
	if err := db.First(&row, "id = ?", sess.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.Active {
		t.Fatal("session still active after EndSession")
	}

	s.mu.RLock()
	_, stillCached := s.sessions[sess.ID]
	s.mu.RUnlock()
	if stillCached {
		t.Fatal("in-memory session not cleaned up")
	}

	// Operations on the ended session must now be refused.
	if err := s.SetCrossfader(bg(), sess.ID, 0.5); !errors.Is(err, ErrSessionNotActive) {
		t.Fatalf("SetCrossfader after end = %v, want ErrSessionNotActive", err)
	}
}

func TestEndSession_UnknownSession(t *testing.T) {
	s, _ := newSvc(t)
	if err := s.EndSession(bg(), "nope"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestGetSession_FallsBackToDatabase(t *testing.T) {
	s, db := newSvc(t)
	db.Create(&models.WebDJSession{ID: "db-only", StationID: "st1", UserID: "u1", Active: true})

	got, err := s.GetSession(bg(), "db-only")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.StationID != "st1" {
		t.Fatalf("station = %q, want st1", got.StationID)
	}

	if _, err := s.GetSession(bg(), "missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing session err = %v, want ErrSessionNotFound", err)
	}
}

func TestLoadSessionFromDB(t *testing.T) {
	s, db := newSvc(t)
	db.Create(&models.WebDJSession{ID: "s1", StationID: "st1", UserID: "u1", Active: true})

	if err := s.LoadSessionFromDB(bg(), "s1"); err != nil {
		t.Fatalf("load: %v", err)
	}
	s.mu.RLock()
	_, cached := s.sessions["s1"]
	s.mu.RUnlock()
	if !cached {
		t.Fatal("session not loaded into memory")
	}

	// Loading twice is a no-op, not an error.
	if err := s.LoadSessionFromDB(bg(), "s1"); err != nil {
		t.Fatalf("reload: %v", err)
	}

	if err := s.LoadSessionFromDB(bg(), "ghost"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("ghost err = %v, want ErrSessionNotFound", err)
	}

	// gorm's `default:true` ignores an explicit false at Create, so flip it after.
	db.Create(&models.WebDJSession{ID: "s2", StationID: "st1", UserID: "u1"})
	db.Model(&models.WebDJSession{}).Where("id = ?", "s2").Update("active", false)
	if err := s.LoadSessionFromDB(bg(), "s2"); !errors.Is(err, ErrSessionNotActive) {
		t.Fatalf("inactive err = %v, want ErrSessionNotActive", err)
	}
}

func TestGetActiveSessions(t *testing.T) {
	s, db := newSvc(t)
	startSession(t, s, "st1")
	other, err := s.StartSession(bg(), StartSessionRequest{StationID: "st2", UserID: "user-2"})
	if err != nil {
		t.Fatalf("start second: %v", err)
	}
	db.Create(&models.WebDJSession{ID: "closed", StationID: "st1", UserID: "u9"})
	db.Model(&models.WebDJSession{}).Where("id = ?", "closed").Update("active", false)

	all, err := s.GetActiveSessions(bg(), "")
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("active sessions = %d, want 2", len(all))
	}

	scoped, err := s.GetActiveSessions(bg(), "st2")
	if err != nil {
		t.Fatalf("scoped: %v", err)
	}
	if len(scoped) != 1 || scoped[0].ID != other.ID {
		t.Fatalf("scoped = %+v, want only %s", scoped, other.ID)
	}
}

func TestIsLive_DefaultsFalse(t *testing.T) {
	s, _ := newSvc(t)
	sess := startSession(t, s, "st1")

	if s.IsLive(sess.ID) {
		t.Fatal("fresh session reported live")
	}
	if s.IsLive("unknown") {
		t.Fatal("unknown session reported live")
	}
}

// GoLive/GoOffAir both need the media engine; with meClient nil they must fail
// cleanly instead of dereferencing it.
func TestGoLive_WithoutMediaEngine(t *testing.T) {
	s, _ := newSvc(t)
	sess := startSession(t, s, "st1")

	err := s.GoLive(bg(), GoLiveRequest{SessionID: sess.ID, MountID: "m1", InputType: "webrtc"})
	if !errors.Is(err, ErrMediaEngineUnavailable) {
		t.Fatalf("GoLive = %v, want ErrMediaEngineUnavailable", err)
	}
}

func TestGoOffAir_WhenNotLive(t *testing.T) {
	s, _ := newSvc(t)
	sess := startSession(t, s, "st1")

	if err := s.GoOffAir(bg(), sess.ID); !errors.Is(err, ErrNotLive) {
		t.Fatalf("GoOffAir = %v, want ErrNotLive", err)
	}
}

// ---------------------------------------------------------------------------
// deck loading
// ---------------------------------------------------------------------------

func TestLoadTrack_PopulatesDeckAndCuePoints(t *testing.T) {
	s, db := newSvc(t)
	sess := startSession(t, s, "st1")
	m := seedMedia(t, db, "media-1", "st1")
	// Analysis markers become pre-seeded hot cues 1 and 2.
	db.Model(&models.MediaItem{}).Where("id = ?", m.ID).
		Update("cue_points", models.CuePointSet{IntroEnd: 12.5, OutroIn: 400})

	deck, err := s.LoadTrack(bg(), LoadTrackRequest{SessionID: sess.ID, Deck: models.DeckA, MediaID: m.ID})
	if err != nil {
		t.Fatalf("load track: %v", err)
	}

	if deck.MediaID != m.ID || deck.Title != "Blue Monday" {
		t.Fatalf("deck = %+v", deck)
	}
	if deck.State != string(models.DeckStateCued) {
		t.Fatalf("state = %q, want cued", deck.State)
	}
	if deck.DurationMS != m.Duration.Milliseconds() {
		t.Fatalf("duration = %d, want %d", deck.DurationMS, m.Duration.Milliseconds())
	}
	if deck.Volume != 1.0 {
		t.Fatalf("volume = %v, want 1.0", deck.Volume)
	}
	if len(deck.HotCues) != 2 {
		t.Fatalf("hot cues = %d, want 2 (intro + outro)", len(deck.HotCues))
	}
	if deck.HotCues[0].PositionMS != 12500 {
		t.Fatalf("intro cue = %dms, want 12500", deck.HotCues[0].PositionMS)
	}
	if deck.HotCues[1].PositionMS != 400000 {
		t.Fatalf("outro cue = %dms, want 400000", deck.HotCues[1].PositionMS)
	}

	// The deck state must survive a round trip through the DB column.
	var row models.WebDJSession
	if err := db.First(&row, "id = ?", sess.ID).Error; err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if row.DeckAState.MediaID != m.ID {
		t.Fatalf("persisted deck A media = %q, want %q", row.DeckAState.MediaID, m.ID)
	}
	if row.DeckBState.MediaID != "" {
		t.Fatal("deck B should be untouched")
	}
}

// Loading media owned by another station is the cross-tenant leak worth
// guarding: the check happens before any deck state is written.
func TestLoadTrack_RejectsForeignStationMedia(t *testing.T) {
	s, db := newSvc(t)
	sess := startSession(t, s, "st1")
	seedMedia(t, db, "media-other", "st2")

	_, err := s.LoadTrack(bg(), LoadTrackRequest{SessionID: sess.ID, Deck: models.DeckA, MediaID: "media-other"})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}

	var row models.WebDJSession
	db.First(&row, "id = ?", sess.ID)
	if row.DeckAState.MediaID != "" {
		t.Fatal("deck A was written despite the authorization failure")
	}
}

func TestLoadTrack_Errors(t *testing.T) {
	s, db := newSvc(t)
	sess := startSession(t, s, "st1")
	seedMedia(t, db, "media-1", "st1")

	if _, err := s.LoadTrack(bg(), LoadTrackRequest{SessionID: sess.ID, Deck: models.DeckA, MediaID: "ghost"}); !errors.Is(err, ErrMediaNotFound) {
		t.Fatalf("missing media = %v, want ErrMediaNotFound", err)
	}
	if _, err := s.LoadTrack(bg(), LoadTrackRequest{SessionID: "ghost", Deck: models.DeckA, MediaID: "media-1"}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing session = %v, want ErrSessionNotFound", err)
	}
	if _, err := s.LoadTrack(bg(), LoadTrackRequest{SessionID: sess.ID, Deck: models.DeckID("c"), MediaID: "media-1"}); !errors.Is(err, ErrInvalidDeck) {
		t.Fatalf("bad deck = %v, want ErrInvalidDeck", err)
	}
}

func TestEjectTrack_ResetsDeck(t *testing.T) {
	s, db := newSvc(t)
	sess := startSession(t, s, "st1")
	seedMedia(t, db, "media-1", "st1")
	if _, err := s.LoadTrack(bg(), LoadTrackRequest{SessionID: sess.ID, Deck: models.DeckB, MediaID: "media-1"}); err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := s.EjectTrack(bg(), sess.ID, models.DeckB); err != nil {
		t.Fatalf("eject: %v", err)
	}

	deck, err := s.getDeckState(bg(), sess.ID, models.DeckB)
	if err != nil {
		t.Fatalf("deck state: %v", err)
	}
	if deck.MediaID != "" {
		t.Fatalf("deck still holds %q after eject", deck.MediaID)
	}

	if err := s.EjectTrack(bg(), sess.ID, models.DeckID("z")); !errors.Is(err, ErrInvalidDeck) {
		t.Fatalf("bad deck = %v, want ErrInvalidDeck", err)
	}
}

// ---------------------------------------------------------------------------
// transport
// ---------------------------------------------------------------------------

func TestPlayPause_RequireLoadedTrack(t *testing.T) {
	s, _ := newSvc(t)
	sess := startSession(t, s, "st1")

	if err := s.Play(bg(), sess.ID, models.DeckA); !errors.Is(err, ErrNoTrackLoaded) {
		t.Fatalf("play empty deck = %v, want ErrNoTrackLoaded", err)
	}
	if err := s.Pause(bg(), sess.ID, models.DeckA); !errors.Is(err, ErrNoTrackLoaded) {
		t.Fatalf("pause empty deck = %v, want ErrNoTrackLoaded", err)
	}
	if err := s.Seek(bg(), sess.ID, models.DeckA, 1000); !errors.Is(err, ErrNoTrackLoaded) {
		t.Fatalf("seek empty deck = %v, want ErrNoTrackLoaded", err)
	}
}

func TestPlayPause_TransitionsDeckState(t *testing.T) {
	s, db := newSvc(t)
	sess := startSession(t, s, "st1")
	seedMedia(t, db, "media-1", "st1")
	if _, err := s.LoadTrack(bg(), LoadTrackRequest{SessionID: sess.ID, Deck: models.DeckA, MediaID: "media-1"}); err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := s.Play(bg(), sess.ID, models.DeckA); err != nil {
		t.Fatalf("play: %v", err)
	}
	deck, _ := s.getDeckState(bg(), sess.ID, models.DeckA)
	if deck.State != string(models.DeckStatePlaying) {
		t.Fatalf("state = %q, want playing", deck.State)
	}

	if err := s.Pause(bg(), sess.ID, models.DeckA); err != nil {
		t.Fatalf("pause: %v", err)
	}
	deck, _ = s.getDeckState(bg(), sess.ID, models.DeckA)
	if deck.State != string(models.DeckStatePaused) {
		t.Fatalf("state = %q, want paused", deck.State)
	}
}

// Seek clamps to [0, duration]; a position past the end would otherwise ask the
// media engine to cue past EOF.
func TestSeek_ClampsToTrackBounds(t *testing.T) {
	s, db := newSvc(t)
	sess := startSession(t, s, "st1")
	m := seedMedia(t, db, "media-1", "st1")
	if _, err := s.LoadTrack(bg(), LoadTrackRequest{SessionID: sess.ID, Deck: models.DeckA, MediaID: m.ID}); err != nil {
		t.Fatalf("load: %v", err)
	}
	durMS := m.Duration.Milliseconds()

	for _, tc := range []struct{ in, want int64 }{
		{-5000, 0},
		{1234, 1234},
		{durMS + 60000, durMS},
	} {
		if err := s.Seek(bg(), sess.ID, models.DeckA, tc.in); err != nil {
			t.Fatalf("seek %d: %v", tc.in, err)
		}
		deck, _ := s.getDeckState(bg(), sess.ID, models.DeckA)
		if deck.PositionMS != tc.want {
			t.Fatalf("seek(%d) position = %d, want %d", tc.in, deck.PositionMS, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// hot cues
// ---------------------------------------------------------------------------

func TestSetCue_RangeAndUpdate(t *testing.T) {
	s, db := newSvc(t)
	sess := startSession(t, s, "st1")
	seedMedia(t, db, "media-1", "st1")
	if _, err := s.LoadTrack(bg(), LoadTrackRequest{SessionID: sess.ID, Deck: models.DeckA, MediaID: "media-1"}); err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, bad := range []int{0, 9, -1} {
		if err := s.SetCue(bg(), sess.ID, models.DeckA, bad, 100); err == nil {
			t.Fatalf("cue ID %d accepted, want rejection", bad)
		}
	}

	if err := s.SetCue(bg(), sess.ID, models.DeckA, 3, 5000); err != nil {
		t.Fatalf("set cue: %v", err)
	}
	deck, _ := s.getDeckState(bg(), sess.ID, models.DeckA)
	if len(deck.HotCues) != 1 || deck.HotCues[0].ID != 3 || deck.HotCues[0].PositionMS != 5000 {
		t.Fatalf("hot cues = %+v", deck.HotCues)
	}

	// Setting the same ID again moves it rather than appending a duplicate.
	if err := s.SetCue(bg(), sess.ID, models.DeckA, 3, 9000); err != nil {
		t.Fatalf("move cue: %v", err)
	}
	deck, _ = s.getDeckState(bg(), sess.ID, models.DeckA)
	if len(deck.HotCues) != 1 || deck.HotCues[0].PositionMS != 9000 {
		t.Fatalf("after move, hot cues = %+v", deck.HotCues)
	}
}

func TestDeleteCue(t *testing.T) {
	s, db := newSvc(t)
	sess := startSession(t, s, "st1")
	seedMedia(t, db, "media-1", "st1")
	if _, err := s.LoadTrack(bg(), LoadTrackRequest{SessionID: sess.ID, Deck: models.DeckA, MediaID: "media-1"}); err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := s.SetCue(bg(), sess.ID, models.DeckA, 1, 1000); err != nil {
		t.Fatalf("cue 1: %v", err)
	}
	if err := s.SetCue(bg(), sess.ID, models.DeckA, 2, 2000); err != nil {
		t.Fatalf("cue 2: %v", err)
	}

	if err := s.DeleteCue(bg(), sess.ID, models.DeckA, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	deck, _ := s.getDeckState(bg(), sess.ID, models.DeckA)
	for _, c := range deck.HotCues {
		if c.ID == 1 {
			t.Fatalf("cue 1 survived deletion: %+v", deck.HotCues)
		}
	}
	if len(deck.HotCues) != 1 {
		t.Fatalf("remaining cues = %d, want 1", len(deck.HotCues))
	}

	// Deleting a cue that was never set leaves the list alone.
	if err := s.DeleteCue(bg(), sess.ID, models.DeckA, 7); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
	deck, _ = s.getDeckState(bg(), sess.ID, models.DeckA)
	if len(deck.HotCues) != 1 {
		t.Fatalf("cues after no-op delete = %d, want 1", len(deck.HotCues))
	}
}

// ---------------------------------------------------------------------------
// deck and mixer clamping
// ---------------------------------------------------------------------------

func TestSetVolumeAndPitch_Clamp(t *testing.T) {
	s, _ := newSvc(t)
	sess := startSession(t, s, "st1")

	for _, tc := range []struct{ in, want float64 }{{-2, 0}, {0.25, 0.25}, {5, 1}} {
		if err := s.SetVolume(bg(), sess.ID, models.DeckA, tc.in); err != nil {
			t.Fatalf("set volume %v: %v", tc.in, err)
		}
		deck, _ := s.getDeckState(bg(), sess.ID, models.DeckA)
		if deck.Volume != tc.want {
			t.Fatalf("SetVolume(%v) = %v, want %v", tc.in, deck.Volume, tc.want)
		}
	}

	for _, tc := range []struct{ in, want float64 }{{-50, -8}, {3.5, 3.5}, {50, 8}} {
		if err := s.SetPitch(bg(), sess.ID, models.DeckA, tc.in); err != nil {
			t.Fatalf("set pitch %v: %v", tc.in, err)
		}
		deck, _ := s.getDeckState(bg(), sess.ID, models.DeckA)
		if deck.Pitch != tc.want {
			t.Fatalf("SetPitch(%v) = %v, want %v", tc.in, deck.Pitch, tc.want)
		}
	}
}

// EQ is clamped to the ±12 dB the mixer UI exposes.
func TestSetEQ_ClampsAllThreeBands(t *testing.T) {
	s, _ := newSvc(t)
	sess := startSession(t, s, "st1")

	if err := s.SetEQ(bg(), sess.ID, models.DeckB, 30, -30, 6); err != nil {
		t.Fatalf("set eq: %v", err)
	}
	deck, _ := s.getDeckState(bg(), sess.ID, models.DeckB)
	if deck.EQHigh != 12 || deck.EQMid != -12 || deck.EQLow != 6 {
		t.Fatalf("eq = (%v, %v, %v), want (12, -12, 6)", deck.EQHigh, deck.EQMid, deck.EQLow)
	}
}

func TestMixerSetters(t *testing.T) {
	s, db := newSvc(t)
	sess := startSession(t, s, "st1")

	if err := s.SetCrossfader(bg(), sess.ID, 7); err != nil {
		t.Fatalf("crossfader: %v", err)
	}
	if err := s.SetMasterVolume(bg(), sess.ID, -3); err != nil {
		t.Fatalf("master volume: %v", err)
	}
	if err := s.SetHeadphoneVolume(bg(), sess.ID, 0.42); err != nil {
		t.Fatalf("headphone volume: %v", err)
	}
	if err := s.SetCueSplit(bg(), sess.ID, true); err != nil {
		t.Fatalf("cue split: %v", err)
	}
	if err := s.SetCueMixLevel(bg(), sess.ID, 0.8); err != nil {
		t.Fatalf("cue mix: %v", err)
	}
	if err := s.SetHeadphoneCue(bg(), sess.ID, "a", true); err != nil {
		t.Fatalf("headphone cue a: %v", err)
	}
	if err := s.SetHeadphoneCue(bg(), sess.ID, "b", true); err != nil {
		t.Fatalf("headphone cue b: %v", err)
	}
	if err := s.SetHeadphoneCue(bg(), sess.ID, "c", true); !errors.Is(err, ErrInvalidDeck) {
		t.Fatalf("headphone cue c = %v, want ErrInvalidDeck", err)
	}

	var row models.WebDJSession
	if err := db.First(&row, "id = ?", sess.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	mix := row.MixerState
	if mix.Crossfader != 1 {
		t.Fatalf("crossfader = %v, want clamped to 1", mix.Crossfader)
	}
	if mix.MasterVolume != 0 {
		t.Fatalf("master volume = %v, want clamped to 0", mix.MasterVolume)
	}
	if mix.HeadphoneVol != 0.42 {
		t.Fatalf("headphone vol = %v, want 0.42", mix.HeadphoneVol)
	}
	if !mix.CueSplit || !mix.HeadphoneCueA || !mix.HeadphoneCueB {
		t.Fatalf("toggles not persisted: %+v", mix)
	}
	if mix.CueMixLevel != 0.8 {
		t.Fatalf("cue mix level = %v, want 0.8", mix.CueMixLevel)
	}
}

// ---------------------------------------------------------------------------
// subscriptions
// ---------------------------------------------------------------------------

func TestSubscribe_ReceivesBroadcastsAndUnsubscribes(t *testing.T) {
	s, db := newSvc(t)
	sess := startSession(t, s, "st1")
	seedMedia(t, db, "media-1", "st1")

	ch, unsubscribe, err := s.Subscribe(sess.ID)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if _, err := s.LoadTrack(bg(), LoadTrackRequest{SessionID: sess.ID, Deck: models.DeckA, MediaID: "media-1"}); err != nil {
		t.Fatalf("load: %v", err)
	}

	select {
	case update := <-ch:
		if update.Type != "deck_loaded" {
			t.Fatalf("update type = %q, want deck_loaded", update.Type)
		}
		if update.Deck != string(models.DeckA) {
			t.Fatalf("update deck = %q, want a", update.Deck)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no state update delivered to subscriber")
	}

	unsubscribe()
	if err := s.SetVolume(bg(), sess.ID, models.DeckA, 0.5); err != nil {
		t.Fatalf("set volume: %v", err)
	}
	select {
	case update := <-ch:
		t.Fatalf("received %q after unsubscribe", update.Type)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSubscribe_UnknownSession(t *testing.T) {
	s, _ := newSvc(t)
	if _, _, err := s.Subscribe("ghost"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

// A subscriber that stops reading must not block the broadcaster: the buffered
// channel fills at 16 and further sends are dropped.
func TestBroadcastUpdate_DropsOnFullSubscriber(t *testing.T) {
	s, _ := newSvc(t)
	sess := startSession(t, s, "st1")

	ch, _, err := s.Subscribe(sess.ID)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			s.broadcastUpdate(sess.ID, &StateUpdate{Type: "noise", SessionID: sess.ID})
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("broadcastUpdate blocked on a full subscriber channel")
	}
	if len(ch) != 16 {
		t.Fatalf("buffered updates = %d, want the 16-slot buffer to be full", len(ch))
	}

	// Broadcasting to a session with no in-memory entry is a no-op.
	s.broadcastUpdate("ghost", &StateUpdate{Type: "noise"})
}
