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

	"github.com/google/uuid"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestDJSelfService_ApplyScheduleRequest_Branches(t *testing.T) {
	a, db := newDJSelfServiceAPITest(t)

	adminClm := &auth.Claims{UserID: "u-admin", Roles: []string{string(models.RoleAdmin)}}

	t.Run("cancel type applies cancellation", func(t *testing.T) {
		instanceID := uuid.NewString()
		// Seed cancel request with targetInstanceID
		cancelReq := models.ScheduleRequest{
			ID:               uuid.NewString(),
			StationID:        "s1",
			RequesterID:      "u-dj",
			RequestType:      models.RequestTypeCancel,
			TargetInstanceID: &instanceID,
			Status:           models.RequestStatusPending,
		}
		if err := db.Create(&cancelReq).Error; err != nil {
			t.Fatalf("seed cancel request: %v", err)
		}

		req := httptest.NewRequest("PUT", "/"+cancelReq.ID+"/approve", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), adminClm))
		req = withRequestID(req, cancelReq.ID)
		rr := httptest.NewRecorder()
		a.handleApproveScheduleRequest(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("approve cancel: got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("reschedule type with valid proposedData", func(t *testing.T) {
		instanceID := uuid.NewString()
		proposedData := map[string]any{
			"starts_at": "2026-06-01T10:00:00Z",
			"ends_at":   "2026-06-01T12:00:00Z",
		}
		reschedReq := models.ScheduleRequest{
			ID:               uuid.NewString(),
			StationID:        "s1",
			RequesterID:      "u-dj",
			RequestType:      models.RequestTypeReschedule,
			TargetInstanceID: &instanceID,
			ProposedData:     proposedData,
			Status:           models.RequestStatusPending,
		}
		if err := db.Create(&reschedReq).Error; err != nil {
			t.Fatalf("seed reschedule request: %v", err)
		}

		req := httptest.NewRequest("PUT", "/"+reschedReq.ID+"/approve", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), adminClm))
		req = withRequestID(req, reschedReq.ID)
		rr := httptest.NewRecorder()
		a.handleApproveScheduleRequest(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("approve reschedule: got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("swap type with swapWithUserID", func(t *testing.T) {
		instanceID := uuid.NewString()
		swapUserID := "u-swap-target"
		swapReq := models.ScheduleRequest{
			ID:               uuid.NewString(),
			StationID:        "s1",
			RequesterID:      "u-dj",
			RequestType:      models.RequestTypeSwap,
			TargetInstanceID: &instanceID,
			SwapWithUserID:   &swapUserID,
			Status:           models.RequestStatusPending,
		}
		if err := db.Create(&swapReq).Error; err != nil {
			t.Fatalf("seed swap request: %v", err)
		}

		req := httptest.NewRequest("PUT", "/"+swapReq.ID+"/approve", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), adminClm))
		req = withRequestID(req, swapReq.ID)
		rr := httptest.NewRecorder()
		a.handleApproveScheduleRequest(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("approve swap: got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("reschedule with invalid dates in proposedData (skips update)", func(t *testing.T) {
		instanceID := uuid.NewString()
		proposedData := map[string]any{
			"starts_at": "not-a-date",
			"ends_at":   "also-not-a-date",
		}
		reschedReq2 := models.ScheduleRequest{
			ID:               uuid.NewString(),
			StationID:        "s1",
			RequesterID:      "u-dj",
			RequestType:      models.RequestTypeReschedule,
			TargetInstanceID: &instanceID,
			ProposedData:     proposedData,
			Status:           models.RequestStatusPending,
		}
		if err := db.Create(&reschedReq2).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}

		req := httptest.NewRequest("PUT", "/"+reschedReq2.ID+"/approve", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), adminClm))
		req = withRequestID(req, reschedReq2.ID)
		rr := httptest.NewRecorder()
		a.handleApproveScheduleRequest(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("approve reschedule bad dates: got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestDJSelfService_HandleUpdateScheduleLock_MorePaths(t *testing.T) {
	t.Run("invalid JSON", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		req := httptest.NewRequest("PUT", "/schedule-lock", bytes.NewReader([]byte("{")))
		rr := httptest.NewRecorder()
		a.handleUpdateScheduleLock(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("invalid JSON: got %d, want 400", rr.Code)
		}
	})

	t.Run("missing station_id", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		body, _ := json.Marshal(map[string]any{"lock_before_days": 7})
		req := httptest.NewRequest("PUT", "/schedule-lock", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleUpdateScheduleLock(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("missing station_id: got %d, want 400", rr.Code)
		}
	})

	t.Run("update existing lock", func(t *testing.T) {
		a, db := newDJSelfServiceAPITest(t)

		// Create initial lock
		body, _ := json.Marshal(map[string]any{
			"station_id":       "s2",
			"lock_before_days": 7,
			"min_bypass_role":  "admin",
		})
		req := httptest.NewRequest("PUT", "/schedule-lock", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleUpdateScheduleLock(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("create lock: got %d, want 200", rr.Code)
		}

		// Verify it exists in DB
		var lock models.ScheduleLock
		if err := db.Where("station_id = ?", "s2").First(&lock).Error; err != nil {
			t.Fatalf("lock not created: %v", err)
		}

		// Update existing lock (should hit the update branch)
		body, _ = json.Marshal(map[string]any{
			"station_id":       "s2",
			"lock_before_days": 14,
			"min_bypass_role":  "manager",
		})
		req = httptest.NewRequest("PUT", "/schedule-lock", bytes.NewReader(body))
		rr = httptest.NewRecorder()
		a.handleUpdateScheduleLock(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("update lock: got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
		var updated models.ScheduleLock
		json.NewDecoder(rr.Body).Decode(&updated) //nolint:errcheck
		if updated.LockBeforeDays != 14 {
			t.Fatalf("expected LockBeforeDays=14, got %d", updated.LockBeforeDays)
		}
	})

	t.Run("create lock with locked dates", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		body, _ := json.Marshal(map[string]any{
			"station_id":       "s3",
			"lock_before_days": 7,
			"locked_dates":     []string{"2026-04-15", "2026-05-01", "invalid-date"},
		})
		req := httptest.NewRequest("PUT", "/schedule-lock", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleUpdateScheduleLock(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("create lock with dates: got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestDJSelfService_ApproveRejectErrors(t *testing.T) {
	t.Run("approve missing requestID", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		req := httptest.NewRequest("PUT", "/requests//approve", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u-admin"}))
		rr := httptest.NewRecorder()
		a.handleApproveScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("missing requestID: got %d, want 400", rr.Code)
		}
	})

	t.Run("approve nonexistent request", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		req := httptest.NewRequest("PUT", "/requests/nonexistent/approve", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u-admin"}))
		req = withRequestID(req, "nonexistent-id")
		rr := httptest.NewRecorder()
		a.handleApproveScheduleRequest(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("nonexistent: got %d, want 404", rr.Code)
		}
	})

	t.Run("approve non-pending request", func(t *testing.T) {
		a, db := newDJSelfServiceAPITest(t)
		schedReq := models.ScheduleRequest{
			ID:          uuid.NewString(),
			StationID:   "s1",
			RequesterID: "u-dj",
			RequestType: models.RequestTypeCancel,
			Status:      models.RequestStatusApproved, // already approved
		}
		db.Create(&schedReq) //nolint:errcheck

		req := httptest.NewRequest("PUT", "/requests/"+schedReq.ID+"/approve", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u-admin"}))
		req = withRequestID(req, schedReq.ID)
		rr := httptest.NewRecorder()
		a.handleApproveScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("non-pending approve: got %d, want 400", rr.Code)
		}
	})

	t.Run("reject missing requestID", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		req := httptest.NewRequest("PUT", "/requests//reject", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u-admin"}))
		rr := httptest.NewRecorder()
		a.handleRejectScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("missing requestID: got %d, want 400", rr.Code)
		}
	})

	t.Run("reject nonexistent request", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		req := httptest.NewRequest("PUT", "/requests/nonexistent/reject", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u-admin"}))
		req = withRequestID(req, "nonexistent-id")
		rr := httptest.NewRecorder()
		a.handleRejectScheduleRequest(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("nonexistent reject: got %d, want 404", rr.Code)
		}
	})

	t.Run("reject non-pending request", func(t *testing.T) {
		a, db := newDJSelfServiceAPITest(t)
		schedReq := models.ScheduleRequest{
			ID:          uuid.NewString(),
			StationID:   "s1",
			RequesterID: "u-dj",
			RequestType: models.RequestTypeCancel,
			Status:      models.RequestStatusRejected, // already rejected
		}
		db.Create(&schedReq) //nolint:errcheck

		req := httptest.NewRequest("PUT", "/requests/"+schedReq.ID+"/reject", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u-admin"}))
		req = withRequestID(req, schedReq.ID)
		rr := httptest.NewRecorder()
		a.handleRejectScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("non-pending reject: got %d, want 400", rr.Code)
		}
	})
}

func TestDJSelfService_GetScheduleRequest_ErrorPaths(t *testing.T) {
	t.Run("missing auth", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		req := httptest.NewRequest("GET", "/requests/r1", nil)
		req = withRequestID(req, "r1")
		rr := httptest.NewRecorder()
		a.handleGetScheduleRequest(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("no auth: got %d, want 401", rr.Code)
		}
	})

	t.Run("missing requestID", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		req := httptest.NewRequest("GET", "/requests/", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u1"}))
		rr := httptest.NewRecorder()
		a.handleGetScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("missing requestID: got %d, want 400", rr.Code)
		}
	})

	t.Run("nonexistent request", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		req := httptest.NewRequest("GET", "/requests/nonexistent", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u1"}))
		req = withRequestID(req, "nonexistent")
		rr := httptest.NewRecorder()
		a.handleGetScheduleRequest(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("nonexistent: got %d, want 404", rr.Code)
		}
	})

	t.Run("forbidden - other user's request", func(t *testing.T) {
		a, db := newDJSelfServiceAPITest(t)
		schedReq := models.ScheduleRequest{
			ID:          uuid.NewString(),
			StationID:   "s1",
			RequesterID: "u-owner",
			RequestType: models.RequestTypeCancel,
			Status:      models.RequestStatusPending,
		}
		db.Create(&schedReq) //nolint:errcheck

		// Different user trying to access
		req := httptest.NewRequest("GET", "/requests/"+schedReq.ID, nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u-other"}))
		req = withRequestID(req, schedReq.ID)
		rr := httptest.NewRecorder()
		a.handleGetScheduleRequest(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("forbidden: got %d, want 403", rr.Code)
		}
	})
}

func TestDJSelfService_CancelScheduleRequest_ErrorPaths(t *testing.T) {
	t.Run("missing auth", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		req := httptest.NewRequest("DELETE", "/requests/r1", nil)
		req = withRequestID(req, "r1")
		rr := httptest.NewRecorder()
		a.handleCancelScheduleRequest(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("no auth: got %d, want 401", rr.Code)
		}
	})

	t.Run("missing requestID", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		req := httptest.NewRequest("DELETE", "/requests/", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u1"}))
		rr := httptest.NewRecorder()
		a.handleCancelScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("missing requestID: got %d, want 400", rr.Code)
		}
	})

	t.Run("nonexistent request", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		req := httptest.NewRequest("DELETE", "/requests/nonexistent", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u1"}))
		req = withRequestID(req, "nonexistent")
		rr := httptest.NewRecorder()
		a.handleCancelScheduleRequest(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("nonexistent: got %d, want 404", rr.Code)
		}
	})

	t.Run("cancel non-pending request", func(t *testing.T) {
		a, db := newDJSelfServiceAPITest(t)
		schedReq := models.ScheduleRequest{
			ID:          uuid.NewString(),
			StationID:   "s1",
			RequesterID: "u-dj",
			RequestType: models.RequestTypeCancel,
			Status:      models.RequestStatusApproved,
		}
		db.Create(&schedReq) //nolint:errcheck

		req := httptest.NewRequest("DELETE", "/requests/"+schedReq.ID, nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u-dj"}))
		req = withRequestID(req, schedReq.ID)
		rr := httptest.NewRecorder()
		a.handleCancelScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("non-pending cancel: got %d, want 400", rr.Code)
		}
	})

	t.Run("cancel another user's request (forbidden)", func(t *testing.T) {
		a, db := newDJSelfServiceAPITest(t)
		schedReq := models.ScheduleRequest{
			ID:          uuid.NewString(),
			StationID:   "s1",
			RequesterID: "u-owner",
			RequestType: models.RequestTypeCancel,
			Status:      models.RequestStatusPending,
		}
		db.Create(&schedReq) //nolint:errcheck

		req := httptest.NewRequest("DELETE", "/requests/"+schedReq.ID, nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u-other"}))
		req = withRequestID(req, schedReq.ID)
		rr := httptest.NewRecorder()
		a.handleCancelScheduleRequest(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("forbidden cancel: got %d, want 403", rr.Code)
		}
	})
}

func TestDJSelfService_Availability_MorePaths(t *testing.T) {
	t.Run("create availability invalid JSON", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u1"}))
		rr := httptest.NewRecorder()
		a.handleCreateAvailability(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("invalid JSON: got %d, want 400", rr.Code)
		}
	})

	t.Run("update availability nonexistent ID", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		body, _ := json.Marshal(map[string]any{"day_of_week": 1, "start_time": "10:00", "end_time": "11:00"})
		req := httptest.NewRequest("PUT", "/nonexistent", bytes.NewReader(body))
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u1"}))
		req = withAPIRouteID(req, "nonexistent-id")
		rr := httptest.NewRecorder()
		a.handleUpdateAvailability(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("update nonexistent: got %d, want 404", rr.Code)
		}
	})

	t.Run("update availability missing times", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		body, _ := json.Marshal(map[string]any{"day_of_week": 1})
		req := httptest.NewRequest("PUT", "/nonexistent", bytes.NewReader(body))
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u1"}))
		req = withAPIRouteID(req, "nonexistent-id")
		rr := httptest.NewRecorder()
		a.handleUpdateAvailability(rr, req)
		if rr.Code != http.StatusBadRequest && rr.Code != http.StatusNotFound {
			t.Fatalf("missing times: got %d, want 400 or 404", rr.Code)
		}
	})

	t.Run("delete availability missing auth", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		req := httptest.NewRequest("DELETE", "/some-id", nil)
		req = withAPIRouteID(req, "some-id")
		rr := httptest.NewRecorder()
		a.handleDeleteAvailability(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("no auth delete: got %d, want 401", rr.Code)
		}
	})

	t.Run("delete availability nonexistent", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		req := httptest.NewRequest("DELETE", "/nonexistent", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u1"}))
		req = withAPIRouteID(req, "nonexistent-id")
		rr := httptest.NewRecorder()
		a.handleDeleteAvailability(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("delete nonexistent: got %d, want 404", rr.Code)
		}
	})
}

