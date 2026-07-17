package scheduler

import (
	"sort"
	"time"
)

// interval is a half-open time window [Start, End).
type interval struct {
	Start time.Time
	End   time.Time
}

// subtractCovered returns the sub-intervals of window not covered by any interval
// in covered. Covered intervals may overlap, extend past the window, and need not
// be sorted. Gaps shorter than minDur are dropped as noise.
func subtractCovered(window interval, covered []interval, minDur time.Duration) []interval {
	clamped := make([]interval, 0, len(covered))
	for _, c := range covered {
		s, e := c.Start, c.End
		if s.Before(window.Start) {
			s = window.Start
		}
		if e.After(window.End) {
			e = window.End
		}
		if !s.Before(e) {
			continue
		}
		clamped = append(clamped, interval{s, e})
	}
	sort.Slice(clamped, func(i, j int) bool { return clamped[i].Start.Before(clamped[j].Start) })

	var gaps []interval
	cursor := window.Start
	for _, c := range clamped {
		if c.Start.After(cursor) {
			gaps = append(gaps, interval{cursor, c.Start})
		}
		if c.End.After(cursor) {
			cursor = c.End
		}
	}
	if cursor.Before(window.End) {
		gaps = append(gaps, interval{cursor, window.End})
	}

	out := gaps[:0]
	for _, g := range gaps {
		if g.End.Sub(g.Start) >= minDur {
			out = append(out, g)
		}
	}
	return out
}
