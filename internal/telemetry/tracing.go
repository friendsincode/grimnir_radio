/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package telemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TracerConfig contains configuration for OpenTelemetry tracing.
type TracerConfig struct {
	ServiceName    string
	ServiceVersion string
	OTLPEndpoint   string // e.g., "localhost:4317"
	Enabled        bool
	SampleRate     float64 // 0.0 to 1.0
}

// TracerProvider wraps the OpenTelemetry tracer provider.
type TracerProvider struct {
	provider *sdktrace.TracerProvider
	logger   zerolog.Logger
}

// InitTracer initializes OpenTelemetry tracing with OTLP exporter.
func InitTracer(ctx context.Context, cfg TracerConfig, logger zerolog.Logger) (*TracerProvider, error) {
	if !cfg.Enabled {
		logger.Info().Msg("tracing disabled")
		// Set up a no-op tracer provider
		otel.SetTracerProvider(trace.NewNoopTracerProvider())
		return &TracerProvider{
			provider: nil,
			logger:   logger,
		}, nil
	}

	logger.Info().
		Str("service_name", cfg.ServiceName).
		Str("otlp_endpoint", cfg.OTLPEndpoint).
		Float64("sample_rate", cfg.SampleRate).
		Msg("initializing OpenTelemetry tracing")

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	// Create OTLP trace exporter
	traceExporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		otlptracegrpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}

	// Determine sampler based on sample rate
	var sampler sdktrace.Sampler
	if cfg.SampleRate >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else if cfg.SampleRate <= 0.0 {
		sampler = sdktrace.NeverSample()
	} else {
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRate)
	}

	// Create trace provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tp)

	// Set global propagator for distributed tracing
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	logger.Info().Msg("OpenTelemetry tracing initialized successfully")

	return &TracerProvider{
		provider: tp,
		logger:   logger,
	}, nil
}

// Shutdown gracefully shuts down the tracer provider.
func (tp *TracerProvider) Shutdown(ctx context.Context) error {
	if tp.provider == nil {
		return nil
	}

	tp.logger.Info().Msg("shutting down tracer provider")

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := tp.provider.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown tracer provider: %w", err)
	}

	tp.logger.Info().Msg("tracer provider shutdown complete")
	return nil
}

// Tracer returns a tracer for the given instrumentation scope.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// StartSpan starts a new span with the given name.
func StartSpan(ctx context.Context, tracerName, spanName string) (context.Context, trace.Span) {
	tracer := Tracer(tracerName)
	return tracer.Start(ctx, spanName)
}

// AddSpanAttributes adds attributes to the current span.
func AddSpanAttributes(span trace.Span, attributes map[string]any) {
	attrs := make([]attribute.KeyValue, 0, len(attributes))
	for key, value := range attributes {
		switch v := value.(type) {
		case string:
			attrs = append(attrs, attribute.String(key, v))
		case int:
			attrs = append(attrs, attribute.Int64(key, int64(v)))
		case int64:
			attrs = append(attrs, attribute.Int64(key, v))
		case float64:
			attrs = append(attrs, attribute.Float64(key, v))
		case bool:
			attrs = append(attrs, attribute.Bool(key, v))
		}
	}
	span.SetAttributes(attrs...)
}

// RecordError records an error on the current span.
func RecordError(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
	}
}
