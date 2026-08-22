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

func TestReconciliationTelemetryMetricsTraceParentageAndRedaction(t *testing.T) {
	spans, reader := testsupport.TelemetryExporters()
	var logs bytes.Buffer
	provider, err := New(context.Background(), Options{Writer: &logs, Build: buildinfo.Info{}, Redactor: NewRedactor("approval-secret-canary"), SpanExporter: spans, MetricReader: reader})
	if err != nil {
		t.Fatal(err)
	}
	ctx, root := provider.tracer.Start(context.Background(), "reconciliation.workflow")
	provider.ObserveReconciliation(ctx, application.ReconciliationObservation{CorrelationID: "correlation", PlanHashPrefix: "abcdef123456", Outcome: "planned", OperationCount: 4, RiskCounts: map[domain.RiskClass]int{domain.RiskAdditive: 3, domain.RiskDestructive: 1}, ManagedDrift: 2, UnmanagedConflicts: 1})
	provider.ObserveReconciliation(ctx, application.ReconciliationObservation{CorrelationID: "correlation", PlanHashPrefix: "abcdef123456", RunID: "run", OperationID: "op_hash", ResourceKind: domain.ResourceChannel, Risk: domain.RiskAdditive, Outcome: "failed", Attempt: 2, Retries: 1, Partial: true, Latency: 10 * time.Millisecond})
	provider.ObserveReconciliation(ctx, application.ReconciliationObservation{CorrelationID: "correlation", PlanHashPrefix: "abcdef123456", RunID: "run", Outcome: "resumed", Resumed: true})
	root.End()
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	metricNames := map[string]bool{}
	for _, scope := range metrics.ScopeMetrics {
		for _, item := range scope.Metrics {
			metricNames[item.Name] = true
		}
	}
	for _, name := range []string{"discord.reconciliation.plan.operation_count", "discord.reconciliation.plan.risk_count", "discord.reconciliation.drift.count", "discord.reconciliation.unmanaged_conflict.count", "discord.reconciliation.step.duration", "discord.reconciliation.retry.count", "discord.reconciliation.partial.count", "discord.reconciliation.resume.count", "discord.reconciliation.run.outcome_count"} {
		if !metricNames[name] {
			t.Fatalf("missing metric %s: %v", name, metricNames)
		}
	}
	rootSpan := spans.GetSpans()[len(spans.GetSpans())-1]
	for _, span := range spans.GetSpans() {
		if strings.HasPrefix(span.Name, "reconciliation.") && span.Name != "reconciliation.workflow" && span.Parent.SpanID() != rootSpan.SpanContext.SpanID() {
			t.Fatalf("span %s parent=%s root=%s", span.Name, span.Parent.SpanID(), rootSpan.SpanContext.SpanID())
		}
	}
	serialized := fmt.Sprint(spans.GetSpans()) + fmt.Sprint(metrics) + logs.String()
	for _, forbidden := range []string{"approval-secret-canary", "webhook_url", "desired_payload", "description", "topic"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("forbidden telemetry value %q found", forbidden)
		}
	}
}
