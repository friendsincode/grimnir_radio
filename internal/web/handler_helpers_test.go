/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"strings"
	"testing"
	"time"
)

// TestTimeago covers branches not yet exercised by handler_utils_test.go.

func TestTimeago_WeeksAgo(t *testing.T) {
	got := timeago(time.Now().Add(-14 * 24 * time.Hour))
	if !strings.Contains(got, "week") {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestTimeago_OneWeekAgo(t *testing.T) {
	got := timeago(time.Now().Add(-8 * 24 * time.Hour))
	if got != "1 week ago" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestTimeago_MonthsAgo(t *testing.T) {
	got := timeago(time.Now().Add(-45 * 24 * time.Hour))
	if !strings.Contains(got, "month") {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestTimeago_OneMonthAgo(t *testing.T) {
	got := timeago(time.Now().Add(-31 * 24 * time.Hour))
	if got != "1 month ago" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestTimeago_YearsAgo(t *testing.T) {
	got := timeago(time.Now().Add(-400 * 24 * time.Hour))
	if !strings.Contains(got, "year") {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestTimeago_OneYearAgo(t *testing.T) {
	got := timeago(time.Now().Add(-366 * 24 * time.Hour))
	if got != "1 year ago" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestTimeago_FutureSeconds(t *testing.T) {
	got := timeago(time.Now().Add(5 * time.Second))
	if got != "in a few seconds" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestTimeago_FutureMinutesPlural(t *testing.T) {
	got := timeago(time.Now().Add(3 * time.Minute))
	if !strings.Contains(got, "minutes") {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestTimeago_FutureOneMinute(t *testing.T) {
	got := timeago(time.Now().Add(90 * time.Second))
	if got != "in 1 minute" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestTimeago_FutureHoursPlural(t *testing.T) {
	got := timeago(time.Now().Add(3 * time.Hour))
	if !strings.Contains(got, "hours") {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestTimeago_FutureOneHour(t *testing.T) {
	got := timeago(time.Now().Add(90 * time.Minute))
	if got != "in 1 hour" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestTimeago_FutureDaysPlural(t *testing.T) {
	got := timeago(time.Now().Add(3 * 24 * time.Hour))
	if !strings.Contains(got, "days") {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestTimeago_FutureOneDay(t *testing.T) {
	got := timeago(time.Now().Add(25 * time.Hour))
	if got != "in 1 day" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestTimeago_TimePtr(t *testing.T) {
	ts := time.Now().Add(-5 * time.Second)
	got := timeago(&ts)
	if got != "just now" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestTimeago_NonTimeValue(t *testing.T) {
	got := timeago(42)
	if got != "never" {
		t.Fatalf("expected 'never' for non-time value, got %q", got)
	}
}

func TestTimeago_MultipleMonthsAgo(t *testing.T) {
	got := timeago(time.Now().Add(-75 * 24 * time.Hour))
	if !strings.Contains(got, "months ago") {
		t.Fatalf("expected plural months ago, got %q", got)
	}
}

func TestTimeago_MultipleYearsAgo(t *testing.T) {
	got := timeago(time.Now().Add(-800 * 24 * time.Hour))
	if !strings.Contains(got, "years ago") {
		t.Fatalf("expected plural years ago, got %q", got)
	}
}

// toFloat covers int8/int16/int32/uint/uint8/uint16/uint32/uint64 not in handler_utils_test.go
func TestToFloat_Int8(t *testing.T) {
	v, ok := toFloat(int8(5))
	if !ok || v != 5.0 {
		t.Fatal("int8")
	}
}
func TestToFloat_Int16(t *testing.T) {
	v, ok := toFloat(int16(5))
	if !ok || v != 5.0 {
		t.Fatal("int16")
	}
}
func TestToFloat_Int32(t *testing.T) {
	v, ok := toFloat(int32(5))
	if !ok || v != 5.0 {
		t.Fatal("int32")
	}
}
func TestToFloat_Uint(t *testing.T) {
	v, ok := toFloat(uint(5))
	if !ok || v != 5.0 {
		t.Fatal("uint")
	}
}
func TestToFloat_Uint8(t *testing.T) {
	v, ok := toFloat(uint8(5))
	if !ok || v != 5.0 {
		t.Fatal("uint8")
	}
}
func TestToFloat_Uint16(t *testing.T) {
	v, ok := toFloat(uint16(5))
	if !ok || v != 5.0 {
		t.Fatal("uint16")
	}
}
func TestToFloat_Uint32(t *testing.T) {
	v, ok := toFloat(uint32(5))
	if !ok || v != 5.0 {
		t.Fatal("uint32")
	}
}
func TestToFloat_Uint64(t *testing.T) {
	v, ok := toFloat(uint64(5))
	if !ok || v != 5.0 {
		t.Fatal("uint64")
	}
}
func TestToFloat_Float32(t *testing.T) {
	v, ok := toFloat(float32(5.0))
	if !ok || v != float64(float32(5.0)) {
		t.Fatal("float32")
	}
}

// eq: cover nil-vs-non-nil and reflect.DeepEqual path
func TestEq_NilVsNonNil(t *testing.T) {
	if eq(nil, 1) {
		t.Fatal("nil vs 1 should be false")
	}
}
func TestEq_DeepEqual(t *testing.T) {
	a := []int{1, 2, 3}
	b := []int{1, 2, 3}
	if !eq(a, b) {
		t.Fatal("deep equal slices")
	}
}

// applyLooseMediaSearch - covers whitespace-only and non-empty query
func TestApplyLooseMediaSearch_WhitespaceOnly(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	_ = h
	result := applyLooseMediaSearch(db, "   ")
	if result == nil {
		t.Fatal("expected non-nil db for whitespace query")
	}
}

func TestApplyLooseMediaSearch_Query(t *testing.T) {
	h, db := newEmptySetupHandler(t)
	_ = h
	result := applyLooseMediaSearch(db, "jazz music")
	if result == nil {
		t.Fatal("expected non-nil db")
	}
}

// normalizeSearchText covers all replacement chars
func TestNormalizeSearchText_AllChars(t *testing.T) {
	input := `A.B-C_D'E"F/G\H(I)J[K]L,M;N:O`
	got := normalizeSearchText(input)
	for _, r := range []string{".", "-", "_", "'", `"`, "/", `\`, "(", ")", "[", "]", ",", ";", ":"} {
		if strings.Contains(got, r) {
			t.Fatalf("normalizeSearchText should remove %q, got %q", r, got)
		}
	}
}

func TestNormalizedSQLExprHelper(t *testing.T) {
	expr := normalizedSQLExpr("artist")
	if !strings.Contains(expr, "artist") {
		t.Fatalf("should contain column name: %q", expr)
	}
}

// formatBytesUint64 (pages_admin.go)
func TestFormatBytesUint64(t *testing.T) {
	if got := formatBytesUint64(0); got != "0 B" {
		t.Fatalf("0: %q", got)
	}
	if got := formatBytesUint64(1024); got == "" {
		t.Fatal("1024 should not be empty")
	}
	if got := formatBytesUint64(1024 * 1024); got == "" {
		t.Fatal("1MB should not be empty")
	}
}
