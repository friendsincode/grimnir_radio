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

func openSyndicationTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&models.Station{},
		&models.Show{},
		&models.ShowInstance{},
		&models.Network{},
		&models.NetworkShow{},
		&models.NetworkSubscription{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db := openSyndicationTestDB(t)
	return NewService(db, zerolog.Nop()), db
}

func TestCreateAndGetNetwork(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	network, err := svc.CreateNetwork(ctx, "Community Net", "shared programming", "owner-1")
	if err != nil {
		t.Fatalf("create network: %v", err)
	}
	if network.ID == "" {
		t.Fatal("expected network ID to be generated")
	}
	if !network.Active {
		t.Fatal("expected new network to be active")
	}

	got, err := svc.GetNetwork(ctx, network.ID)
	if err != nil {
		t.Fatalf("get network: %v", err)
	}
	if got.Name != "Community Net" || got.Description != "shared programming" || got.OwnerID != "owner-1" {
		t.Fatalf("unexpected network: %+v", got)
	}

	if _, err := svc.GetNetwork(ctx, "missing-id"); err == nil {
		t.Fatal("expected error for missing network")
	}
}

func TestListNetworks(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.CreateNetwork(ctx, "B Net", "", "owner-1"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.CreateNetwork(ctx, "A Net", "", "owner-1"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.CreateNetwork(ctx, "C Net", "", "owner-2"); err != nil {
		t.Fatalf("create: %v", err)
	}

	tests := []struct {
		name      string
		ownerID   string
		wantNames []string
	}{
		{name: "all networks ordered by name", ownerID: "", wantNames: []string{"A Net", "B Net", "C Net"}},
		{name: "filtered by owner", ownerID: "owner-1", wantNames: []string{"A Net", "B Net"}},
		{name: "owner with no networks", ownerID: "owner-3", wantNames: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			networks, err := svc.ListNetworks(ctx, tt.ownerID)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(networks) != len(tt.wantNames) {
				t.Fatalf("got %d networks, want %d", len(networks), len(tt.wantNames))
			}
			for i, want := range tt.wantNames {
				if networks[i].Name != want {
					t.Errorf("network[%d].Name = %q, want %q", i, networks[i].Name, want)
				}
			}
		})
	}
}

func TestCreateNetworkShow(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	t.Run("generates ID when empty", func(t *testing.T) {
		show := &models.NetworkShow{Name: "Syndicated Hour", Duration: 60, Active: true}
		if err := svc.CreateNetworkShow(ctx, show); err != nil {
			t.Fatalf("create: %v", err)
		}
		if show.ID == "" {
			t.Fatal("expected generated ID")
		}
	})

	t.Run("preserves provided ID", func(t *testing.T) {
		show := &models.NetworkShow{ID: "fixed-id", Name: "Fixed Show", Duration: 30, Active: true}
		if err := svc.CreateNetworkShow(ctx, show); err != nil {
			t.Fatalf("create: %v", err)
		}
		if show.ID != "fixed-id" {
			t.Fatalf("ID = %q, want fixed-id", show.ID)
		}
	})

	t.Run("duplicate ID fails", func(t *testing.T) {
		show := &models.NetworkShow{ID: "fixed-id", Name: "Dup", Duration: 30}
		if err := svc.CreateNetworkShow(ctx, show); err == nil {
			t.Fatal("expected primary key conflict")
		}
	})
}

func TestGetNetworkShow(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()

	show := &models.NetworkShow{ID: "ns-1", Name: "News Hour", Duration: 60, Active: true}
	if err := svc.CreateNetworkShow(ctx, show); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Create(&models.Station{ID: "st-1", Name: "Station One"}).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	if _, err := svc.Subscribe(ctx, "st-1", "ns-1", "09:00", "MO", ""); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	got, err := svc.GetNetworkShow(ctx, "ns-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "News Hour" {
		t.Fatalf("Name = %q, want News Hour", got.Name)
	}
	if len(got.Subscriptions) != 1 || got.Subscriptions[0].StationID != "st-1" {
		t.Fatalf("expected preloaded subscription for st-1, got %+v", got.Subscriptions)
	}

	if _, err := svc.GetNetworkShow(ctx, "missing"); err == nil {
		t.Fatal("expected error for missing show")
	}
}

func TestListNetworkShows(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	netID := "net-1"
	shows := []*models.NetworkShow{
		{ID: "s-b", NetworkID: &netID, Name: "Beta", Duration: 60, Active: true},
		{ID: "s-a", NetworkID: &netID, Name: "Alpha", Duration: 60, Active: true},
		{ID: "s-inactive", NetworkID: &netID, Name: "Gone", Duration: 60, Active: false},
		{ID: "s-other", Name: "Other Net Show", Duration: 60, Active: true},
	}
	for _, s := range shows {
		if err := svc.CreateNetworkShow(ctx, s); err != nil {
			t.Fatalf("create %s: %v", s.ID, err)
		}
	}
	// gorm skips zero-value fields with a default tag on insert, so
	// deactivate s-inactive explicitly.
	if err := svc.UpdateNetworkShow(ctx, "s-inactive", map[string]any{"active": false}); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	tests := []struct {
		name      string
		networkID string
		wantIDs   []string
	}{
		{name: "network filter excludes inactive and other networks", networkID: "net-1", wantIDs: []string{"s-a", "s-b"}},
		{name: "no filter returns all active ordered by name", networkID: "", wantIDs: []string{"s-a", "s-b", "s-other"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.ListNetworkShows(ctx, tt.networkID)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("got %d shows, want %d", len(got), len(tt.wantIDs))
			}
			for i, want := range tt.wantIDs {
				if got[i].ID != want {
					t.Errorf("show[%d].ID = %q, want %q", i, got[i].ID, want)
				}
			}
		})
	}
}

