package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/auth"
)

func TestHandleMediaUpload_RejectsOverLimitBody(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "big.mp3")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("a"), 1024)); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/media/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u1", StationID: "s1"}))
	rr := httptest.NewRecorder()

	a := &API{maxUploadBytes: 128}
	a.handleMediaUpload(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "file_too_large") {
		t.Fatalf("expected file_too_large error, got %s", rr.Body.String())
	}
}

func TestHandleMediaUpload_InLimitBodyParses(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("station_id", "s1"); err != nil {
		t.Fatalf("write station field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/media/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u1", StationID: "s1"}))
	rr := httptest.NewRecorder()

	a := &API{maxUploadBytes: 1024 * 1024}
	a.handleMediaUpload(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "file_required") {
		t.Fatalf("expected file_required error, got %s", rr.Body.String())
	}
}
