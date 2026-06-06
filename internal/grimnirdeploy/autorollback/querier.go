/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package autorollback

import (
	"context"
	"fmt"
	"time"

	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// Querier is the surface the Monitor depends on. Production wires a
// PromQuerier built around the Prometheus HTTP v1 client; tests pass in a
// fake that returns canned (value, err) tuples per query string.
//
// Contract: the implementation MUST return a float64 scalar. For PromQL
// vector results, the impl picks the first sample's value. For empty
// vectors, the impl returns (0, nil) — an empty result is "no breach", not
// an error.
type Querier interface {
	Query(ctx context.Context, promql string, at time.Time) (float64, error)
}

// PromQuerier is the production Querier backed by Prometheus's HTTP v1 API.
// Built via NewPromQuerier.
type PromQuerier struct {
	api promv1.API
}

// NewPromQuerier dials a Prometheus server at baseURL (e.g.
// "http://prometheus:9090") and returns a Querier. The underlying
// promapi.Client uses Prometheus's default HTTP transport; a non-2xx
// response surfaces as a query error rather than panicking.
func NewPromQuerier(baseURL string) (*PromQuerier, error) {
	client, err := promapi.NewClient(promapi.Config{Address: baseURL})
	if err != nil {
		return nil, fmt.Errorf("autorollback: build prom client: %w", err)
	}
	return &PromQuerier{api: promv1.NewAPI(client)}, nil
}

// Query runs the PromQL expression at the given evaluation timestamp. Vector
// results collapse to the first sample's value; an empty vector returns 0.
// Scalar results return the bare value. Warnings from Prometheus are
// ignored — they're informational, not failures.
func (p *PromQuerier) Query(ctx context.Context, q string, at time.Time) (float64, error) {
	val, _, err := p.api.Query(ctx, q, at)
	if err != nil {
		return 0, err
	}
	switch v := val.(type) {
	case *model.Scalar:
		return float64(v.Value), nil
	case model.Vector:
		if len(v) == 0 {
			return 0, nil
		}
		return float64(v[0].Value), nil
	case *model.String:
		return 0, fmt.Errorf("autorollback: query %q returned string, expected scalar/vector", q)
	default:
		return 0, fmt.Errorf("autorollback: query %q returned unexpected type %T", q, val)
	}
}
