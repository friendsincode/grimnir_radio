/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestDJSelfService_GetMyAvailability covers handleGetMyAvailability.
func TestDJSelfService_GetMyAvailability(t *testing.T) {
	a, db := newDJSelfServiceAPITest(t)

	t.Run("unauthorized", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		a.handleGetMyAvailability(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", rr.Code)
		}
	})

	t.Run("success no station filter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req = withDJUserClaims(req, "u-me")
		rr := httptest.NewRecorder()
		a.handleGetMyAvailability(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
	})

	t.Run("success with station filter", func(t *testing.T) {
		// seed an availability
		dow := 3
		stationID := "s-me"
		db.Create(&models.DJAvailability{
			ID:        "avail-me",
			UserID:    "u-me2",
			StationID: &stationID,
			DayOfWeek: &dow,
			StartTime: "09:00",
			EndTime:   "17:00",
			Available: true,
		}) //nolint:errcheck
		req := httptest.NewRequest("GET", "/?station_id=s-me", nil)
		req = withDJUserClaims(req, "u-me2")
		rr := httptest.NewRecorder()
		a.handleGetMyAvailability(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
		_ = db
	})
}

// TestDJSelfService_GetUserAvailabilityMore covers handleGetUserAvailability extra paths.
func TestDJSelfService_GetUserAvailabilityMore(t *testing.T) {
	a, db := newDJSelfServiceAPITest(t)

	t.Run("missing userID returns 400", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		// No chi param
		rr := httptest.NewRecorder()
		a.handleGetUserAvailability(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("success with userID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req = withChiParam(req, "userID", "u-some")
		rr := httptest.NewRecorder()
		a.handleGetUserAvailability(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
	})

	t.Run("success with userID and station filter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?station_id=s1", nil)
		req = withChiParam(req, "userID", "u-some2")
		rr := httptest.NewRecorder()
		a.handleGetUserAvailability(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
		_ = db
	})
}

// TestDJSelfService_CreateScheduleRequest_Success covers the success path + invalid type.
func TestDJSelfService_CreateScheduleRequest_ValidType(t *testing.T) {
	a, _ := newDJSelfServiceAPITest(t)

	t.Run("invalid request type", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{
			"station_id":   "s1",
			"request_type": "teleport",
		})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
		req = withDJUserClaims(req, "u1")
		rr := httptest.NewRecorder()
		a.handleCreateScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("success time_off", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{
			"station_id":   "s1",
			"request_type": string(models.RequestTypeTimeOff),
		})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
		req = withDJUserClaims(req, "u1")
		rr := httptest.NewRecorder()
		a.handleCreateScheduleRequest(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("got %d, want 201, body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("success new_show", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{
			"station_id":   "s1",
			"request_type": string(models.RequestTypeNewShow),
		})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
		req = withDJUserClaims(req, "u1")
		rr := httptest.NewRecorder()
		a.handleCreateScheduleRequest(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("got %d, want 201, body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("success swap type", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{
			"station_id":   "s1",
			"request_type": string(models.RequestTypeSwap),
		})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
		req = withDJUserClaims(req, "u1")
		rr := httptest.NewRecorder()
		a.handleCreateScheduleRequest(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("got %d, want 201, body=%s", rr.Code, rr.Body.String())
		}
	})
}

