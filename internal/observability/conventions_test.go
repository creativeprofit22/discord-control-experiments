package observability

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/buildinfo"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/testsupport"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestMetricContractDropsIdentifiersAndUnknownClosedValues(t *testing.T) {
	for _, key := range []string{"actor_id", "guild_id", "channel_id", "workflow_id", "run_id", "resource_id", FieldCorrelationID, FieldPlanHashPrefix, "url", "content"} {
		if metricAttributeAllowed(key) {
			t.Fatalf("identifier %q allowed as metric attribute", key)
		}
	}
	spans, reader := testsupport.TelemetryExporters()
	redactor := NewRedactor()
	redactor.RegisterSecret("registered-secret-canary")
	provider, err := New(context.Background(), Options{Writer: &bytes.Buffer{}, Build: buildinfo.Info{}, Redactor: redactor, SpanExporter: spans, MetricReader: reader})
	if err != nil {
		t.Fatal(err)
	}
	provider.Logger.Info("registered-secret-canary")
	provider.ObserveWorkflow(context.Background(), application.WorkflowObservation{WorkflowID: "metric-workflow-canary", Event: "unbounded-event-canary", Outcome: "unbounded-outcome-canary"})
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	serialized := fmt.Sprint(metrics)
	if strings.Contains(serialized, "canary") || !strings.Contains(serialized, "other") {
		t.Fatalf("metric dimensions were not bounded: %s", serialized)
	}
	if !strings.Contains(serialized, "discord.telemetry.redaction.interventions") {
		t.Fatalf("redaction intervention metric missing: %s", serialized)
	}
}

type failingSpanExporter struct{}

func (failingSpanExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return errors.New("export unavailable")
}
func (failingSpanExporter) Shutdown(context.Context) error { return nil }

func TestExporterFailureAndSamplingDropsAreObservableAndContained(t *testing.T) {
	_, reader := testsupport.TelemetryExporters()
	provider, err := New(context.Background(), Options{Writer: &bytes.Buffer{}, Build: buildinfo.Info{}, Redactor: NewRedactor(), SpanExporter: failingSpanExporter{}, MetricReader: reader, SampleRatio: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Observe(context.Background(), domain.Operation{Name: "export-test", Adapter: "test"}, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("exporter failure changed operation result: %v", err)
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	serialized := fmt.Sprint(metrics)
	if !strings.Contains(serialized, "discord.telemetry.export.failed") {
		t.Fatalf("export failure metric missing: %s", serialized)
	}

	_, dropReader := testsupport.TelemetryExporters()
	dropProvider, err := New(context.Background(), Options{Writer: &bytes.Buffer{}, Build: buildinfo.Info{}, Redactor: NewRedactor(), MetricReader: dropReader, SampleRatio: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}, Remote: true})
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), parent)
	if err := dropProvider.Observe(ctx, domain.Operation{Name: "drop-test", Adapter: "test"}, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	metrics = metricdata.ResourceMetrics{}
	if err := dropReader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	if serialized = fmt.Sprint(metrics); !strings.Contains(serialized, "discord.telemetry.export.dropped") {
		t.Fatalf("sampling drop metric missing: %s", serialized)
	}
}

func TestLogsInsideSpanCarryTraceAndSpanIDs(t *testing.T) {
	spans, reader := testsupport.TelemetryExporters()
	var logs bytes.Buffer
	provider, err := New(context.Background(), Options{Writer: &logs, Build: buildinfo.Info{}, Redactor: NewRedactor(), SpanExporter: spans, MetricReader: reader})
	if err != nil {
		t.Fatal(err)
	}
	err = provider.Observe(context.Background(), domain.Operation{Name: "contract", Adapter: "test", CorrelationID: "join-key"}, func(ctx context.Context) error {
		provider.Logger.InfoContext(ctx, "inside span")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{FieldTraceID, FieldSpanID} {
		if !strings.Contains(logs.String(), `"`+key+`"`) {
			t.Fatalf("log missing %s: %s", key, logs.String())
		}
	}
}
