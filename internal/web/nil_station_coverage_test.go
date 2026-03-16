/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package web - tests covering nil-station early-return paths across all station-scoped handlers.
// These tests verify that handlers guard correctly against missing station context
// and contribute to statement coverage for the entry points of all affected handlers.
package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// noStationReq creates a request with no station in the context.
func noStationReq(method, target string) *http.Request {
	return httptest.NewRequest(method, target, nil)
}

// assertNoStation runs a handler and checks for redirect or 4xx when no station is set.
func assertNoStation(t *testing.T, name string, fn func(http.ResponseWriter, *http.Request)) {
	t.Helper()
	rr := httptest.NewRecorder()
	fn(rr, noStationReq(http.MethodGet, "/"))
	if rr.Code < 300 {
		t.Fatalf("%s: expected redirect or error with no station, got %d", name, rr.Code)
	}
}

func assertNoStationPOST(t *testing.T, name string, fn func(http.ResponseWriter, *http.Request)) {
	t.Helper()
	rr := httptest.NewRecorder()
	fn(rr, noStationReq(http.MethodPost, "/"))
	if rr.Code < 300 {
		t.Fatalf("%s: expected redirect or error with no station, got %d", name, rr.Code)
	}
}

// ---------------------------------------------------------------------------
// pages_smartblocks.go
// ---------------------------------------------------------------------------

func TestSmartBlockList_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "SmartBlockList", h.SmartBlockList)
}

func TestSmartBlockCreate_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "SmartBlockCreate", h.SmartBlockCreate)
}

func TestSmartBlockDetail_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "SmartBlockDetail", h.SmartBlockDetail)
}

func TestSmartBlockEdit_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "SmartBlockEdit", h.SmartBlockEdit)
}

func TestSmartBlockUpdate_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "SmartBlockUpdate", h.SmartBlockUpdate)
}

func TestSmartBlockDelete_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "SmartBlockDelete", h.SmartBlockDelete)
}

func TestSmartBlockDuplicate_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "SmartBlockDuplicate", h.SmartBlockDuplicate)
}

func TestSmartBlockPreview_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "SmartBlockPreview", h.SmartBlockPreview)
}

// ---------------------------------------------------------------------------
// pages_playlists.go
// ---------------------------------------------------------------------------

func TestPlaylistList_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "PlaylistList", h.PlaylistList)
}

func TestPlaylistCreate_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "PlaylistCreate", h.PlaylistCreate)
}

func TestPlaylistDetail_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "PlaylistDetail", h.PlaylistDetail)
}

func TestPlaylistEdit_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "PlaylistEdit", h.PlaylistEdit)
}

func TestPlaylistUpdate_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "PlaylistUpdate", h.PlaylistUpdate)
}

func TestPlaylistDelete_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "PlaylistDelete", h.PlaylistDelete)
}

func TestPlaylistBulk_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "PlaylistBulk", h.PlaylistBulk)
}

func TestPlaylistAddItem_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "PlaylistAddItem", h.PlaylistAddItem)
}

func TestPlaylistRemoveItem_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "PlaylistRemoveItem", h.PlaylistRemoveItem)
}

func TestPlaylistReorderItems_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "PlaylistReorderItems", h.PlaylistReorderItems)
}

func TestPlaylistCover_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "PlaylistCover", h.PlaylistCover)
}

func TestPlaylistUploadCover_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "PlaylistUploadCover", h.PlaylistUploadCover)
}

func TestPlaylistDeleteCover_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "PlaylistDeleteCover", h.PlaylistDeleteCover)
}

func TestPlaylistMediaSearch_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "PlaylistMediaSearch", h.PlaylistMediaSearch)
}

// ---------------------------------------------------------------------------
// pages_media.go
// ---------------------------------------------------------------------------

func TestMediaReanalyzeDurations_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "MediaReanalyzeDurations", h.MediaReanalyzeDurations)
}

func TestMediaReanalyzeDurationsStatus_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "MediaReanalyzeDurationsStatus", h.MediaReanalyzeDurationsStatus)
}

