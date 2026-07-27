/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package analyzer

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
	pb "github.com/friendsincode/grimnir_radio/proto/mediaengine/v1"
)

// fakeME is an injectable stand-in for the media-engine gRPC client.
type fakeME struct {
	connected    bool
	connectErr   error
	connectCalls int
	closeErr     error

	analyzeResp *pb.AnalyzeMediaResponse
	analyzeErr  error
	analyzePath string

	artworkResp *pb.ExtractArtworkResponse
	artworkErr  error
}

func (f *fakeME) Connect(context.Context) error {
	f.connectCalls++
	if f.connectErr != nil {
		return f.connectErr
	}
	f.connected = true
	return nil
}

func (f *fakeME) IsConnected() bool { return f.connected }
func (f *fakeME) Close() error      { return f.closeErr }

func (f *fakeME) AnalyzeMedia(_ context.Context, filePath string) (*pb.AnalyzeMediaResponse, error) {
	f.analyzePath = filePath
	return f.analyzeResp, f.analyzeErr
}

// The real client only ever returns a nil response alongside an error, so the
// fake mirrors that: no configured response means an empty, unsuccessful one.
func (f *fakeME) ExtractArtwork(context.Context, string, int32, int32, string, int32) (*pb.ExtractArtworkResponse, error) {
	if f.artworkErr != nil {
		return nil, f.artworkErr
	}
	if f.artworkResp == nil {
		return &pb.ExtractArtworkResponse{}, nil
	}
	return f.artworkResp, nil
}

func newSvcWithME(t *testing.T, me MediaEngine) (*Service, *gorm.DB, string) {
	t.Helper()
	s, db, workDir := newSvc(t)
	s.mediaEngineClient = me
	s.cfg = Config{MediaEngineGRPCAddr: "127.0.0.1:9999"}
	return s, db, workDir
}

func okAnalysis() *pb.AnalyzeMediaResponse {
	return &pb.AnalyzeMediaResponse{
		Success:      true,
		DurationMs:   215000,
		Bitrate:      320,
		SampleRate:   44100,
		LoudnessLufs: -14.5,
		ReplayGain:   2.5,
		IntroEnd:     8.25,
		OutroIn:      190.5,
		Metadata: &pb.MediaMetadata{
			Title:  "Blue Monday",
			Artist: "New Order",
			Album:  "Power, Corruption & Lies",
			Genre:  "Synth-pop",
			Year:   "1983",
		},
	}
}

// ---------------------------------------------------------------------------
// analyzeViaMediaEngine
// ---------------------------------------------------------------------------

func TestAnalyzeViaMediaEngine_MapsResponse(t *testing.T) {
	me := &fakeME{connected: true, analyzeResp: okAnalysis()}
	s, _, _ := newSvcWithME(t, me)

	result, err := s.analyzeViaMediaEngine(bg(), "/media/track.mp3", "m1")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	if me.analyzePath != "/media/track.mp3" {
		t.Fatalf("analyzed %q, want the full path", me.analyzePath)
	}
	if result.Duration != 215*time.Second {
		t.Fatalf("duration = %v, want 215s", result.Duration)
	}
	if result.Bitrate != 320 || result.Samplerate != 44100 {
		t.Fatalf("bitrate/samplerate = %d/%d", result.Bitrate, result.Samplerate)
	}
	if result.Loudness > -14.4 || result.Loudness < -14.6 {
		t.Fatalf("loudness = %v, want ~-14.5", result.Loudness)
	}
	if result.IntroEnd < 8.2 || result.IntroEnd > 8.3 {
		t.Fatalf("intro end = %v, want ~8.25", result.IntroEnd)
	}
	if result.Title != "Blue Monday" || result.Artist != "New Order" || result.Year != "1983" {
		t.Fatalf("metadata = %+v", result)
	}
	if len(result.Artwork) != 0 {
		t.Fatal("no artwork was offered, but some was recorded")
	}
}

// A disconnected client is reconnected once before the analysis call.
func TestAnalyzeViaMediaEngine_ConnectsWhenDisconnected(t *testing.T) {
	me := &fakeME{connected: false, analyzeResp: okAnalysis()}
	s, _, _ := newSvcWithME(t, me)

	if _, err := s.analyzeViaMediaEngine(bg(), "/media/track.mp3", "m1"); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if me.connectCalls != 1 {
		t.Fatalf("connect calls = %d, want 1", me.connectCalls)
	}
}

