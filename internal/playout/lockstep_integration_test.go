//go:build integration

/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/broadcast"
	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/smartblock"
	ws "github.com/friendsincode/grimnir_radio/internal/webstream"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Acceptance test for #238: two Director instances driven against a shared
// schedule must emit the same sequence of pipeline-launch commands. The C1-C9
// patches (commits b5f5e9d, e61b401, c781426) closed the audit's determinism
// gaps; this test fails if any regression reopens them.
//
// We compare what the director sends to its ManagerInterface — in this codebase
// that's the equivalent of the mediaengine gRPC stream. The audit (Section 3)
// calls these "play/crossfade/stop commands"; here they're EnsurePipeline*
// and StopPipeline. Same surface, different transport.

// recordedCall is one manager invocation, stripped of anything that varies
// between instances by design (function pointers, fd-backed os.Files,
// io.WriteCloser stdins). Only what should be byte-identical lives here.
type recordedCall struct {
	Method  string // "EnsurePipelineWithDualOutput", "StopPipeline", etc.
	MountID string
	Launch  string // GStreamer pipeline spec; the "command payload"
}

func (c recordedCall) String() string {
	return fmt.Sprintf("%s mount=%s launch=%q", c.Method, c.MountID, c.Launch)
}

// recordingManager wraps mockManager so each call gets appended to a slice in
// arrival order. Two Directors backed by their own recordingManager produce
// two slices the test diffs at the end.
type recordingManager struct {
	mu        sync.Mutex
	pipelines map[string]*mockPipeline
	calls     []recordedCall
}

func newRecordingManager() *recordingManager {
	return &recordingManager{pipelines: make(map[string]*mockPipeline)}
}

func (r *recordingManager) record(c recordedCall) {
	r.mu.Lock()
	r.calls = append(r.calls, c)
	r.mu.Unlock()
}

func (r *recordingManager) EnsurePipeline(_ context.Context, mountID, launch string) error {
	r.record(recordedCall{Method: "EnsurePipeline", MountID: mountID, Launch: launch})
	r.mu.Lock()
	if _, ok := r.pipelines[mountID]; !ok {
		r.pipelines[mountID] = newMockPipeline()
	}
	r.mu.Unlock()
	return nil
}

func (r *recordingManager) EnsurePipelineWithOutput(_ context.Context, mountID, launch string, _ func(io.Reader)) error {
	r.record(recordedCall{Method: "EnsurePipelineWithOutput", MountID: mountID, Launch: launch})
	r.mu.Lock()
	if _, ok := r.pipelines[mountID]; !ok {
		r.pipelines[mountID] = newMockPipeline()
	}
	r.mu.Unlock()
	return nil
}

func (r *recordingManager) EnsurePipelineWithDualOutput(_ context.Context, mountID, launch string, _ *os.File, _, _ func(io.Reader)) error {
	r.record(recordedCall{Method: "EnsurePipelineWithDualOutput", MountID: mountID, Launch: launch})
	r.mu.Lock()
	if _, ok := r.pipelines[mountID]; !ok {
		r.pipelines[mountID] = newMockPipeline()
	}
	r.mu.Unlock()
	return nil
}

func (r *recordingManager) EnsurePipelineWithDualOutputAndInput(_ context.Context, mountID, launch string, _, _ func(io.Reader)) (io.WriteCloser, error) {
	r.record(recordedCall{Method: "EnsurePipelineWithDualOutputAndInput", MountID: mountID, Launch: launch})
	r.mu.Lock()
	if _, ok := r.pipelines[mountID]; !ok {
		r.pipelines[mountID] = newMockPipeline()
	}
	r.mu.Unlock()
	return lockstepNopWriteCloser{}, nil
}

func (r *recordingManager) GetPipeline(mountID string) PipelineInterface {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.pipelines[mountID]
	if !ok {
		return nil
	}
	return p
}

func (r *recordingManager) StopPipeline(mountID string) error {
	r.record(recordedCall{Method: "StopPipeline", MountID: mountID})
	r.mu.Lock()
	delete(r.pipelines, mountID)
	r.mu.Unlock()
	return nil
}

func (r *recordingManager) Shutdown() error { return nil }

// snapshot returns a copy of the recorded calls for diffing.
func (r *recordingManager) snapshot() []recordedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedCall, len(r.calls))
	copy(out, r.calls)
	return out
}