func TestMediaReanalyzeDurationsCurrentStatus_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "MediaReanalyzeDurationsCurrentStatus", h.MediaReanalyzeDurationsCurrentStatus)
}

func TestMediaList_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "MediaList", h.MediaList)
}

func TestMediaUpload_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "MediaUpload", h.MediaUpload)
}

func TestMediaDetail_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "MediaDetail", h.MediaDetail)
}

func TestMediaEdit_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "MediaEdit", h.MediaEdit)
}

func TestMediaUpdate_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "MediaUpdate", h.MediaUpdate)
}

func TestMediaDelete_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "MediaDelete", h.MediaDelete)
}

func TestMediaBulk_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "MediaBulk", h.MediaBulk)
}

func TestMediaGenres_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "MediaGenres", h.MediaGenres)
}

func TestMediaGenreReassign_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "MediaGenreReassign", h.MediaGenreReassign)
}

func TestMediaWaveform_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "MediaWaveform", h.MediaWaveform)
}

func TestMediaArtwork_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "MediaArtwork", h.MediaArtwork)
}

func TestMediaStream_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "MediaStream", h.MediaStream)
}

func TestMediaDuplicates_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "MediaDuplicates", h.MediaDuplicates)
}

func TestMediaPurgeDuplicates_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "MediaPurgeDuplicates", h.MediaPurgeDuplicates)
}

func TestMediaBackfillHashes_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "MediaBackfillHashes", h.MediaBackfillHashes)
}

func TestMediaTablePartial_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "MediaTablePartial", h.MediaTablePartial)
}

func TestMediaGridPartial_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "MediaGridPartial", h.MediaGridPartial)
}

func TestMediaSearchJSON_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "MediaSearchJSON", h.MediaSearchJSON)
}

// ---------------------------------------------------------------------------
// pages_analytics.go
// ---------------------------------------------------------------------------

func TestAnalyticsDashboard_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "AnalyticsDashboard", h.AnalyticsDashboard)
}

func TestAnalyticsSpins_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "AnalyticsSpins", h.AnalyticsSpins)
}

func TestAnalyticsListeners_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "AnalyticsListeners", h.AnalyticsListeners)
}

func TestAnalyticsListenersTimeSeries_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "AnalyticsListenersTimeSeries", h.AnalyticsListenersTimeSeries)
}

func TestAnalyticsListenersExportCSV_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "AnalyticsListenersExportCSV", h.AnalyticsListenersExportCSV)
}

func TestAnalyticsHistoryExportCSV_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "AnalyticsHistoryExportCSV", h.AnalyticsHistoryExportCSV)
}

func TestAnalyticsSpinsExportCSV_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "AnalyticsSpinsExportCSV", h.AnalyticsSpinsExportCSV)
}

func TestPlayoutSkip_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "PlayoutSkip", h.PlayoutSkip)
}

func TestPlayoutStop_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "PlayoutStop", h.PlayoutStop)
}

func TestPlayoutReload_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "PlayoutReload", h.PlayoutReload)
}

// ---------------------------------------------------------------------------
// pages_clocks.go
// ---------------------------------------------------------------------------

func TestClockList_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "ClockList", h.ClockList)
}

func TestClockCreate_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "ClockCreate", h.ClockCreate)
}

func TestClockDetail_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "ClockDetail", h.ClockDetail)
}

func TestClockEdit_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "ClockEdit", h.ClockEdit)
}

func TestClockUpdate_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "ClockUpdate", h.ClockUpdate)
}

func TestClockDelete_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "ClockDelete", h.ClockDelete)
}

func TestClockSimulate_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "ClockSimulate", h.ClockSimulate)
}

// ---------------------------------------------------------------------------
// pages_shows.go
// ---------------------------------------------------------------------------

func TestShowsJSON_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "ShowsJSON", h.ShowsJSON)
}

func TestShowCreate_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "ShowCreate", h.ShowCreate)
}

func TestShowUpdate_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "ShowUpdate", h.ShowUpdate)
}

func TestShowDelete_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "ShowDelete", h.ShowDelete)
}

func TestShowInstanceUpdate_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "ShowInstanceUpdate", h.ShowInstanceUpdate)
}

