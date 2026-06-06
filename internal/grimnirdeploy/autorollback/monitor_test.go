/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package autorollback

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeQuerier is a deterministic Querier for tests. Per-query responses are
// keyed by PromQL string; a per-query call counter lets tests drive a series
// of values across ticks.
type fakeQuerier struct {
	mu        sync.Mutex
	responses map[string][]queryResp
	calls     map[string]int
	defaultV  float64
	defaultE  error
}

type queryResp struct {
	value float64
	err   error
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{
		responses: make(map[string][]queryResp),
		calls:     make(map[string]int),
	}
}

// setSeries assigns a sequence of (value, err) responses for a PromQL query.
// The Nth call to Query for that query string returns responses[N]; once the
// sequence is exhausted, the last entry repeats.
func (f *fakeQuerier) setSeries(q string, vals ...queryResp) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[q] = vals
}

// setDefault sets a fallback (value, err) for any query without an explicit
// series. Used by tests that only care about one rule out of many.
func (f *fakeQuerier) setDefault(v float64, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defaultV, f.defaultE = v, err
}

func (f *fakeQuerier) Query(_ context.Context, q string, _ time.Time) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	series, ok := f.responses[q]
	if !ok {
		f.calls[q]++
		return f.defaultV, f.defaultE
	}
	idx := f.calls[q]
	f.calls[q]++
	if idx >= len(series) {
		idx = len(series) - 1
	}
	return series[idx].value, series[idx].err
}

func (f *fakeQuerier) callCount(q string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[q]
}

func TestMonitor_PassWhenAllRulesQuiet(t *testing.T) {
	fq := newFakeQuerier()
	fq.setDefault(0, nil)
	m := &Monitor{
		Querier:      fq,
		Rules:        DefaultRules(),
		Window:       60 * time.Millisecond,
		TickInterval: 10 * time.Millisecond,
		Now:          time.Now,
	}
	v := m.Observe(context.Background())
	if v.Decision != DecisionPass {
		t.Fatalf("want DecisionPass, got %v (reason=%s)", v.Decision, v.Reason)
	}
	if v.TicksObserved < 4 {
		t.Errorf("want >=4 ticks across 60ms window with 10ms tick; got %d", v.TicksObserved)
	}
}

func TestMonitor_RollbackOnSustainedBreach(t *testing.T) {
	fq := newFakeQuerier()
	// listener_reconnects breaches (10 > 5) for every tick.
	fq.setSeries("sum(rate(grimnir_listener_reconnects_total[1m]))",
		queryResp{value: 10}, queryResp{value: 10}, queryResp{value: 10}, queryResp{value: 10})
	// every other rule is quiet.
	fq.setDefault(0, nil)

	m := &Monitor{
		Querier:      fq,
		Rules:        DefaultRules(),
		Window:       50 * time.Millisecond,
		TickInterval: 10 * time.Millisecond,
		Now:          time.Now,
	}
	v := m.Observe(context.Background())
	if v.Decision != DecisionRollback {
		t.Fatalf("want DecisionRollback, got %v (reason=%s)", v.Decision, v.Reason)
	}
	if v.TriggeringRule != "listener_reconnects" {
		t.Errorf("want TriggeringRule=listener_reconnects, got %q", v.TriggeringRule)
	}
}

func TestMonitor_SingleBreachDoesNotTrigger(t *testing.T) {
	fq := newFakeQuerier()
	// listener_reconnects needs 2 consecutive breaches; provide exactly 1.
	fq.setSeries("sum(rate(grimnir_listener_reconnects_total[1m]))",
		queryResp{value: 10}, queryResp{value: 0}, queryResp{value: 0}, queryResp{value: 0}, queryResp{value: 0})
	fq.setDefault(0, nil)

	m := &Monitor{
		Querier:      fq,
		Rules:        DefaultRules(),
		Window:       60 * time.Millisecond,
		TickInterval: 10 * time.Millisecond,
		Now:          time.Now,
	}
	v := m.Observe(context.Background())
	if v.Decision == DecisionRollback {
		t.Fatalf("single breach should not trigger; got Rollback (reason=%s)", v.Reason)
	}
}

func TestMonitor_AlertRuleTriggersOnFirstBreach(t *testing.T) {
	fq := newFakeQuerier()
	fq.setSeries(`sum(ALERTS{severity="page-and-rollback",alertstate="firing"})`,
		queryResp{value: 1})
	fq.setDefault(0, nil)

	m := &Monitor{
		Querier:      fq,
		Rules:        DefaultRules(),
		Window:       50 * time.Millisecond,
		TickInterval: 10 * time.Millisecond,
		Now:          time.Now,
	}
	v := m.Observe(context.Background())
	if v.Decision != DecisionRollback {
		t.Fatalf("want DecisionRollback, got %v", v.Decision)
	}
	if v.TriggeringRule != "alert_firing" {
		t.Errorf("want TriggeringRule=alert_firing, got %q", v.TriggeringRule)
	}
}

