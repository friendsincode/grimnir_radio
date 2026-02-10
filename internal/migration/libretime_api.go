/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LibreTimeAPIClient is a LibreTime v3/v4 API client for live imports.
type LibreTimeAPIClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client

	// Cache for playlist contents (LibreTime API ignores filter params)
	playlistContentsCache []LTPlaylistContent
}

// NewLibreTimeAPIClient creates a new LibreTime API client.
func NewLibreTimeAPIClient(baseURL, apiKey string) (*LibreTimeAPIClient, error) {
	// Validate and normalize base URL
	baseURL = strings.TrimSuffix(baseURL, "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}

	// Validate URL
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	return &LibreTimeAPIClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// doRequest performs an authenticated API request.
func (c *LibreTimeAPIClient) doRequest(ctx context.Context, method, path string) (*http.Response, error) {
	requestURL := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, method, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Api-Key "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}

	return resp, nil
}

// decodeResponse decodes a JSON response body.
func decodeResponse[T any](resp *http.Response) (T, error) {
	var result T
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return result, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return result, fmt.Errorf("decode response: %w", err)
	}

	return result, nil
}

// LibreTimeConnectionStatus represents the status of a connection test.
type LibreTimeConnectionStatus struct {
	Online          bool   `json:"online"`
	FilesAccessible bool   `json:"files_accessible"`
	Version         string `json:"version,omitempty"`
	Warning         string `json:"warning,omitempty"`
}

// TestConnection tests the API connection and returns status.
func (c *LibreTimeAPIClient) TestConnection(ctx context.Context) (*LibreTimeConnectionStatus, error) {
	// Test API access with version endpoint
	resp, err := c.doRequest(ctx, "GET", "/api/v2/version")
	if err != nil {
		return nil, fmt.Errorf("API unreachable: %w", err)
	}

	var versionInfo struct {
		APIVersion string `json:"api_version"`
	}
	if _, err := decodeResponse[any](resp); err != nil {
		// Try alternate version detection
		resp2, err2 := c.doRequest(ctx, "GET", "/api/v2/info")
		if err2 != nil {
			return nil, fmt.Errorf("API connection failed: %w", err)
		}
		defer resp2.Body.Close()
		if resp2.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API connection failed (status %d)", resp2.StatusCode)
		}
	}

	status := &LibreTimeConnectionStatus{
		Online:          true,
		FilesAccessible: true,
		Version:         versionInfo.APIVersion,
	}

	// Test file access with first file
	files, err := c.GetFiles(ctx)
	if err != nil {
		status.Warning = "API works but file listing may fail: " + err.Error()
	} else if len(files) > 0 {
		reader, _, err := c.DownloadFile(ctx, files[0].ID)
		if err != nil {
			status.FilesAccessible = false
			status.Warning = "API works but file downloads may fail"
		} else {
			reader.Close()
		}
	}

	return status, nil
}

// GetFiles returns all files from LibreTime.
func (c *LibreTimeAPIClient) GetFiles(ctx context.Context) ([]LTFile, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v2/files")
	if err != nil {
		return nil, err
	}

	return decodeResponse[[]LTFile](resp)
}

// DownloadFile downloads a file and returns the reader.
// The caller is responsible for closing the reader.
func (c *LibreTimeAPIClient) DownloadFile(ctx context.Context, fileID int) (io.ReadCloser, int64, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/v2/files/%d/download", fileID))
	if err != nil {
		return nil, 0, err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, 0, fmt.Errorf("download error (status %d): %s", resp.StatusCode, string(body))
	}

	return resp.Body, resp.ContentLength, nil
}

// GetPlaylists returns all playlists from LibreTime.
func (c *LibreTimeAPIClient) GetPlaylists(ctx context.Context) ([]LTPlaylist, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v2/playlists")
	if err != nil {
		return nil, err
	}

	return decodeResponse[[]LTPlaylist](resp)
}