func TestAnalyzeViaMediaEngine_ConnectFailure(t *testing.T) {
	me := &fakeME{connectErr: errors.New("dial tcp: refused")}
	s, _, _ := newSvcWithME(t, me)

	_, err := s.analyzeViaMediaEngine(bg(), "/media/track.mp3", "m1")
	if !errors.Is(err, ErrAnalyzerUnavailable) {
		t.Fatalf("err = %v, want ErrAnalyzerUnavailable", err)
	}
}

func TestAnalyzeViaMediaEngine_RPCError(t *testing.T) {
	rpcErr := errors.New("stream closed")
	me := &fakeME{connected: true, analyzeErr: rpcErr}
	s, _, _ := newSvcWithME(t, me)

	if _, err := s.analyzeViaMediaEngine(bg(), "/media/track.mp3", "m1"); !errors.Is(err, rpcErr) {
		t.Fatalf("err = %v, want the RPC error", err)
	}
}

// A response with success=false carries the reason in Error, which must surface
// rather than being treated as a clean zero-valued analysis.
func TestAnalyzeViaMediaEngine_UnsuccessfulResponse(t *testing.T) {
	me := &fakeME{connected: true, analyzeResp: &pb.AnalyzeMediaResponse{Success: false, Error: "unsupported codec"}}
	s, _, _ := newSvcWithME(t, me)

	_, err := s.analyzeViaMediaEngine(bg(), "/media/track.mp3", "m1")
	if err == nil || err.Error() != "unsupported codec" {
		t.Fatalf("err = %v, want the engine's reason", err)
	}
}

func TestAnalyzeViaMediaEngine_ArtworkAttached(t *testing.T) {
	me := &fakeME{
		connected:   true,
		analyzeResp: okAnalysis(),
		artworkResp: &pb.ExtractArtworkResponse{Success: true, ArtworkData: []byte{0xff, 0xd8, 0xff}, MimeType: "image/jpeg"},
	}
	s, _, _ := newSvcWithME(t, me)

	result, err := s.analyzeViaMediaEngine(bg(), "/media/track.mp3", "m1")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(result.Artwork) != 3 || result.ArtworkMime != "image/jpeg" {
		t.Fatalf("artwork = %v (%q)", result.Artwork, result.ArtworkMime)
	}
}

