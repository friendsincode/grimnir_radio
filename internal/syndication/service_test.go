/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package syndication

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newSvc(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Station{}, &models.Show{}, &models.ShowInstance{},
		&models.Network{}, &models.NetworkShow{}, &models.NetworkSubscription{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewService(db, zerolog.Nop()), db
}

func ctx() context.Context { return context.Background() }

func sp(v string) *string { return &v }

// ---------------------------------------------------------------------------
// pure helpers
// ---------------------------------------------------------------------------

func TestParseDays(t *testing.T) {
	got := parseDays("MO, we ,FR,bogus")
	want := []int{1, 3, 5} // MO=1, WE=3, FR=5; bogus dropped
	if len(got) != len(want) {
		t.Fatalf("parseDays = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseDays[%d] = %d, want %d", i, got[i], want[i])
		}
	}
	if len(parseDays("")) != 0 {
		t.Fatal("empty days should parse to nothing")
	}
}

func TestParseTime(t *testing.T) {
	if h, m := parseTime("09:30:00"); h != 9 || m != 30 {
		t.Fatalf("HH:MM:SS = %d:%d", h, m)
	}
	if h, m := parseTime("21:05"); h != 21 || m != 5 {
		t.Fatalf("HH:MM = %d:%d", h, m)
	}
	if h, m := parseTime("garbage"); h != 0 || m != 0 {
		t.Fatalf("malformed should be 0:0, got %d:%d", h, m)
	}
}

func TestContainsDay(t *testing.T) {
	days := []int{1, 3, 5}
	if !containsDay(days, 3) || containsDay(days, 2) {
		t.Fatal("containsDay wrong")
	}
}

// ---------------------------------------------------------------------------
// network + show CRUD
// ---------------------------------------------------------------------------

func TestNetworkCRUD(t *testing.T) {
	svc, _ := newSvc(t)
	n, err := svc.CreateNetwork(ctx(), "Public Radio Exchange", "desc", "owner1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := svc.GetNetwork(ctx(), n.ID)
	if err != nil || got.Name != "Public Radio Exchange" {
		t.Fatalf("get: %v / %+v", err, got)
	}
	svc.CreateNetwork(ctx(), "Other Net", "", "owner2")

	byOwner, _ := svc.ListNetworks(ctx(), "owner1")
	if len(byOwner) != 1 {
		t.Fatalf("owner filter = %d, want 1", len(byOwner))
	}
	all, _ := svc.ListNetworks(ctx(), "")
	if len(all) != 2 {
		t.Fatalf("all networks = %d, want 2", len(all))
	}
	if _, err := svc.GetNetwork(ctx(), "missing"); err == nil {
		t.Fatal("expected error for missing network")
	}
}

func TestNetworkShowCRUD(t *testing.T) {
	svc, db := newSvc(t)
	show := &models.NetworkShow{NetworkID: sp("net1"), Name: "Morning Edition", Duration: 120, Active: true}
	if err := svc.CreateNetworkShow(ctx(), show); err != nil {
		t.Fatalf("create: %v", err)
	}
	if show.ID == "" {
		t.Fatal("CreateNetworkShow should assign an ID")
	}
	got, err := svc.GetNetworkShow(ctx(), show.ID)
	if err != nil || got.Duration != 120 {
		t.Fatalf("get: %v / %+v", err, got)
	}

	shows, _ := svc.ListNetworkShows(ctx(), "net1")
	if len(shows) != 1 {
		t.Fatalf("list = %d, want 1", len(shows))
	}

	if err := svc.UpdateNetworkShow(ctx(), show.ID, map[string]any{"duration": 90}); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, _ := svc.GetNetworkShow(ctx(), show.ID)
	if updated.Duration != 90 {
		t.Fatalf("duration after update = %d", updated.Duration)
	}

	// Subscribe then delete: subscriptions are cascaded.
	svc.Subscribe(ctx(), "st1", show.ID, "09:00:00", "MO")
	if err := svc.DeleteNetworkShow(ctx(), show.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var subs int64
	db.Model(&models.NetworkSubscription{}).Count(&subs)
	if subs != 0 {
		t.Fatalf("delete should cascade subscriptions, %d remain", subs)
	}
}

// ---------------------------------------------------------------------------
// subscriptions
// ---------------------------------------------------------------------------

func TestSubscribeUnsubscribe(t *testing.T) {
	svc, _ := newSvc(t)
	show := &models.NetworkShow{ID: "sh1", NetworkID: sp("net1"), Name: "Show", Duration: 60, Active: true}
	svc.CreateNetworkShow(ctx(), show)

	sub, err := svc.Subscribe(ctx(), "st1", "sh1", "09:00:00", "MO,WE")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// Duplicate subscription is rejected.
	if _, err := svc.Subscribe(ctx(), "st1", "sh1", "10:00:00", "TU"); err == nil {
		t.Fatal("duplicate subscription should be rejected")
	}

	got, _ := svc.GetStationSubscriptions(ctx(), "st1")
	if len(got) != 1 || got[0].LocalDays != "MO,WE" {
		t.Fatalf("station subscriptions = %+v", got)
	}

	if err := svc.Unsubscribe(ctx(), sub.ID); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	after, _ := svc.GetStationSubscriptions(ctx(), "st1")
	if len(after) != 0 {
		t.Fatalf("after unsubscribe = %d, want 0", len(after))
	}
}

// ---------------------------------------------------------------------------
// materialization
// ---------------------------------------------------------------------------

func TestMaterializeSubscriptions(t *testing.T) {
	svc, db := newSvc(t)
	show := &models.NetworkShow{ID: "sh1", NetworkID: sp("net1"), Name: "Daily Show", Duration: 60, Active: true}
	svc.CreateNetworkShow(ctx(), show)
	// Air every day at 09:00 local.
	svc.Subscribe(ctx(), "st1", "sh1", "09:00:00", "MO,TU,WE,TH,FR,SA,SU")

	start := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 7)

	instances, err := svc.MaterializeSubscriptions(ctx(), "st1", start, end)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if len(instances) != 7 {
		t.Fatalf("expected 7 daily instances, got %d", len(instances))
	}
	if instances[0].EndsAt.Sub(instances[0].StartsAt) != time.Hour {
		t.Fatalf("instance duration = %v, want 1h", instances[0].EndsAt.Sub(instances[0].StartsAt))
	}
	// They are persisted.
	var stored int64
	db.Model(&models.ShowInstance{}).Count(&stored)
	if stored != 7 {
		t.Fatalf("persisted instances = %d, want 7", stored)
	}
}

func TestMaterializeSubscriptions_SkipsConflicts(t *testing.T) {
	svc, db := newSvc(t)
	show := &models.NetworkShow{ID: "sh1", NetworkID: sp("net1"), Name: "Daily", Duration: 60, Active: true}
	svc.CreateNetworkShow(ctx(), show)
	svc.Subscribe(ctx(), "st1", "sh1", "09:00:00", "MO,TU,WE,TH,FR,SA,SU")

	start := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 7)

	// Pre-seed a conflicting instance overlapping the first day's 09:00-10:00 slot.
	db.Create(&models.ShowInstance{
		ID: "existing", StationID: "st1",
		StartsAt: start.Add(9 * time.Hour), EndsAt: start.Add(10 * time.Hour),
		Status: models.ShowInstanceScheduled,
	})

	instances, err := svc.MaterializeSubscriptions(ctx(), "st1", start, end)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	// One day collides and is skipped => 6 new instances.
	if len(instances) != 6 {
		t.Fatalf("expected 6 instances after conflict skip, got %d", len(instances))
	}
}
