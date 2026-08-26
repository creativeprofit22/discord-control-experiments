package observability

import (
	"context"
	"sync/atomic"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type exportHealth struct {
	succeeded atomic.Int64
	failed    atomic.Int64
	dropped   atomic.Int64
}

type countingSpanExporter struct {
	next   sdktrace.SpanExporter
	health *exportHealth
}

func (e countingSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := e.next.ExportSpans(ctx, spans)
	if err != nil {
		e.health.failed.Add(int64(len(spans)))
	} else {
		e.health.succeeded.Add(int64(len(spans)))
	}
	return err
}

func (e countingSpanExporter) Shutdown(ctx context.Context) error { return e.next.Shutdown(ctx) }

type countingSampler struct {
	next   sdktrace.Sampler
	health *exportHealth
}

func (s countingSampler) ShouldSample(parameters sdktrace.SamplingParameters) sdktrace.SamplingResult {
	result := s.next.ShouldSample(parameters)
	if result.Decision == sdktrace.Drop {
		s.health.dropped.Add(1)
	}
	return result
}

func (s countingSampler) Description() string { return s.next.Description() }