// Artwork extraction is best-effort: its failure must not fail the analysis.
func TestAnalyzeViaMediaEngine_ArtworkFailureIsNotFatal(t *testing.T) {
	for _, tc := range []struct {
		name string
		me   *fakeME
	}{
		{"rpc error", &fakeME{connected: true, analyzeResp: okAnalysis(), artworkErr: errors.New("no artwork")}},
		{"unsuccessful", &fakeME{connected: true, analyzeResp: okAnalysis(), artworkResp: &pb.ExtractArtworkResponse{Success: false}}},
		{"empty data", &fakeME{connected: true, analyzeResp: okAnalysis(), artworkResp: &pb.ExtractArtworkResponse{Success: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := newSvcWithME(t, tc.me)
			result, err := s.analyzeViaMediaEngine(bg(), "/media/track.mp3", "m1")
			if err != nil {
				t.Fatalf("analyze: %v", err)
			}
			if len(result.Artwork) != 0 {
				t.Fatalf("artwork = %v, want none recorded", result.Artwork)
			}
			if result.Title != "Blue Monday" {
				t.Fatal("analysis result was discarded along with the artwork failure")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// processJob metadata merge
// ---------------------------------------------------------------------------

func TestProcessJob_WritesAnalysisAndFillsMissingMetadata(t *testing.T) {
	me := &fakeME{
		connected:   true,
		analyzeResp: okAnalysis(),
		artworkResp: &pb.ExtractArtworkResponse{Success: true, ArtworkData: []byte{0x89, 0x50}, MimeType: "image/png"},
	}
	s, db, workDir := newSvcWithME(t, me)
	// Title equals the filename stem, which is how the importer seeds an
	// un-analyzed item; that is the case where the tag title may replace it.
	m := seedMediaFile(t, db, workDir, "media-1", "track.mp3")
	job := &models.AnalysisJob{ID: "job-1", MediaID: m.ID, Status: "running"}
	db.Create(job)

	if err := s.processJob(bg(), job); err != nil {
		t.Fatalf("processJob: %v", err)
	}

	var media models.MediaItem
	if err := db.First(&media, "id = ?", m.ID).Error; err != nil {
		t.Fatalf("reload media: %v", err)
	}
	if media.AnalysisState != models.AnalysisComplete {
		t.Fatalf("analysis state = %q, want complete", media.AnalysisState)
	}
	if media.Duration != 215*time.Second {
		t.Fatalf("duration = %v, want 215s", media.Duration)
	}
	if media.Bitrate != 320 || media.Samplerate != 44100 {
		t.Fatalf("bitrate/samplerate = %d/%d", media.Bitrate, media.Samplerate)
	}
	if media.Title != "Blue Monday" {
		t.Fatalf("title = %q, want the tag title to replace the filename stem", media.Title)
	}
	if media.Artist != "New Order" || media.Album != "Power, Corruption & Lies" {
		t.Fatalf("artist/album = %q/%q", media.Artist, media.Album)
	}
	if media.Genre != "Synth-pop" || media.Year != "1983" {
		t.Fatalf("genre/year = %q/%q", media.Genre, media.Year)
	}
	if len(media.Artwork) != 2 || media.ArtworkMime != "image/png" {
		t.Fatalf("artwork = %v (%q)", media.Artwork, media.ArtworkMime)
	}
	if media.CuePoints.IntroEnd < 8.2 || media.CuePoints.IntroEnd > 8.3 {
		t.Fatalf("intro cue = %v, want ~8.25", media.CuePoints.IntroEnd)
	}

	var row models.AnalysisJob
	db.First(&row, "id = ?", "job-1")
	if row.Status != "complete" {
		t.Fatalf("job status = %q, want complete", row.Status)
	}
}

// Metadata a human already curated wins over whatever the file tags say.
func TestProcessJob_KeepsExistingMetadata(t *testing.T) {
	me := &fakeME{
		connected:   true,
		analyzeResp: okAnalysis(),
		artworkResp: &pb.ExtractArtworkResponse{Success: true, ArtworkData: []byte{0x89, 0x50}, MimeType: "image/png"},
	}
	s, db, workDir := newSvcWithME(t, me)
	m := seedMediaFile(t, db, workDir, "media-1", "track.mp3")
	db.Model(&models.MediaItem{}).Where("id = ?", m.ID).Updates(map[string]any{
		"title":        "Curated Title",
		"artist":       "Curated Artist",
		"album":        "Curated Album",
		"genre":        "Curated Genre",
		"year":         "1999",
		"artwork":      []byte{0x01},
		"artwork_mime": "image/gif",
	})
	job := &models.AnalysisJob{ID: "job-1", MediaID: m.ID, Status: "running"}
	db.Create(job)

	if err := s.processJob(bg(), job); err != nil {
		t.Fatalf("processJob: %v", err)
	}

	var media models.MediaItem
	db.First(&media, "id = ?", m.ID)
	if media.Title != "Curated Title" {
		t.Fatalf("title = %q, want the curated value preserved", media.Title)
	}
	if media.Artist != "Curated Artist" || media.Album != "Curated Album" {
		t.Fatalf("artist/album = %q/%q, want the curated values", media.Artist, media.Album)
	}
	if media.Genre != "Curated Genre" || media.Year != "1999" {
		t.Fatalf("genre/year = %q/%q, want the curated values", media.Genre, media.Year)
	}
	if len(media.Artwork) != 1 || media.ArtworkMime != "image/gif" {
		t.Fatalf("artwork = %v (%q), want the curated artwork kept", media.Artwork, media.ArtworkMime)
	}
	// Analysis numbers are always refreshed, curated or not.
	if media.Duration != 215*time.Second {
		t.Fatalf("duration = %v, want the analyzed value", media.Duration)
	}
}

// Empty tag values must never blank out fields that already hold data.
func TestProcessJob_EmptyTagsDoNotClearFields(t *testing.T) {
	me := &fakeME{
		connected: true,
		analyzeResp: &pb.AnalyzeMediaResponse{
			Success:    true,
			DurationMs: 60000,
			Metadata:   &pb.MediaMetadata{},
		},
	}
	s, db, workDir := newSvcWithME(t, me)
	m := seedMediaFile(t, db, workDir, "media-1", "track.mp3")
	db.Model(&models.MediaItem{}).Where("id = ?", m.ID).Update("artist", "Known Artist")
	job := &models.AnalysisJob{ID: "job-1", MediaID: m.ID, Status: "running"}
	db.Create(job)

	if err := s.processJob(bg(), job); err != nil {
		t.Fatalf("processJob: %v", err)
	}

	var media models.MediaItem
	db.First(&media, "id = ?", m.ID)
	if media.Artist != "Known Artist" {
		t.Fatalf("artist = %q, want it untouched by the empty tag", media.Artist)
	}
	if media.Title != "track" {
		t.Fatalf("title = %q, want the filename stem retained", media.Title)
	}
}

// A response with no Metadata block at all must not panic on the nil pointer.
func TestProcessJob_NilMetadataBlock(t *testing.T) {
	me := &fakeME{connected: true, analyzeResp: &pb.AnalyzeMediaResponse{Success: true, DurationMs: 1000}}
	s, db, workDir := newSvcWithME(t, me)
	m := seedMediaFile(t, db, workDir, "media-1", "track.mp3")
	job := &models.AnalysisJob{ID: "job-1", MediaID: m.ID, Status: "running"}
	db.Create(job)

	if err := s.processJob(bg(), job); err != nil {
		t.Fatalf("processJob: %v", err)
	}
	var media models.MediaItem
	db.First(&media, "id = ?", m.ID)
	if media.AnalysisState != models.AnalysisComplete {
		t.Fatalf("state = %q, want complete", media.AnalysisState)
	}
}

func TestProcessJob_EngineFailureMarksJobFailed(t *testing.T) {
	me := &fakeME{connected: true, analyzeErr: errors.New("decoder crashed")}
	s, db, workDir := newSvcWithME(t, me)
	m := seedMediaFile(t, db, workDir, "media-1", "track.mp3")
	job := &models.AnalysisJob{ID: "job-1", MediaID: m.ID, Status: "running"}
	db.Create(job)

	if err := s.processJob(bg(), job); err == nil {
		t.Fatal("processJob succeeded despite an engine failure")
	}

	var row models.AnalysisJob
	db.First(&row, "id = ?", "job-1")
	if row.Status != "failed" || row.Error != "decoder crashed" {
		t.Fatalf("job = %+v, want failed with the engine's message", row)
	}
	var media models.MediaItem
	db.First(&media, "id = ?", m.ID)
	if media.AnalysisState != models.AnalysisFailed {
		t.Fatalf("media state = %q, want failed", media.AnalysisState)
	}
}

// ---------------------------------------------------------------------------
// status and shutdown
// ---------------------------------------------------------------------------

func TestGetMediaEngineStatus_Connected(t *testing.T) {
	s, _, _ := newSvcWithME(t, &fakeME{connected: true})

	status := s.GetMediaEngineStatus(bg())
	if !status.Configured || !status.Connected {
		t.Fatalf("status = %+v, want configured and connected", status)
	}
	if status.Error != "" {
		t.Fatalf("error = %q, want empty", status.Error)
	}
}

func TestGetMediaEngineStatus_ReconnectsOnDemand(t *testing.T) {
	me := &fakeME{connected: false}
	s, _, _ := newSvcWithME(t, me)

	status := s.GetMediaEngineStatus(bg())
	if !status.Connected {
		t.Fatalf("status = %+v, want the on-demand reconnect to succeed", status)
	}
	if me.connectCalls != 1 {
		t.Fatalf("connect calls = %d, want 1", me.connectCalls)
	}
}

func TestGetMediaEngineStatus_ReconnectFailureReported(t *testing.T) {
	s, _, _ := newSvcWithME(t, &fakeME{connectErr: errors.New("connection refused")})

	status := s.GetMediaEngineStatus(bg())
	if status.Connected {
		t.Fatal("reported connected after a failed reconnect")
	}
	if status.Error != "connection refused" {
		t.Fatalf("error = %q, want the dial failure", status.Error)
	}
}

func TestTestMediaEngine(t *testing.T) {
	if err := (&Service{mediaEngineClient: &fakeME{connected: true}}).TestMediaEngine(bg()); err != nil {
		t.Fatalf("connected engine = %v, want nil", err)
	}

	me := &fakeME{connected: false}
	if err := (&Service{mediaEngineClient: me}).TestMediaEngine(bg()); err != nil {
		t.Fatalf("reconnect = %v, want nil", err)
	}
	if me.connectCalls != 1 {
		t.Fatalf("connect calls = %d, want 1", me.connectCalls)
	}

	dialErr := errors.New("no route to host")
	if err := (&Service{mediaEngineClient: &fakeME{connectErr: dialErr}}).TestMediaEngine(bg()); !errors.Is(err, dialErr) {
		t.Fatalf("err = %v, want the dial error", err)
	}
}

func TestClose_ClosesClient(t *testing.T) {
	closeErr := errors.New("already shut down")
	s, _, _ := newSvcWithME(t, &fakeME{closeErr: closeErr})
	if err := s.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("Close = %v, want the client's error", err)
	}
}