// GetPlaylistContents returns the contents of a specific playlist.
// Note: LibreTime API ignores the playlist filter parameter, so we fetch all
// contents and filter client-side.
func (c *LibreTimeAPIClient) GetPlaylistContents(ctx context.Context, playlistID int) ([]LTPlaylistContent, error) {
	// Cache all playlist contents on first call
	if c.playlistContentsCache == nil {
		resp, err := c.doRequest(ctx, "GET", "/api/v2/playlist-contents")
		if err != nil {
			return nil, err
		}
		allContents, err := decodeResponse[[]LTPlaylistContent](resp)
		if err != nil {
			return nil, err
		}
		c.playlistContentsCache = allContents
	}

	// Filter for the requested playlist
	var filtered []LTPlaylistContent
	for _, content := range c.playlistContentsCache {
		if content.PlaylistID != nil && *content.PlaylistID == playlistID {
			filtered = append(filtered, content)
		}
	}

	return filtered, nil
}

// GetShows returns all shows from LibreTime.
func (c *LibreTimeAPIClient) GetShows(ctx context.Context) ([]LTShow, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v2/shows")
	if err != nil {
		return nil, err
	}

	return decodeResponse[[]LTShow](resp)
}

// GetShowInstances returns all show instances from LibreTime.
func (c *LibreTimeAPIClient) GetShowInstances(ctx context.Context, showID int) ([]LTShowInstance, error) {
	var path string
	if showID > 0 {
		// Filter by show ID
		path = fmt.Sprintf("/api/v2/show-instances?show=%d", showID)
	} else {
		path = "/api/v2/show-instances"
	}

	resp, err := c.doRequest(ctx, "GET", path)
	if err != nil {
		return nil, err
	}

	return decodeResponse[[]LTShowInstance](resp)
}

// GetShowDays returns all show days (recurrence) from LibreTime.
func (c *LibreTimeAPIClient) GetShowDays(ctx context.Context) ([]LTShowDays, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v2/show-days")
	if err != nil {
		return nil, err
	}

	return decodeResponse[[]LTShowDays](resp)
}

// GetSchedule returns schedule entries for a time range.
func (c *LibreTimeAPIClient) GetSchedule(ctx context.Context, start, end time.Time) ([]LTScheduleEntry, error) {
	path := fmt.Sprintf("/api/v2/schedule?starts_after=%s&ends_before=%s",
		url.QueryEscape(start.Format(time.RFC3339)),
		url.QueryEscape(end.Format(time.RFC3339)))

	resp, err := c.doRequest(ctx, "GET", path)
	if err != nil {
		return nil, err
	}

	return decodeResponse[[]LTScheduleEntry](resp)
}

// GetStationInfo returns station metadata/preferences.
func (c *LibreTimeAPIClient) GetStationInfo(ctx context.Context) (*LTStationInfo, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v2/info")
	if err != nil {
		return nil, err
	}

	return decodeResponse[*LTStationInfo](resp)
}

// GetWebstreams returns all webstreams (remote streams).
func (c *LibreTimeAPIClient) GetWebstreams(ctx context.Context) ([]LTWebstream, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v2/webstreams")
	if err != nil {
		return nil, err
	}

	return decodeResponse[[]LTWebstream](resp)
}

// GetSmartBlocks returns all smart blocks (dynamic playlists).
func (c *LibreTimeAPIClient) GetSmartBlocks(ctx context.Context) ([]LTSmartBlock, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v2/smart-blocks")
	if err != nil {
		return nil, err
	}

	return decodeResponse[[]LTSmartBlock](resp)
}

// GetSmartBlockCriteria returns criteria for a smart block.
func (c *LibreTimeAPIClient) GetSmartBlockCriteria(ctx context.Context, blockID int) ([]LTSmartBlockCriteria, error) {
	// LibreTime API v2 uses a separate endpoint with query filter, not nested routes
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/v2/smart-block-criteria?block=%d", blockID))
	if err != nil {
		return nil, err
	}

	return decodeResponse[[]LTSmartBlockCriteria](resp)
}

// GetListenerStats returns listener statistics.
func (c *LibreTimeAPIClient) GetListenerStats(ctx context.Context) (*LTListenerStats, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v2/listener-count")
	if err != nil {
		return nil, err
	}

	return decodeResponse[*LTListenerStats](resp)
}

