package observability

import (
	"context"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/ken-kai/discord-control-experiments/internal/buildinfo"
	"github.com/ken-kai/discord-control-experiments/internal/testsupport"
)

func TestCLIFailureClasses(t *testing.T) {
	spans, reader := testsupport.TelemetryExporters()
	provider, err := New(context.Background(), Options{Writer: discardWriter{}, Build: buildinfo.Info{}, Redactor: NewRedactor(), SpanExporter: spans, MetricReader: reader})
	if err != nil {
		t.Fatal(err)
	}
	for _, class := range []string{"parse", "policy", "discord", "reconciliation", "timeout", "internal", "interrupted", "attacker-controlled"} {
		provider.RecordCLIFailure(context.Background(), class)
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	serialized := fmt.Sprint(metrics)
	if containsAll(serialized, "attacker-controlled") || !containsAll(serialized, "discord.cli.failure.count", "cli.failure.class") {
		t.Fatalf("unexpected metrics: %s", serialized)
	}
}

type discardWriter struct{}

func (discardWriter) Write(payload []byte) (int, error) { return len(payload), nil }
