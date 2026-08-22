/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestStationUserUpdate_OwnershipTransfer guards the 3-step ownership-transfer
// transaction (demote old owner, promote target, sync station.owner_id). The
// existing test tolerated a 500 and asserted nothing, so a rollback or a wrong
// column would ship green. This asserts all three effects.
func TestStationUserUpdate_OwnershipTransfer(t *testing.T) {
	h, stationID := newShowsWebPGTest(t)

	oldOwnerUID := dbtest.UUID("oldOwnerU")
	targetUID := dbtest.UUID("targetU")
	must(t, h.db.Create(&models.User{ID: oldOwnerUID, Email: "old@t.local"}).Error)
	must(t, h.db.Create(&models.User{ID: targetUID, Email: "target@t.local"}).Error)
	oldSU := dbtest.UUID("oldSU")
	targetSU := dbtest.UUID("targetSU")
	must(t, h.db.Create(&models.StationUser{ID: oldSU, UserID: oldOwnerUID, StationID: stationID, Role: models.StationRoleOwner}).Error)
	must(t, h.db.Create(&models.StationUser{ID: targetSU, UserID: targetUID, StationID: stationID, Role: models.StationRoleDJ}).Error)
	must(t, h.db.Model(&models.Station{}).Where("id = ?", stationID).Update("owner_id", oldOwnerUID).Error)

	admin := &models.User{ID: dbtest.UUID("admin"), Email: "a@t.local", PlatformRole: models.PlatformRoleAdmin}
	form := url.Values{"role": {string(models.StationRoleOwner)}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", targetSU)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, ctxKeyStation, &models.Station{ID: stationID})
	ctx = context.WithValue(ctx, ctxKeyUser, admin)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.StationUserUpdate(rr, req)
	if rr.Code >= 400 {
		t.Fatalf("transfer failed: %d %s", rr.Code, rr.Body.String())
	}

	var oldRow, targetRow models.StationUser
	must(t, h.db.First(&oldRow, "id = ?", oldSU).Error)
	must(t, h.db.First(&targetRow, "id = ?", targetSU).Error)
	if oldRow.Role != models.StationRoleAdmin {
		t.Errorf("previous owner not demoted, role = %q", oldRow.Role)
	}
	if targetRow.Role != models.StationRoleOwner {
		t.Errorf("target not promoted to owner, role = %q", targetRow.Role)
	}
	var st models.Station
	must(t, h.db.First(&st, "id = ?", stationID).Error)
	if st.OwnerID != targetUID {
		t.Errorf("station.owner_id not synced, = %q want target", st.OwnerID)
	}
}