func TestDJSelfService_CreateScheduleRequest_MorePaths(t *testing.T) {
	t.Run("missing auth", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		body, _ := json.Marshal(map[string]any{"station_id": "s1", "request_type": "cancel"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleCreateScheduleRequest(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("no auth: got %d, want 401", rr.Code)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u1"}))
		rr := httptest.NewRecorder()
		a.handleCreateScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("invalid JSON: got %d, want 400", rr.Code)
		}
	})

	t.Run("missing station_id", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		body, _ := json.Marshal(map[string]any{"request_type": "cancel"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u1"}))
		rr := httptest.NewRecorder()
		a.handleCreateScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("missing station_id: got %d, want 400", rr.Code)
		}
	})

	t.Run("missing request_type", func(t *testing.T) {
		a, _ := newDJSelfServiceAPITest(t)
		body, _ := json.Marshal(map[string]any{"station_id": "s1"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u1"}))
		rr := httptest.NewRecorder()
		a.handleCreateScheduleRequest(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("missing request_type: got %d, want 400", rr.Code)
		}
	})
}

func TestDJSelfService_ListScheduleRequests_MorePaths(t *testing.T) {
	a, db := newDJSelfServiceAPITest(t)

	// Seed some requests
	for i, rtype := range []models.RequestType{models.RequestTypeCancel, models.RequestTypeReschedule} {
		schedReq := models.ScheduleRequest{
			ID:          uuid.NewString(),
			StationID:   "s1",
			RequesterID: "u-dj",
			RequestType: rtype,
			Status:      []models.RequestStatus{models.RequestStatusPending, models.RequestStatusApproved}[i],
		}
		db.Create(&schedReq) //nolint:errcheck
	}

	t.Run("filter by status pending", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?station_id=s1&status=pending", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u-dj"}))
		rr := httptest.NewRecorder()
		a.handleListScheduleRequests(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("list pending: got %d, want 200", rr.Code)
		}
	})

	t.Run("filter by user_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?station_id=s1&user_id=u-dj", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u-dj"}))
		rr := httptest.NewRecorder()
		a.handleListScheduleRequests(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("list by user: got %d, want 200", rr.Code)
		}
	})
}