type lockstepNopWriteCloser struct{}

func (lockstepNopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (lockstepNopWriteCloser) Close() error                { return nil }

// newDirectorOnDB builds a Director that shares the supplied *gorm.DB and its
// own recordingManager. Mirrors newMockDirector but skips the per-Director DB
// & smartblock engine so two instances really do read & write the same rows.
func newDirectorOnDB(t *testing.T, db *gorm.DB, sbEng *smartblock.Engine) (*Director, *recordingManager) {
	t.Helper()

	bus := events.NewBus()
	broadcastSrv := broadcast.NewServer(zerolog.Nop(), bus)
	cfg := &config.Config{GStreamerBin: "gst-launch-1.0"}

	mgr := newRecordingManager()
	d := &Director{
		db:            db,
		cfg:           cfg,
		manager:       mgr,
		bus:           bus,
		broadcast:     broadcastSrv,
		smartblockEng: sbEng,
		active:        make(map[string]playoutState),
		played:        make(map[string]time.Time),
		sbGeneration:  make(map[string]int),
		policyCache:   make(map[string]cachedScheduleBoundaryPolicy),
		webrtcCache:   make(map[string]cachedWebRTCPort),
		xfadeSessions: make(map[string]*pcmCrossfadeSession),
		xfadeCfgCache: make(map[string]cachedCrossfadeConfig),
		tzCache:       make(map[string]cachedStationTimezone),
		scheduleCache: cachedScheduleSnapshot{dirty: true},
		webstreamSvc:  ws.NewService(db, bus, zerolog.Nop()),
		icyPollers:    make(map[string]ws.MetadataPoller),
		logger:        zerolog.Nop(),
	}
	return d, mgr
}

// setupSharedTestDB returns an in-memory sqlite with the schema both
// directors will read & write. cache=shared lets the two *Director values
// (each holding its own *gorm.DB pointer would defeat the test, so they share
// the same one) see each other's writes.
func setupSharedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.MediaItem{},
		&models.Mount{},
		&models.Station{},
		&models.Playlist{},
		&models.PlaylistItem{},
		&models.SmartBlock{},
		&models.ScheduleEntry{},
		&models.MountPlayoutState{},
		&models.SmartBlockGeneration{},
		&models.PlayoutQueueItem{},
		&models.PlayHistory{},
		&models.Webstream{},
		&models.Clock{},
		&models.ClockHour{},
		&models.ClockSlot{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	// Mirror internal/db/migrate.go::applyPlayHistoryUniqueIndex so
	// recordPlayHistory's ON CONFLICT clause has a unique index to target
	// (issue #239).
	if err := db.Exec(
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_play_history_entry_position_started
		 ON play_histories (entry_id, position, started_at)
		 WHERE entry_id <> ''`,
	).Error; err != nil {
		t.Fatalf("create play_history unique index: %v", err)
	}
	return db
}

// scenarioIDs are the fixed identifiers used by insertRepresentativeSchedule
// so two separate DBs end up with byte-identical rows. uuid.NewString() would
// produce different IDs per call and the test'd diff on UUIDs that should
// have been seeded identically across the two instances.
type scenarioIDs struct {
	stationID    string
	mountID      string
	playlistID   string
	smartBlockID string
	mediaIDs     []string
	entryIDs     []string
}

func newScenarioIDs() scenarioIDs {
	mediaIDs := make([]string, 8)
	for i := range mediaIDs {
		mediaIDs[i] = fmt.Sprintf("00000000-0000-0000-0000-%012d", i)
	}
	return scenarioIDs{
		stationID:    "10000000-0000-0000-0000-000000000001",
		mountID:      "10000000-0000-0000-0000-000000000002",
		playlistID:   "10000000-0000-0000-0000-000000000003",
		smartBlockID: "10000000-0000-0000-0000-000000000004",
		mediaIDs:     mediaIDs,
		entryIDs: []string{
			"20000000-0000-0000-0000-000000000001",
			"20000000-0000-0000-0000-000000000002",
			"20000000-0000-0000-0000-000000000003",
		},
	}
}

// insertRepresentativeSchedule seeds a deterministic schedule. The startsAt
// window is chosen so each entry is in the "happy path" — in the lookahead
// window but not yet past, so calculateTimeAwarePosition stays at 0
// (avoiding the time.Now-driven branch that's an I-class divergence, not a
// C-class one).
//
// Caller supplies `ids` & `epoch` so two separate DBs receive byte-identical
// rows; that's how the test isolates the determinism property (C1-C9) from
// the double-write noise that arises when two directors share one DB and
// both write play_history.
func insertRepresentativeSchedule(t *testing.T, db *gorm.DB, ids scenarioIDs, epoch time.Time) {
	t.Helper()

	stationID := ids.stationID
	mountID := ids.mountID

	// Station: hard schedule boundary keeps things simple (no soft-overrun
	// time math in the tick).
	if err := db.Create(&models.Station{
		ID:                   stationID,
		Name:                 "lockstep-test",
		ScheduleBoundaryMode: "hard",
	}).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}

	// Mount: fixed encoder config so buildDualBroadcastPipeline produces
	// a deterministic launch string.
	if err := db.Create(&models.Mount{
		ID:         mountID,
		StationID:  stationID,
		Name:       "main",
		Format:     "mp3",
		Bitrate:    128,
		SampleRate: 44100,
		Channels:   2,
	}).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}

	// Media catalog: 8 analyzed tracks, stable IDs sorted by ID ASC so the
	// fallback query (deterministicMediaPick uses ORDER BY id ASC) sees the
	// same candidate ordering from both directors.
	for i, mid := range ids.mediaIDs {
		if err := db.Create(&models.MediaItem{
			ID:            mid,
			StationID:     stationID,
			Title:         fmt.Sprintf("track-%d", i),
			Artist:        fmt.Sprintf("artist-%d", i%3),
			Path:          fmt.Sprintf("/tmp/track-%d.mp3", i),
			Duration:      3 * time.Minute,
			AnalysisState: models.AnalysisComplete,
		}).Error; err != nil {
			t.Fatalf("seed media %d: %v", i, err)
		}
	}
	sortedMedia := append([]string(nil), ids.mediaIDs...)
	sort.Strings(sortedMedia)

	// Playlist with first 3 tracks.
	if err := db.Create(&models.Playlist{
		ID:        ids.playlistID,
		StationID: stationID,
		Name:      "test-playlist",
	}).Error; err != nil {
		t.Fatalf("seed playlist: %v", err)
	}
	for i, mid := range sortedMedia[:3] {
		// Deterministic item ID so the two DBs match.
		itemID := fmt.Sprintf("30000000-0000-0000-0000-%012d", i)
		if err := db.Create(&models.PlaylistItem{
			ID:         itemID,
			PlaylistID: ids.playlistID,
			MediaID:    mid,
			Position:   i,
		}).Error; err != nil {
			t.Fatalf("seed playlist item %d: %v", i, err)
		}
	}

	// SmartBlock with no rules — Generate produces a sequence using only the
	// deterministic-seed code path (rules.Separation empty → no recent-play
	// filtering, no quotas). Both instances pass the same Seed (C7) & Now
	// (C4/C5/C6) so the generated sequence is identical.
	if err := db.Create(&models.SmartBlock{
		ID:        ids.smartBlockID,
		StationID: stationID,
		Name:      "test-smartblock",
		Rules:     map[string]any{},
	}).Error; err != nil {
		t.Fatalf("seed smart block: %v", err)
	}

	// Entries: stagger over the lookahead window so each is picked up by a
	// single tick. starts_at must be > now or within the small lookback
	// window; we put them 1-5s in the future so the first tick handles them.
	entries := []models.ScheduleEntry{
		{
			ID:         ids.entryIDs[0],
			StationID:  stationID,
			MountID:    mountID,
			SourceType: "media",
			SourceID:   sortedMedia[0],
			StartsAt:   epoch.Add(1 * time.Second),
			EndsAt:     epoch.Add(5 * time.Minute),
		},
		{
			ID:         ids.entryIDs[1],
			StationID:  stationID,
			MountID:    mountID,
			SourceType: "playlist",
			SourceID:   ids.playlistID,
			StartsAt:   epoch.Add(6 * time.Minute),
			EndsAt:     epoch.Add(15 * time.Minute),
		},
		{
			ID:         ids.entryIDs[2],
			StationID:  stationID,
			MountID:    mountID,
			SourceType: "smart_block",
			SourceID:   ids.smartBlockID,
			StartsAt:   epoch.Add(16 * time.Minute),
			EndsAt:     epoch.Add(25 * time.Minute),
		},
	}
	for i, e := range entries {
		if err := db.Create(&e).Error; err != nil {
			t.Fatalf("seed entry %d (%s): %v", i, e.SourceType, err)
		}
	}
}

// TestLockstep_TwoDirectorsAgainstSameSchedule is the acceptance test for
// #238. Both directors share a DB & a schedule; both should produce the same
// sequence of pipeline-launch commands to their respective managers.
//
// We drive entries by calling handleEntry directly on each entry rather than
// relying on Run()'s wall-clock ticker. The audit explicitly classifies
// per-tick time.Now() as an I-class issue (not a C-class one), so testing
// against a synthetic schedule with handleEntry isolates the C-class
// determinism surface — which is what C1-C9 actually fixed.
func TestLockstep_TwoDirectorsAgainstSameSchedule(t *testing.T) {
	// Two separate DBs, seeded identically. The audit's lockstep claim is
	// "given the same scheduling inputs, two directors produce the same
	// media-engine command stream." Sharing one DB conflates that with the
	// double-write problem on play_history (`uuid.New()` PKs differ per
	// instance, & each director's row leaks into the other's smartblock
	// recent-plays query). In production, only the leader writes play
	// history. Modelling that as "two identical DBs" gives the cleanest
	// signal on the C1-C9 surface without faking a leadership protocol.
	db1 := setupSharedTestDB(t)
	db2 := setupSharedTestDB(t)

	ids := newScenarioIDs()
	epoch := time.Now().UTC()
	insertRepresentativeSchedule(t, db1, ids, epoch)
	insertRepresentativeSchedule(t, db2, ids, epoch)

	d1, m1 := newDirectorOnDB(t, db1, smartblock.New(db1, zerolog.Nop()))
	d2, m2 := newDirectorOnDB(t, db2, smartblock.New(db2, zerolog.Nop()))

	// Load entries from db1 (db2 has the same rows). Both directors run the
	// same entry against their own DB.
	var entries []models.ScheduleEntry
	if err := db1.Order("starts_at ASC").Find(&entries).Error; err != nil {
		t.Fatalf("load entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no entries to drive")
	}

	ctx := context.Background()
	for _, e := range entries {
		// Reset per-mount active state between entries so each handleEntry
		// call starts from the same baseline on both directors. In a real
		// tick the hard-boundary preemption path does this; we skip the
		// preemption code path here since we're not testing it.
		d1.mu.Lock()
		delete(d1.active, e.MountID)
		d1.mu.Unlock()
		d2.mu.Lock()
		delete(d2.active, e.MountID)
		d2.mu.Unlock()

		if err := d1.handleEntry(ctx, e); err != nil {
			t.Fatalf("d1 handleEntry %s (%s): %v", e.ID, e.SourceType, err)
		}
		if err := d2.handleEntry(ctx, e); err != nil {
			t.Fatalf("d2 handleEntry %s (%s): %v", e.ID, e.SourceType, err)
		}
	}

	c1 := m1.snapshot()
	c2 := m2.snapshot()

	if len(c1) == 0 {
		t.Fatal("d1 made no manager calls — test setup is wrong")
	}

	if !reflect.DeepEqual(c1, c2) {
		t.Errorf("LOCKSTEP BROKEN: directors produced different command streams")
		t.Logf("d1: %d calls, d2: %d calls", len(c1), len(c2))
		max := len(c1)
		if len(c2) > max {
			max = len(c2)
		}
		for i := 0; i < max; i++ {
			var a, b string
			if i < len(c1) {
				a = c1[i].String()
			} else {
				a = "(missing)"
			}
			if i < len(c2) {
				b = c2[i].String()
			} else {
				b = "(missing)"
			}
			if a == b {
				continue
			}
			t.Logf("  diff [%d]:", i)
			t.Logf("    d1: %s", a)
			t.Logf("    d2: %s", b)
		}
		return
	}
	t.Logf("Lockstep verified: %d manager calls byte-identical across two directors", len(c1))
}

// TestPlayHistory_UPSERTIdempotent is the acceptance test for #239. Two
// directors share one DB & both call recordPlayHistory for the same logical
// play (same entry_id + position, same truncated-to-second started_at). The
// partial unique index over (entry_id, position, started_at) plus the
// ON CONFLICT DO NOTHING clause must yield exactly one row.
//
// Without #239's UPSERT we'd see two rows: one INSERT from each director,
// each with its own uuid.NewString() primary key — the bug that doubled
// listener-facing play counts under HA lockstep.
func TestPlayHistory_UPSERTIdempotent(t *testing.T) {
	db := setupSharedTestDB(t)

	ids := newScenarioIDs()
	epoch := time.Now().UTC()
	insertRepresentativeSchedule(t, db, ids, epoch)

	d1, _ := newDirectorOnDB(t, db, smartblock.New(db, zerolog.Nop()))
	d2, _ := newDirectorOnDB(t, db, smartblock.New(db, zerolog.Nop()))

	// Same entry, same position, same media. Both directors record the play.
	// recordPlayHistory uses time.Now().UTC().Truncate(time.Second); within a
	// single test goroutine the two calls fall in the same second, so the
	// composite key is identical → conflict path exercised.
	entry := models.ScheduleEntry{
		ID:         ids.entryIDs[0],
		StationID:  ids.stationID,
		MountID:    ids.mountID,
		SourceType: "media",
		SourceID:   ids.mediaIDs[0],
		StartsAt:   epoch,
		EndsAt:     epoch.Add(5 * time.Minute),
	}
	payload := map[string]any{
		"media_id": ids.mediaIDs[0],
		"title":    "track-0",
		"artist":   "artist-0",
		"position": 0,
	}

	d1.recordPlayHistory(entry, payload)
	d2.recordPlayHistory(entry, payload)

	var count int64
	if err := db.Model(&models.PlayHistory{}).
		Where("entry_id = ? AND position = ?", entry.ID, 0).
		Count(&count).Error; err != nil {
		t.Fatalf("count play_history rows: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 row after two lockstep writes, got %d (UPSERT broken — issue #239 regression)", count)
	}

	// Negative control: a different position for the same entry must produce
	// a new row. Otherwise the constraint is too broad & loses playlist plays.
	payload2 := map[string]any{
		"media_id": ids.mediaIDs[1],
		"title":    "track-1",
		"artist":   "artist-1",
		"position": 1,
	}
	d1.recordPlayHistory(entry, payload2)

	var total int64
	if err := db.Model(&models.PlayHistory{}).
		Where("entry_id = ?", entry.ID).
		Count(&total).Error; err != nil {
		t.Fatalf("count play_history rows after second position: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2 rows (pos=0 + pos=1), got %d — constraint over-collapses positions", total)
	}
}

// TestLockstep_SmartBlockGenerationPersistedAcrossInstances is a focused
// regression on C9: when d1 exhausts a smart block & bumps the generation
// counter, a freshly-constructed d2 must read that counter from the DB so
// its first sequence uses the same shuffle seed d1 just rolled. Without
// the DB persistence, d2 would seed from 0 & diverge.
func TestLockstep_SmartBlockGenerationPersistedAcrossInstances(t *testing.T) {
	db := setupSharedTestDB(t)
	stationID := uuid.NewString()
	if err := db.Create(&models.Station{ID: stationID, Name: "s"}).Error; err != nil {
		t.Fatal(err)
	}

	sbEng := smartblock.New(db, zerolog.Nop())
	d1, _ := newDirectorOnDB(t, db, sbEng)

	entryID := uuid.NewString()
	startsAt := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	playKey := entryID + "@" + startsAt.Format(time.RFC3339Nano)
	ctx := context.Background()

	if got := d1.loadSmartBlockGeneration(ctx, entryID, startsAt, playKey); got != 0 {
		t.Fatalf("d1 fresh: expected gen 0, got %d", got)
	}
	d1.bumpSmartBlockGeneration(ctx, entryID, startsAt, playKey)
	d1.bumpSmartBlockGeneration(ctx, entryID, startsAt, playKey)

	// Now spin up d2 (simulating instance B joining mid-occurrence). It must
	// see gen=2 from DB, NOT 0 from its empty in-memory cache.
	d2, _ := newDirectorOnDB(t, db, sbEng)
	if got := d2.loadSmartBlockGeneration(ctx, entryID, startsAt, playKey); got != 2 {
		t.Fatalf("d2 after d1 bumps: expected gen 2 from DB, got %d (C9 regression — sbGeneration not persisted)", got)
	}
}
