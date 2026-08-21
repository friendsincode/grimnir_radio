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

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestUserUpdate_DemoteLastAdmin_Returns400 guards #82: UserUpdate (the
// non-admin-scoped user editor) must refuse to demote the last platform admin,
// the same as AdminUserUpdate. Without the guard the platform can be left with
// zero admins.
func TestUserUpdate_DemoteLastAdmin_Returns400(t *testing.T) {
	db := newAdminTestDB(t)
	h := newAdminTestHandler(t, db)
	admin := seedAdminUser(t, db)

	form := url.Values{
		"email":         {admin.Email},
		"platform_role": {string(models.PlatformRoleUser)},
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", admin.ID)
	adminCtx := models.User{ID: admin.ID, Email: admin.Email, PlatformRole: models.PlatformRoleAdmin}
	req = req.WithContext(context.WithValue(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx),
		ctxKeyUser, &adminCtx,
	))

	rr := httptest.NewRecorder()
	h.UserUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 demoting the last admin, got %d: %s", rr.Code, rr.Body.String())
	}
	// The admin must still be an admin.
	var got models.User
	if err := db.First(&got, "id = ?", admin.ID).Error; err != nil {
		t.Fatalf("reload admin: %v", err)
	}
	if got.PlatformRole != models.PlatformRoleAdmin {
		t.Fatalf("admin was demoted to %q despite being the last admin", got.PlatformRole)
	}
}