// TestDJSelfService_ApproveScheduleRequest_NoAuth tests approve with no auth
func TestDJSelfService_ApproveScheduleRequest_NoAuth(t *testing.T) {
	a, _ := newDJSelfServiceAPITest(t)
	req := httptest.NewRequest("PUT", "/requests/r1/approve", nil)
	req = withRequestID(req, "r1")
	// No claims in context
	rr := httptest.NewRecorder()
	a.handleApproveScheduleRequest(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: got %d, want 401", rr.Code)
	}
}

// TestDJSelfService_RejectScheduleRequest_NoAuth tests reject with no auth
func TestDJSelfService_RejectScheduleRequest_NoAuth(t *testing.T) {
	a, _ := newDJSelfServiceAPITest(t)
	req := httptest.NewRequest("PUT", "/requests/r1/reject", nil)
	req = withRequestID(req, "r1")
	// No claims
	rr := httptest.NewRecorder()
	a.handleRejectScheduleRequest(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: got %d, want 401", rr.Code)
	}
}

// TestDJSelfService_GetMyAvailability_NoAuth tests handleGetMyAvailability with no auth
func TestDJSelfService_GetMyAvailability_NoAuth(t *testing.T) {
	a, _ := newDJSelfServiceAPITest(t)
	req := httptest.NewRequest("GET", "/", nil)
	// No claims
	rr := httptest.NewRecorder()
	a.handleGetMyAvailability(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: got %d, want 401", rr.Code)
	}
}

// TestDJSelfService_ScheduleLock_MissingStationID
func TestDJSelfService_ScheduleLock_GetMissingID(t *testing.T) {
	a, _ := newDJSelfServiceAPITest(t)
	req := httptest.NewRequest("GET", "/", nil)
	// station_id empty → 400
	rr := httptest.NewRecorder()
	a.handleGetScheduleLock(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestDJSelfService_UpdateAvailability_NoAuth(t *testing.T) {
	a, _ := newDJSelfServiceAPITest(t)
	body, _ := json.Marshal(map[string]any{"day_of_week": 1, "start_time": "10:00", "end_time": "11:00"})
	req := httptest.NewRequest("PUT", "/", bytes.NewReader(body))
	req = withAPIRouteID(req, "some-id")
	// No claims
	rr := httptest.NewRecorder()
	a.handleUpdateAvailability(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: got %d, want 401", rr.Code)
	}
}

// Use a unique time to avoid race in parallel subtests
var testTime = time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC)
