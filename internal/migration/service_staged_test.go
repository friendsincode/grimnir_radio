package migration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

type fakeStagedImporter struct {
	db            *gorm.DB
	commitDelay   time.Duration
	commitStarted chan struct{}
}

func (f *fakeStagedImporter) Validate(context.Context, Options) error { return nil }
func (f *fakeStagedImporter) Analyze(context.Context, Options) (*Result, error) {
	return &Result{}, nil
}
func (f *fakeStagedImporter) Import(context.Context, Options, ProgressCallback) (*Result, error) {
	return &Result{}, nil
}

func (f *fakeStagedImporter) AnalyzeForStaging(ctx context.Context, jobID string, options Options) (*models.StagedImport, error) {
	staged := &models.StagedImport{
		ID:         uuid.NewString(),
		JobID:      jobID,
		SourceType: string(SourceTypeAzuraCast),
		Status:     models.StagedImportStatusReady,
		StagedMedia: models.StagedMediaItems{
			{SourceID: "m1", Title: "Track 1", Selected: true},
			{SourceID: "m2", Title: "Track 2", Selected: true},
		},
		StagedPlaylists: models.StagedPlaylistItems{
			{SourceID: "p1", Name: "Playlist 1", Selected: true},
		},
		StagedSmartBlocks: models.StagedSmartBlockItems{
			{SourceID: "sb1", Name: "Smart Block 1", Selected: true},
		},
		StagedShows: models.StagedShowItems{
			{
				SourceID:    "sh1",
				Name:        "Show 1",
				Selected:    true,
				CreateShow:  true,
				CreateClock: false,
			},
		},
		StagedWebstreams: models.StagedWebstreamItems{
			{SourceID: "ws1", Name: "Webstream 1", Selected: true},
		},
	}
	if err := f.db.WithContext(ctx).Create(staged).Error; err != nil {
		return nil, err
	}
	return staged, nil
}

func (f *fakeStagedImporter) CommitStagedImport(ctx context.Context, staged *models.StagedImport, _ string, _ Options, cb ProgressCallback) (*Result, error) {
	if f.commitStarted != nil {
		select {
		case f.commitStarted <- struct{}{}:
		default:
		}
	}
	if cb != nil {
		cb(Progress{
			Phase:       "importing_selected",
			CurrentStep: "Importing selected items",
			Percentage:  30,
		})
	}
	if f.commitDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.commitDelay):
		}
	}

	mediaCount := 0
	for _, m := range staged.StagedMedia {
		if m.Selected {
			mediaCount++
		}
	}
	playlistCount := 0
	for _, p := range staged.StagedPlaylists {
		if p.Selected {
			playlistCount++
		}
	}
	return &Result{
		MediaItemsImported: mediaCount,
		PlaylistsCreated:   playlistCount,
	}, nil
}

func TestStagedFlow_AzuraCast_StateTransitions_AndSelectionsPersist(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&Job{}, &models.StagedImport{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	bus := events.NewBus()
	svc := NewService(db, bus, zerolog.Nop())
	importer := &fakeStagedImporter{
		db:            db,
		commitDelay:   150 * time.Millisecond,
		commitStarted: make(chan struct{}, 1),
	}
	svc.RegisterImporter(SourceTypeAzuraCast, importer)

	ctx := context.Background()
	job, err := svc.CreateStagedJob(ctx, SourceTypeAzuraCast, Options{
		StagedMode:      true,
		AzuraCastAPIURL: "https://example.invalid",
		AzuraCastAPIKey: "token",
	})
	if err != nil {
		t.Fatalf("CreateStagedJob: %v", err)
	}
	if job.Status != JobStatusAnalyzing {
		t.Fatalf("expected analyzing, got %s", job.Status)
	}

	if err := svc.StartStagedJob(ctx, job.ID); err != nil {
		t.Fatalf("StartStagedJob: %v", err)
	}

	stagedJob := waitForJobStatus(t, svc, job.ID, JobStatusStaged, 2*time.Second)
	if stagedJob.StagedImportID == nil || *stagedJob.StagedImportID == "" {
		t.Fatalf("expected staged import id on staged job")
	}
	stagedID := *stagedJob.StagedImportID

	selections := models.ImportSelections{
		MediaIDs:      []string{"m1"},
		PlaylistIDs:   []string{"p1"},
		SmartBlockIDs: []string{},
		ShowIDs:       []string{"sh1"},
		WebstreamIDs:  []string{},
		ShowsAsShows:  []string{"sh1"},
		ShowsAsClocks: []string{},
		CustomRRules:  map[string]string{"sh1": "FREQ=WEEKLY;BYDAY=MO"},
	}
	if err := svc.UpdateSelections(ctx, stagedID, selections); err != nil {
		t.Fatalf("UpdateSelections: %v", err)
	}

	staged, err := svc.GetStagedImport(ctx, stagedID)
	if err != nil {
		t.Fatalf("GetStagedImport: %v", err)
	}
	if staged.Status != models.StagedImportStatusReady {
		t.Fatalf("expected staged ready, got %s", staged.Status)
	}
	if !staged.StagedMedia[0].Selected || staged.StagedMedia[1].Selected {
		t.Fatalf("media selection persistence failed")
	}
	if !staged.StagedShows[0].CreateShow || staged.StagedShows[0].CreateClock {
		t.Fatalf("show mode selection persistence failed")
	}
	if staged.StagedShows[0].CustomRRule != "FREQ=WEEKLY;BYDAY=MO" {
		t.Fatalf("custom rrule not persisted")
	}

	if err := svc.CommitStagedImport(ctx, stagedID); err != nil {
		t.Fatalf("CommitStagedImport: %v", err)
	}

	select {
	case <-importer.commitStarted:
	case <-time.After(1 * time.Second):
		t.Fatalf("commit did not start")
	}

	waitForJobStatus(t, svc, job.ID, JobStatusRunning, 2*time.Second)
	completed := waitForJobStatus(t, svc, job.ID, JobStatusCompleted, 3*time.Second)
	if completed.Result == nil {
		t.Fatalf("expected result after completion")
	}
	if completed.Result.MediaItemsImported != 1 {
		t.Fatalf("expected selected media count 1, got %d", completed.Result.MediaItemsImported)
	}
	if completed.Result.PlaylistsCreated != 1 {
		t.Fatalf("expected selected playlists count 1, got %d", completed.Result.PlaylistsCreated)
	}
}

func waitForJobStatus(t *testing.T, svc *Service, jobID string, status JobStatus, timeout time.Duration) *Job {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := svc.GetJob(context.Background(), jobID)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if job.Status == status {
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}
	job, err := svc.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("GetJob final: %v", err)
	}
	t.Fatalf("timed out waiting for status %s (last: %s)", status, job.Status)
	return nil
}

