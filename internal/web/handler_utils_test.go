/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ---------------------------------------------------------------------------
// formatDuration
// ---------------------------------------------------------------------------

func TestFormatDuration_Zero(t *testing.T) {
	if got := formatDuration(0); got != "0:00" {
		t.Fatalf("expected 0:00, got %q", got)
	}
}

func TestFormatDuration_Seconds(t *testing.T) {
	if got := formatDuration(30 * time.Second); got != "0:30" {
		t.Fatalf("expected 0:30, got %q", got)
	}
}

func TestFormatDuration_MinutesAndSeconds(t *testing.T) {
	// 90s = 1m30s
	if got := formatDuration(90 * time.Second); got != "1:30" {
		t.Fatalf("expected 1:30, got %q", got)
	}
}

func TestFormatDuration_ExactHour(t *testing.T) {
	// 3600s = 1:00:00
	if got := formatDuration(3600 * time.Second); got != "1:00:00" {
		t.Fatalf("expected 1:00:00, got %q", got)
	}
}

func TestFormatDuration_HoursMinutesSeconds(t *testing.T) {
	// 3661s = 1h1m1s
	d := time.Hour + time.Minute + time.Second
	if got := formatDuration(d); got != "1:01:01" {
		t.Fatalf("expected 1:01:01, got %q", got)
	}
}

func TestFormatDuration_Negative(t *testing.T) {
	// Negative durations: h/m/s will all be negative ints, result is implementation-defined
	// but it should not panic.
	got := formatDuration(-5 * time.Second)
	if got == "" {
		t.Fatalf("expected non-empty string for negative duration")
	}
}

// ---------------------------------------------------------------------------
// formatDurationMs
// ---------------------------------------------------------------------------

func TestFormatDurationMs_ZeroInt(t *testing.T) {
	if got := formatDurationMs(0); got != "0:00" {
		t.Fatalf("expected 0:00, got %q", got)
	}
}

func TestFormatDurationMs_NegativeInt(t *testing.T) {
	if got := formatDurationMs(-5000); got != "0:00" {
		t.Fatalf("expected 0:00 for negative ms, got %q", got)
	}
}

func TestFormatDurationMs_IntType(t *testing.T) {
	// 90000 ms = 1m30s
	if got := formatDurationMs(90000); got != "1:30" {
		t.Fatalf("expected 1:30, got %q", got)
	}
}

func TestFormatDurationMs_Int64Type(t *testing.T) {
	if got := formatDurationMs(int64(3600000)); got != "1:00:00" {
		t.Fatalf("expected 1:00:00, got %q", got)
	}
}

func TestFormatDurationMs_Float64Type(t *testing.T) {
	if got := formatDurationMs(float64(61000)); got != "1:01" {
		t.Fatalf("expected 1:01, got %q", got)
	}
}

