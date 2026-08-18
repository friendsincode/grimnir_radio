/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func mustUUID(t *testing.T, id string) {
	t.Helper()
	if _, err := uuid.Parse(id); err != nil {
		t.Fatalf("id %q is not a valid uuid: %v", id, err)
	}
}

func TestIsValidSchedulerLookahead(t *testing.T) {
	for _, v := range []string{"24h", "48h", "72h", "168h"} {
		if !IsValidSchedulerLookahead(v) {
			t.Errorf("IsValidSchedulerLookahead(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"12h", "1h", "", "168"} {
		if IsValidSchedulerLookahead(v) {
			t.Errorf("IsValidSchedulerLookahead(%q) = true, want false", v)
		}
	}
}

func TestIsValidLogLevel(t *testing.T) {
	for _, v := range []string{"debug", "info", "warn", "error"} {
		if !IsValidLogLevel(v) {
			t.Errorf("IsValidLogLevel(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"trace", "fatal", "", "INFO"} {
		if IsValidLogLevel(v) {
			t.Errorf("IsValidLogLevel(%q) = true, want false", v)
		}
	}
}

// TestConstructors_SetDefaultsAndIDs checks each New* constructor stamps a valid
// uuid, carries its arguments through, and applies the documented defaults
// (Active true, spot duration 30s, deck/mixer levels). Several of these were the
// dead constructors flagged in the earlier audit, so guarding their invariants
// matters before any caller starts using them.
func TestConstructors_SetDefaultsAndIDs(t *testing.T) {
	net := NewNetwork("Indie")
	mustUUID(t, net.ID)
	if net.Name != "Indie" || !net.Active {
		t.Errorf("NewNetwork = %+v, want Name=Indie Active=true", net)
	}

	show := NewNetworkShow("Morning")
	mustUUID(t, show.ID)
	if show.Name != "Morning" || !show.Active {
		t.Errorf("NewNetworkShow = %+v", show)
	}

	sub := NewNetworkSubscription("st1", "sh1")
	mustUUID(t, sub.ID)
	if sub.StationID != "st1" || sub.NetworkShowID != "sh1" || !sub.Active {
		t.Errorf("NewNetworkSubscription = %+v", sub)
	}

	wh := NewWebhookTarget("st1", "https://x/y", "media.created")
	mustUUID(t, wh.ID)
	if wh.URL != "https://x/y" || wh.Events != "media.created" || wh.Secret == "" || !wh.Active {
		t.Errorf("NewWebhookTarget = %+v", wh)
	}

	sp := NewSponsor("st1", "Acme")
	mustUUID(t, sp.ID)
	if sp.Name != "Acme" || !sp.Active {
		t.Errorf("NewSponsor = %+v", sp)
	}

	ob := NewUnderwritingObligation("sp1", "st1", 5)
	mustUUID(t, ob.ID)
	if ob.SpotsPerWeek != 5 || ob.SpotDurationSeconds != 30 || !ob.Active {
		t.Errorf("NewUnderwritingObligation = %+v, want SpotsPerWeek=5 SpotDurationSeconds=30 Active=true", ob)
	}

	when := time.Now()
	spot := NewUnderwritingSpot("ob1", when)
	mustUUID(t, spot.ID)
	if spot.ObligationID != "ob1" || spot.Status != SpotStatusScheduled || !spot.ScheduledAt.Equal(when) {
		t.Errorf("NewUnderwritingSpot = %+v", spot)
	}

	// Two constructions must produce distinct ids.
	if NewNetwork("a").ID == NewNetwork("b").ID {
		t.Error("NewNetwork produced duplicate ids")
	}

	deck := NewDeckState()
	if deck.Volume != 1.0 || deck.State != string(DeckStateIdle) || deck.HotCues == nil {
		t.Errorf("NewDeckState = %+v, want Volume=1 State=idle non-nil HotCues", deck)
	}

	mix := NewMixerState()
	if mix.Crossfader != 0.5 || mix.MasterVolume != 1.0 || mix.TalkoverDuck != 0.7 {
		t.Errorf("NewMixerState = %+v", mix)
	}
}

// TestStagedImportCounts checks the count aggregates over staged media flags.
func TestStagedImportCounts(t *testing.T) {
	s := &StagedImport{
		StagedMedia: []StagedMediaItem{
			{Selected: true, IsDuplicate: true},
			{Selected: true, OrphanMatch: true},
			{Selected: false},
		},
	}
	if got := s.TotalCount(); got != 3 {
		t.Errorf("TotalCount = %d, want 3", got)
	}
	if got := s.SelectedCount(); got != 2 {
		t.Errorf("SelectedCount = %d, want 2", got)
	}
	if got := s.DuplicateCount(); got != 1 {
		t.Errorf("DuplicateCount = %d, want 1", got)
	}
	if got := s.OrphanMatchCount(); got != 1 {
		t.Errorf("OrphanMatchCount = %d, want 1", got)
	}
}

func TestImportedItemsTotalCount(t *testing.T) {
	i := &ImportedItems{
		MediaIDs:     []string{"a", "b"},
		PlaylistIDs:  []string{"c"},
		WebstreamIDs: []string{"d", "e"},
	}
	if got := i.TotalCount(); got != 5 {
		t.Errorf("ImportedItems.TotalCount = %d, want 5", got)
	}
}