func TestUpdateNetworkShow(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()

	show := &models.NetworkShow{ID: "ns-up", Name: "Before", Duration: 60, Active: true}
	if err := svc.CreateNetworkShow(ctx, show); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.UpdateNetworkShow(ctx, "ns-up", map[string]any{"name": "After", "delay_minutes": 15}); err != nil {
		t.Fatalf("update: %v", err)
	}

	var got models.NetworkShow
	if err := db.First(&got, "id = ?", "ns-up").Error; err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.Name != "After" || got.DelayMinutes != 15 {
		t.Fatalf("update not applied: %+v", got)
	}
}

func TestDeleteNetworkShowCascadesSubscriptions(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()

	show := &models.NetworkShow{ID: "ns-del", Name: "Doomed", Duration: 60, Active: true}
	if err := svc.CreateNetworkShow(ctx, show); err != nil {
		t.Fatalf("create: %v", err)
	}
	keep := &models.NetworkShow{ID: "ns-keep", Name: "Keeper", Duration: 60, Active: true}
	if err := svc.CreateNetworkShow(ctx, keep); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Subscribe(ctx, "st-1", "ns-del", "09:00", "MO", ""); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, err := svc.Subscribe(ctx, "st-1", "ns-keep", "10:00", "TU", ""); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := svc.DeleteNetworkShow(ctx, "ns-del"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var showCount, subCount int64
	db.Model(&models.NetworkShow{}).Where("id = ?", "ns-del").Count(&showCount)
	if showCount != 0 {
		t.Fatal("network show still present after delete")
	}
	db.Model(&models.NetworkSubscription{}).Where("network_show_id = ?", "ns-del").Count(&subCount)
	if subCount != 0 {
		t.Fatal("subscriptions not cascaded on delete")
	}
	db.Model(&models.NetworkSubscription{}).Where("network_show_id = ?", "ns-keep").Count(&subCount)
	if subCount != 1 {
		t.Fatal("unrelated subscription was deleted")
	}
}

func TestSubscribe(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	sub, err := svc.Subscribe(ctx, "st-1", "ns-1", "09:30", "MO,WE", "America/New_York")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if sub.LocalTime != "09:30" || sub.LocalDays != "MO,WE" || sub.Timezone != "America/New_York" {
		t.Fatalf("unexpected subscription: %+v", sub)
	}
	if !sub.Active {
		t.Fatal("expected new subscription active")
	}

	t.Run("empty timezone defaults to UTC", func(t *testing.T) {
		sub2, err := svc.Subscribe(ctx, "st-2", "ns-1", "10:00", "TU", "")
		if err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		if sub2.Timezone != "UTC" {
			t.Fatalf("Timezone = %q, want UTC", sub2.Timezone)
		}
	})

	t.Run("duplicate subscription rejected", func(t *testing.T) {
		if _, err := svc.Subscribe(ctx, "st-1", "ns-1", "11:00", "FR", ""); err == nil {
			t.Fatal("expected duplicate subscription error")
		}
	})
}

func TestUnsubscribeAndStationScoping(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()

	show := &models.NetworkShow{ID: "ns-1", Name: "Shared", Duration: 60, Active: true}
	if err := svc.CreateNetworkShow(ctx, show); err != nil {
		t.Fatalf("create: %v", err)
	}

	subA, err := svc.Subscribe(ctx, "st-a", "ns-1", "09:00", "MO", "")
	if err != nil {
		t.Fatalf("subscribe a: %v", err)
	}
	if _, err := svc.Subscribe(ctx, "st-b", "ns-1", "09:00", "MO", ""); err != nil {
		t.Fatalf("subscribe b: %v", err)
	}
	// An inactive subscription for st-a must not be listed. gorm skips
	// zero-value fields with a default tag on insert, so deactivate via Update.
	inactive := models.NewNetworkSubscription("st-a", "ns-other")
	if err := db.Create(inactive).Error; err != nil {
		t.Fatalf("seed inactive: %v", err)
	}
	if err := db.Model(inactive).Update("active", false).Error; err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	subs, err := svc.GetStationSubscriptions(ctx, "st-a")
	if err != nil {
		t.Fatalf("get subs: %v", err)
	}
	if len(subs) != 1 || subs[0].ID != subA.ID {
		t.Fatalf("expected only st-a's active subscription, got %+v", subs)
	}
	if subs[0].NetworkShow == nil || subs[0].NetworkShow.Name != "Shared" {
		t.Fatal("expected NetworkShow preloaded")
	}

	if err := svc.Unsubscribe(ctx, subA.ID); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	subs, err = svc.GetStationSubscriptions(ctx, "st-a")
	if err != nil {
		t.Fatalf("get subs: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("expected no subscriptions after unsubscribe, got %d", len(subs))
	}
}

func TestMaterializeSubscriptions(t *testing.T) {
	// 2026-07-06 is a Monday.
	weekStart := time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.AddDate(0, 0, 7)

	setup := func(t *testing.T) (*Service, *gorm.DB) {
		svc, db := newTestService(t)
		show := &models.NetworkShow{ID: "ns-1", Name: "Net Show", Duration: 60, DelayMinutes: 15, Active: true}
		if err := svc.CreateNetworkShow(context.Background(), show); err != nil {
			t.Fatalf("create show: %v", err)
		}
		return svc, db
	}

	t.Run("creates instances on matching days with delay applied", func(t *testing.T) {
		svc, db := setup(t)
		ctx := context.Background()
		if _, err := svc.Subscribe(ctx, "st-1", "ns-1", "09:30", "MO,WE", ""); err != nil {
			t.Fatalf("subscribe: %v", err)
		}

		instances, err := svc.MaterializeSubscriptions(ctx, "st-1", weekStart, weekEnd)
		if err != nil {
			t.Fatalf("materialize: %v", err)
		}
		if len(instances) != 2 {
			t.Fatalf("got %d instances, want 2 (MO and WE)", len(instances))
		}

		// 09:30 local + 15 minute delay = 09:45.
		wantStarts := []time.Time{
			time.Date(2026, 7, 6, 9, 45, 0, 0, time.UTC),
			time.Date(2026, 7, 8, 9, 45, 0, 0, time.UTC),
		}
		for i, want := range wantStarts {
			if !instances[i].StartsAt.Equal(want) {
				t.Errorf("instance[%d].StartsAt = %v, want %v", i, instances[i].StartsAt, want)
			}
			if !instances[i].EndsAt.Equal(want.Add(60 * time.Minute)) {
				t.Errorf("instance[%d].EndsAt = %v, want %v", i, instances[i].EndsAt, want.Add(60*time.Minute))
			}
			if instances[i].Status != models.ShowInstanceScheduled {
				t.Errorf("instance[%d].Status = %q, want scheduled", i, instances[i].Status)
			}
			if instances[i].ExceptionNote != "Syndicated: Net Show" {
				t.Errorf("instance[%d].ExceptionNote = %q", i, instances[i].ExceptionNote)
			}
		}

		var saved int64
		db.Model(&models.ShowInstance{}).Where("station_id = ?", "st-1").Count(&saved)
		if saved != 2 {
			t.Fatalf("expected 2 persisted instances, got %d", saved)
		}
	})

	t.Run("skips slots that conflict with existing scheduled instances", func(t *testing.T) {
		svc, db := setup(t)
		ctx := context.Background()
		if _, err := svc.Subscribe(ctx, "st-1", "ns-1", "09:30", "MO,WE", ""); err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		// Overlaps the Monday 09:45-10:45 slot.
		existing := &models.ShowInstance{
			ID:        "existing",
			ShowID:    "local-show",
			StationID: "st-1",
			StartsAt:  time.Date(2026, 7, 6, 10, 0, 0, 0, time.UTC),
			EndsAt:    time.Date(2026, 7, 6, 11, 0, 0, 0, time.UTC),
			Status:    models.ShowInstanceScheduled,
		}
		if err := db.Create(existing).Error; err != nil {
			t.Fatalf("seed conflict: %v", err)
		}

		instances, err := svc.MaterializeSubscriptions(ctx, "st-1", weekStart, weekEnd)
		if err != nil {
			t.Fatalf("materialize: %v", err)
		}
		if len(instances) != 1 {
			t.Fatalf("got %d instances, want 1 (Monday conflicts)", len(instances))
		}
		if instances[0].StartsAt.Day() != 8 {
			t.Fatalf("expected Wednesday instance, got %v", instances[0].StartsAt)
		}
	})

	t.Run("skips subscriptions without local time or days", func(t *testing.T) {
		svc, _ := setup(t)
		ctx := context.Background()
		if _, err := svc.Subscribe(ctx, "st-1", "ns-1", "", "", ""); err != nil {
			t.Fatalf("subscribe: %v", err)
		}

		instances, err := svc.MaterializeSubscriptions(ctx, "st-1", weekStart, weekEnd)
		if err != nil {
			t.Fatalf("materialize: %v", err)
		}
		if len(instances) != 0 {
			t.Fatalf("expected no instances, got %d", len(instances))
		}
	})

	t.Run("no subscriptions yields no instances", func(t *testing.T) {
		svc, _ := setup(t)
		instances, err := svc.MaterializeSubscriptions(context.Background(), "st-empty", weekStart, weekEnd)
		if err != nil {
			t.Fatalf("materialize: %v", err)
		}
		if len(instances) != 0 {
			t.Fatalf("expected no instances, got %d", len(instances))
		}
	})
}

func TestParseDays(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []int
	}{
		{name: "two days", input: "MO,WE", want: []int{1, 3}},
		{name: "lowercase with spaces", input: "mo, we ,fr", want: []int{1, 3, 5}},
		{name: "all days", input: "SU,MO,TU,WE,TH,FR,SA", want: []int{0, 1, 2, 3, 4, 5, 6}},
		{name: "invalid codes ignored", input: "XX,MO,??", want: []int{1}},
		{name: "empty string", input: "", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDays(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("parseDays(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("parseDays(%q) = %v, want %v", tt.input, got, tt.want)
				}
			}
		})
	}
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantHour int
		wantMin  int
	}{
		{name: "HH:MM", input: "09:30", wantHour: 9, wantMin: 30},
		{name: "HH:MM:SS ignores seconds", input: "23:15:45", wantHour: 23, wantMin: 15},
		{name: "midnight", input: "00:00", wantHour: 0, wantMin: 0},
		{name: "no colon", input: "7", wantHour: 0, wantMin: 0},
		{name: "empty", input: "", wantHour: 0, wantMin: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, m := parseTime(tt.input)
			if h != tt.wantHour || m != tt.wantMin {
				t.Fatalf("parseTime(%q) = (%d, %d), want (%d, %d)", tt.input, h, m, tt.wantHour, tt.wantMin)
			}
		})
	}
}

func TestContainsDay(t *testing.T) {
	days := []int{1, 3, 5}
	tests := []struct {
		day  int
		want bool
	}{
		{day: 1, want: true},
		{day: 3, want: true},
		{day: 0, want: false},
		{day: 6, want: false},
	}
	for _, tt := range tests {
		if got := containsDay(days, tt.day); got != tt.want {
			t.Errorf("containsDay(%v, %d) = %v, want %v", days, tt.day, got, tt.want)
		}
	}
	if containsDay(nil, 1) {
		t.Error("containsDay(nil, 1) should be false")
	}
}