func TestShowInstanceCancel_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "ShowInstanceCancel", h.ShowInstanceCancel)
}

func TestShowMaterialize_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "ShowMaterialize", h.ShowMaterialize)
}

// ---------------------------------------------------------------------------
// pages_landing_editor.go
// ---------------------------------------------------------------------------

func TestLandingPageEditor_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "LandingPageEditor", h.LandingPageEditor)
}

func TestLandingPageEditorSave_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "LandingPageEditorSave", h.LandingPageEditorSave)
}

func TestLandingPageEditorPublish_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "LandingPageEditorPublish", h.LandingPageEditorPublish)
}

func TestLandingPageEditorDiscard_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "LandingPageEditorDiscard", h.LandingPageEditorDiscard)
}

func TestLandingPageEditorPreview_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "LandingPageEditorPreview", h.LandingPageEditorPreview)
}

func TestLandingPageVersions_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "LandingPageVersions", h.LandingPageVersions)
}

func TestLandingPageVersionRestore_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "LandingPageVersionRestore", h.LandingPageVersionRestore)
}

func TestLandingPageAssetUpload_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "LandingPageAssetUpload", h.LandingPageAssetUpload)
}

func TestLandingPageAssetDelete_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "LandingPageAssetDelete", h.LandingPageAssetDelete)
}

func TestLandingPageThemeUpdate_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "LandingPageThemeUpdate", h.LandingPageThemeUpdate)
}

func TestLandingPageCustomCSS_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "LandingPageCustomCSS", h.LandingPageCustomCSS)
}

// ---------------------------------------------------------------------------
// pages_station_settings.go
// ---------------------------------------------------------------------------

func TestStationSettings_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "StationSettings", h.StationSettings)
}

func TestStationSettingsUpdate_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "StationSettingsUpdate", h.StationSettingsUpdate)
}

func TestStationStopPlayout_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "StationStopPlayout", h.StationStopPlayout)
}

// ---------------------------------------------------------------------------
// pages_station_users.go
// ---------------------------------------------------------------------------

func TestStationUserList_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "StationUserList", h.StationUserList)
}

func TestStationUserInvite_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "StationUserInvite", h.StationUserInvite)
}

func TestStationUserAdd_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "StationUserAdd", h.StationUserAdd)
}

func TestStationUserEdit_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "StationUserEdit", h.StationUserEdit)
}

func TestStationUserUpdate_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "StationUserUpdate", h.StationUserUpdate)
}

func TestStationUserRemove_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "StationUserRemove", h.StationUserRemove)
}

// ---------------------------------------------------------------------------
// pages_live.go
// ---------------------------------------------------------------------------

func TestLiveDashboard_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "LiveDashboard", h.LiveDashboard)
}

func TestLiveSessions_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "LiveSessions", h.LiveSessions)
}

func TestLiveGenerateToken_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "LiveGenerateToken", h.LiveGenerateToken)
}

func TestLiveDisconnect_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "LiveDisconnect", h.LiveDisconnect)
}

func TestLiveHandover_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "LiveHandover", h.LiveHandover)
}

func TestLiveReleaseHandover_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStationPOST(t, "LiveReleaseHandover", h.LiveReleaseHandover)
}

// ---------------------------------------------------------------------------
// pages_webdj.go
// ---------------------------------------------------------------------------

func TestWebDJConsole_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "WebDJConsole", h.WebDJConsole)
}

func TestWebDJLibrarySearch_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "WebDJLibrarySearch", h.WebDJLibrarySearch)
}

func TestWebDJGenres_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "WebDJGenres", h.WebDJGenres)
}

func TestWebDJPlaylists_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "WebDJPlaylists", h.WebDJPlaylists)
}

func TestWebDJPlaylistItems_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "WebDJPlaylistItems", h.WebDJPlaylistItems)
}

func TestWebDJMediaArtwork_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "WebDJMediaArtwork", h.WebDJMediaArtwork)
}

func TestWebDJMediaStream_NoStation(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	assertNoStation(t, "WebDJMediaStream", h.WebDJMediaStream)
}
