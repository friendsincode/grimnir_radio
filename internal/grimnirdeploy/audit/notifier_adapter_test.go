/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package audit

import (
	"context"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/notify"
)

func TestNotifierPoster_DefaultPriorityRoutesToTier1(t *testing.T) {
	fn := &notify.FakeNotifier{}
	p := NewNotifierPoster(fn)
	if err := p.Post(context.Background(), "ignored-topic", "deploy started", "op@host ran X", PriorityDefault); err != nil {
		t.Fatal(err)
	}
	if len(fn.Calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(fn.Calls))
	}
	if fn.Calls[0].Tier != 1 {
		t.Errorf("tier = %d, want 1", fn.Calls[0].Tier)
	}
	if fn.Calls[0].Title != "deploy started" {
		t.Errorf("title = %q", fn.Calls[0].Title)
	}
}

func TestNotifierPoster_HighPriorityRoutesToTier2(t *testing.T) {
	fn := &notify.FakeNotifier{}
	p := NewNotifierPoster(fn)
	if err := p.Post(context.Background(), "", "deploy failed", "boom", PriorityHigh); err != nil {
		t.Fatal(err)
	}
	if len(fn.Calls) != 1 || fn.Calls[0].Tier != 2 {
		t.Fatalf("want one tier-2 call, got %+v", fn.Calls)
	}
}

func TestNotifierPoster_UrgentPriorityRoutesToTier2(t *testing.T) {
	fn := &notify.FakeNotifier{}
	p := NewNotifierPoster(fn)
	if err := p.Post(context.Background(), "", "PANICKED", "stack", PriorityUrgent); err != nil {
		t.Fatal(err)
	}
	if len(fn.Calls) != 1 || fn.Calls[0].Tier != 2 {
		t.Fatalf("want one tier-2 call, got %+v", fn.Calls)
	}
}

func TestNotifierPoster_NilNotifierReturnsNilPoster(t *testing.T) {
	if p := NewNotifierPoster(nil); p != nil {
		t.Errorf("expected nil poster for nil notifier, got %T", p)
	}
}

func TestNotifierPoster_ImplementsPoster(t *testing.T) {
	var _ Poster = (*NotifierPoster)(nil)
}