// DownloadShowImage downloads the image for a show.
// Returns nil, nil if no image is available.
func (c *LibreTimeAPIClient) DownloadShowImage(ctx context.Context, showID int) ([]byte, string, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/v2/shows/%d/image", showID))
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent {
		return nil, "", nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	return data, contentType, nil
}

// =============================================================================
// LibreTime API Response Types
// =============================================================================

// LTFile represents a file/media item from the LibreTime API.
type LTFile struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	Artist       string  `json:"artist_name"`
	Title        string  `json:"track_title"`
	Album        string  `json:"album_title"`
	Genre        string  `json:"genre"`
	Date         string  `json:"date"`    // Year as string (was "year" in old API)
	Length       string  `json:"length"`  // "HH:MM:SS.mmm"
	CueIn        *string `json:"cue_in"`  // Duration string
	CueOut       *string `json:"cue_out"` // Duration string
	ReplayGain   *string `json:"replay_gain"`
	Filepath     string  `json:"filepath"`
	Bitrate      *int    `json:"bit_rate"`
	Samplerate   *int    `json:"sample_rate"`
	Mime         string  `json:"mime"`
	ImportStatus *int    `json:"import_status"`
	FileExists   bool    `json:"exists"` // API uses "exists" not "file_exists"
	Hidden       bool    `json:"hidden"`
	TrackNumber  *int    `json:"track_number"`
	Size         int64   `json:"size"` // API uses "size" not "filesize"
}

// LTPlaylist represents a playlist from the LibreTime API.
type LTPlaylist struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Length      string `json:"length"` // "HH:MM:SS"
	CreatorID   *int   `json:"creator_id"`
}

// LTPlaylistContent represents a playlist item from the LibreTime API.
type LTPlaylistContent struct {
	ID         int     `json:"id"`
	PlaylistID *int    `json:"playlist"` // API uses "playlist" not "playlist_id"
	FileID     *int    `json:"file"`     // API uses "file" not "file_id"
	BlockID    *int    `json:"block"`    // API uses "block" not "block_id"
	StreamID   *int    `json:"stream"`   // API uses "stream" not "stream_id"
	Position   int     `json:"position"`
	Offset     float64 `json:"offset"`
	Length     string  `json:"length"`
	CueIn      *string `json:"cue_in"`   // Duration string
	CueOut     *string `json:"cue_out"`  // Duration string
	FadeIn     *string `json:"fade_in"`  // Duration string (underscore format)
	FadeOut    *string `json:"fade_out"` // Duration string (underscore format)
}

// LTShow represents a show from the LibreTime API.
type LTShow struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	URL                string `json:"url"`
	Genre              string `json:"genre"`
	Color              string `json:"color"`
	BackgroundColor    string `json:"background_color"`
	Timezone           string `json:"timezone"`       // Show timezone
	LinkedShow         *int   `json:"linked_show_id"` // For multi-show/calendar association
	HasAutoplaylist    bool   `json:"has_autoplaylist"`
	AutoplaylistID     *int   `json:"autoplaylist_id"`
	AutoplaylistRepeat bool   `json:"autoplaylist_repeat"`
}

// LTShowInstance represents a show instance from the LibreTime API.
type LTShowInstance struct {
	ID          int       `json:"id"`
	ShowID      int       `json:"show_id"`
	Starts      time.Time `json:"starts"`
	Ends        time.Time `json:"ends"`
	Timezone    string    `json:"timezone"` // Instance timezone
	Record      int       `json:"record"`
	Rebroadcast int       `json:"rebroadcast"`
	TimeFilled  string    `json:"time_filled"`
	Modified    bool      `json:"modified_instance"`
}

// LTShowDays represents show recurrence days from the LibreTime API.
type LTShowDays struct {
	ID          int     `json:"id"`
	ShowID      int     `json:"show_id"`
	FirstShow   string  `json:"first_show"` // Date string
	LastShow    *string `json:"last_show"`  // Date string, nullable
	StartTime   string  `json:"start_time"` // Time string HH:MM:SS
	Timezone    string  `json:"timezone"`
	Duration    string  `json:"duration"`    // HH:MM:SS
	Day         int     `json:"day"`         // 0=Mon, 6=Sun
	RepeatType  int     `json:"repeat_type"` // 0=weekly, 1=biweekly, etc.
	NextPopDate string  `json:"next_pop_date"`
}

