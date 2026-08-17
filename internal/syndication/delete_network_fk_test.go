/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package syndication

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestDeleteNetwork_CascadesShowsAndSubscriptions guards the FK-integrity bug:
// network_shows FK to networks and subscriptions FK to those shows (both ON
// DELETE NO ACTION), so on Postgres deleting a network with any show fails; the
// old handler deleted the network directly. DeleteNetwork must clear the tree.
func TestDeleteNetwork_CascadesShowsAndSubscriptions(t *testing.T) {
	db := dbtest.Open(t, &models.Station{}, &models.Network{}, &models.NetworkShow{}, &models.NetworkSubscription{})
	svc := NewService(db, zerolog.Nop())

	netID := dbtest.UUID("net")
	showID := dbtest.UUID("show")
	if err := db.Create(&models.Network{ID: netID, Name: "N", OwnerID: dbtest.UUID("owner")}).Error; err != nil {
		t.Fatalf("seed network: %v", err)
	}
	if err := db.Create(&models.NetworkShow{ID: showID, NetworkID: &netID, Name: "Show"}).Error; err != nil {
		t.Fatalf("seed network show: %v", err)
	}
	if err := db.Create(&models.Station{ID: dbtest.UUID("st"), OwnerID: dbtest.UUID("owner"), Name: "S"}).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	if err := db.Create(&models.NetworkSubscription{ID: dbtest.UUID("sub"), StationID: dbtest.UUID("st"), NetworkShowID: showID}).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	if err := svc.DeleteNetwork(context.Background(), netID); err != nil {
		t.Fatalf("DeleteNetwork: %v", err)
	}

	for _, c := range []struct {
		model any
		where string
		arg   string
	}{
		{&models.Network{}, "id = ?", netID},
		{&models.NetworkShow{}, "network_id = ?", netID},
		{&models.NetworkSubscription{}, "network_show_id = ?", showID},
	} {
		var n int64
		db.Model(c.model).Where(c.where, c.arg).Count(&n)
		if n != 0 {
			t.Errorf("%T not cleaned up: %d rows remain", c.model, n)
		}
	}
}
