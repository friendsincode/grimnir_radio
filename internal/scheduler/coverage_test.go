package scheduler

import (
	"testing"
	"time"
)

func ts(h, m int) time.Time { return time.Date(2026, 7, 20, h, m, 0, 0, time.UTC) }

func TestSubtractCovered(t *testing.T) {
	win := interval{ts(16, 0), ts(0, 0).AddDate(0, 0, 1)} // 16:00 -> next-day 00:00
	min := time.Minute

	cases := []struct {
		name    string
		covered []interval
		want    []interval
	}{
		{"nothing covered", nil, []interval{win}},
		{"fully covered", []interval{win}, nil},
		{"JW: 16-17 covered", []interval{{ts(16, 0), ts(17, 0)}}, []interval{{ts(17, 0), win.End}}},
		{"mid-block hole", []interval{{ts(18, 0), ts(19, 0)}}, []interval{{ts(16, 0), ts(18, 0)}, {ts(19, 0), win.End}}},
		{"overlapping covers merge", []interval{{ts(16, 0), ts(18, 0)}, {ts(17, 0), ts(19, 0)}}, []interval{{ts(19, 0), win.End}}},
		{"cover extends past window clamps", []interval{{ts(15, 0), ts(17, 0)}}, []interval{{ts(17, 0), win.End}}},
		{"sub-min gap dropped", []interval{{ts(16, 0), win.End.Add(-30 * time.Second)}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := subtractCovered(win, tc.covered, min)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d gaps %v, want %d %v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if !got[i].Start.Equal(tc.want[i].Start) || !got[i].End.Equal(tc.want[i].End) {
					t.Errorf("gap %d = %v, want %v", i, got[i], tc.want[i])
				}
			}
		})
	}
}
