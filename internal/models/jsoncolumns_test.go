/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"database/sql/driver"
	"reflect"
	"testing"
)

// roundTrip drives a jsonb column type through its driver.Valuer and sql.Scanner
// and asserts the value survives the trip unchanged. These types back real jsonb
// columns, so a broken Value/Scan silently loses or corrupts data.
func roundTrip(t *testing.T, name string, in driver.Valuer, out interface {
	Scan(any) error
}) {
	t.Helper()
	dv, err := in.Value()
	if err != nil {
		t.Fatalf("%s Value: %v", name, err)
	}
	if err := out.Scan(dv); err != nil {
		t.Fatalf("%s Scan: %v", name, err)
	}
	got := reflect.ValueOf(out).Elem().Interface()
	if !reflect.DeepEqual(in, got) {
		t.Errorf("%s round-trip mismatch:\n in =%#v\n out=%#v", name, in, got)
	}
}

func TestJSONColumns_RoundTrip(t *testing.T) {
	roundTrip(t, "StagedMediaItems", StagedMediaItems{{}}, &StagedMediaItems{})
	roundTrip(t, "StagedPlaylistItems", StagedPlaylistItems{{}}, &StagedPlaylistItems{})
	roundTrip(t, "StagedSmartBlockItems", StagedSmartBlockItems{{}}, &StagedSmartBlockItems{})
	roundTrip(t, "StagedShowItems", StagedShowItems{{}}, &StagedShowItems{})
	roundTrip(t, "StagedWebstreamItems", StagedWebstreamItems{{}}, &StagedWebstreamItems{})
	roundTrip(t, "ImportWarnings", ImportWarnings{{}}, &ImportWarnings{})
	roundTrip(t, "ImportSuggestions", ImportSuggestions{{}}, &ImportSuggestions{})
	roundTrip(t, "ImportSelections", ImportSelections{}, &ImportSelections{})
	roundTrip(t, "ImportedItems", ImportedItems{}, &ImportedItems{})
	roundTrip(t, "PlatformPermissions", PlatformPermissions{}, &PlatformPermissions{})
	roundTrip(t, "StationPermissions", StationPermissions{}, &StationPermissions{})
	roundTrip(t, "CuePointSet", CuePointSet{}, &CuePointSet{})
	// DeckState/MixerState use omitempty slice fields, so an empty slice
	// marshals away and scans back nil; their Value + Scan are covered by
	// TestDeckMixerScan_Defaults instead of a strict DeepEqual round-trip.
}

// TestDeckMixerScan_Defaults covers the nil and string-input Scan branches:
// a nil DB value yields the documented defaults, and a string payload decodes
// the same as a []byte payload.
func TestDeckMixerScan_Defaults(t *testing.T) {
	// Cover the Value side too (round-trip skips these two).
	if _, err := NewDeckState().Value(); err != nil {
		t.Fatalf("DeckState.Value: %v", err)
	}
	if _, err := NewMixerState().Value(); err != nil {
		t.Fatalf("MixerState.Value: %v", err)
	}

	var d DeckState
	if err := d.Scan(nil); err != nil {
		t.Fatalf("DeckState.Scan(nil): %v", err)
	}
	if d.State != "idle" || d.Volume != 1.0 {
		t.Errorf("DeckState nil default = %+v, want State=idle Volume=1", d)
	}

	var d2 DeckState
	if err := d2.Scan(`{"volume":0.5,"state":"playing"}`); err != nil {
		t.Fatalf("DeckState.Scan(string): %v", err)
	}
	if d2.Volume != 0.5 || d2.State != "playing" {
		t.Errorf("DeckState string scan = %+v", d2)
	}

	var m MixerState
	if err := m.Scan(nil); err != nil {
		t.Fatalf("MixerState.Scan(nil): %v", err)
	}
	if m.Crossfader != 0.5 || m.MasterVolume != 1.0 {
		t.Errorf("MixerState nil default = %+v", m)
	}
}
