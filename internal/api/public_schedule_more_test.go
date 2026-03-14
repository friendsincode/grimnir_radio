/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestPublicArtworkURL(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"", ""},
		{"  ", ""},
		{"https://example.com/art.png", "https://example.com/art.png"},
		{"http://example.com/art.jpg", "http://example.com/art.jpg"},
		{"/media/art.png", "/media/art.png"},
		{"relative/path.png", ""},
		{"s3://bucket/key.png", ""},
	}

	for _, c := range cases {
		got := publicArtworkURL(c.path)
		if got != c.want {
			t.Errorf("publicArtworkURL(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestPublicScheduleAPI_WithDateParams(t *testing.T) {
	a, db := newPublicScheduleTest(t)

	station := seedPublicStation(t, db, "st-dates", "Date Station")
	show := seedShowForPublic(t, db, "show-dates", station.ID, "Date Show")
	now := time.Now()
	seedShowInstanceForPublic(t, db, "inst-dates", show, now.Add(time.Hour), now.Add(2*time.Hour))

	// RFC3339 start/end params
	start := now.UTC().Format(time.RFC3339)
	end := now.Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	req := httptest.NewRequest("GET", "/?station_id=st-dates&start="+start+"&end="+end, nil)
	rr := httptest.NewRecorder()
	a.handlePublicSchedule(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("with RFC3339 dates: got %d, want 200", rr.Code)
	}

	// Date-only start/end params
	startDate := now.Format("2006-01-02")
	endDate := now.Add(3 * 24 * time.Hour).Format("2006-01-02")
	req = httptest.NewRequest("GET", "/?station_id=st-dates&start="+startDate+"&end="+endDate, nil)
	rr = httptest.NewRecorder()
	a.handlePublicSchedule(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("with date-only params: got %d, want 200", rr.Code)
	}

	// Range > 30 days is capped
	bigEnd := now.Add(60 * 24 * time.Hour).Format("2006-01-02")
	req = httptest.NewRequest("GET", "/?station_id=st-dates&start="+startDate+"&end="+bigEnd, nil)
	rr = httptest.NewRecorder()
	a.handlePublicSchedule(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("with capped range: got %d, want 200", rr.Code)
	}
}

func TestInstanceToPublic_WithHost(t *testing.T) {
	hostEmail := "host@example.com"
	host := &models.User{ID: "u-host", Email: hostEmail}
	show := &models.Show{
		ID:   "show-host",
		Name: "Hosted Show",
		Host: host,
	}
	now := time.Now()
	inst := &models.ShowInstance{
		ID:       "inst-host",
		ShowID:   show.ID,
		Show:     show,
		StartsAt: now,
		EndsAt:   now.Add(time.Hour),
		Status:   models.ShowInstanceScheduled,
	}

	pi := instanceToPublic(inst)
	if pi.Show.HostName != hostEmail {
		t.Fatalf("expected HostName=%q, got %q", hostEmail, pi.Show.HostName)
	}
}

func TestInstanceToPublic_WithDirectHost(t *testing.T) {
	hostEmail := "direct@example.com"
	host := &models.User{ID: "u-direct", Email: hostEmail}
	show := &models.Show{
		ID:   "show-direct",
		Name: "Direct Host Show",
	}
	now := time.Now()
	inst := &models.ShowInstance{
		ID:       "inst-direct",
		ShowID:   show.ID,
		Show:     show,
		Host:     host,
		StartsAt: now,
		EndsAt:   now.Add(time.Hour),
		Status:   models.ShowInstanceScheduled,
	}

	pi := instanceToPublic(inst)
	if pi.Show.HostName != hostEmail {
		t.Fatalf("expected HostName=%q, got %q", hostEmail, pi.Show.HostName)
	}
}
