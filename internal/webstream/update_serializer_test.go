/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webstream

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestUpdateWebstream_PersistsSerializedColumns guards the serializer map-write
// bug: URLs and CustomMetadata are serializer:json columns, so passing them in
// the raw updates map hands a []string / map straight to the driver for a jsonb
// column, which fails to encode. UpdateWebstream must persist both, and apply
// the URL-change side effect (current_url follows the new primary).
func TestUpdateWebstream_PersistsSerializedColumns(t *testing.T) {
	db := dbtest.Open(t, &models.Webstream{})
	svc := NewService(db, events.NewBus(), zerolog.Nop())
	defer svc.Shutdown()
	ctx := context.Background()

	id := dbtest.UUID("ws")
	ws := &models.Webstream{
		ID:        id,
		StationID: dbtest.UUID("st"),
		Name:      "S",
		URLs:      []string{"http://a/1.mp3"},
	}
	if err := svc.CreateWebstream(ctx, ws); err != nil {
		t.Fatalf("CreateWebstream: %v", err)
	}

	err := svc.UpdateWebstream(ctx, id, map[string]any{
		"urls":            []string{"http://b/1.mp3", "http://b/2.mp3"},
		"custom_metadata": map[string]any{"genre": "jazz"},
	})
	if err != nil {
		t.Fatalf("UpdateWebstream: %v", err)
	}

	var got models.Webstream
	if err := db.First(&got, "id = ?", id).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(got.URLs) != 2 || got.URLs[0] != "http://b/1.mp3" {
		t.Fatalf("URLs = %v, want [http://b/1.mp3 http://b/2.mp3]", got.URLs)
	}
	if got.CustomMetadata["genre"] != "jazz" {
		t.Fatalf("CustomMetadata = %v, want genre=jazz", got.CustomMetadata)
	}
	if got.CurrentURL != "http://b/1.mp3" {
		t.Errorf("CurrentURL = %q, want the new primary http://b/1.mp3", got.CurrentURL)
	}
}