func TestMonitor_InconclusiveWhenPrometheusDown(t *testing.T) {
	fq := newFakeQuerier()
	fq.setDefault(0, errors.New("prometheus: connection refused"))

	m := &Monitor{
		Querier:      fq,
		Rules:        DefaultRules(),
		Window:       30 * time.Millisecond,
		TickInterval: 10 * time.Millisecond,
		Now:          time.Now,
	}
	v := m.Observe(context.Background())
	if v.Decision != DecisionInconclusive {
		t.Fatalf("want DecisionInconclusive, got %v (errors=%d, ticks=%d)", v.Decision, v.QueryErrors, v.TicksObserved)
	}
	if v.QueryErrors == 0 {
		t.Error("want QueryErrors > 0")
	}
}

func TestMonitor_PassWhenErrorsAreOccasional(t *testing.T) {
	fq := newFakeQuerier()
	// 1 error, then clean. Quorum says >50% errors = inconclusive; 1/4 is fine.
	fq.setSeries("sum(rate(grimnir_listener_reconnects_total[1m]))",
		queryResp{err: errors.New("scrape error")},
		queryResp{value: 0},
		queryResp{value: 0},
		queryResp{value: 0})
	fq.setDefault(0, nil)

	m := &Monitor{
		Querier:      fq,
		Rules:        DefaultRules(),
		Window:       50 * time.Millisecond,
		TickInterval: 10 * time.Millisecond,
		Now:          time.Now,
	}
	v := m.Observe(context.Background())
	if v.Decision != DecisionPass {
		t.Fatalf("want DecisionPass with occasional errors, got %v (reason=%s)", v.Decision, v.Reason)
	}
}

func TestMonitor_ContextCancelEndsEarly(t *testing.T) {
	fq := newFakeQuerier()
	fq.setDefault(0, nil)
	ctx, cancel := context.WithCancel(context.Background())
	m := &Monitor{
		Querier:      fq,
		Rules:        DefaultRules(),
		Window:       5 * time.Second,
		TickInterval: 10 * time.Millisecond,
		Now:          time.Now,
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	v := m.Observe(ctx)
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("Observe did not honor ctx cancel; took %v", time.Since(start))
	}
	if v.Decision == DecisionRollback {
		t.Error("cancelled context should not produce Rollback")
	}
}

func TestMonitor_NilQuerierIsInconclusive(t *testing.T) {
	m := &Monitor{
		Rules:        DefaultRules(),
		Window:       10 * time.Millisecond,
		TickInterval: 5 * time.Millisecond,
	}
	v := m.Observe(context.Background())
	if v.Decision != DecisionInconclusive {
		t.Fatalf("nil Querier should yield Inconclusive; got %v", v.Decision)
	}
}

func TestMonitor_EmptyRulesIsPass(t *testing.T) {
	fq := newFakeQuerier()
	m := &Monitor{
		Querier:      fq,
		Rules:        nil,
		Window:       20 * time.Millisecond,
		TickInterval: 10 * time.Millisecond,
		Now:          time.Now,
	}
	v := m.Observe(context.Background())
	if v.Decision != DecisionPass {
		t.Fatalf("empty rule set should yield Pass; got %v", v.Decision)
	}
}

// TestMonitor_RecordsSamples verifies that the Verdict carries one Sample
// per (tick, rule) so the post-mortem audit row has enough context to
// reconstruct what the Monitor saw.
func TestMonitor_RecordsSamples(t *testing.T) {
	fq := newFakeQuerier()
	fq.setDefault(0, nil)
	rules := []Rule{
		{Name: "r1", Query: "vector(0)", Threshold: 1, Compare: CompGreater, ConsecutiveBreaches: 1},
	}
	m := &Monitor{
		Querier:      fq,
		Rules:        rules,
		Window:       30 * time.Millisecond,
		TickInterval: 10 * time.Millisecond,
		Now:          time.Now,
	}
	v := m.Observe(context.Background())
	if len(v.Samples) == 0 {
		t.Fatal("want Samples recorded")
	}
	for _, s := range v.Samples {
		if s.RuleName != "r1" {
			t.Errorf("sample rule = %q, want r1", s.RuleName)
		}
	}
	// Sanity: querier was called once per (tick, rule).
	if fq.callCount("vector(0)") != len(v.Samples) {
		t.Errorf("call count %d != sample count %d", fq.callCount("vector(0)"), len(v.Samples))
	}
}

// fmt is referenced only for the sake of go vet visibility in some go test
// configurations; harmless if unused.
var _ = fmt.Sprintf