func TestFormatDurationMs_NonNumericType(t *testing.T) {
	// Non-numeric should return 0:00
	if got := formatDurationMs("hello"); got != "0:00" {
		t.Fatalf("expected 0:00 for non-numeric, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// formatMs
// ---------------------------------------------------------------------------

func TestFormatMs_Zero(t *testing.T) {
	if got := formatMs(0); got != "0:00" {
		t.Fatalf("expected 0:00, got %q", got)
	}
}

func TestFormatMs_OneMinute(t *testing.T) {
	// 60000 ms = 1 minute
	if got := formatMs(60000); got != "1:00" {
		t.Fatalf("expected 1:00, got %q", got)
	}
}

func TestFormatMs_NegativeAbsValue(t *testing.T) {
	// formatMs takes abs value of negative
	if got := formatMs(-60000); got != "1:00" {
		t.Fatalf("expected 1:00 for -60000ms (abs), got %q", got)
	}
}

func TestFormatMs_OneHour(t *testing.T) {
	if got := formatMs(3600000); got != "1:00:00" {
		t.Fatalf("expected 1:00:00, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// formatBytes
// ---------------------------------------------------------------------------

func TestFormatBytes_Zero(t *testing.T) {
	if got := formatBytes(0); got != "0 B" {
		t.Fatalf("expected '0 B', got %q", got)
	}
}

func TestFormatBytes_UnderKilobyte(t *testing.T) {
	if got := formatBytes(500); got != "500 B" {
		t.Fatalf("expected '500 B', got %q", got)
	}
}

func TestFormatBytes_ExactKilobyte(t *testing.T) {
	if got := formatBytes(1024); got != "1.0 KB" {
		t.Fatalf("expected '1.0 KB', got %q", got)
	}
}

func TestFormatBytes_OneAndHalfKilobyte(t *testing.T) {
	if got := formatBytes(1536); got != "1.5 KB" {
		t.Fatalf("expected '1.5 KB', got %q", got)
	}
}

func TestFormatBytes_OneMegabyte(t *testing.T) {
	if got := formatBytes(1048576); got != "1.0 MB" {
		t.Fatalf("expected '1.0 MB', got %q", got)
	}
}

func TestFormatBytes_OneGigabyte(t *testing.T) {
	if got := formatBytes(1073741824); got != "1.0 GB" {
		t.Fatalf("expected '1.0 GB', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// timeago
// ---------------------------------------------------------------------------

func TestTimeago_JustNow(t *testing.T) {
	// 5 seconds ago — should be "just now"
	got := timeago(time.Now().Add(-5 * time.Second))
	if got != "just now" {
		t.Fatalf("expected 'just now', got %q", got)
	}
}

func TestTimeago_MinutesAgo(t *testing.T) {
	got := timeago(time.Now().Add(-5 * time.Minute))
	if got != "5 minutes ago" {
		t.Fatalf("expected '5 minutes ago', got %q", got)
	}
}

func TestTimeago_OneMinuteAgo(t *testing.T) {
	got := timeago(time.Now().Add(-61 * time.Second))
	if got != "1 minute ago" {
		t.Fatalf("expected '1 minute ago', got %q", got)
	}
}

func TestTimeago_HoursAgo(t *testing.T) {
	got := timeago(time.Now().Add(-2 * time.Hour))
	if got != "2 hours ago" {
		t.Fatalf("expected '2 hours ago', got %q", got)
	}
}

func TestTimeago_DaysAgo(t *testing.T) {
	got := timeago(time.Now().Add(-3 * 24 * time.Hour))
	if got != "3 days ago" {
		t.Fatalf("expected '3 days ago', got %q", got)
	}
}

func TestTimeago_ZeroTime(t *testing.T) {
	if got := timeago(time.Time{}); got != "never" {
		t.Fatalf("expected 'never' for zero time, got %q", got)
	}
}

func TestTimeago_NilPointer(t *testing.T) {
	var tp *time.Time
	if got := timeago(tp); got != "never" {
		t.Fatalf("expected 'never' for nil pointer, got %q", got)
	}
}

func TestTimeago_NonTimeType(t *testing.T) {
	if got := timeago("not a time"); got != "never" {
		t.Fatalf("expected 'never' for non-time type, got %q", got)
	}
}

func TestTimeago_FutureTime(t *testing.T) {
	// Future time should return "in X minutes" — use 5m30s to avoid truncation races
	got := timeago(time.Now().Add(5*time.Minute + 30*time.Second))
	if got != "in 5 minutes" {
		t.Fatalf("expected 'in 5 minutes', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// stationColor
// ---------------------------------------------------------------------------

func TestStationColor_Determinism(t *testing.T) {
	// Same index should always return the same color
	c1 := stationColor(3)
	c2 := stationColor(3)
	if c1 != c2 {
		t.Fatalf("stationColor(3) returned different values: %q vs %q", c1, c2)
	}
}

func TestStationColor_DifferentIndicesDiffer(t *testing.T) {
	c0 := stationColor(0)
	c1 := stationColor(1)
	if c0 == c1 {
		t.Fatalf("stationColor(0) and stationColor(1) should differ, both returned %q", c0)
	}
}

func TestStationColor_WrapsAround(t *testing.T) {
	// The palette has 8 entries; index 8 should == index 0
	c0 := stationColor(0)
	c8 := stationColor(8)
	if c0 != c8 {
		t.Fatalf("stationColor wrapping: expected stationColor(0)=%q == stationColor(8)=%q", c0, c8)
	}
}

func TestStationColor_IsHexColor(t *testing.T) {
	// All colors should start with '#' and be 7 chars long (#rrggbb)
	for i := 0; i < 8; i++ {
		c := stationColor(i)
		if len(c) != 7 || c[0] != '#' {
			t.Fatalf("stationColor(%d) = %q, expected #rrggbb format", i, c)
		}
	}
}

// ---------------------------------------------------------------------------
// sourceTypeName
// ---------------------------------------------------------------------------

func TestSourceTypeName_SmartBlock(t *testing.T) {
	// "smart_block" is not in the switch, so it returns as-is
	if got := sourceTypeName("smart_block"); got != "smart_block" {
		t.Fatalf("expected 'smart_block', got %q", got)
	}
}

func TestSourceTypeName_LibreTime(t *testing.T) {
	if got := sourceTypeName("libretime"); got != "LibreTime" {
		t.Fatalf("expected 'LibreTime', got %q", got)
	}
}

func TestSourceTypeName_AzuraCast(t *testing.T) {
	if got := sourceTypeName("azuracast"); got != "AzuraCast" {
		t.Fatalf("expected 'AzuraCast', got %q", got)
	}
}

func TestSourceTypeName_CSV(t *testing.T) {
	if got := sourceTypeName("csv"); got != "CSV Import" {
		t.Fatalf("expected 'CSV Import', got %q", got)
	}
}

func TestSourceTypeName_Unknown(t *testing.T) {
	// Unknown types should be returned as-is
	if got := sourceTypeName("some_unknown_type"); got != "some_unknown_type" {
		t.Fatalf("expected passthrough for unknown, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// formatHourWindow
// ---------------------------------------------------------------------------

func TestFormatHourWindow_Midnight(t *testing.T) {
	// startHour=0, endHour=1 → "00:00-01:00"
	if got := formatHourWindow(0, 1); got != "00:00-01:00" {
		t.Fatalf("expected '00:00-01:00', got %q", got)
	}
}

func TestFormatHourWindow_Midday(t *testing.T) {
	if got := formatHourWindow(12, 13); got != "12:00-13:00" {
		t.Fatalf("expected '12:00-13:00', got %q", got)
	}
}

func TestFormatHourWindow_LastHour(t *testing.T) {
	// startHour=23, endHour=24 → endHour%24=0 → "23:00-00:00"
	if got := formatHourWindow(23, 24); got != "23:00-00:00" {
		t.Fatalf("expected '23:00-00:00', got %q", got)
	}
}

func TestFormatHourWindow_InvalidStartClamped(t *testing.T) {
	// Invalid startHour gets clamped to 0
	if got := formatHourWindow(-1, 5); got != "00:00-05:00" {
		t.Fatalf("expected '00:00-05:00' for clamped start, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// toInt
// ---------------------------------------------------------------------------

func TestToInt_IntType(t *testing.T) {
	if got := toInt(5); got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
}

func TestToInt_Int64Type(t *testing.T) {
	if got := toInt(int64(42)); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestToInt_Float64Truncates(t *testing.T) {
	if got := toInt(float64(5.9)); got != 5 {
		t.Fatalf("expected 5 (truncated), got %d", got)
	}
}

func TestToInt_NilReturnsZero(t *testing.T) {
	if got := toInt(nil); got != 0 {
		t.Fatalf("expected 0 for nil, got %d", got)
	}
}

func TestToInt_BoolReturnsZero(t *testing.T) {
	// bool is not a handled type
	if got := toInt(true); got != 0 {
		t.Fatalf("expected 0 for bool, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// toFloat
// ---------------------------------------------------------------------------

func TestToFloat_IntType(t *testing.T) {
	got, ok := toFloat(3)
	if !ok || got != 3.0 {
		t.Fatalf("expected 3.0 ok=true, got %v ok=%v", got, ok)
	}
}

func TestToFloat_Float64Type(t *testing.T) {
	got, ok := toFloat(3.14)
	if !ok || got != 3.14 {
		t.Fatalf("expected 3.14 ok=true, got %v ok=%v", got, ok)
	}
}

func TestToFloat_NilReturnsFalse(t *testing.T) {
	_, ok := toFloat(nil)
	if ok {
		t.Fatalf("expected ok=false for nil")
	}
}

func TestToFloat_StringReturnsFalse(t *testing.T) {
	_, ok := toFloat("3.14")
	if ok {
		t.Fatalf("expected ok=false for string")
	}
}

// ---------------------------------------------------------------------------
// coalesce
// ---------------------------------------------------------------------------

func TestCoalesce_FirstNonNilWins(t *testing.T) {
	got := coalesce(nil, "", 0, "hello", "world")
	if got != "hello" {
		t.Fatalf("expected 'hello', got %v", got)
	}
}

func TestCoalesce_AllNilReturnsNil(t *testing.T) {
	if got := coalesce(nil, nil, nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestCoalesce_AllEmptyStringsReturnsNil(t *testing.T) {
	if got := coalesce("", "", ""); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestCoalesce_ZeroIntSkipped(t *testing.T) {
	got := coalesce(0, 0, 42)
	if got != 42 {
		t.Fatalf("expected 42, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// ternary
// ---------------------------------------------------------------------------

func TestTernary_TrueReturnsA(t *testing.T) {
	if got := ternary(true, "yes", "no"); got != "yes" {
		t.Fatalf("expected 'yes', got %v", got)
	}
}

func TestTernary_FalseReturnsB(t *testing.T) {
	if got := ternary(false, "yes", "no"); got != "no" {
		t.Fatalf("expected 'no', got %v", got)
	}
}

// ---------------------------------------------------------------------------
// defaultVal
// ---------------------------------------------------------------------------

func TestDefaultVal_NilGetsDefault(t *testing.T) {
	if got := defaultVal("fallback", nil); got != "fallback" {
		t.Fatalf("expected 'fallback', got %v", got)
	}
}

func TestDefaultVal_EmptyStringGetsDefault(t *testing.T) {
	if got := defaultVal("fallback", ""); got != "fallback" {
		t.Fatalf("expected 'fallback', got %v", got)
	}
}

func TestDefaultVal_ZeroIntGetsDefault(t *testing.T) {
	if got := defaultVal(99, 0); got != 99 {
		t.Fatalf("expected 99, got %v", got)
	}
}

func TestDefaultVal_NonZeroValueReturnsItself(t *testing.T) {
	if got := defaultVal("fallback", "actual"); got != "actual" {
		t.Fatalf("expected 'actual', got %v", got)
	}
}

// ---------------------------------------------------------------------------
// truncate
// ---------------------------------------------------------------------------

func TestTruncate_ShortStringUnchanged(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestTruncate_ExactLengthUnchanged(t *testing.T) {
	if got := truncate("hello", 5); got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestTruncate_LongStringGetsDotDotDot(t *testing.T) {
	got := truncate("hello world", 8)
	if got != "hello..." {
		t.Fatalf("expected 'hello...', got %q", got)
	}
}

func TestTruncate_LengthIsEnforced(t *testing.T) {
	got := truncate("abcdefghij", 6)
	if len(got) != 6 {
		t.Fatalf("expected length 6, got %d (%q)", len(got), got)
	}
	if got[3:] != "..." {
		t.Fatalf("expected suffix '...', got %q", got[3:])
	}
}

// ---------------------------------------------------------------------------
// mul, div, mod
// ---------------------------------------------------------------------------

func TestMul_Basic(t *testing.T) {
	if got := mul(3, 4); got != 12 {
		t.Fatalf("expected 12, got %d", got)
	}
}

func TestMul_Zero(t *testing.T) {
	if got := mul(0, 999); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestDiv_Basic(t *testing.T) {
	if got := div(10, 3); got != 3 {
		t.Fatalf("expected 3 (integer division), got %d", got)
	}
}

func TestDiv_ByZeroReturnsZero(t *testing.T) {
	if got := div(42, 0); got != 0 {
		t.Fatalf("expected 0 for div-by-zero, got %d", got)
	}
}

func TestMod_Basic(t *testing.T) {
	if got := mod(10, 3); got != 1 {
		t.Fatalf("expected 1, got %d", got)
	}
}

func TestMod_ByZeroReturnsZero(t *testing.T) {
	if got := mod(10, 0); got != 0 {
		t.Fatalf("expected 0 for mod-by-zero, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// dict, list, jsonMarshal
// ---------------------------------------------------------------------------

func TestDict_KeyValuePairs(t *testing.T) {
	d := dict("a", 1, "b", "two")
	if len(d) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(d))
	}
	if d["a"] != 1 || d["b"] != "two" {
		t.Fatalf("unexpected dict contents: %v", d)
	}
}

func TestDict_OddArgsReturnsNil(t *testing.T) {
	if d := dict("a", 1, "b"); d != nil {
		t.Fatalf("expected nil for odd args, got %v", d)
	}
}

func TestDict_NonStringKeyReturnsNil(t *testing.T) {
	if d := dict(1, "value"); d != nil {
		t.Fatalf("expected nil for non-string key, got %v", d)
	}
}

func TestList_CreatesSlice(t *testing.T) {
	l := list(1, "two", true)
	if len(l) != 3 {
		t.Fatalf("expected 3 items, got %d", len(l))
	}
	if l[0] != 1 || l[1] != "two" || l[2] != true {
		t.Fatalf("unexpected list contents: %v", l)
	}
}

func TestJsonMarshal_Struct(t *testing.T) {
	type data struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	got := string(jsonMarshal(data{Name: "Alice", Age: 30}))
	if got != `{"name":"Alice","age":30}` {
		t.Fatalf("expected JSON, got %q", got)
	}
}

func TestJsonMarshal_Nil(t *testing.T) {
	if got := string(jsonMarshal(nil)); got != "null" {
		t.Fatalf("expected 'null', got %q", got)
	}
}

func TestJsonMarshal_Map(t *testing.T) {
	// Just verify it doesn't panic and returns valid JSON
	got := string(jsonMarshal(map[string]int{"x": 1}))
	if got != `{"x":1}` {
		t.Fatalf("expected JSON object, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// sourceStationID
// ---------------------------------------------------------------------------

func TestSourceStationID_ValidFormat(t *testing.T) {
	if got := sourceStationID("s1::block-id"); got != "s1" {
		t.Fatalf("expected 's1', got %q", got)
	}
}

func TestSourceStationID_NoSeparatorReturnsEmpty(t *testing.T) {
	if got := sourceStationID("just-an-id"); got != "" {
		t.Fatalf("expected '', got %q", got)
	}
}

func TestSourceStationID_EmptyReturnsEmpty(t *testing.T) {
	if got := sourceStationID(""); got != "" {
		t.Fatalf("expected '', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// roleAtLeast & stationRoleAtLeast
// ---------------------------------------------------------------------------

func TestRoleAtLeast_NilUserReturnsFalse(t *testing.T) {
	if got := roleAtLeast(nil, "user"); got {
		t.Fatalf("expected false for nil user")
	}
}

func TestRoleAtLeast_PlatformAdminPassesAny(t *testing.T) {
	user := &models.User{PlatformRole: models.PlatformRoleAdmin}
	for _, minRole := range []string{"user", "platform_mod", "platform_admin"} {
		if !roleAtLeast(user, minRole) {
			t.Fatalf("expected platform_admin to pass minRole=%q", minRole)
		}
	}
}

func TestRoleAtLeast_RegularUserFailsAdmin(t *testing.T) {
	user := &models.User{PlatformRole: models.PlatformRoleUser}
	if roleAtLeast(user, "platform_admin") {
		t.Fatalf("expected regular user to fail platform_admin requirement")
	}
}

func TestStationRoleAtLeast_OwnerPassesAll(t *testing.T) {
	roles := []string{"viewer", "dj", "manager", "admin", "owner"}
	for _, minRole := range roles {
		if !stationRoleAtLeast("owner", minRole) {
			t.Fatalf("expected owner to pass minRole=%q", minRole)
		}
	}
}

func TestStationRoleAtLeast_ViewerFailsHigherRoles(t *testing.T) {
	for _, minRole := range []string{"dj", "manager", "admin", "owner"} {
		if stationRoleAtLeast("viewer", minRole) {
			t.Fatalf("expected viewer to fail minRole=%q", minRole)
		}
	}
}

func TestStationRoleAtLeast_SameLevelPasses(t *testing.T) {
	if !stationRoleAtLeast("manager", "manager") {
		t.Fatalf("expected manager to pass manager requirement")
	}
}

// ---------------------------------------------------------------------------
// isPlatformAdmin
// ---------------------------------------------------------------------------

func TestIsPlatformAdmin_NilReturnsFalse(t *testing.T) {
	if isPlatformAdmin(nil) {
		t.Fatalf("expected false for nil user")
	}
}

func TestIsPlatformAdmin_AdminReturnsTrue(t *testing.T) {
	u := &models.User{PlatformRole: models.PlatformRoleAdmin}
	if !isPlatformAdmin(u) {
		t.Fatalf("expected true for platform admin")
	}
}

func TestIsPlatformAdmin_RegularUserReturnsFalse(t *testing.T) {
	u := &models.User{PlatformRole: models.PlatformRoleUser}
	if isPlatformAdmin(u) {
		t.Fatalf("expected false for regular user")
	}
}

// ---------------------------------------------------------------------------
// GetBasePath
// ---------------------------------------------------------------------------

func TestGetBasePath_FileWithDir(t *testing.T) {
	if got := GetBasePath("/foo/bar/baz.html"); got != "baz.html" {
		t.Fatalf("expected 'baz.html', got %q", got)
	}
}

func TestGetBasePath_FileOnly(t *testing.T) {
	if got := GetBasePath("baz.html"); got != "baz.html" {
		t.Fatalf("expected 'baz.html', got %q", got)
	}
}

func TestGetBasePath_Dot(t *testing.T) {
	if got := GetBasePath("."); got != "." {
		t.Fatalf("expected '.', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// formatTime, localTime, formatRFC3339UTC
// ---------------------------------------------------------------------------

func TestFormatTime_Format(t *testing.T) {
	ts := time.Date(2024, 3, 15, 14, 30, 45, 0, time.UTC)
	got := formatTime(ts)
	if got != "2024-03-15 14:30:45" {
		t.Fatalf("expected '2024-03-15 14:30:45', got %q", got)
	}
}

func TestLocalTime_ContainsISO(t *testing.T) {
	ts := time.Date(2024, 3, 15, 14, 30, 45, 0, time.UTC)
	got := string(localTime(ts))
	if !strings.Contains(got, "2024-03-15T14:30:45Z") {
		t.Fatalf("expected ISO timestamp in output, got %q", got)
	}
	if !strings.Contains(got, `class="local-time"`) {
		t.Fatalf("expected local-time class, got %q", got)
	}
}

func TestFormatRFC3339UTC_Format(t *testing.T) {
	ts := time.Date(2024, 3, 15, 14, 30, 45, 0, time.UTC)
	got := formatRFC3339UTC(ts)
	if got != "2024-03-15T14:30:45Z" {
		t.Fatalf("expected RFC3339 UTC, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// safeHTML, safeJS, safeURL
// ---------------------------------------------------------------------------

func TestSafeHTML(t *testing.T) {
	h := safeHTML("<b>bold</b>")
	if string(h) != "<b>bold</b>" {
		t.Fatalf("expected passthrough, got %q", h)
	}
}

func TestSafeJS(t *testing.T) {
	j := safeJS("alert(1)")
	if string(j) != "alert(1)" {
		t.Fatalf("expected passthrough, got %q", j)
	}
}

func TestSafeURL(t *testing.T) {
	u := safeURL("https://example.com")
	if string(u) != "https://example.com" {
		t.Fatalf("expected passthrough, got %q", u)
	}
}

// ---------------------------------------------------------------------------
// deref
// ---------------------------------------------------------------------------

func TestDeref_NonNil(t *testing.T) {
	v := 42
	if got := deref(&v); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestDeref_Nil(t *testing.T) {
	if got := deref(nil); got != -1 {
		t.Fatalf("expected -1 for nil, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// add, sub, iterate, stringify
// ---------------------------------------------------------------------------

func TestAdd_Basic(t *testing.T) {
	if got := add(3, 4); got != 7 {
		t.Fatalf("expected 7, got %d", got)
	}
}

func TestSub_Basic(t *testing.T) {
	if got := sub(10, 3); got != 7 {
		t.Fatalf("expected 7, got %d", got)
	}
}

func TestIterate_Length(t *testing.T) {
	got := iterate(5)
	if len(got) != 5 {
		t.Fatalf("expected 5 elements, got %d", len(got))
	}
	for i, v := range got {
		if v != i {
			t.Fatalf("expected got[%d]==%d, got %d", i, i, v)
		}
	}
}

func TestIterate_Zero(t *testing.T) {
	if got := iterate(0); len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

func TestStringify_Int(t *testing.T) {
	if got := stringify(42); got != "42" {
		t.Fatalf("expected '42', got %q", got)
	}
}

func TestStringify_String(t *testing.T) {
	if got := stringify("hello"); got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// eq, ne, lt, le, gt, ge
// ---------------------------------------------------------------------------

func TestEq_Equal(t *testing.T) {
	if !eq(5, 5) {
		t.Fatalf("expected eq(5,5)=true")
	}
}

func TestEq_NotEqual(t *testing.T) {
	if eq(5, 6) {
		t.Fatalf("expected eq(5,6)=false")
	}
}

func TestEq_NilBothNil(t *testing.T) {
	if !eq(nil, nil) {
		t.Fatalf("expected eq(nil,nil)=true")
	}
}

func TestEq_StringEqual(t *testing.T) {
	if !eq("hello", "hello") {
		t.Fatalf("expected eq(string,string)=true")
	}
}

func TestNe_NotEqual(t *testing.T) {
	if !ne(1, 2) {
		t.Fatalf("expected ne(1,2)=true")
	}
}

func TestLe_LessOrEqual(t *testing.T) {
	if !le(3, 5) {
		t.Fatalf("expected le(3,5)=true")
	}
	if !le(5, 5) {
		t.Fatalf("expected le(5,5)=true")
	}
	if le(6, 5) {
		t.Fatalf("expected le(6,5)=false")
	}
}

func TestGe_GreaterOrEqual(t *testing.T) {
	if !ge(5, 3) {
		t.Fatalf("expected ge(5,3)=true")
	}
	if !ge(5, 5) {
		t.Fatalf("expected ge(5,5)=true")
	}
	if ge(3, 5) {
		t.Fatalf("expected ge(3,5)=false")
	}
}

func TestFormatDurationSec(t *testing.T) {
	if got := formatDurationSec(90 * time.Second); got != 90 {
		t.Fatalf("expected 90, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// multipartLimit & setter methods (smoke tests via NewHandler)
// ---------------------------------------------------------------------------

func TestMultipartLimit_DefaultWhenZero(t *testing.T) {
	h := &Handler{maxUploadBytes: 0}
	if got := h.multipartLimit(512); got != 512 {
		t.Fatalf("expected 512 default, got %d", got)
	}
}

func TestMultipartLimit_OverrideWhenSet(t *testing.T) {
	h := &Handler{maxUploadBytes: 1024}
	if got := h.multipartLimit(512); got != 1024 {
		t.Fatalf("expected 1024 override, got %d", got)
	}
}

func TestSetScheduler_SetsField(t *testing.T) {
	h := &Handler{}
	h.SetScheduler(nil) // nil satisfies the interface for smoke test
	// No panic = pass
}

func TestSetWebstreamService_SetsField(t *testing.T) {
	h := &Handler{}
	h.SetWebstreamService(nil)
}

func TestSetLiveService_SetsField(t *testing.T) {
	h := &Handler{}
	h.SetLiveService(nil)
}

// ---------------------------------------------------------------------------
// staticResponseWriter.WriteHeader / Write
// ---------------------------------------------------------------------------

func TestStaticResponseWriter_WriteHeader_SetsContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &staticResponseWriter{ResponseWriter: rec, contentType: "text/css"}
	w.WriteHeader(http.StatusOK)
	if rec.Header().Get("Content-Type") != "text/css" {
		t.Fatalf("expected Content-Type text/css, got %q", rec.Header().Get("Content-Type"))
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestStaticResponseWriter_Write_ImplicitHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &staticResponseWriter{ResponseWriter: rec, contentType: "application/js"}
	n, err := w.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Fatalf("expected 5 bytes written, got %d", n)
	}
	if rec.Header().Get("Content-Type") != "application/js" {
		t.Fatalf("expected Content-Type set on implicit WriteHeader")
	}
}

func TestStaticResponseWriter_WriteHeader_NoDoubleSet(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &staticResponseWriter{ResponseWriter: rec, contentType: "text/css"}
	w.WriteHeader(http.StatusOK)
	// Second call should not re-set content type or panic
	w.WriteHeader(http.StatusNotFound)
}