// TestDJSelfService_GetScheduleRequest covers handleGetScheduleRequest paths.
func TestDJSelfService_GetScheduleRequest(t *testing.T) {
	a, db := newDJSelfServiceAPITest(t)

	// seed a schedule request
	sr := models.ScheduleRequest{
		ID:          "req-get",
		StationID:   "s1",
		RequestType: models.RequestTypeTimeOff,
		RequesterID: "u-owner",
		Status:      models.RequestStatusPending,
	}
	if err := db.Create(&sr).Error; err != nil {
		t.Fatalf("seed request: %v", err)
	}

	t.Run("unauthorized", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req = withChiParam(req, "requestID", sr.ID)
		rr := httptest.NewRecorder()
		a.handleGetScheduleRequest(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", rr.Code)
		}
	})

	t.Run("missing requestID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req = withDJUserClaims(req, "u-owner")
		rr := httptest.NewRecorder()
		a.handleGetScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req = withDJUserClaims(req, "u-owner")
		req = withChiParam(req, "requestID", "nonexistent")
		rr := httptest.NewRecorder()
		a.handleGetScheduleRequest(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", rr.Code)
		}
	})

	t.Run("forbidden — not requester not manager", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req = withDJUserClaims(req, "u-stranger")
		req = withChiParam(req, "requestID", sr.ID)
		rr := httptest.NewRecorder()
		a.handleGetScheduleRequest(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("got %d, want 403", rr.Code)
		}
	})

	t.Run("success as owner", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req = withDJUserClaims(req, "u-owner")
		req = withChiParam(req, "requestID", sr.ID)
		rr := httptest.NewRecorder()
		a.handleGetScheduleRequest(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
	})

	t.Run("success as manager", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req = withDJUserClaims(req, "u-mgr", string(models.RoleManager))
		req = withChiParam(req, "requestID", sr.ID)
		rr := httptest.NewRecorder()
		a.handleGetScheduleRequest(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
	})
}

// TestDJSelfService_ApproveScheduleRequest covers handleApproveScheduleRequest.
func TestDJSelfService_ApproveScheduleRequest(t *testing.T) {
	a, db := newDJSelfServiceAPITest(t)

	srPending := models.ScheduleRequest{
		ID:          "req-approve-pending",
		StationID:   "s1",
		RequestType: models.RequestTypeCancel,
		RequesterID: "u-dj",
		Status:      models.RequestStatusPending,
	}
	srApproved := models.ScheduleRequest{
		ID:          "req-approve-done",
		StationID:   "s1",
		RequestType: models.RequestTypeTimeOff,
		RequesterID: "u-dj",
		Status:      models.RequestStatusApproved,
	}
	db.Create(&srPending)  //nolint:errcheck
	db.Create(&srApproved) //nolint:errcheck

	t.Run("missing requestID", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{}")))
		req = withDJUserClaims(req, "u-mgr", string(models.RoleManager))
		rr := httptest.NewRecorder()
		a.handleApproveScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{}")))
		req = withDJUserClaims(req, "u-mgr", string(models.RoleManager))
		req = withChiParam(req, "requestID", "nope")
		rr := httptest.NewRecorder()
		a.handleApproveScheduleRequest(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", rr.Code)
		}
	})

	t.Run("not pending", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{}")))
		req = withDJUserClaims(req, "u-mgr", string(models.RoleManager))
		req = withChiParam(req, "requestID", srApproved.ID)
		rr := httptest.NewRecorder()
		a.handleApproveScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("success with note", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"note": "approved!"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
		req = withDJUserClaims(req, "u-mgr", string(models.RoleManager))
		req = withChiParam(req, "requestID", srPending.ID)
		rr := httptest.NewRecorder()
		a.handleApproveScheduleRequest(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})
}

// TestDJSelfService_RejectScheduleRequest covers handleRejectScheduleRequest.
func TestDJSelfService_RejectScheduleRequest(t *testing.T) {
	a, db := newDJSelfServiceAPITest(t)

	srPending := models.ScheduleRequest{
		ID:          "req-reject-pending",
		StationID:   "s1",
		RequestType: models.RequestTypeTimeOff,
		RequesterID: "u-dj2",
		Status:      models.RequestStatusPending,
	}
	srApproved := models.ScheduleRequest{
		ID:          "req-reject-approved",
		StationID:   "s1",
		RequestType: models.RequestTypeTimeOff,
		RequesterID: "u-dj2",
		Status:      models.RequestStatusApproved,
	}
	db.Create(&srPending)  //nolint:errcheck
	db.Create(&srApproved) //nolint:errcheck

	t.Run("missing requestID", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{}")))
		req = withDJUserClaims(req, "u-mgr", string(models.RoleManager))
		rr := httptest.NewRecorder()
		a.handleRejectScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{}")))
		req = withDJUserClaims(req, "u-mgr", string(models.RoleManager))
		req = withChiParam(req, "requestID", "nope")
		rr := httptest.NewRecorder()
		a.handleRejectScheduleRequest(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", rr.Code)
		}
	})

	t.Run("not pending", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{}")))
		req = withDJUserClaims(req, "u-mgr", string(models.RoleManager))
		req = withChiParam(req, "requestID", srApproved.ID)
		rr := httptest.NewRecorder()
		a.handleRejectScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{}")))
		req = withDJUserClaims(req, "u-mgr", string(models.RoleManager))
		req = withChiParam(req, "requestID", srPending.ID)
		rr := httptest.NewRecorder()
		a.handleRejectScheduleRequest(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})
}

