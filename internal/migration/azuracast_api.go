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

// AzuraCastAPIClient is an AzuraCast API client for live imports.
type AzuraCastAPIClient struct {
	baseURL      string
	apiKey       string
	sessionToken string // For username/password auth
	httpClient   *http.Client
}

// NewAzuraCastAPIClient creates a new AzuraCast API client with API key authentication.
func NewAzuraCastAPIClient(baseURL, apiKey string) (*AzuraCastAPIClient, error) {
	// Validate and normalize base URL
	baseURL = strings.TrimSuffix(baseURL, "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}

	// Validate URL
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	return &AzuraCastAPIClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// NewAzuraCastAPIClientWithCredentials creates a new AzuraCast API client and authenticates
// with username/password to get a session token.
func NewAzuraCastAPIClientWithCredentials(baseURL, username, password string) (*AzuraCastAPIClient, error) {
	// Validate and normalize base URL
	baseURL = strings.TrimSuffix(baseURL, "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}

	// Validate URL
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	client := &AzuraCastAPIClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	// Authenticate with credentials
	if err := client.authenticate(username, password); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	return client, nil
}

// authenticate performs login with username/password to get a session token.
func (c *AzuraCastAPIClient) authenticate(username, password string) error {
	loginURL := c.baseURL + "/api/internal/login"

	// Create login request body
	loginData := struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{
		Username: username,
		Password: password,
	}

	body, err := json.Marshal(loginData)
	if err != nil {
		return fmt.Errorf("marshal login data: %w", err)
	}

	req, err := http.NewRequest("POST", loginURL, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid username or password")
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("login failed (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response for token
	var loginResp struct {
		Success bool   `json:"success"`
		Token   string `json:"token,omitempty"`
		CSRF    string `json:"csrf,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		// If we can't decode, try to extract session from cookies
		for _, cookie := range resp.Cookies() {
			if cookie.Name == "PHPSESSID" || strings.Contains(cookie.Name, "session") {
				c.sessionToken = cookie.Value
				return nil
			}
		}
		return fmt.Errorf("failed to get session token")
	}

	if loginResp.Token != "" {
		c.sessionToken = loginResp.Token
	}

	return nil
}

// doRequest performs an authenticated API request.
func (c *AzuraCastAPIClient) doRequest(ctx context.Context, method, path string) (*http.Response, error) {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	// Use API key if available, otherwise use session token
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	} else if c.sessionToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.sessionToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}

	return resp, nil
}

// decodeAPIResponse decodes a JSON response body.
func decodeAPIResponse[T any](resp *http.Response) (T, error) {
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

// TestConnection tests the API connection and returns server info.
func (c *AzuraCastAPIClient) TestConnection(ctx context.Context) (*AzuraCastAPIStatus, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/status")
	if err != nil {
		return nil, err
	}

	return decodeAPIResponse[*AzuraCastAPIStatus](resp)
}

// GetStations returns all stations the API key has access to.
func (c *AzuraCastAPIClient) GetStations(ctx context.Context) ([]AzuraCastAPIStation, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/stations")
	if err != nil {
		return nil, err
	}

	return decodeAPIResponse[[]AzuraCastAPIStation](resp)
}

// GetMedia returns all media files for a station.
func (c *AzuraCastAPIClient) GetMedia(ctx context.Context, stationID int) ([]AzuraCastAPIMediaFile, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/station/%d/files/list", stationID))
	if err != nil {
		return nil, err
	}

	return decodeAPIResponse[[]AzuraCastAPIMediaFile](resp)
}

// GetPlaylists returns all playlists for a station.
func (c *AzuraCastAPIClient) GetPlaylists(ctx context.Context, stationID int) ([]AzuraCastAPIPlaylist, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/station/%d/playlists", stationID))
	if err != nil {
		return nil, err
	}

	return decodeAPIResponse[[]AzuraCastAPIPlaylist](resp)
}

// GetMounts returns all mount points for a station.
func (c *AzuraCastAPIClient) GetMounts(ctx context.Context, stationID int) ([]AzuraCastAPIMount, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/station/%d/mounts", stationID))
	if err != nil {
		return nil, err
	}

	return decodeAPIResponse[[]AzuraCastAPIMount](resp)
}

// GetSchedules returns all schedules for a station.
func (c *AzuraCastAPIClient) GetSchedules(ctx context.Context, stationID int) ([]AzuraCastAPISchedule, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/station/%d/schedules", stationID))
	if err != nil {
		return nil, err
	}

	return decodeAPIResponse[[]AzuraCastAPISchedule](resp)
}

// GetStreamers returns all streamers/DJs for a station.
func (c *AzuraCastAPIClient) GetStreamers(ctx context.Context, stationID int) ([]AzuraCastAPIStreamer, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/station/%d/streamers", stationID))
	if err != nil {
		return nil, err
	}

	return decodeAPIResponse[[]AzuraCastAPIStreamer](resp)
}

// DownloadMedia downloads a media file and returns the reader.
// The caller is responsible for closing the reader.
func (c *AzuraCastAPIClient) DownloadMedia(ctx context.Context, stationID, mediaID int) (io.ReadCloser, int64, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/station/%d/file/%d/download", stationID, mediaID))
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

// DownloadMediaArt downloads album art for a media file.
// Returns nil, nil if no artwork is available.
func (c *AzuraCastAPIClient) DownloadMediaArt(ctx context.Context, stationID, mediaID int) ([]byte, string, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/station/%d/file/%d/art", stationID, mediaID))
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	// No artwork available
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent {
		return nil, "", nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", nil // Silently skip artwork errors
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg" // Default to JPEG
	}

	return data, contentType, nil
}

// DownloadStationArt downloads the station logo/artwork.
// Returns nil, nil if no artwork is available.
func (c *AzuraCastAPIClient) DownloadStationArt(ctx context.Context, stationID int) ([]byte, string, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/station/%d/art", stationID))
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	// No artwork available
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent {
		return nil, "", nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", nil // Silently skip artwork errors
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

// DownloadStreamerArt downloads artwork for a streamer/DJ.
// Returns nil, nil if no artwork is available.
func (c *AzuraCastAPIClient) DownloadStreamerArt(ctx context.Context, stationID, streamerID int) ([]byte, string, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/station/%d/streamer/%d/art", stationID, streamerID))
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

// GetStationProfile gets detailed station profile information.
func (c *AzuraCastAPIClient) GetStationProfile(ctx context.Context, stationID int) (*AzuraCastAPIStationProfile, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/station/%d/profile", stationID))
	if err != nil {
		return nil, err
	}

	return decodeAPIResponse[*AzuraCastAPIStationProfile](resp)
}

// GetWebhooks returns all webhooks/web hooks for a station.
func (c *AzuraCastAPIClient) GetWebhooks(ctx context.Context, stationID int) ([]AzuraCastAPIWebhook, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/station/%d/webhooks", stationID))
	if err != nil {
		return nil, err
	}

	return decodeAPIResponse[[]AzuraCastAPIWebhook](resp)
}

// GetPodcasts returns all podcasts for a station.
func (c *AzuraCastAPIClient) GetPodcasts(ctx context.Context, stationID int) ([]AzuraCastAPIPodcast, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/station/%d/podcasts", stationID))
	if err != nil {
		return nil, err
	}

	return decodeAPIResponse[[]AzuraCastAPIPodcast](resp)
}

// GetPodcastEpisodes returns all episodes for a podcast.
func (c *AzuraCastAPIClient) GetPodcastEpisodes(ctx context.Context, stationID, podcastID int) ([]AzuraCastAPIPodcastEpisode, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/station/%d/podcast/%d/episodes", stationID, podcastID))
	if err != nil {
		return nil, err
	}

	return decodeAPIResponse[[]AzuraCastAPIPodcastEpisode](resp)
}

// =============================================================================
// AzuraCast API Response Types
// =============================================================================

// AzuraCastAPIStatus represents the AzuraCast /api/status response.
type AzuraCastAPIStatus struct {
	Online      bool   `json:"online"`
	Timestamp   int64  `json:"timestamp"`
	Station     string `json:"station,omitempty"`
	Description string `json:"description,omitempty"`
}

// AzuraCastAPIStation represents a station from the AzuraCast API.
type AzuraCastAPIStation struct {
	ID               int                  `json:"id"`
	Name             string               `json:"name"`
	ShortName        string               `json:"shortcode"`
	Description      string               `json:"description"`
	Frontend         string               `json:"frontend"`
	Backend          string               `json:"backend"`
	ListenURL        string               `json:"listen_url"`
	URL              string               `json:"url"`
	PublicPlayerURL  string               `json:"public_player_url"`
	PlaylistPLSURL   string               `json:"playlist_pls_url"`
	PlaylistM3UURL   string               `json:"playlist_m3u_url"`
	IsPublic         bool                 `json:"is_public"`
	Mounts           []AzuraCastAPIMount  `json:"mounts,omitempty"`
}

// AzuraCastAPIMediaFile represents a media file from the AzuraCast API.
type AzuraCastAPIMediaFile struct {
	ID           int                      `json:"id"`
	UniqueID     string                   `json:"unique_id"`
	SongID       string                   `json:"song_id"`
	Title        string                   `json:"title"`
	Artist       string                   `json:"artist"`
	Album        string                   `json:"album"`
	Genre        string                   `json:"genre"`
	Lyrics       string                   `json:"lyrics"`
	ISRC         string                   `json:"isrc"`
	Length       float64                  `json:"length"`
	LengthText   string                   `json:"length_text"`
	Path         string                   `json:"path"`
	Size         int64                    `json:"size"` // File size in bytes (may be 0 if not provided)
	MTime        int64                    `json:"mtime"`
	FadeOverlap  *float64                 `json:"fade_overlap"`
	FadeIn       *float64                 `json:"fade_in"`
	FadeOut      *float64                 `json:"fade_out"`
	CueIn        *float64                 `json:"cue_in"`
	CueOut       *float64                 `json:"cue_out"`
	Amplify      *float64                 `json:"amplify"`
	ArtUpdatedAt int64                    `json:"art_updated_at"`
	CustomFields map[string]string        `json:"custom_fields,omitempty"`
}

// AzuraCastAPIPlaylist represents a playlist from the AzuraCast API.
type AzuraCastAPIPlaylist struct {
	ID                   int    `json:"id"`
	Name                 string `json:"name"`
	Type                 string `json:"type"`
	Source               string `json:"source"`
	Order                string `json:"order"`
	RemoteURL            string `json:"remote_url,omitempty"`
	RemoteType           string `json:"remote_type,omitempty"`
	RemoteBuffer         int    `json:"remote_buffer,omitempty"`
	IsEnabled            bool   `json:"is_enabled"`
	IsJingle             bool   `json:"is_jingle"`
	PlayPerSongs         int    `json:"play_per_songs,omitempty"`
	PlayPerMinutes       int    `json:"play_per_minutes,omitempty"`
	PlayPerHourMinute    int    `json:"play_per_hour_minute,omitempty"`
	Weight               int    `json:"weight"`
	IncludeInRequests    bool   `json:"include_in_requests"`
	IncludeInOnDemand    bool   `json:"include_in_on_demand"`
	IncludeInAutomation  bool   `json:"include_in_automation"`
	AvoidDuplicates      bool   `json:"avoid_duplicates"`
	BackendOptions       string `json:"backend_options,omitempty"`
	NumSongs             int    `json:"num_songs"`
	TotalLength          int    `json:"total_length"`
}

// AzuraCastAPIMount represents a mount point from the AzuraCast API.
type AzuraCastAPIMount struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	DisplayName       string `json:"display_name"`
	IsVisibleOnPublic bool   `json:"is_visible_on_public_pages"`
	IsDefault         bool   `json:"is_default"`
	IsPublic          bool   `json:"is_public"`
	FallbackMount     string `json:"fallback_mount,omitempty"`
	RelayURL          string `json:"relay_url,omitempty"`
	AutodjBitrate     int    `json:"autodj_bitrate,omitempty"`
	AutodjFormat      string `json:"autodj_format,omitempty"`
	CustomListenURL   string `json:"custom_listen_url,omitempty"`
	URL               string `json:"url"`
}

// AzuraCastAPISchedule represents a schedule from the AzuraCast API.
type AzuraCastAPISchedule struct {
	ID          int     `json:"id"`
	StartTime   int     `json:"start_time"`
	EndTime     int     `json:"end_time"`
	StartDate   *string `json:"start_date,omitempty"`
	EndDate     *string `json:"end_date,omitempty"`
	Days        []int   `json:"days"`
	LoopOnce    bool    `json:"loop_once"`
	PlaylistID  int     `json:"playlist_id,omitempty"`
	StreamerID  int     `json:"streamer_id,omitempty"`
}

// AzuraCastAPIStreamer represents a streamer/DJ from the AzuraCast API.
type AzuraCastAPIStreamer struct {
	ID               int    `json:"id"`
	StreamerUsername string `json:"streamer_username"`
	DisplayName      string `json:"display_name"`
	Comments         string `json:"comments"`
	IsActive         bool   `json:"is_active"`
	EnforceSchedule  bool   `json:"enforce_schedule"`
	ReactivateAt     *int64 `json:"reactivate_at,omitempty"`
	Art              string `json:"art,omitempty"`
	ArtUpdatedAt     int64  `json:"art_updated_at,omitempty"`
}

// AzuraCastAPIStationProfile represents detailed station profile from the AzuraCast API.
type AzuraCastAPIStationProfile struct {
	Station           AzuraCastAPIStation `json:"station"`
	Name              string              `json:"name"`
	ShortName         string              `json:"short_name"`
	Description       string              `json:"description"`
	URL               string              `json:"url"`
	Genre             string              `json:"genre"`
	Timezone          string              `json:"timezone"`
	DefaultMount      string              `json:"default_mount"`
	PublicPlayerURL   string              `json:"public_player_url"`
	PlaylistPLSURL    string              `json:"playlist_pls_url"`
	PlaylistM3UURL    string              `json:"playlist_m3u_url"`
	EnablePublicPage  bool                `json:"enable_public_page"`
	EnableOnDemand    bool                `json:"enable_on_demand"`
	EnableRequests    bool                `json:"enable_requests"`
	RequestDelay      int                 `json:"request_delay"`
	RequestThreshold  int                 `json:"request_threshold"`
	EnableStreamers   bool                `json:"enable_streamers"`
	IsEnabled         bool                `json:"is_enabled"`
	Branding          AzuraCastBranding   `json:"branding,omitempty"`
}

// AzuraCastBranding represents station branding configuration.
type AzuraCastBranding struct {
	DefaultAlbumArtURL string `json:"default_album_art_url,omitempty"`
	PublicCustomCSS    string `json:"public_custom_css,omitempty"`
	PublicCustomJS     string `json:"public_custom_js,omitempty"`
}

// AzuraCastAPIWebhook represents a webhook from the AzuraCast API.
type AzuraCastAPIWebhook struct {
	ID         int               `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	IsEnabled  bool              `json:"is_enabled"`
	Triggers   []string          `json:"triggers"`
	Config     map[string]string `json:"config,omitempty"`
}

// AzuraCastAPIPodcast represents a podcast from the AzuraCast API.
type AzuraCastAPIPodcast struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Link        string `json:"link"`
	Description string `json:"description"`
	Language    string `json:"language"`
	Author      string `json:"author"`
	Email       string `json:"email"`
	HasArt      bool   `json:"has_custom_art"`
	ArtURL      string `json:"art,omitempty"`
	Categories  []string `json:"categories,omitempty"`
}

// AzuraCastAPIPodcastEpisode represents a podcast episode from the AzuraCast API.
type AzuraCastAPIPodcastEpisode struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Link        string `json:"link"`
	Description string `json:"description"`
	PublishAt   int64  `json:"publish_at"`
	Explicit    bool   `json:"explicit"`
	HasArt      bool   `json:"has_custom_art"`
	ArtURL      string `json:"art,omitempty"`
	MediaID     *int   `json:"playlist_media_id,omitempty"`
}