func TestUpdateSelections_StationFilterScopedIDs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.StagedImport{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	svc := NewService(db, events.NewBus(), zerolog.Nop())
	ctx := context.Background()

	staged := &models.StagedImport{
		ID:     uuid.NewString(),
		JobID:  uuid.NewString(),
		Status: models.StagedImportStatusReady,
		StagedMedia: models.StagedMediaItems{
			{SourceID: "1::m1", Selected: false},
			{SourceID: "2::m2", Selected: false},
			{SourceID: "m3", Selected: false},
		},
		StagedPlaylists: models.StagedPlaylistItems{
			{SourceID: "1::p1", Selected: false},
			{SourceID: "2::p2", Selected: false},
		},
		StagedShows: models.StagedShowItems{
			{SourceID: "1::sh1", Selected: false},
			{SourceID: "2::sh2", Selected: false},
		},
		StagedSmartBlocks: models.StagedSmartBlockItems{
			{SourceID: "2::sb1", Selected: false},
		},
		StagedWebstreams: models.StagedWebstreamItems{
			{SourceID: "2::ws1", Selected: false},
		},
	}
	if err := db.WithContext(ctx).Create(staged).Error; err != nil {
		t.Fatalf("create staged import: %v", err)
	}

	selections := models.ImportSelections{
		StationIDs:    []string{"2"},
		MediaIDs:      []string{"1::m1", "2::m2", "m3"},
		PlaylistIDs:   []string{"1::p1", "2::p2"},
		SmartBlockIDs: []string{"2::sb1"},
		ShowIDs:       []string{"1::sh1", "2::sh2"},
		WebstreamIDs:  []string{"2::ws1"},
		ShowsAsShows:  []string{"2::sh2"},
		ShowsAsClocks: []string{},
		CustomRRules:  map[string]string{"2::sh2": "FREQ=WEEKLY;BYDAY=MO"},
	}
	if err := svc.UpdateSelections(ctx, staged.ID, selections); err != nil {
		t.Fatalf("UpdateSelections: %v", err)
	}

	updated, err := svc.GetStagedImport(ctx, staged.ID)
	if err != nil {
		t.Fatalf("GetStagedImport: %v", err)
	}

	if updated.StagedMedia[0].Selected {
		t.Fatalf("station 1 media should be filtered out")
	}
	if !updated.StagedMedia[1].Selected {
		t.Fatalf("station 2 media should remain selected")
	}
	if !updated.StagedMedia[2].Selected {
		t.Fatalf("unscoped media should remain selectable")
	}
	if updated.StagedPlaylists[0].Selected {
		t.Fatalf("station 1 playlist should be filtered out")
	}
	if !updated.StagedPlaylists[1].Selected {
		t.Fatalf("station 2 playlist should remain selected")
	}
	if updated.StagedShows[0].Selected {
		t.Fatalf("station 1 show should be filtered out")
	}
	if !updated.StagedShows[1].Selected {
		t.Fatalf("station 2 show should remain selected")
	}
	if !updated.StagedShows[1].CreateShow || updated.StagedShows[1].CreateClock {
		t.Fatalf("show mode selection should persist for selected station show")
	}
	if updated.StagedShows[1].CustomRRule != "FREQ=WEEKLY;BYDAY=MO" {
		t.Fatalf("custom rrule should persist for selected station show")
	}
	if !updated.StagedSmartBlocks[0].Selected {
		t.Fatalf("station 2 smart block should remain selected")
	}
	if !updated.StagedWebstreams[0].Selected {
		t.Fatalf("station 2 webstream should remain selected")
	}
}

func TestSourcePassesStationFilter_UnscopedSource(t *testing.T) {
	filter := map[int]struct{}{2: {}}
	if !sourcePassesStationFilter("m3", filter) {
		t.Fatalf("expected unscoped source to pass station filter")
	}
	if sourcePassesStationFilter("1::m1", filter) {
		t.Fatalf("expected scoped source from another station to fail station filter")
	}
	if !sourcePassesStationFilter("2::m2", filter) {
		t.Fatalf("expected scoped source from selected station to pass station filter")
	}
}