// TestDJSelfService_CancelScheduleRequest covers handleCancelScheduleRequest.
func TestDJSelfService_CancelScheduleRequest(t *testing.T) {
	a, db := newDJSelfServiceAPITest(t)

	srPending := models.ScheduleRequest{
		ID:          "req-cancel-pending",
		StationID:   "s1",
		RequestType: models.RequestTypeTimeOff,
		RequesterID: "u-owner3",
		Status:      models.RequestStatusPending,
	}
	srApproved := models.ScheduleRequest{
		ID:          "req-cancel-approved",
		StationID:   "s1",
		RequestType: models.RequestTypeTimeOff,
		RequesterID: "u-owner3",
		Status:      models.RequestStatusApproved,
	}
	db.Create(&srPending)  //nolint:errcheck
	db.Create(&srApproved) //nolint:errcheck

	t.Run("forbidden wrong user", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", nil)
		req = withDJUserClaims(req, "u-stranger2")
		req = withChiParam(req, "requestID", srPending.ID)
		rr := httptest.NewRecorder()
		a.handleCancelScheduleRequest(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("got %d, want 403", rr.Code)
		}
	})

	t.Run("not pending", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", nil)
		req = withDJUserClaims(req, "u-owner3")
		req = withChiParam(req, "requestID", srApproved.ID)
		rr := httptest.NewRecorder()
		a.handleCancelScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", nil)
		req = withDJUserClaims(req, "u-owner3")
		req = withChiParam(req, "requestID", srPending.ID)
		rr := httptest.NewRecorder()
		a.handleCancelScheduleRequest(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})
}

