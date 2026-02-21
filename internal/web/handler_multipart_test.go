package web

import "testing"

func TestMultipartLimit_UsesConfiguredLimit(t *testing.T) {
	h := &Handler{maxUploadBytes: 64 << 20}
	got := h.multipartLimit(1 << 30)
	if got != 64<<20 {
		t.Fatalf("expected configured limit, got %d", got)
	}
}

func TestMultipartLimit_FallsBackToDefault(t *testing.T) {
	h := &Handler{maxUploadBytes: 0}
	got := h.multipartLimit(1 << 30)
	if got != 1<<30 {
		t.Fatalf("expected default limit, got %d", got)
	}
}
