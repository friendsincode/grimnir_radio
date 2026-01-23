/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package libretime

// LibreTime database schema structures
// These represent the tables in a LibreTime PostgreSQL database

// Show represents a LibreTime show/program
type Show struct {
	ID          int    `db:"id"`
	Name        string `db:"name"`
	URL         string `db:"url"`
	Genre       string `db:"genre"`
	Description string `db:"description"`
	Color       string `db:"color"`
}

// ShowInstance represents a scheduled instance of a show
type ShowInstance struct {
	ID              int    `db:"id"`
	ShowID          int    `db:"show_id"`
	Starts          string `db:"starts"`
	Ends            string `db:"ends"`
	Record          bool   `db:"record"`
	Rebroadcast     bool   `db:"rebroadcast"`
	AutoPlaylistID  *int   `db:"autoplaylist_id"`
	FileFilled      bool   `db:"file_filled"`
	TimeFilled      string `db:"time_filled"`
	Created         string `db:"created"`
	LastScheduled   string `db:"last_scheduled"`
	ModifiedInstance bool  `db:"modified_instance"`
}

// Playlist represents a LibreTime playlist
type Playlist struct {
	ID          int    `db:"id"`
	Name        string `db:"name"`
	Mtime       string `db:"mtime"`
	Utime       string `db:"utime"`
	CreatorID   int    `db:"creator_id"`
	Description string `db:"description"`
	Length      string `db:"length"`
}

// PlaylistContents links playlists to files
type PlaylistContents struct {
	ID           int     `db:"id"`
	PlaylistID   int     `db:"playlist_id"`
	FileID       *int    `db:"file_id"`
	BlockID      *int    `db:"block_id"`
	StreamID     *int    `db:"stream_id"`
	Type         int     `db:"type"` // 0=file, 1=block, 2=stream
	Position     int     `db:"position"`
	TrackOffset  float64 `db:"trackoffset"`
	Cliplength   string  `db:"cliplength"`
	Cuein        string  `db:"cuein"`
	Cueout       string  `db:"cueout"`
	Fadein       string  `db:"fadein"`
	Fadeout      string  `db:"fadeout"`
}

// File represents a media file
type File struct {
	ID              int     `db:"id"`
	Name            string  `db:"name"`
	Mime            string  `db:"mime"`
	Ftype           string  `db:"ftype"`
	Filepath        string  `db:"filepath"`
	ImportStatus    int     `db:"import_status"`
	CurrentlyAccessing int  `db:"currently_accessing"`
	EditedBy        *int    `db:"editedby"`
	Mtime           string  `db:"mtime"`
	Utime           string  `db:"utime"`
	Lptime          *string `db:"lptime"`
	Md5             string  `db:"md5"`
	TrackTitle      string  `db:"track_title"`
	ArtistName      string  `db:"artist_name"`
	AlbumTitle      string  `db:"album_title"`
	Genre           string  `db:"genre"`
	Mood            string  `db:"mood"`
	TrackNumber     *int    `db:"track_number"`
	Channels        *int    `db:"channels"`
	URL             string  `db:"url"`
	BPM             *int    `db:"bpm"`
	Rating          string  `db:"rating"`
	EncodedBy       string  `db:"encoded_by"`
	Isrc            string  `db:"isrc"`
	Label           string  `db:"label"`
	Composer        string  `db:"composer"`
	Encoder         string  `db:"encoder"`
	Checksum        string  `db:"checksum"`
	Lyrics          string  `db:"lyrics"`
	Orchestra       string  `db:"orchestra"`
	Conductor       string  `db:"conductor"`
	Language        string  `db:"language"`
	Year            string  `db:"year"`
	ReplayGain      string  `db:"replay_gain"`
	Owner           *int    `db:"owner_id"`
	CueIn           string  `db:"cuein"`
	CueOut          string  `db:"cueout"`
	Silan_check     bool    `db:"silan_check"`
	Hidden          bool    `db:"hidden"`
	IsScheduled     bool    `db:"is_scheduled"`
	IsPlaylist      bool    `db:"is_playlist"`
	ResourceID      string  `db:"resource_id"`
	Length          string  `db:"length"`
}

// Schedule represents a scheduled item
type Schedule struct {
	ID               int     `db:"id"`
	Starts           string  `db:"starts"`
	Ends             string  `db:"ends"`
	FileID           *int    `db:"file_id"`
	StreamID         *int    `db:"stream_id"`
	ClipLength       string  `db:"clip_length"`
	FadeIn           string  `db:"fade_in"`
	FadeOut          string  `db:"fade_out"`
	CueIn            string  `db:"cue_in"`
	CueOut           string  `db:"cue_out"`
	MediaItemPlayed  bool    `db:"media_item_played"`
	InstanceID       int     `db:"instance_id"`
	PlayoutStatus    int     `db:"playout_status"`
	BroadcastedBy    *int    `db:"broadcasted"`
	Position         int     `db:"position"`
}

// Webstream represents a LibreTime webstream
type Webstream struct {
	ID          int    `db:"id"`
	Name        string `db:"name"`
	Description string `db:"description"`
	URL         string `db:"url"`
	Length      string `db:"length"`
	CreatorID   int    `db:"creator_id"`
	Mtime       string `db:"mtime"`
	Utime       string `db:"utime"`
	Mime        string `db:"mime"`
}

// User represents a LibreTime user (cc_subjs table)
type User struct {
	ID        int    `db:"id"`
	Login     string `db:"login"`
	Pass      string `db:"pass"`
	Type      string `db:"type"`
	FirstName string `db:"first_name"`
	LastName  string `db:"last_name"`
	Email     string `db:"email"`
	Skype     string `db:"skype_contact"`
	Jabber    string `db:"jabber_contact"`
}

// SmartBlock represents a LibreTime smart block (autoplaylist)
type SmartBlock struct {
	ID          int    `db:"id"`
	Name        string `db:"name"`
	Description string `db:"description"`
	Length      string `db:"length"`
	Type        string `db:"type"` // static or dynamic
	Shuffle     bool   `db:"shuffle"`
	Mtime       string `db:"mtime"`
	Utime       string `db:"utime"`
	CreatorID   int    `db:"creator_id"`
}

// SmartBlockCriteria represents smart block rules
type SmartBlockCriteria struct {
	ID          int    `db:"id"`
	CriteriaID  int    `db:"criteria_id"`
	Criteria    string `db:"criteria"`
	Modifier    string `db:"modifier"`
	Value       string `db:"value"`
	Extra       string `db:"extra"`
	BlockID     int    `db:"block_id"`
}
