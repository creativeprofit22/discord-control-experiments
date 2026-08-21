package testsupport

import (
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type FixedClock struct{ Time time.Time }

func (c FixedClock) Now() time.Time { return c.Time }

func TelemetryExporters() (*tracetest.InMemoryExporter, *metric.ManualReader) {
	return tracetest.NewInMemoryExporter(), metric.NewManualReader()
}
