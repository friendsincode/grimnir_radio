/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package logbuffer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

const testUUID = "11111111-2222-3333-4444-555555555555"

func entry(level, component, msg string, fields map[string]interface{}) LogEntry {
	return LogEntry{Timestamp: time.Now(), Level: level, Component: component, Message: msg, Fields: fields}
}

func TestNew_DefaultCapacityOnNonPositive(t *testing.T) {
	if b := New(0); b.capacity != 10000 {
		t.Fatalf("capacity for New(0) = %d, want 10000", b.capacity)
	}
	if b := New(-5); b.capacity != 10000 {
		t.Fatalf("capacity for New(-5) = %d, want 10000", b.capacity)
	}
	if b := New(7); b.capacity != 7 {
		t.Fatalf("capacity for New(7) = %d, want 7", b.capacity)
	}
}

func TestAdd_RingWraparoundKeepsNewest(t *testing.T) {
	b := New(3)
	for i := 0; i < 5; i++ {
		b.Add(entry("info", "c", fmt.Sprintf("m%d", i), nil))
	}
	all := b.GetAll()
	if len(all) != 3 {
		t.Fatalf("GetAll len = %d, want 3", len(all))
	}
	// Oldest two (m0, m1) evicted; remaining in chronological order.
	want := []string{"m2", "m3", "m4"}
	for i, w := range want {
		if all[i].Message != w {
			t.Fatalf("entry[%d] = %q, want %q", i, all[i].Message, w)
		}
	}
}

func TestGetAll_EmptyBuffer(t *testing.T) {
	if got := New(4).GetAll(); len(got) != 0 {
		t.Fatalf("empty GetAll len = %d, want 0", len(got))
	}
}

func TestGetAll_PartialFillOrder(t *testing.T) {
	b := New(10)
	b.Add(entry("info", "c", "first", nil))
	b.Add(entry("info", "c", "second", nil))
	all := b.GetAll()
	if len(all) != 2 || all[0].Message != "first" || all[1].Message != "second" {
		t.Fatalf("unexpected partial-fill order: %+v", all)
	}
}

func TestQuery_Filters(t *testing.T) {
	b := New(50)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	b.Add(LogEntry{Timestamp: base, Level: "info", Component: "playout", Message: "started playback"})
	b.Add(LogEntry{Timestamp: base.Add(time.Minute), Level: "error", Component: "harbor", Message: "connection refused"})
	b.Add(LogEntry{Timestamp: base.Add(2 * time.Minute), Level: "warn", Component: "playout", Message: "buffer low"})

	if got := b.Query(QueryParams{Level: "error"}); len(got) != 1 || got[0].Component != "harbor" {
		t.Fatalf("level filter: %+v", got)
	}
	if got := b.Query(QueryParams{Component: "playout"}); len(got) != 2 {
		t.Fatalf("component filter len = %d, want 2", len(got))
	}
	if got := b.Query(QueryParams{Search: "REFUSED"}); len(got) != 1 {
		t.Fatalf("case-insensitive search len = %d, want 1", len(got))
	}
	if got := b.Query(QueryParams{Since: base.Add(90 * time.Second)}); len(got) != 1 || got[0].Message != "buffer low" {
		t.Fatalf("since filter: %+v", got)
	}
	if got := b.Query(QueryParams{Limit: 2}); len(got) != 2 {
		t.Fatalf("limit len = %d, want 2", len(got))
	}
	desc := b.Query(QueryParams{Descending: true})
	if len(desc) != 3 || desc[0].Message != "buffer low" {
		t.Fatalf("descending order wrong: %+v", desc)
	}
}

func TestQuery_SearchInFields(t *testing.T) {
	b := New(10)
	b.Add(entry("info", "playout", "generic", map[string]interface{}{"track": "Highway Star"}))
	if got := b.Query(QueryParams{Search: "highway"}); len(got) != 1 {
		t.Fatalf("search-in-fields len = %d, want 1", len(got))
	}
}

