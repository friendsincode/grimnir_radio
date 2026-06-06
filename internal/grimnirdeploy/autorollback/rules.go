/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package autorollback

// Comparison is the relational operator a Rule uses to compare the query's
// scalar value against its threshold. The Monitor uses these to flag a
// sample as breached.
type Comparison int

const (
	// CompGreater means breach when sample > threshold.
	CompGreater Comparison = iota
	// CompGreaterEqual means breach when sample >= threshold.
	CompGreaterEqual
	// CompLess means breach when sample < threshold.
	CompLess
	// CompLessEqual means breach when sample <= threshold.
	CompLessEqual
)

func (c Comparison) breached(value, threshold float64) bool {
	switch c {
	case CompGreater:
		return value > threshold
	case CompGreaterEqual:
		return value >= threshold
	case CompLess:
		return value < threshold
	case CompLessEqual:
		return value <= threshold
	}
	return false
}

// Rule is one (PromQL query, threshold) pair the Monitor evaluates on every
// tick. ConsecutiveBreaches is the dwell-time count: a rule must breach for
// this many consecutive ticks before it flips the soak-window verdict to
// Rollback. Defaults & rule-set construction live in DefaultRules.
type Rule struct {
	// Name is the short identifier used in audit notes + ntfy bodies.
	// MUST be unique within a rule set.
	Name string
	// Query is the PromQL expression. The Monitor expects a scalar result;
	// vector results use the first sample's value.
	Query string
	// Threshold is the numeric comparison target.
	Threshold float64
	// Compare is how Value is compared against Threshold.
	Compare Comparison
	// ConsecutiveBreaches is the dwell-time count. A rule must breach for
	// this many ticks in a row to flip the verdict. 1 = first breach wins.
	ConsecutiveBreaches int
	// Description is a human-readable explanation; ends up in the Verdict
	// reason when this rule triggers.
	Description string
}

// DefaultRules returns the soak-window rule set used in production. The
// thresholds are tuned for the listener-facing failure modes operators
// actually woke up to during the v1.40.x troubleshooting cycle:
//
//   - listener_reconnects: a sudden surge means new clients can't hold a
//     session against the new build. Threshold 5/sec rate over 1m; 2
//     consecutive breaches (i.e. ~1m of sustained spike) flips the verdict.
//   - http_5xx_rate: the control-plane API returning 5xx at >0.5/sec for 2
//     ticks means the new build is failing requests, not just slow.
//   - alert_firing: the Alertmanager-side bridge sets ALERTS{...} when its
//     rules fire; any tick that sees a page-and-rollback alert active flips
//     the verdict immediately (ConsecutiveBreaches=1).
func DefaultRules() []Rule {
	return []Rule{
		{
			Name:                "listener_reconnects",
			Query:               "sum(rate(grimnir_listener_reconnects_total[1m]))",
			Threshold:           5,
			Compare:             CompGreater,
			ConsecutiveBreaches: 2,
			Description:         "listener reconnect rate exceeded 5/sec for 2 consecutive ticks",
		},
		{
			Name:                "http_5xx_rate",
			Query:               `sum(rate(grimnir_http_requests_total{status=~"5.."}[1m]))`,
			Threshold:           0.5,
			Compare:             CompGreater,
			ConsecutiveBreaches: 2,
			Description:         "control-plane 5xx rate exceeded 0.5/sec for 2 consecutive ticks",
		},
		{
			Name:                "alert_firing",
			Query:               `sum(ALERTS{severity="page-and-rollback",alertstate="firing"})`,
			Threshold:           0,
			Compare:             CompGreater,
			ConsecutiveBreaches: 1,
			Description:         "Alertmanager has a page-and-rollback alert firing",
		},
	}
}
