package observability

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/buildinfo"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/testsupport"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace"
)

func TestMCPInvocationSpanCarriesCorrelationAndCancellationClass(t *testing.T) {
	spans, reader := testsupport.TelemetryExporters()
	provider, err := New(context.Background(), Options{Writer: &bytes.Buffer{}, Build: buildinfo.Info{}, Redactor: NewRedactor(), SpanExporter: spans, MetricReader: reader})
	if err != nil {
		t.Fatal(err)
	}
	ctx, finish := provider.StartMCPInvocation(context.Background(), "get_channel", "correlation-123")
	if finish == nil || !trace.SpanFromContext(ctx).IsRecording() {
		t.Fatal("MCP invocation did not start a recording span")
	}
	finish("cancelled", "", time.Millisecond)
	if trace.SpanFromContext(ctx).IsRecording() {
		t.Fatal("finished MCP span is still recording")
	}

	exported := spans.GetSpans()
	if len(exported) != 1 {
		t.Fatalf("exported spans = %d", len(exported))
	}
	span := exported[0]
	if span.Name != "mcp.tool" || span.Status.Code.String() != "Error" || span.Status.Description != "cancelled" {
		t.Fatalf("span name=%q status=%#v", span.Name, span.Status)
	}
	serialized := fmt.Sprint(span)
	for _, expected := range []string{"mcp.tool", "get_channel", "mcp.result", "cancelled", "correlation.id", "correlation-123"} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("span missing %q: %s", expected, serialized)
		}
	}
}

func TestMCPPolicyReasonCardinalityUsesOnlyAllowlistedValues(t *testing.T) {
	spans, reader := testsupport.TelemetryExporters()
	provider, err := New(context.Background(), Options{Writer: &bytes.Buffer{}, Build: buildinfo.Info{}, Redactor: NewRedactor(), SpanExporter: spans, MetricReader: reader})
	if err != nil {
		t.Fatal(err)
	}
	for _, reason := range []domain.PolicyReason{
		domain.PolicyModeDenied,
		domain.PolicyCapabilityDenied,
		domain.PolicyUnknownCommand,
		domain.PolicyError,
		domain.PolicyResourceDenied,
	} {
		_, finish := provider.StartMCPInvocation(context.Background(), "get_channel", "correlation")
		finish("policy_denied", reason, time.Millisecond)
	}
	_, finish := provider.StartMCPInvocation(context.Background(), "get_channel", "correlation")
	finish("policy_denied", domain.PolicyReason("raw-policy-reason-canary"), time.Millisecond)

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	serialized := fmt.Sprint(spans.GetSpans()) + fmt.Sprint(metrics)
	if !strings.Contains(serialized, "mcp.policy_reason") {
		t.Fatalf("policy reason attribute missing: %s", serialized)
	}
	for _, reason := range []domain.PolicyReason{
		domain.PolicyModeDenied,
		domain.PolicyCapabilityDenied,
		domain.PolicyUnknownCommand,
		domain.PolicyError,
		domain.PolicyResourceDenied,
	} {
		if !strings.Contains(serialized, string(reason)) {
			t.Fatalf("allowlisted policy reason %q missing", reason)
		}
	}
	if strings.Contains(serialized, "raw-policy-reason-canary") {
		t.Fatal("unknown policy reason became a telemetry dimension")
	}
}
