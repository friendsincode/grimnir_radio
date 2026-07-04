/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package logbuffer

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
	"time"
)

const testUUID = "11111111-2222-3333-4444-555555555555"
const otherUUID = "99999999-8888-7777-6666-555555555555"

func entry(level, component, msg string, fields map[string]interface{}) LogEntry {
	return LogEntry{Timestamp: time.Now(), Level: level, Component: component, Message: msg, Fields: fields}
}

func TestStationIDFromFields(t *testing.T) {
	cases := []struct {
		name   string
		fields map[string]interface{}
		want   string
	}{
		{"nil fields", nil, ""},
		{"preferred key", map[string]interface{}{"station_id": testUUID}, testUUID},
		{"alternate stationID", map[string]interface{}{"stationID": testUUID}, testUUID},
		{"alternate station_uuid", map[string]interface{}{"station_uuid": testUUID}, testUUID},
		{"ambiguous station key with uuid", map[string]interface{}{"station": testUUID}, testUUID},
		{"ambiguous station key with a name", map[string]interface{}{"station": "RLM Radio"}, ""},
		{"non-uuid station_id rejected", map[string]interface{}{"station_id": "not-a-uuid"}, ""},
		{"bytes coerced", map[string]interface{}{"station_id": []byte(testUUID)}, testUUID},
		{"structured value rejected", map[string]interface{}{"station_id": map[string]interface{}{"x": 1}}, ""},
		{"preferred beats alternate", map[string]interface{}{"station_id": testUUID, "stationID": otherUUID}, testUUID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StationIDFromFields(tc.fields); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuffer_RingWraparoundKeepsChronologicalOrder(t *testing.T) {
	b := New(3)
	for i := 1; i <= 5; i++ {
		b.Add(entry("info", "c", fmt.Sprintf("m%d", i), nil))
	}
	got := b.GetAll()
	if len(got) != 3 {
		t.Fatalf("len = %d, want capacity 3", len(got))
	}
	for i, want := range []string{"m3", "m4", "m5"} {
		if got[i].Message != want {
			t.Errorf("entry %d = %q, want %q (oldest evicted, order kept)", i, got[i].Message, want)
		}
	}
}

func TestBuffer_ZeroCapacityDefaults(t *testing.T) {
	b := New(0)
	if b.capacity != 10000 {
		t.Errorf("default capacity = %d, want 10000", b.capacity)
	}
}

func TestBuffer_StationMirroringSurvivesGlobalEviction(t *testing.T) {
	b := New(2) // tiny global buffer
	b.EnableStationBuffers(10, 5)

	b.Add(entry("info", "playout", "station log", map[string]interface{}{"station_id": testUUID}))
	// Flood the global ring so the station entry is evicted globally.
	for i := 0; i < 10; i++ {
		b.Add(entry("info", "system", fmt.Sprintf("noise%d", i), nil))
	}

	// Global view lost it...
	for _, e := range b.GetAll() {
		if e.Message == "station log" {
			t.Fatal("expected the station entry to be evicted from the tiny global ring")
		}
	}
	// ...the station-scoped query still has it.
	got := b.Query(QueryParams{StationID: testUUID})
	if len(got) != 1 || got[0].Message != "station log" {
		t.Fatalf("station query = %v", got)
	}
}

func TestBuffer_StationBufferCapEnforced(t *testing.T) {
	b := New(50)
	b.EnableStationBuffers(5, 2) // at most 2 station buffers

	ids := []string{
		"aaaaaaaa-0000-0000-0000-000000000001",
		"aaaaaaaa-0000-0000-0000-000000000002",
		"aaaaaaaa-0000-0000-0000-000000000003",
	}
	for _, id := range ids {
		b.Add(entry("info", "c", "m", map[string]interface{}{"station_id": id}))
	}
	b.stationMu.RLock()
	n := len(b.stationBuffers)
	b.stationMu.RUnlock()
	if n != 2 {
		t.Errorf("station buffers = %d, want capped at 2", n)
	}
	// The third station still appears via the global buffer's field filter.
	if got := b.Query(QueryParams{StationID: ids[2]}); len(got) != 1 {
		t.Errorf("uncapped station lost its logs entirely: %v", got)
	}
}

func TestBuffer_QueryFilters(t *testing.T) {
	b := New(50)
	base := time.Now()
	b.Add(LogEntry{Timestamp: base.Add(-2 * time.Hour), Level: "info", Component: "playout", Message: "old track started"})
	b.Add(LogEntry{Timestamp: base, Level: "error", Component: "playout", Message: "pipeline crashed"})
	b.Add(LogEntry{Timestamp: base, Level: "info", Component: "web", Message: "Request handled"})

	if got := b.Query(QueryParams{Level: "error"}); len(got) != 1 || got[0].Message != "pipeline crashed" {
		t.Errorf("level filter: %v", got)
	}
	if got := b.Query(QueryParams{Component: "web"}); len(got) != 1 || got[0].Message != "Request handled" {
		t.Errorf("component filter: %v", got)
	}
	if got := b.Query(QueryParams{Since: base.Add(-time.Hour)}); len(got) != 2 {
		t.Errorf("since filter: %d entries, want 2", len(got))
	}
	// Case-insensitive search across message.
	if got := b.Query(QueryParams{Search: "PIPELINE"}); len(got) != 1 {
		t.Errorf("search filter: %v", got)
	}
	// Limit + Descending: newest first, trimmed.
	got := b.Query(QueryParams{Limit: 2, Descending: true})
	if len(got) != 2 || got[0].Message == "old track started" {
		t.Errorf("limit+descending: %v", got)
	}
}

func TestBuffer_ClearAndStats(t *testing.T) {
	b := New(10)
	b.EnableStationBuffers(5, 5)
	b.Add(entry("error", "c", "boom", map[string]interface{}{"station_id": testUUID}))
	b.Add(entry("info", "c", "fine", map[string]interface{}{"station_id": testUUID}))

	st := b.StatsForStation(testUUID)
	if st.Count != 2 {
		t.Errorf("station stats count = %d, want 2", st.Count)
	}
	if st.LevelCount["error"] != 1 || st.LevelCount["info"] != 1 {
		t.Errorf("level counts = %v", st.LevelCount)
	}

	comps := b.GetComponentsForStation(testUUID)
	if len(comps) != 1 || comps[0] != "c" {
		t.Errorf("components = %v", comps)
	}

	b.Clear()
	if got := b.GetAll(); len(got) != 0 {
		t.Errorf("entries after Clear: %d", len(got))
	}
	if got := b.Query(QueryParams{StationID: testUUID}); len(got) != 0 {
		t.Errorf("station entries after Clear: %d", len(got))
	}
}

func TestWriter_ParsesZerologAndFallsThrough(t *testing.T) {
	b := New(10)
	var fallback bytes.Buffer
	w := NewWriter(b, &fallback)

	line := []byte(`{"level":"warn","component":"director","message":"stall detected","station":"` + testUUID + `","mount":"main","time":"2026-07-04T12:00:00Z"}` + "\n")
	n, err := w.Write(line)
	if err != nil || n != len(line) {
		t.Fatalf("write: n=%d err=%v", n, err)
	}

	got := b.GetAll()
	if len(got) != 1 {
		t.Fatalf("entries = %d", len(got))
	}
	e := got[0]
	if e.Level != "warn" || e.Component != "director" || e.Message != "stall detected" {
		t.Errorf("parsed entry = %+v", e)
	}
	// The ambiguous "station" key is normalized into station_id when it's a UUID.
	if e.Fields["station_id"] != testUUID {
		t.Errorf("station_id not normalized: %v", e.Fields)
	}
	if e.Timestamp.UTC().Hour() != 12 {
		t.Errorf("timestamp not parsed: %v", e.Timestamp)
	}
	if fallback.Len() != len(line) {
		t.Errorf("fallback got %d bytes, want %d", fallback.Len(), len(line))
	}

	// Garbage input: buffered nothing, still forwarded to the fallback.
	fallback.Reset()
	if _, err := w.Write([]byte("not json\n")); err != nil {
		t.Fatalf("garbage write: %v", err)
	}
	if len(b.GetAll()) != 1 {
		t.Error("garbage line was buffered")
	}
	if fallback.Len() == 0 {
		t.Error("garbage line not forwarded to fallback")
	}

	// No fallback: write still succeeds (zerolog must never error on logging).
	w2 := NewWriter(b, nil)
	if n, err := w2.Write(line); err != nil || n != len(line) {
		t.Errorf("nil-fallback write: n=%d err=%v", n, err)
	}
}

func TestBuffer_ConcurrentAddQuery(t *testing.T) {
	b := New(100)
	b.EnableStationBuffers(20, 10)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				b.Add(entry("info", "c", "m", map[string]interface{}{"station_id": testUUID}))
				_ = b.Query(QueryParams{StationID: testUUID, Limit: 5})
				_ = b.GetAll()
			}
		}(i)
	}
	wg.Wait()
}