// TestDJSelfService_GetScheduleLock covers handleGetScheduleLock paths.
func TestDJSelfService_GetScheduleLock(t *testing.T) {
	a, db := newDJSelfServiceAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		a.handleGetScheduleLock(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("not found returns defaults", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?station_id=no-lock-station", nil)
		rr := httptest.NewRecorder()
		a.handleGetScheduleLock(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
	})

	t.Run("found returns lock", func(t *testing.T) {
		db.Create(&models.ScheduleLock{ //nolint:errcheck
			ID:             "lock-get",
			StationID:      "s-lock",
			LockBeforeDays: 5,
			MinBypassRole:  models.RoleManager,
		})
		req := httptest.NewRequest("GET", "/?station_id=s-lock", nil)
		rr := httptest.NewRecorder()
		a.handleGetScheduleLock(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
	})
}

// TestDJSelfService_UpdateScheduleLock covers handleUpdateScheduleLock paths.
func TestDJSelfService_UpdateScheduleLock(t *testing.T) {
	a, db := newDJSelfServiceAPITest(t)

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/", bytes.NewReader([]byte("{")))
		req = withDJUserClaims(req, "u-admin", string(models.RoleAdmin))
		rr := httptest.NewRecorder()
		a.handleUpdateScheduleLock(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing station_id", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"lock_before_days": 7})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(b))
		req = withDJUserClaims(req, "u-admin", string(models.RoleAdmin))
		rr := httptest.NewRecorder()
		a.handleUpdateScheduleLock(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("create new lock with dates", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{
			"station_id":       "s-newlock",
			"lock_before_days": 3,
			"min_bypass_role":  string(models.RoleAdmin),
			"locked_dates":     []string{"2026-04-01", "2026-04-02", "invalid-date"},
		})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(b))
		req = withDJUserClaims(req, "u-admin", string(models.RoleAdmin))
		rr := httptest.NewRecorder()
		a.handleUpdateScheduleLock(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("create lock: got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("update existing lock", func(t *testing.T) {
		db.Create(&models.ScheduleLock{ //nolint:errcheck
			ID:             "lock-upd",
			StationID:      "s-upd",
			LockBeforeDays: 7,
			MinBypassRole:  models.RoleManager,
		})
		b, _ := json.Marshal(map[string]any{
			"station_id":       "s-upd",
			"lock_before_days": 14,
			"min_bypass_role":  string(models.RoleAdmin),
		})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(b))
		req = withDJUserClaims(req, "u-admin", string(models.RoleAdmin))
		rr := httptest.NewRecorder()
		a.handleUpdateScheduleLock(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("update lock: got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})
}

// TestDJSelfService_ApplyScheduleRequest_MoreBranches covers additional applyScheduleRequest branches.
func TestDJSelfService_ApplyScheduleRequest_MoreBranches(t *testing.T) {
	a, db := newDJSelfServiceAPITest(t)

	// Need ShowInstance for reschedule/cancel/swap
	if err := db.AutoMigrate(&models.Station{}, &models.Show{}, &models.ShowInstance{}); err != nil {
		t.Fatalf("migrate shows: %v", err)
	}

	now := time.Now()
	db.Create(&models.ShowInstance{ //nolint:errcheck
		ID:       "inst-apply",
		ShowID:   "show-x",
		StartsAt: now,
		EndsAt:   now.Add(time.Hour),
		Status:   models.ShowInstanceScheduled,
	})

	instID := "inst-apply"

	t.Run("cancel type", func(t *testing.T) {
		a2, db2 := newDJSelfServiceAPITest(t)
		if err := db2.AutoMigrate(&models.Station{}, &models.Show{}, &models.ShowInstance{}); err != nil {
			t.Fatalf("migrate shows: %v", err)
		}
		db2.Create(&models.ShowInstance{ //nolint:errcheck
			ID:       "inst-cancel",
			ShowID:   "show-y",
			StartsAt: now,
			EndsAt:   now.Add(time.Hour),
			Status:   models.ShowInstanceScheduled,
		})
		iid := "inst-cancel"
		sr := &models.ScheduleRequest{
			ID:               "req-cancel-apply",
			RequestType:      models.RequestTypeCancel,
			TargetInstanceID: &iid,
		}
		a2.applyScheduleRequest(t.Context(), sr)
	})

	t.Run("reschedule type with proposed data", func(t *testing.T) {
		a3, db3 := newDJSelfServiceAPITest(t)
		if err := db3.AutoMigrate(&models.Station{}, &models.Show{}, &models.ShowInstance{}); err != nil {
			t.Fatalf("migrate shows: %v", err)
		}
		db3.Create(&models.ShowInstance{ //nolint:errcheck
			ID:       "inst-resched",
			ShowID:   "show-z",
			StartsAt: now,
			EndsAt:   now.Add(time.Hour),
			Status:   models.ShowInstanceScheduled,
		})
		iid := "inst-resched"
		sr := &models.ScheduleRequest{
			ID:               "req-resched",
			RequestType:      models.RequestTypeReschedule,
			TargetInstanceID: &iid,
			ProposedData: map[string]any{
				"starts_at": now.Add(2 * time.Hour).Format(time.RFC3339),
				"ends_at":   now.Add(3 * time.Hour).Format(time.RFC3339),
			},
		}
		a3.applyScheduleRequest(t.Context(), sr)
	})

	t.Run("swap type", func(t *testing.T) {
		swapUser := "u-swap"
		sr := &models.ScheduleRequest{
			ID:               "req-swap",
			RequestType:      models.RequestTypeSwap,
			TargetInstanceID: &instID,
			SwapWithUserID:   &swapUser,
		}
		a.applyScheduleRequest(t.Context(), sr)
	})
}
