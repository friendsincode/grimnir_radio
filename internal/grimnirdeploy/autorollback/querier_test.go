/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package autorollback

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestPromQuerier_ParsesScalarResponse confirms the production binding
// against a fake Prometheus that returns the canonical /api/v1/query JSON
// envelope. The real client_golang/api/v1 library does the parsing; this
// test exercises the round-trip.
func TestPromQuerier_ParsesScalarResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/query") {
			http.NotFound(w, r)
			return
		}
		// Scalar result envelope: data.resultType=scalar, data.result=[timestamp, "value"].
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"scalar","result":[1700000000,"42.5"]}}`)
	}))
	defer srv.Close()

	q, err := NewPromQuerier(srv.URL)
	if err != nil {
		t.Fatalf("NewPromQuerier: %v", err)
	}
	v, err := q.Query(context.Background(), "vector(42.5)", time.Now())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if v != 42.5 {
		t.Errorf("value = %v, want 42.5", v)
	}
}

// TestPromQuerier_ParsesVectorResponse covers the vector path (the common
// case for rate(...) queries).
func TestPromQuerier_ParsesVectorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"x"},"value":[1700000000,"7.25"]}]}}`)
	}))
	defer srv.Close()

	q, _ := NewPromQuerier(srv.URL)
	v, err := q.Query(context.Background(), "sum(x)", time.Now())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if v != 7.25 {
		t.Errorf("value = %v, want 7.25", v)
	}
}

// TestPromQuerier_EmptyVectorIsZero verifies that an empty result vector
// (the common "no data, no breach" case) returns 0 without error.
func TestPromQuerier_EmptyVectorIsZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
	}))
	defer srv.Close()

	q, _ := NewPromQuerier(srv.URL)
	v, err := q.Query(context.Background(), "absent_thing", time.Now())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if v != 0 {
		t.Errorf("empty vector value = %v, want 0", v)
	}
}

// TestPromQuerier_PropagatesServerError ensures that a non-2xx response
// surfaces as an error so the Monitor counts it toward Inconclusive rather
// than silently treating it as "no breach".
func TestPromQuerier_PropagatesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"status":"error","errorType":"internal","error":"boom"}`)
	}))
	defer srv.Close()

	q, _ := NewPromQuerier(srv.URL)
	_, err := q.Query(context.Background(), "x", time.Now())
	if err == nil {
		t.Fatal("want error from 500 response; got nil")
	}
}
