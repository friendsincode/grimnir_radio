package azuracast

// AzuraCast database schema structures
// These represent the tables in an AzuraCast backup

// Station represents an AzuraCast station
type Station struct {
	ID                    int    `json:"id"`
	Name                  string `json:"name"`
	ShortName             string `json:"short_name"`
	Description           string `json:"description"`
	Frontend              string `json:"frontend"`
	Backend               string `json:"backend"`
	ListenURL             string `json:"listen_url"`
	URL                   string `json:"url"`
	RadioBaseDir          string `json:"radio_base_dir"`
	IsEnabled             bool   `json:"is_enabled"`
	EnableRequests        bool   `json:"enable_requests"`
	RequestDelay          int    `json:"request_delay"`
	RequestThreshold      int    `json:"request_threshold"`
	EnableStreamers       bool   `json:"enable_streamers"`
	EnablePublicPage      bool   `json:"enable_public_page"`
	EnableOnDemand        bool   `json:"enable_on_demand"`
	Timezone              string `json:"timezone"`
}

// StationMount represents a mount point
type StationMount struct {
	ID               int    `json:"id"`
	StationID        int    `json:"station_id"`
	Name             string `json:"name"`
	DisplayName      string `json:"display_name"`
	IsVisible        bool   `json:"is_visible_on_public_pages"`
	IsDefault        bool   `json:"is_default"`
	FallbackMount    string `json:"fallback_mount"`
	RelayURL         string `json:"relay_url"`
	Authhash         string `json:"authhash"`
	EnableAutodj     bool   `json:"enable_autodj"`
	AutodjFormat     string `json:"autodj_format"`
	AutodjBitrate    int    `json:"autodj_bitrate"`
}

// StationPlaylist represents an AzuraCast playlist
type StationPlaylist struct {
	ID                  int       `json:"id"`
	StationID           int       `json:"station_id"`
	Name                string    `json:"name"`
	Type                string    `json:"type"` // default, once_per_x_songs, once_per_x_minutes, scheduled
	Source              string    `json:"source"` // songs, remote_url
	Order               string    `json:"order"` // shuffle, random, sequential
	RemoteURL           string    `json:"remote_url"`
	RemoteType          string    `json:"remote_type"` // stream, playlist
	RemoteBuffer        int       `json:"remote_buffer"`
	IsEnabled           bool      `json:"is_enabled"`
	IsJingle            bool      `json:"is_jingle"`
	PlayPerSongs        int       `json:"play_per_songs"`
	PlayPerMinutes      int       `json:"play_per_minutes"`
	PlayPerHourMin      int       `json:"play_per_hour_minute"`
	PlayPerHourMax      int       `json:"play_per_hour_minute_max"`
	Weight              int       `json:"weight"`
	IncludeInAutomation bool      `json:"include_in_automation"`
	IncludeInRequests   bool      `json:"include_in_requests"`
	IncludeInOnDemand   bool      `json:"include_in_on_demand"`
	BackendOptions      string    `json:"backend_options"` // JSON
	FilterType          string    `json:"filter_type"` // custom, smart
}

// StationPlaylistMedia links playlists to media
type StationPlaylistMedia struct {
	ID         int `json:"id"`
	PlaylistID int `json:"playlist_id"`
	MediaID    int `json:"media_id"`
	Weight     int `json:"weight"`
	Order      int `json:"order"`
}

// StationSchedule represents a scheduled playlist
type StationSchedule struct {
	ID         int       `json:"id"`
	PlaylistID int       `json:"playlist_id"`
	StartTime  int       `json:"start_time"` // Minutes since midnight
	EndTime    int       `json:"end_time"`   // Minutes since midnight
	StartDate  *string   `json:"start_date"`
	EndDate    *string   `json:"end_date"`
	Days       string    `json:"days"` // Comma-separated: 1,2,3,4,5,6,7
	LoopOnce   bool      `json:"loop_once"`
}

// StationMedia represents a media file
type StationMedia struct {
	ID             int       `json:"id"`
	StorageID      int       `json:"storage_location_id"`
	AlbumID        *int      `json:"album_id"`
	UniqueID       string    `json:"unique_id"`
	SongID         string    `json:"song_id"`
	Title          string    `json:"title"`
	Artist         string    `json:"artist"`
	Album          string    `json:"album"`
	Genre          string    `json:"genre"`
	Lyrics         string    `json:"lyrics"`
	ISRC           string    `json:"isrc"`
	Length         float64   `json:"length"`
	LengthText     string    `json:"length_text"`
	Path           string    `json:"path"`
	MTime          int64     `json:"mtime"`
	Amplify        *float64  `json:"amplify"`
	FadeOverlap    *float64  `json:"fade_overlap"`
	FadeIn         *float64  `json:"fade_in"`
	FadeOut        *float64  `json:"fade_out"`
	CueIn          *float64  `json:"cue_in"`
	CueOut         *float64  `json:"cue_out"`
	Art            string    `json:"art_updated_at"`
}

// User represents an AzuraCast user
type User struct {
	ID       int    `json:"id"`
	Email    string `json:"email"`
	AuthPass string `json:"auth_password"`
	Name     string `json:"name"`
	Locale   string `json:"locale"`
	Theme    string `json:"theme"`
}

// RolePermission represents a user role
type RolePermission struct {
	ID          int    `json:"id"`
	RoleID      int    `json:"role_id"`
	StationID   *int   `json:"station_id"`
	Action      string `json:"action"`
}

// UserRole links users to roles
type UserRole struct {
	UserID int `json:"user_id"`
	RoleID int `json:"role_id"`
}
