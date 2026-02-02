#!/usr/bin/env python3
"""
Grimnir Radio API Client

A Python client library for interacting with the Grimnir Radio API.

Usage:
    from grimnir_client import GrimnirClient

    # Initialize with your API key (get it from your profile page)
    client = GrimnirClient("https://your-instance.com", api_key="gr_your-api-key")

    # Get stations
    stations = client.get_stations()

    # Get now playing
    now_playing = client.get_now_playing(station_id)

Requirements:
    pip install requests

License: AGPL-3.0
"""

import requests
from datetime import datetime
from typing import Optional, List, Dict, Any
from pathlib import Path


class GrimnirClient:
    """Client for the Grimnir Radio API."""

    def __init__(self, base_url: str, api_key: Optional[str] = None, timeout: int = 30):
        """
        Initialize the client.

        Args:
            base_url: Base URL of the Grimnir Radio instance (e.g., "https://radio.example.com")
            api_key: API key for authentication (get from your profile page)
            timeout: Request timeout in seconds
        """
        self.base_url = base_url.rstrip("/")
        self.api_url = f"{self.base_url}/api/v1"
        self.timeout = timeout
        self.api_key = api_key
        self.session = requests.Session()

    def _headers(self) -> Dict[str, str]:
        """Get request headers with API key auth."""
        headers = {"Content-Type": "application/json"}
        if self.api_key:
            headers["X-API-Key"] = self.api_key
        return headers

    def _request(
        self,
        method: str,
        endpoint: str,
        data: Optional[Dict] = None,
        params: Optional[Dict] = None,
        files: Optional[Dict] = None,
    ) -> Dict[str, Any]:
        """Make an API request."""
        url = f"{self.api_url}{endpoint}"
        headers = self._headers()

        if files:
            # Remove Content-Type for multipart uploads
            headers.pop("Content-Type", None)

        response = self.session.request(
            method=method,
            url=url,
            json=data if not files else None,
            params=params,
            files=files,
            headers=headers,
            timeout=self.timeout,
        )

        if response.status_code >= 400:
            raise GrimnirAPIError(
                response.status_code,
                response.text,
                endpoint,
            )

        if response.content:
            return response.json()
        return {}

    # =========================================================================
    # Stations
    # =========================================================================

    def get_stations(self) -> List[Dict[str, Any]]:
        """
        Get all stations the user has access to.

        Returns:
            List of station objects

        Example:
            >>> stations = client.get_stations()
            >>> for s in stations:
            ...     print(f"{s['name']} ({s['id']})")
        """
        response = self._request("GET", "/stations")
        return response.get("stations", [])

    def get_public_stations(self) -> List[Dict[str, Any]]:
        """
        Get all public stations (no auth required).

        Returns:
            List of public station objects
        """
        response = self._request("GET", "/public/stations")
        return response.get("stations", [])

    def get_station(self, station_id: str) -> Dict[str, Any]:
        """
        Get details of a specific station.

        Args:
            station_id: Station UUID

        Returns:
            Station object
        """
        return self._request("GET", f"/stations/{station_id}")

    def get_station_mounts(self, station_id: str) -> List[Dict[str, Any]]:
        """
        Get stream mounts for a station.

        Args:
            station_id: Station UUID

        Returns:
            List of mount objects
        """
        response = self._request("GET", f"/stations/{station_id}/mounts")
        return response.get("mounts", [])

    # =========================================================================
    # Media
    # =========================================================================

    def upload_media(
        self,
        station_id: str,
        file_path: str,
    ) -> Dict[str, Any]:
        """
        Upload an audio file to the media library.

        Args:
            station_id: Target station UUID
            file_path: Path to audio file

        Returns:
            Created media item

        Example:
            >>> media = client.upload_media(station_id, "/path/to/song.mp3")
            >>> print(f"Uploaded: {media['title']} by {media['artist']}")
        """
        path = Path(file_path)
        with open(path, "rb") as f:
            files = {"file": (path.name, f, "audio/mpeg")}
            return self._request(
                "POST",
                "/media/upload",
                files=files,
                params={"station_id": station_id},
            )

    def get_media(self, media_id: str) -> Dict[str, Any]:
        """
        Get details of a media item.

        Args:
            media_id: Media item UUID

        Returns:
            Media item object
        """
        return self._request("GET", f"/media/{media_id}")

    # =========================================================================
    # Playlists
    # =========================================================================

    def get_playlists(self, station_id: str) -> List[Dict[str, Any]]:
        """
        Get all playlists for a station.

        Args:
            station_id: Station UUID

        Returns:
            List of playlist objects
        """
        response = self._request(
            "GET", "/playlists", params={"station_id": station_id}
        )
        return response.get("playlists", [])

    # =========================================================================
    # Smart Blocks
    # =========================================================================

    def get_smart_blocks(self, station_id: str) -> List[Dict[str, Any]]:
        """
        Get all smart blocks for a station.

        Args:
            station_id: Station UUID

        Returns:
            List of smart block objects
        """
        response = self._request(
            "GET", "/smart-blocks", params={"station_id": station_id}
        )
        return response.get("smart_blocks", [])

    def create_smart_block(
        self,
        station_id: str,
        name: str,
        rules: List[Dict[str, Any]],
        limit: int = 10,
        sort_by: str = "random",
        description: str = "",
    ) -> Dict[str, Any]:
        """
        Create a new smart block.

        Args:
            station_id: Station UUID
            name: Block name
            rules: List of rule objects with field, operator, value
            limit: Max tracks to generate
            sort_by: Sort order (random, newest, oldest, title, artist)
            description: Optional description

        Returns:
            Created smart block

        Example:
            >>> block = client.create_smart_block(
            ...     station_id=station_id,
            ...     name="Rock Music",
            ...     rules=[
            ...         {"field": "genre", "operator": "equals", "value": "Rock"},
            ...         {"field": "bpm", "operator": "between", "value": "100", "value2": "140"},
            ...     ],
            ...     limit=20,
            ...     sort_by="random",
            ... )
        """
        return self._request(
            "POST",
            "/smart-blocks",
            data={
                "station_id": station_id,
                "name": name,
                "description": description,
                "rules": rules,
                "limit": limit,
                "sort_by": sort_by,
            },
        )

    def materialize_smart_block(
        self, block_id: str, limit: int = 10
    ) -> List[Dict[str, Any]]:
        """
        Generate tracks from a smart block.

        Args:
            block_id: Smart block UUID
            limit: Max tracks to generate

        Returns:
            List of media items
        """
        response = self._request(
            "POST",
            f"/smart-blocks/{block_id}/materialize",
            data={"limit": limit},
        )
        return response.get("tracks", [])

    # =========================================================================
    # Schedule
    # =========================================================================

    def get_schedule(
        self, station_id: str, hours: int = 24
    ) -> List[Dict[str, Any]]:
        """
        Get upcoming schedule entries.

        Args:
            station_id: Station UUID
            hours: Hours ahead to fetch

        Returns:
            List of schedule entries

        Example:
            >>> schedule = client.get_schedule(station_id, hours=48)
            >>> for entry in schedule:
            ...     print(f"{entry['start_time']}: {entry['title']}")
        """
        response = self._request(
            "GET",
            "/schedule",
            params={"station_id": station_id, "hours": hours},
        )
        return response.get("entries", [])

    def refresh_schedule(self, station_id: str) -> Dict[str, Any]:
        """
        Force regeneration of the schedule.

        Args:
            station_id: Station UUID
        """
        return self._request(
            "POST",
            "/schedule/refresh",
            data={"station_id": station_id},
        )

    # =========================================================================
    # Live DJ
    # =========================================================================

    def generate_live_token(
        self, station_id: str, mount_id: str
    ) -> Dict[str, Any]:
        """
        Generate a token for live DJ streaming.

        Args:
            station_id: Station UUID
            mount_id: Mount UUID

        Returns:
            Token info with token string and expiry

        Example:
            >>> token_info = client.generate_live_token(station_id, mount_id)
            >>> print(f"Stream to: {base_url}/live/{token_info['token']}")
        """
        return self._request(
            "POST",
            "/live/tokens",
            data={"station_id": station_id, "mount_id": mount_id},
        )

    def get_live_sessions(
        self, station_id: Optional[str] = None
    ) -> List[Dict[str, Any]]:
        """
        Get active live DJ sessions.

        Args:
            station_id: Optional station filter

        Returns:
            List of live session objects
        """
        params = {}
        if station_id:
            params["station_id"] = station_id
        response = self._request("GET", "/live/sessions", params=params)
        return response.get("sessions", [])

    # =========================================================================
    # Webstreams
    # =========================================================================

    def get_webstreams(self, station_id: str) -> List[Dict[str, Any]]:
        """
        Get webstream relays for a station.

        Args:
            station_id: Station UUID

        Returns:
            List of webstream objects
        """
        response = self._request(
            "GET", "/webstreams", params={"station_id": station_id}
        )
        return response.get("webstreams", [])

    def create_webstream(
        self,
        station_id: str,
        name: str,
        url: str,
        format: str,
        fallback_url: Optional[str] = None,
    ) -> Dict[str, Any]:
        """
        Create a webstream relay.

        Args:
            station_id: Station UUID
            name: Stream name
            url: Primary stream URL
            format: Audio format (mp3, ogg, aac)
            fallback_url: Optional fallback URL

        Returns:
            Created webstream object

        Example:
            >>> webstream = client.create_webstream(
            ...     station_id=station_id,
            ...     name="News Feed",
            ...     url="https://news.example.com/live.mp3",
            ...     format="mp3",
            ... )
        """
        data = {
            "station_id": station_id,
            "name": name,
            "url": url,
            "format": format,
        }
        if fallback_url:
            data["fallback_url"] = fallback_url
        return self._request("POST", "/webstreams", data=data)

    # =========================================================================
    # Playout Control
    # =========================================================================

    def skip_track(self, station_id: str) -> Dict[str, Any]:
        """
        Skip the currently playing track.

        Args:
            station_id: Station UUID
        """
        return self._request(
            "POST", "/playout/skip", data={"station_id": station_id}
        )

    def stop_playout(self, station_id: str) -> Dict[str, Any]:
        """
        Stop all playout for a station.

        Args:
            station_id: Station UUID
        """
        return self._request(
            "POST", "/playout/stop", data={"station_id": station_id}
        )

    # =========================================================================
    # Analytics
    # =========================================================================

    def get_now_playing(
        self, station_id: Optional[str] = None
    ) -> Dict[str, Any]:
        """
        Get currently playing track info.

        Args:
            station_id: Optional station filter

        Returns:
            Now playing info

        Example:
            >>> np = client.get_now_playing(station_id)
            >>> print(f"Now Playing: {np['title']} by {np['artist']}")
        """
        params = {}
        if station_id:
            params["station_id"] = station_id
        return self._request("GET", "/analytics/now-playing", params=params)

    def get_listeners(self, station_id: Optional[str] = None) -> Dict[str, Any]:
        """
        Get current listener count.

        Args:
            station_id: Optional station filter

        Returns:
            Listener stats
        """
        params = {}
        if station_id:
            params["station_id"] = station_id
        return self._request("GET", "/analytics/listeners", params=params)

    def get_spins(
        self,
        station_id: str,
        since: Optional[datetime] = None,
        limit: int = 100,
    ) -> List[Dict[str, Any]]:
        """
        Get track play history.

        Args:
            station_id: Station UUID
            since: Optional start time filter
            limit: Max results

        Returns:
            List of spin records
        """
        params = {"station_id": station_id, "limit": limit}
        if since:
            params["since"] = since.isoformat()
        response = self._request("GET", "/analytics/spins", params=params)
        return response.get("spins", [])

    # =========================================================================
    # Logs (Station-level)
    # =========================================================================

    def get_station_logs(
        self,
        station_id: str,
        level: Optional[str] = None,
        component: Optional[str] = None,
        search: Optional[str] = None,
        limit: int = 500,
    ) -> Dict[str, Any]:
        """
        Get logs for a specific station.

        Args:
            station_id: Station UUID
            level: Filter by level (debug, info, warn, error)
            component: Filter by component
            search: Search in messages
            limit: Max entries

        Returns:
            Log response with entries and count
        """
        params = {"limit": limit}
        if level:
            params["level"] = level
        if component:
            params["component"] = component
        if search:
            params["search"] = search
        return self._request(
            "GET", f"/stations/{station_id}/logs", params=params
        )

    # =========================================================================
    # Shows (Phase 8)
    # =========================================================================

    def get_shows(self, station_id: str) -> List[Dict[str, Any]]:
        """
        Get all shows for a station.

        Args:
            station_id: Station UUID

        Returns:
            List of show objects
        """
        response = self._request("GET", "/shows", params={"station_id": station_id})
        return response.get("shows", [])

    def create_show(
        self,
        station_id: str,
        name: str,
        rrule: str,
        dtstart: str,
        duration_minutes: int = 60,
        description: str = "",
        color: str = "#3B82F6",
    ) -> Dict[str, Any]:
        """
        Create a recurring show.

        Args:
            station_id: Station UUID
            name: Show name
            rrule: RFC 5545 recurrence rule (e.g., "FREQ=WEEKLY;BYDAY=MO")
            dtstart: Start datetime ISO format
            duration_minutes: Show duration
            description: Optional description
            color: Hex color for calendar

        Returns:
            Created show object

        Example:
            >>> show = client.create_show(
            ...     station_id=station_id,
            ...     name="Morning Jazz",
            ...     rrule="FREQ=WEEKLY;BYDAY=MO,WE,FR",
            ...     dtstart="2026-02-01T08:00:00Z",
            ...     duration_minutes=120,
            ... )
        """
        return self._request(
            "POST",
            "/shows",
            data={
                "station_id": station_id,
                "name": name,
                "rrule": rrule,
                "dtstart": dtstart,
                "default_duration_minutes": duration_minutes,
                "description": description,
                "color": color,
            },
        )

    def get_show_instances(
        self,
        station_id: str,
        start: str,
        end: str,
    ) -> List[Dict[str, Any]]:
        """
        Get show instances for a date range.

        Args:
            station_id: Station UUID
            start: Start date ISO format
            end: End date ISO format

        Returns:
            List of show instance objects
        """
        response = self._request(
            "GET",
            "/show-instances",
            params={"station_id": station_id, "start": start, "end": end},
        )
        return response.get("instances", [])

    # =========================================================================
    # Schedule Analytics (Phase 8)
    # =========================================================================

    def get_show_performance(
        self,
        station_id: str,
        start: Optional[str] = None,
        end: Optional[str] = None,
    ) -> Dict[str, Any]:
        """
        Get show performance analytics.

        Args:
            station_id: Station UUID
            start: Start date (default: 30 days ago)
            end: End date (default: today)

        Returns:
            Performance metrics by show
        """
        params = {"station_id": station_id}
        if start:
            params["start"] = start
        if end:
            params["end"] = end
        return self._request("GET", "/schedule-analytics/shows", params=params)

    def get_best_time_slots(
        self, station_id: str, limit: int = 10
    ) -> Dict[str, Any]:
        """
        Get best performing time slots.

        Args:
            station_id: Station UUID
            limit: Number of slots to return

        Returns:
            Best time slots with metrics
        """
        return self._request(
            "GET",
            "/schedule-analytics/best-slots",
            params={"station_id": station_id, "limit": limit},
        )

    def get_scheduling_suggestions(self, station_id: str) -> Dict[str, Any]:
        """
        Get data-driven scheduling suggestions.

        Args:
            station_id: Station UUID

        Returns:
            Scheduling suggestions
        """
        return self._request(
            "GET",
            "/schedule-analytics/suggestions",
            params={"station_id": station_id},
        )

    # =========================================================================
    # Public Schedule (Phase 8)
    # =========================================================================

    def get_public_schedule(
        self,
        station_id: str,
        start: Optional[str] = None,
        end: Optional[str] = None,
    ) -> Dict[str, Any]:
        """
        Get public schedule (no auth required).

        Args:
            station_id: Station UUID
            start: Start date
            end: End date

        Returns:
            Public schedule data
        """
        params = {"station_id": station_id}
        if start:
            params["start"] = start
        if end:
            params["end"] = end
        return self._request("GET", "/public/schedule", params=params)

    def get_public_now_playing(self, station_id: str) -> Dict[str, Any]:
        """
        Get current and next show (no auth required).

        Args:
            station_id: Station UUID

        Returns:
            Now playing with current and next show
        """
        return self._request(
            "GET",
            "/public/now-playing",
            params={"station_id": station_id},
        )

    # =========================================================================
    # Syndication (Phase 8)
    # =========================================================================

    def get_networks(self, owner_id: Optional[str] = None) -> List[Dict[str, Any]]:
        """
        Get syndication networks.

        Args:
            owner_id: Optional owner filter

        Returns:
            List of network objects
        """
        params = {}
        if owner_id:
            params["owner_id"] = owner_id
        response = self._request("GET", "/networks", params=params)
        return response.get("networks", [])

    def get_network_shows(
        self, network_id: Optional[str] = None
    ) -> List[Dict[str, Any]]:
        """
        Get network shows available for syndication.

        Args:
            network_id: Optional network filter

        Returns:
            List of network show objects
        """
        params = {}
        if network_id:
            params["network_id"] = network_id
        response = self._request("GET", "/network-shows", params=params)
        return response.get("shows", [])

    def subscribe_to_network_show(
        self,
        station_id: str,
        network_show_id: str,
        local_time: str,
        local_days: str,
        timezone: str = "UTC",
    ) -> Dict[str, Any]:
        """
        Subscribe station to a network show.

        Args:
            station_id: Station UUID
            network_show_id: Network show UUID
            local_time: Local broadcast time (HH:MM)
            local_days: Days to broadcast (e.g., "MO,WE,FR")
            timezone: Station timezone

        Returns:
            Subscription object
        """
        return self._request(
            "POST",
            "/network-subscriptions",
            data={
                "station_id": station_id,
                "network_show_id": network_show_id,
                "local_time": local_time,
                "local_days": local_days,
                "timezone": timezone,
            },
        )

    # =========================================================================
    # Underwriting (Phase 8)
    # =========================================================================

    def get_sponsors(self, station_id: str) -> List[Dict[str, Any]]:
        """
        Get sponsors for a station.

        Args:
            station_id: Station UUID

        Returns:
            List of sponsor objects
        """
        response = self._request(
            "GET", "/sponsors", params={"station_id": station_id}
        )
        return response.get("sponsors", [])

    def create_sponsor(
        self,
        station_id: str,
        name: str,
        contact_info: Optional[Dict[str, str]] = None,
    ) -> Dict[str, Any]:
        """
        Create a sponsor.

        Args:
            station_id: Station UUID
            name: Sponsor name
            contact_info: Optional contact details

        Returns:
            Created sponsor object
        """
        data = {"station_id": station_id, "name": name}
        if contact_info:
            data["contact_info"] = contact_info
        return self._request("POST", "/sponsors", data=data)

    def get_underwriting_fulfillment(
        self,
        station_id: str,
        start: Optional[str] = None,
        end: Optional[str] = None,
    ) -> Dict[str, Any]:
        """
        Get underwriting fulfillment report.

        Args:
            station_id: Station UUID
            start: Start date
            end: End date

        Returns:
            Fulfillment report with obligations and spots
        """
        params = {"station_id": station_id}
        if start:
            params["start"] = start
        if end:
            params["end"] = end
        return self._request("GET", "/underwriting/fulfillment", params=params)

    # =========================================================================
    # Schedule Export (Phase 8)
    # =========================================================================

    def export_schedule_ical(
        self,
        station_id: str,
        start: Optional[str] = None,
        end: Optional[str] = None,
    ) -> str:
        """
        Export schedule as iCal format.

        Args:
            station_id: Station UUID
            start: Start date
            end: End date

        Returns:
            iCal string (text/calendar)
        """
        params = {"station_id": station_id, "format": "ical"}
        if start:
            params["start"] = start
        if end:
            params["end"] = end
        # This returns text, not JSON
        url = f"{self.api_url}/schedule/export"
        response = self.session.get(
            url, params=params, headers=self._headers(), timeout=self.timeout
        )
        return response.text

    # =========================================================================
    # System (Platform Admin only)
    # =========================================================================

    def get_system_status(self) -> Dict[str, Any]:
        """
        Get system health status (Platform Admin only).

        Returns:
            System status object
        """
        return self._request("GET", "/system/status")

    def get_system_logs(
        self,
        level: Optional[str] = None,
        component: Optional[str] = None,
        search: Optional[str] = None,
        limit: int = 500,
    ) -> Dict[str, Any]:
        """
        Get system logs (Platform Admin only).

        Args:
            level: Filter by level
            component: Filter by component
            search: Search in messages
            limit: Max entries

        Returns:
            Log response with entries, count, and station_names mapping
        """
        params = {"limit": limit}
        if level:
            params["level"] = level
        if component:
            params["component"] = component
        if search:
            params["search"] = search
        return self._request("GET", "/system/logs", params=params)