func TestStationIDFromFields(t *testing.T) {
	cases := []struct {
		name   string
		fields map[string]interface{}
		want   string
	}{
		{"nil", nil, ""},
		{"preferred station_id", map[string]interface{}{"station_id": testUUID}, testUUID},
		{"alternate stationID", map[string]interface{}{"stationID": testUUID}, testUUID},
		{"alternate station_uuid", map[string]interface{}{"station_uuid": testUUID}, testUUID},
		{"ambiguous station as uuid", map[string]interface{}{"station": testUUID}, testUUID},
		{"ambiguous station as name ignored", map[string]interface{}{"station": "Rock FM"}, ""},
		{"non-uuid station_id ignored", map[string]interface{}{"station_id": "not-a-uuid"}, ""},
		{"bytes value", map[string]interface{}{"station_id": []byte(testUUID)}, testUUID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StationIDFromFields(tc.fields); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCoerceString(t *testing.T) {
	if coerceString(map[string]interface{}{"a": 1}) != "" {
		t.Fatal("map should coerce to empty")
	}
	if coerceString([]interface{}{1, 2}) != "" {
		t.Fatal("slice should coerce to empty")
	}
	if coerceString(42) != "42" {
		t.Fatalf("int coerce = %q, want 42", coerceString(42))
	}
	if coerceString([]byte("hi")) != "hi" {
		t.Fatal("bytes coerce failed")
	}
}

func TestLooksLikeUUID(t *testing.T) {
	if !looksLikeUUID(testUUID) {
		t.Fatal("canonical UUID rejected")
	}
	if looksLikeUUID("tooshort") {
		t.Fatal("short string accepted")
	}
	if looksLikeUUID("111111112222333344445555555555556666") { // 36 chars, no hyphens
		t.Fatal("hyphen-less 36-char string accepted")
	}
}

func TestStatsAndComponents(t *testing.T) {
	b := New(10)
	b.Add(entry("info", "playout", "a", nil))
	b.Add(entry("error", "harbor", "b", nil))
	b.Add(entry("info", "playout", "c", nil))

	s := b.Stats()
	if s.Count != 3 || s.Capacity != 10 {
		t.Fatalf("stats = %+v", s)
	}
	if s.LevelCount["info"] != 2 || s.LevelCount["error"] != 1 {
		t.Fatalf("level counts = %+v", s.LevelCount)
	}
	comps := b.GetComponents()
	if len(comps) != 2 {
		t.Fatalf("components = %v, want 2 unique", comps)
	}
}

func TestClear(t *testing.T) {
	b := New(5)
	b.Add(entry("info", "c", "x", nil))
	b.Clear()
	if b.Stats().Count != 0 {
		t.Fatal("Clear did not reset count")
	}
	if len(b.GetAll()) != 0 {
		t.Fatal("GetAll not empty after Clear")
	}
}

func TestStationBuffers_MirrorAndScopedQuery(t *testing.T) {
	b := New(100)
	b.EnableStationBuffers(10, 5)
	b.Add(entry("info", "playout", "station log", map[string]interface{}{"station_id": testUUID}))
	b.Add(entry("info", "system", "global log", nil))

	// Scoped query pulls from the per-station buffer.
	got := b.Query(QueryParams{StationID: testUUID})
	if len(got) != 1 || got[0].Message != "station log" {
		t.Fatalf("scoped query: %+v", got)
	}
	if b.StatsForStation(testUUID).Count != 1 {
		t.Fatalf("station stats count = %d, want 1", b.StatsForStation(testUUID).Count)
	}
	if comps := b.GetComponentsForStation(testUUID); len(comps) != 1 || comps[0] != "playout" {
		t.Fatalf("station components = %v", comps)
	}
}

func TestEnableStationBuffers_Guards(t *testing.T) {
	b := New(10)
	b.EnableStationBuffers(0, 5) // non-positive capacity is a no-op
	if b.stationBuffers != nil {
		t.Fatal("station buffers should stay disabled for capacity 0")
	}
	b.EnableStationBuffers(10, 0) // zero maxStations defaults to 200
	if b.maxStationBuffers != 200 {
		t.Fatalf("maxStationBuffers = %d, want default 200", b.maxStationBuffers)
	}
}

func TestWriter_ParsesJSONAndNormalizesStation(t *testing.T) {
	b := New(10)
	var fallback bytes.Buffer
	w := NewWriter(b, &fallback)

	rec := map[string]interface{}{
		"level":     "warn",
		"message":   "disk low",
		"component": "storage",
		"time":      "2026-01-01T00:00:00Z",
		"stationID": testUUID,
	}
	raw, _ := json.Marshal(rec)
	n, err := w.Write(raw)
	if err != nil {
		t.Fatalf("write err: %v", err)
	}
	if n != len(raw) {
		t.Fatalf("write n = %d, want %d", n, len(raw))
	}
	if fallback.Len() == 0 {
		t.Fatal("fallback did not receive the raw bytes")
	}

	all := b.GetAll()
	if len(all) != 1 {
		t.Fatalf("buffer entries = %d, want 1", len(all))
	}
	e := all[0]
	if e.Level != "warn" || e.Message != "disk low" || e.Component != "storage" {
		t.Fatalf("parsed entry wrong: %+v", e)
	}
	if e.Fields["station_id"] != testUUID {
		t.Fatalf("station_id not normalized: %+v", e.Fields)
	}
	if !e.Timestamp.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("timestamp not parsed: %v", e.Timestamp)
	}
}

func TestWriter_UnixTimeAndInvalidJSON(t *testing.T) {
	b := New(10)
	w := NewWriter(b, nil)

	// Unix-timestamp form.
	raw, _ := json.Marshal(map[string]interface{}{"level": "info", "message": "m", "time": float64(1735689600)})
	if _, err := w.Write(raw); err != nil {
		t.Fatalf("write err: %v", err)
	}
	if got := b.GetAll()[0].Timestamp.Unix(); got != 1735689600 {
		t.Fatalf("unix ts = %d", got)
	}

	// Invalid JSON is ignored (no panic, nothing added), and Write with nil fallback returns len.
	n, err := w.Write([]byte("not json"))
	if err != nil {
		t.Fatalf("invalid json err: %v", err)
	}
	if n != len("not json") {
		t.Fatalf("n = %d, want %d", n, len("not json"))
	}
	if len(b.GetAll()) != 1 {
		t.Fatal("invalid JSON should not add an entry")
	}
}

func TestContainsIgnoreCase(t *testing.T) {
	if !containsIgnoreCase("Hello World", "world") {
		t.Fatal("should match case-insensitively")
	}
	if containsIgnoreCase("abc", "abcd") {
		t.Fatal("substr longer than string should not match")
	}
	if !containsIgnoreCase("abc", "") {
		t.Fatal("empty substr should match")
	}
}