// LTScheduleEntry represents a schedule entry from the LibreTime API.
type LTScheduleEntry struct {
	ID            int       `json:"id"`
	Starts        time.Time `json:"starts"`
	Ends          time.Time `json:"ends"`
	FileID        *int      `json:"file_id"`
	StreamID      *int      `json:"stream_id"`
	ClipLength    string    `json:"clip_length"`
	FadeIn        *float64  `json:"fade_in"`
	FadeOut       *float64  `json:"fade_out"`
	CueIn         *float64  `json:"cue_in"`
	CueOut        *float64  `json:"cue_out"`
	InstanceID    int       `json:"instance_id"`
	PlayoutStatus int       `json:"playout_status"`
	Broadcasted   int       `json:"broadcasted"`
	Position      int       `json:"position"`
}

// =============================================================================
// LibreTime Analysis Report
// =============================================================================

// LibreTimeAnalysisReport provides detailed dry-run analysis results for LibreTime.
type LibreTimeAnalysisReport struct {
	// Summary counts
	TotalFiles     int `json:"total_files"`
	TotalPlaylists int `json:"total_playlists"`
	TotalShows     int `json:"total_shows"`

	// Detailed breakdown
	Files     []LTFileSummary     `json:"files,omitempty"`
	Playlists []LTPlaylistSummary `json:"playlists,omitempty"`
	Shows     []LTShowSummary     `json:"shows,omitempty"`

	// Estimated storage requirements
	EstimatedStorageBytes int64  `json:"estimated_storage_bytes"`
	EstimatedStorageHuman string `json:"estimated_storage_human"`

	// Potential issues
	Warnings []string `json:"warnings"`
}

// LTFileSummary provides a summary of a file for reporting.
type LTFileSummary struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Size   int64  `json:"size"`
}

// LTPlaylistSummary provides a summary of a playlist for reporting.
type LTPlaylistSummary struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	ItemCount   int    `json:"item_count"`   // Total items (files + blocks + streams)
	FileCount   int    `json:"file_count"`   // Direct media files
	BlockCount  int    `json:"block_count"`  // Smart block references
	StreamCount int    `json:"stream_count"` // Webstream references
	Length      string `json:"length"`
}

// LTShowSummary provides a summary of a show for reporting.
type LTShowSummary struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Genre       string `json:"genre"`
}

// LTStationInfo represents station metadata from the LibreTime API.
type LTStationInfo struct {
	StationName         string `json:"station_name"`
	StationDescription  string `json:"station_description"`
	StationWebsite      string `json:"station_website"`
	StationGenre        string `json:"station_genre"`
	StationLanguage     string `json:"station_language"`
	StationTimezone     string `json:"station_timezone"`
	StationContactEmail string `json:"station_contact_email"`
	StationLogo         string `json:"station_logo,omitempty"` // URL or base64
}

// LTWebstream represents a remote webstream from the LibreTime API.
type LTWebstream struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Length      string `json:"length"`
	OwnerID     *int   `json:"owner"` // API uses "owner" not "creator_id"
	Mime        string `json:"mime"`
}

// LTSmartBlock represents a smart block (dynamic playlist) from the LibreTime API.
// Note: LibreTime API v2 has a simplified SmartBlock model. Limit/sort settings
// are managed through the UI and not exposed via the API.
type LTSmartBlock struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Length      string `json:"length"`
	Kind        string `json:"kind"`  // API uses "kind" not "type"
	OwnerID     *int   `json:"owner"` // API uses "owner" not "creator_id"
}

// LTSmartBlockCriteria represents criteria for a smart block.
type LTSmartBlockCriteria struct {
	ID        int    `json:"id"`
	BlockID   int    `json:"block"`     // API uses "block" not "block_id"
	Criteria  string `json:"criteria"`  // Field name (artist, album, genre, etc.)
	Condition string `json:"condition"` // API uses "condition" not "modifier"
	Value     string `json:"value"`
	Extra     string `json:"extra,omitempty"`
	Group     int    `json:"group"` // API uses "group" not "criteriagroup"
}

// LTListenerStats represents listener statistics from the LibreTime API.
type LTListenerStats struct {
	ListenerCount int `json:"listener_count"`
}