class GrimnirAPIError(Exception):
    """Exception raised for API errors."""

    def __init__(self, status_code: int, message: str, endpoint: str):
        self.status_code = status_code
        self.message = message
        self.endpoint = endpoint
        super().__init__(f"API Error {status_code} on {endpoint}: {message}")


# =============================================================================
# Example Usage
# =============================================================================

if __name__ == "__main__":
    # Example usage
    import os

    # Get API key from environment
    BASE_URL = os.getenv("GRIMNIR_URL", "http://localhost:8080")
    API_KEY = os.getenv("GRIMNIR_API_KEY")

    if not API_KEY:
        print("Error: Set GRIMNIR_API_KEY environment variable")
        print("Generate an API key from your profile page in the web dashboard")
        exit(1)

    # Create client with API key
    client = GrimnirClient(BASE_URL, api_key=API_KEY)

    try:
        # Get stations
        print("Stations:")
        stations = client.get_stations()
        for station in stations:
            print(f"  - {station['name']} ({station['id']})")

        if stations:
            station_id = stations[0]["id"]

            # Get now playing
            print("\nNow Playing:")
            np = client.get_now_playing(station_id)
            if np.get("title"):
                print(f"  {np['title']} by {np.get('artist', 'Unknown')}")
            else:
                print("  Nothing playing")

            # Get schedule
            print("\nUpcoming Schedule:")
            schedule = client.get_schedule(station_id, hours=4)
            for entry in schedule[:5]:
                print(f"  {entry['start_time']}: {entry.get('title', 'Unknown')}")

    except GrimnirAPIError as e:
        print(f"API Error: {e}")
    except Exception as e:
        print(f"Error: {e}")
