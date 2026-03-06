# Grimnir Radio - User Guide

This guide explains how to use Grimnir Radio, a broadcast automation system for running internet radio stations. It covers every screen and feature in the application.

---

## Table of Contents

1. [Getting Started](#getting-started)
2. [The Dashboard](#the-dashboard)
3. [Media Library](#media-library)
4. [Playlists](#playlists)
5. [Smart Blocks](#smart-blocks)
6. [Clock Templates](#clock-templates)
7. [Schedule](#schedule)
8. [Live DJ Broadcasting](#live-dj-broadcasting)
9. [WebDJ Console](#webdj-console)
10. [Recordings](#recordings)
11. [Webstreams](#webstreams)
12. [Analytics](#analytics)
13. [Station Users](#station-users)
14. [Station Settings](#station-settings)
15. [Landing Page Editor](#landing-page-editor)
16. [Station Logs & Audit](#station-logs--audit)
17. [Your Profile](#your-profile)
18. [Platform Admin](#platform-admin)
19. [Platform Settings](#platform-settings)
20. [Public Pages](#public-pages)
21. [Keyboard Shortcuts](#keyboard-shortcuts)
22. [User Roles Explained](#user-roles-explained)

---

## Getting Started

### First-Time Setup

When you open Grimnir Radio for the very first time (fresh install), you'll see a **Setup Wizard**. This creates:

- Your admin account (email and password)
- Your first station

After setup, you'll be taken to the login page.

### Logging In

Go to `/login` and enter your email and password. After logging in, you'll land on the **Dashboard**.

If you belong to more than one station, you'll first see a **Station Selector** page where you pick which station you want to work with.

---

## The Dashboard

**Where:** `/dashboard`
**Sidebar icon:** Home (house)

The dashboard is your home base. It shows a quick overview of your station:

- **Upcoming Schedule** — The next scheduled items (up to 10, looking 24 hours ahead)
- **Recent Media Uploads** — The 5 most recently added tracks
- **Active Live Sessions** — Any DJs currently broadcasting live
- **Quick Stats** — Total media count, playlist count, smart block count
- **Listener Stats** — Current listeners, today's peak, 24-hour average

---

## Media Library

**Where:** `/dashboard/media`
**Sidebar icon:** Music note

The media library is where all your audio files live. This is the foundation of your station — every track that gets played on air comes from here.

### Browsing Your Library

You can view your tracks in two ways:
- **Table view** — A list showing title, artist, album, genre, duration, and BPM
- **Grid view** — Album art thumbnails in a card layout

**Searching and filtering:**
- Type in the search box to find tracks by title, artist, or album
- Filter by genre or artist using the dropdown filters
- Sort by name, date added, or duration

### Uploading Music

**Where:** `/dashboard/media/upload`

1. Click the **Upload** button in the media library
2. Drag and drop files onto the upload area, or click to browse your computer
3. You can upload multiple files at once
4. The system automatically reads metadata from the file tags (title, artist, album, genre, artwork)
5. After upload, each file goes through analysis to detect duration and loudness levels

### Editing Track Metadata

Click on any track to see its detail page. Click **Edit** to change:

- **Title, Artist, Album** — Basic info
- **Genre** — Pick from existing genres or type a new one
- **Mood** — Optional mood tag
- **Year, Label, Composer** — Additional metadata
- **Cue Points** — Intro end and outro start markers (in seconds). These tell the system where the "meat" of the track starts and where it begins to fade out
- **Archive settings** — Whether this track shows up in the public archive, and whether listeners can download it

### Managing Genres

**Where:** `/dashboard/media/genres`

This page lets you clean up and organize your genre tags. You can:
- See all genres in use and how many tracks use each one
- **Reassign** — Merge one genre into another (e.g., change all "Electronica" to "Electronic")

### Finding Duplicates

**Where:** `/dashboard/media/duplicates`

The duplicate finder helps you clean up your library. It can find duplicates by:
- **Content hash** — Files with identical audio content (most accurate)
- **Metadata** — Files with matching title and artist

You can then bulk-delete the duplicates.

### Bulk Operations

Select multiple tracks using the checkboxes, then use the bulk actions menu to:
- Delete selected tracks
- Change genre for all selected tracks

---

## Playlists

**Where:** `/dashboard/playlists`
**Sidebar icon:** List

Playlists are ordered lists of tracks. You use them to organize music for scheduling.

### Creating a Playlist

1. Click **New Playlist**
2. Give it a name
3. Optionally upload a cover image
4. Save it

### Adding Tracks

Open a playlist and use the media search to find tracks. Click the **+** button to add a track to the playlist.

### Ordering Tracks

Drag and drop tracks in the playlist to rearrange their order. The order matters — when this playlist is scheduled, tracks play from top to bottom.

### Removing Tracks

Click the trash icon next to any track to remove it from the playlist. This doesn't delete the track from your library — it just removes it from this playlist.

---

## Smart Blocks

**Where:** `/dashboard/smart-blocks`
**Sidebar icon:** Lightning bolt

Smart blocks are **rule-based playlist generators**. Instead of picking tracks manually, you define rules and the system picks tracks that match.

### Why Use Smart Blocks?

- **Variety** — The system picks different tracks each time, so your station doesn't sound repetitive
- **Artist separation** — You can tell it "don't play the same artist within 3 tracks"
- **Automatic** — Once set up, it fills your schedule without you touching it

### Creating a Smart Block

1. Click **New Smart Block**
2. Give it a name
3. Add rules:
   - **Genre** — Only pick tracks from certain genres (e.g., "Rock", "Jazz")
   - **Artist** — Include or exclude specific artists
   - **BPM range** — Only pick tracks within a tempo range
   - **Mood** — Filter by mood tag
   - **Year range** — Only tracks from certain years
   - **Duration range** — Only tracks within a length range
4. Set **rotation** options:
   - **Artist separation** — Minimum number of tracks between the same artist
   - **Album separation** — Minimum tracks between songs from the same album
5. Click **Preview** to see what tracks the system would pick
6. Save

### Duplicating a Smart Block

Click **Duplicate** to create a copy of an existing smart block. Useful when you want a variation (e.g., "Daytime Rock" vs "Evening Rock" with slightly different rules).

---

## Clock Templates

**Where:** `/dashboard/clocks`
**Sidebar icon:** Clock

Clock templates define what happens during each hour of broadcast. Think of them as hour-long show templates.

### How Clocks Work

A clock divides one hour into **slots**. Each slot says "play something from this source." For example:

- Slot 1 (0:00–0:15): Play from "Top Hits" playlist
- Slot 2 (0:15–0:30): Play from "New Releases" smart block
- Slot 3 (0:30–0:45): Play from "Deep Cuts" smart block
- Slot 4 (0:45–0:60): Play from "Requests" playlist

### Creating a Clock

1. Click **New Clock Template**
2. Name it (e.g., "Morning Drive", "Late Night Jazz")
3. Add slots — for each slot, pick:
   - **Time range** within the hour
   - **Source** — which playlist, smart block, or single track to pull from
4. Click **Simulate** to preview what the clock would generate
5. Save

### Using Clocks in the Schedule

Once you've created clock templates, you assign them to hours in the [Schedule](#schedule). The scheduler then expands each clock into concrete tracks.

---

## Schedule

**Where:** `/dashboard/schedule`
**Sidebar icon:** Calendar

The schedule is a calendar that controls what plays on your station and when.

### Calendar View

You'll see a full calendar with month, week, and day views. Scheduled items appear as colored blocks.

**Color coding:**
- Each source type (playlist, smart block, live, webstream) gets a distinct color
- Colors can be customized in your [Profile](#your-profile) settings with different calendar color themes

### Adding Schedule Entries

Click on a time slot in the calendar, or use the **Add Entry** button:

1. Pick a **start time** and **end time**
2. Choose the **source type**:
   - **Playlist** — Plays tracks from a playlist in order
   - **Smart Block** — Generates tracks from smart block rules
   - **Clock Template** — Uses a clock template to fill the hour
   - **Single Track** — Plays one specific track
   - **Webstream** — Relays an external audio stream
   - **Live** — Reserved time for a live DJ
3. Pick the specific source (which playlist, which smart block, etc.)
4. Optionally set it to **repeat** (daily, weekdays only, weekly, or custom)
5. Save

### Editing and Deleting

- Click any entry to edit its details
- Drag the edges of an entry to resize it (change start/end time)
- Delete entries from the edit panel

### Schedule Validation

The schedule has a built-in validator that checks for problems:
- **Gaps** — Unscheduled time where nothing will play
- **Overlaps** — Two things scheduled at the same time

A green checkmark means the schedule is clean. Yellow or red indicators mean there are issues to fix.

### Refreshing the Timeline

Click **Refresh** to tell the scheduler to regenerate the timeline. This re-expands all smart blocks and clocks into concrete tracks. Do this after making changes to smart blocks or playlists that are currently scheduled.

---

## Live DJ Broadcasting

**Where:** `/dashboard/live`
**Sidebar icon:** Microphone

This is where DJs connect to broadcast live, overriding the automated schedule.

### How Live Broadcasting Works

1. A DJ requests a **connection token** from this page
2. The DJ connects using their preferred method:
   - **Icecast** — Connect with software like BUTT, Mixxx, or any Icecast-compatible encoder
   - **WebRTC** — Connect directly from the browser (via the [WebDJ Console](#webdj-console))
   - **RTP/SRT** — For professional broadcast setups
3. Once connected, the live feed takes priority over automation
4. When the DJ disconnects, automation resumes

### The Live Dashboard Shows

- **Active sessions** — Who's currently live, how long they've been on, which mount they're using
- **Connection tokens** — Generate new tokens for DJs to connect
- **Disconnect button** — Managers/admins can kick a DJ off air if needed

### Priority System

Grimnir Radio uses a 5-level priority system to decide what plays:

1. **Emergency** — Highest priority (emergency alerts)
2. **Live Override** — A DJ who manually goes live outside their scheduled slot
3. **Live Scheduled** — A DJ broadcasting during their scheduled show time
4. **Automation** — Normal scheduled playout (playlists, smart blocks, etc.)
5. **Fallback** — Safety net content if nothing else is available

A higher-priority source always takes over from a lower one.

---

## WebDJ Console

**Where:** `/dashboard/webdj`
**Sidebar icon:** Disc

The WebDJ Console is a browser-based DJ mixing interface. It lets you mix tracks and broadcast live without any external software.

### Starting a Session

Click **Start Session** (or **Resume Session** if you have one open). This connects you to the server via WebSocket for real-time control.

### The Console Layout

The console has these main sections:

#### Decks (Left and Right)

You have two decks — **Deck A** (blue, left side) and **Deck B** (red, right side). Each deck shows:

- **Waveform** — A visual representation of the audio. Click anywhere on it to jump to that position
- **Track info** — Title, artist, current time / total time, BPM
- **Transport controls:**
  - **Rewind** — Jump to the start of the track
  - **Play/Pause** — Start or pause playback
  - **Stop** — Stop and return to the beginning
  - **Eject** — Remove the track from the deck
- **Hot Cues** — 8 numbered buttons per deck. Click an empty one to save the current position. Click a saved one to jump to that spot. Right-click to delete a cue
- **Pitch slider** — Speed up or slow down the track (-8% to +8%)

#### Center Mixer

Between the two decks is the mixer strip with:

- **Master Volume** — Controls the overall output level (vertical fader)
- **EQ A / EQ B** — Three-band equalizer for each deck (HI, MID, LO). Each goes from -12dB to +12dB
- **Volume A / Volume B** — Individual volume faders for each deck (vertical)
- **Crossfader** — Slide left to hear more of Deck A, right for Deck B, center for both equally

#### Monitor Section

Below the mixer is the headphone monitoring section:

- **CUE A / CUE B** — Toggle buttons. Turn on CUE A to hear Deck A in your headphones for previewing
- **CUE / MIX slider** — Controls the blend between your cue signal and the master output in your headphones
- **SPLIT** — When enabled, sends cue to one ear and master to the other
- **Headphone Volume** — Controls the headphone output level

#### Going Live

At the top of the console is the **broadcast bar**:

1. Select a **mount** (output stream) from the dropdown
2. Choose your **input type** (WebRTC, Icecast, or RTP)
3. Click **Go Live** — your mix starts broadcasting to listeners
4. Click **Go Off Air** to stop broadcasting

#### Media Library

At the bottom is a searchable media library. Find tracks and load them onto either deck:
- Click **A** to load onto Deck A
- Click **B** to load onto Deck B
- Double-click a track to load it onto Deck A
- You can also drag tracks from the library to a deck

### Keyboard Shortcuts

- **Q** — Play/pause Deck A
- **W** — Play/pause Deck B
- **1-4** — Hot cues 1-4 on Deck A
- **5-8** — Hot cues 1-4 on Deck B

---

## Recordings

**Where:** `/dashboard/recordings`
**Sidebar icon:** Circle (record)

The recordings page lets you record what's being broadcast and manage those recordings.

### Starting a Recording

Click **Start Recording** to begin capturing the station's output. The recording saves as either FLAC (lossless) or Opus (compressed), depending on your station settings.

### Managing Recordings

Each recording shows:
- Date and time
- Duration
- Which DJ was live (if applicable)
- File size

You can:
- **Play** — Listen to the recording right in the browser
- **Download** — Save the file to your computer
- **Change visibility** — Make it public (appears in the public archive), private, or use the station default
- **Delete** — Remove the recording permanently

### Auto-Recording

Stations can be configured to automatically start recording whenever a DJ goes live. This is set up in [Station Settings](#station-settings).

---

## Webstreams

**Where:** `/dashboard/webstreams`
**Sidebar icon:** Globe

Webstreams let you relay external audio streams through your station. For example, you could relay a network news feed, a partner station, or a podcast stream.

### Creating a Webstream

1. Click **New Webstream**
2. Enter a name
3. Enter the **stream URL** (the HTTP/HTTPS address of the audio stream)
4. Optionally enter a **failover URL** — a backup stream to switch to if the primary goes down
5. Save

### Using Webstreams

Once created, webstreams appear as a source option when adding entries to the [Schedule](#schedule). When a webstream is scheduled, Grimnir Radio connects to the external URL and relays that audio to your listeners.

### Health Monitoring

The system periodically checks if your webstream URLs are still working. If a stream goes down and you have a failover URL configured, it switches automatically.

You can also manually trigger:
- **Switch to failover** — Force-switch to the backup URL
- **Reset to primary** — Switch back to the main URL

---

## Analytics

**Where:** `/dashboard/analytics`
**Sidebar icon:** Bar chart

Analytics shows you how your station is performing.

### What You Can See

- **Current listeners** — How many people are tuned in right now
- **Peak listeners** — The highest listener count for today, this week, and all time
- **Listener trend** — A line chart showing listener counts over time. You can adjust the time range
- **Play history** — A log of every track that played, with timestamps and duration
- **Top tracks (Spins)** — Which tracks have been played the most, filterable by time period (today, this week, this month)

### Exporting Data

You can export listener data as a CSV file for use in spreadsheets or reports.

---

## Station Users

**Where:** `/dashboard/station/users`
**Sidebar icon:** People

This is where you manage who has access to your station and what they can do.

### Viewing Users

The user list shows everyone who has access to this station, their role, and when they were added.

### Inviting Users

1. Click **Invite User**
2. Enter their email address
3. Choose their **role** (see [User Roles Explained](#user-roles-explained) below)
4. Optionally set a recording storage quota for this user
5. Send the invite

### Editing User Roles

Click **Edit** next to a user to change their role or permissions. You can also set custom permission overrides for individual users.

### Removing Users

Click **Remove** to revoke a user's access to this station. This doesn't delete their account — it just removes them from this station.

---

## Station Settings

**Where:** `/dashboard/station/settings`
**Sidebar icon:** Gear

Station settings control how your station operates.

### General Settings

- **Station name** — The display name for your station
- **Description** — A short description
- **Timezone** — What timezone your schedule operates in
- **Genre** — Your station's primary genre
- **Website URL** — Your station's website
- **Contact email** — Public contact address
- **Social links** — Twitter, Facebook, Instagram, etc.

### Broadcasting Settings

- **Schedule boundary mode:**
  - **Hard** — Entries cut off exactly at their end time
  - **Soft** — Entries can overrun slightly into the next slot (configurable duration)
- **Crossfade** — Enable/disable crossfading between tracks and set the crossfade duration

### Recording Settings

- **Auto-record** — Automatically start recording when a DJ goes live
- **Format** — FLAC (lossless) or Opus (compressed)
- **Storage quota** — Maximum total recording storage (0 = disabled, -1 = unlimited)
- **Per-DJ quota** — Storage limit per individual DJ

### Archive Defaults

- **Show in archive** — Whether recordings appear in the public archive by default
- **Allow download** — Whether listeners can download recordings by default

### Emergency Controls

The settings page also has a **Stop Playout** button for emergencies. This immediately stops all automated playback on the station.

---

## Landing Page Editor

**Where:** `/dashboard/station/landing-page/editor`
**Sidebar icon:** Window

Each station can have a custom public landing page that listeners see when they visit your station's URL.

### Editing Your Landing Page

The editor lets you customize:

- **Theme** — Choose from built-in themes (DAW Dark, Clean Light, Broadcast, Classic, SM Theme)
- **Hero section** — Background image and tagline
- **Now Playing widget** — Shows what's currently on air
- **Schedule widget** — Displays upcoming shows
- **Player** — A listen button for your stream
- **About section** — Tell visitors about your station
- **Social links** — Links to your social media
- **Custom CSS** — Advanced users can add their own CSS styles

### Assets

Upload images (logos, backgrounds, photos) to use on your landing page. These are managed in the asset library within the editor.

### Draft and Publish

Changes you make are saved as a **draft** first. They're not visible to the public until you click **Publish**. You can also:

- **Preview** — See what your changes look like before publishing
- **Discard** — Throw away your draft and revert to the published version
- **Version history** — See previous versions and restore an older one if needed

---

## Station Logs & Audit

### Station Logs

**Where:** `/dashboard/station/logs`
**Sidebar icon:** Terminal

Shows event logs for your station — things like playback events, DJ connections, schedule changes, and errors.

### Audit Logs

**Where:** `/dashboard/station/audit`
**Sidebar icon:** Shield

The audit log records important actions that users took on this station — who changed what and when. This is useful for accountability and troubleshooting.

---

## Your Profile

**Where:** `/dashboard/profile`

Your profile page lets you manage your account.

### What You Can Change

- **Display name** — How your name appears in the system
- **Email** — Your login email
- **Password** — Change your password
- **Calendar color theme** — Choose how schedule entries are colored in the calendar. Options: Default, Ocean, Forest, Sunset, Berry, Earth, Neon, Pastel
- **Logout all devices** — Sign out everywhere (useful if you think someone else has access)

### API Keys

If you need programmatic access to the Grimnir Radio API, you can generate API keys here. Each key can be individually revoked.

---

## Platform Admin

These sections are only visible to users with the **Platform Admin** role. They manage the entire Grimnir Radio installation, not just one station.

### All Stations

**Where:** `/dashboard/admin/stations`

View and manage every station on the platform:
- **Approve/unapprove** stations for broadcasting
- **Enable/disable** stations
- **Make public/private**
- **Delete** stations

### Platform Landing Page

**Where:** `/dashboard/admin/landing-page/editor`

Edit the main landing page that visitors see at the root URL (`/`). This showcases the platform and lists featured stations.

### All Users

**Where:** `/dashboard/admin/users`

Manage all user accounts across the platform:
- Change platform roles (admin, moderator, regular user)
- Suspend or unsuspend accounts
- Reset passwords
- Delete accounts

### All Media

**Where:** `/dashboard/admin/media`

Browse media across all stations. Useful for:
- Finding and removing duplicate files across stations
- Moving media between stations
- Cleaning up orphaned files

### System Logs

**Where:** `/dashboard/admin/logs`

View system-level logs from all components. Filter by component to find specific issues.

### Platform Audit Logs

**Where:** `/dashboard/admin/audit`

Platform-wide audit trail showing all sensitive operations across all stations.

### Integrity Check

**Where:** `/dashboard/admin/integrity`

Runs data integrity checks on the database and media storage. Finds problems like:
- Media files referenced in the database but missing from disk
- Files on disk not tracked in the database
- Broken relationships between records

---

## Platform Settings

**Where:** `/dashboard/settings`
**Sidebar icon:** Sliders

Platform-wide configuration and tools.

### Migration / Import Tools

**Where:** `/dashboard/settings/migrations`

Import data from other broadcast automation systems:

- **AzuraCast** — Import stations, media, playlists, smart blocks, shows, and schedules
- **LibreTime** — Import from LibreTime installations

The import process works in stages:
1. **Connect** — Enter the source system's API URL and credentials
2. **Test** — Verify the connection works
3. **Import** — Pull the data into a staging area
4. **Review** — Look over what was imported and select what to keep
5. **Commit** — Finalize the import into your station

You can also **rollback** a committed import if something went wrong, or **redo** it.

### Orphan Media Manager

**Where:** `/dashboard/settings/orphans`

Finds media files that exist on disk but aren't tracked in the database, or vice versa. You can:
- **Adopt** orphan files — Create database records for them
- **Delete** orphan files — Remove them from disk
- **Bulk operations** — Handle multiple orphans at once

---

## Public Pages

These pages are visible to anyone — no login required.

### Home Page (`/`)

The platform landing page. Shows featured stations, a description of the platform, and links to listen.

### Listen Page (`/listen`)

Lists all public stations with a built-in audio player. Visitors can click to start listening to any station.

### Public Schedule (`/schedule`)

Shows the upcoming schedule for a station. Visitors can see what's playing now and what's coming up.

### Public Archive (`/archive`)

Browse recordings and archived media that stations have made public. Visitors can listen to and (if allowed) download archived content.

### Station Pages (`/s/{shortcode}`)

Each station can have a custom public page at a short URL. This shows the station's landing page with branding, now-playing info, schedule, and a player.

### Embeddable Widgets

Stations can embed widgets on external websites:

- **Now Playing widget** (`/embed/now-playing`) — Shows what's currently playing
- **Schedule widget** (`/embed/schedule`) — Shows upcoming scheduled items

Both support light and dark themes.

---

## Keyboard Shortcuts

### WebDJ Console
| Key | Action |
|-----|--------|
| Q | Play/Pause Deck A |
| W | Play/Pause Deck B |
| 1-4 | Hot Cues 1-4 on Deck A |
| 5-8 | Hot Cues 1-4 on Deck B |

Note: Keyboard shortcuts only work when the session is active and you're not typing in a text field.

---

## User Roles Explained

### Platform Roles

These apply across the entire Grimnir Radio installation:

| Role | What You Can Do |
|------|-----------------|
| **Admin** | Everything. Manage all stations, all users, platform settings, system configuration |
| **Moderator** | Moderate content, approve stations, manage some users |
| **User** | Regular account. Can create and manage your own stations |

### Station Roles

These apply within a specific station:

| Role | What You Can Do |
|------|-----------------|
| **Owner** | Full control over the station. Can delete the station, manage all settings and users |
| **Admin** | Manage station settings and users. Cannot delete the station |
| **Manager** | Manage content — media, playlists, smart blocks, clocks, schedule, webstreams, analytics, recordings. Cannot change station settings or manage users |
| **DJ** | Upload media, edit track metadata, go live, record. Cannot manage playlists, schedule, or other content |
| **Viewer** | Read-only access. Can see the dashboard but cannot make changes |

### What Each Role Can Access

| Feature | Owner | Admin | Manager | DJ | Viewer |
|---------|:-----:|:-----:|:-------:|:--:|:------:|
| View Dashboard | Yes | Yes | Yes | Yes | Yes |
| Upload Media | Yes | Yes | Yes | Yes | No |
| Delete Media | Yes | Yes | Yes | No | No |
| Edit Track Metadata | Yes | Yes | Yes | Yes | No |
| Manage Playlists | Yes | Yes | Yes | No | No |
| Manage Smart Blocks | Yes | Yes | Yes | No | No |
| Manage Clock Templates | Yes | Yes | Yes | No | No |
| Manage Schedule | Yes | Yes | Yes | No | No |
| Go Live (DJ) | Yes | Yes | Yes | Yes | No |
| Record | Yes | Yes | Yes | Yes | No |
| Manage Recordings | Yes | Yes | Yes | No | No |
| Manage Webstreams | Yes | Yes | Yes | No | No |
| View Analytics | Yes | Yes | Yes | No | No |
| Manage Station Users | Yes | Yes | No | No | No |
| Change Station Settings | Yes | Yes | No | No | No |
| Edit Landing Page | Yes | Yes | Yes | No | No |
| Manage Mounts | Yes | Yes | No | No | No |
| Kick DJ Off Air | Yes | Yes | No | No | No |
| View Station Logs | Yes | Yes | Yes | Yes | No |
| View Audit Logs | Yes | Yes | Yes | No | No |
