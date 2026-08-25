package observability

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/buildinfo"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/testsupport"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestWorkflowTelemetryCoversEventsWithoutContentOrSecrets(t *testing.T) {
	spans, reader := testsupport.TelemetryExporters()
	provider, err := New(context.Background(), Options{Writer: &bytes.Buffer{}, Build: buildinfo.Info{}, Redactor: NewRedactor("secret-canary"), SpanExporter: spans, MetricReader: reader})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"queue_depth", "schedule_due", "lease_contention", "retry", "dead_letter", "source_failure", "deduplicated", "published", "approval_wait", "partial_failure"} {
		provider.ObserveWorkflow(context.Background(), application.WorkflowObservation{WorkflowID: "workflow-1", RunID: "run-1", GuildID: "101", StepID: "step-1", Event: event, Outcome: "recorded", Lag: 2 * time.Second, Provenance: domain.AutomationProvenance{Definition: "hash-1", SourceID: "source-1", SourceURL: "https://secret-canary.example/content"}})
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	serialized := fmt.Sprint(spans.GetSpans()) + fmt.Sprint(metrics)
	if !strings.Contains(serialized, "workflow.events") || !strings.Contains(serialized, "workflow.schedule.lag") {
		t.Fatalf("missing workflow metrics: %s", serialized)
	}
	if strings.Contains(serialized, "secret-canary") || strings.Contains(serialized, "SourceURL") {
		t.Fatal("workflow telemetry retained content or secret URL")
	}
	for _, event := range []string{"queue_depth", "lease_contention", "dead_letter", "partial_failure"} {
		if !strings.Contains(serialized, event) {
			t.Fatalf("missing workflow event %s", event)
		}
	}
}
