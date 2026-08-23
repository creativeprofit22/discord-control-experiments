package observability

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/buildinfo"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/testsupport"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type captureAlertSink struct {
	mu     sync.Mutex
	alerts []SecurityAlert
}

func (s *captureAlertSink) Alert(_ context.Context, alert SecurityAlert) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alerts = append(s.alerts, alert)
}
func (s *captureAlertSink) count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.alerts) }

func securityTestProvider(t *testing.T, options SecurityOptions) (*Provider, *captureAlertSink) {
	t.Helper()
	sink, _ := options.AlertSink.(*captureAlertSink)
	if sink == nil {
		sink = &captureAlertSink{}
		options.AlertSink = sink
	}
	spans, reader := testsupport.TelemetryExporters()
	provider, err := New(context.Background(), Options{Writer: &bytes.Buffer{}, Build: buildinfo.Info{}, Redactor: NewRedactor(), SpanExporter: spans, MetricReader: reader, Security: options})
	if err != nil {
		t.Fatal(err)
	}
	return provider, sink
}

func TestSecurityAlertsImmediateEvents(t *testing.T) {
	provider, sink := securityTestProvider(t, SecurityOptions{})
	for _, kind := range []domain.SecurityEventKind{"cross_guild", "policy_error", "approval_bypass", "approval_invalid", "redaction_intervention", "audit_precommit_failure", "audit_completion_failure"} {
		provider.RecordSecurityEvent(context.Background(), domain.SecurityEvent{Kind: kind, Reason: domain.PolicyError, Mode: domain.ModeApply, DestructiveClass: domain.DestructiveDelete, CorrelationID: "safe-correlation"})
	}
	if sink.count() != 7 {
		t.Fatalf("immediate alerts = %d", sink.count())
	}
}

func TestInteractionSecurityAlertThresholds(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	sink := &captureAlertSink{}
	provider, _ := securityTestProvider(t, SecurityOptions{SignatureFailureThreshold: 2, SignatureFailureWindow: time.Minute, AckDeadlineMissThreshold: 2, AckDeadlineMissWindow: time.Minute, CallbackFailureThreshold: 2, CallbackFailureWindow: time.Minute, HandlerSaturationThreshold: 2, HandlerSaturationWindow: time.Minute, AlertSink: sink, Now: func() time.Time { return now }})
	for range 2 {
		provider.RecordVerification(context.Background(), "invalid", -1)
		provider.ObserveAcknowledgement(context.Background(), "command", time.Millisecond, true)
		provider.RecordCallback(context.Background(), "failure")
		provider.RecordSaturation(context.Background())
	}
	if sink.count() != 4 {
		t.Fatalf("interaction threshold alerts = %d", sink.count())
	}
	provider.RecordDuplicate(context.Background(), "cached")
	provider.ObserveDeferred(context.Background(), time.Millisecond, "ok")
	provider.RecordApproval(context.Background(), "accepted")
	provider.ObserveQueueDepth(context.Background(), 2)
	provider.ObserveHandlerActive(context.Background(), 1)
	provider.RecordRegistration(context.Background(), "updated")
}

func TestSecurityAlertsThresholdWindows(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }
	provider, sink := securityTestProvider(t, SecurityOptions{DenialThreshold: 3, DenialWindow: time.Minute, DestructiveThreshold: 2, DestructiveWindow: time.Minute, Now: nowFn})
	denial := domain.SecurityEvent{Kind: "policy_denial", Reason: domain.PolicyCapabilityDenied, Mode: domain.ModeApply}
	for range 5 {
		provider.RecordSecurityEvent(context.Background(), denial)
	}
	if sink.count() != 1 {
		t.Fatalf("denial alerts = %d", sink.count())
	}
	now = now.Add(time.Minute + time.Second)
	for range 3 {
		provider.RecordSecurityEvent(context.Background(), denial)
	}
	if sink.count() != 2 {
		t.Fatalf("next-window denial alerts = %d", sink.count())
	}
	destructive := domain.SecurityEvent{Kind: "destructive_apply", Reason: domain.PolicyAllowed, Mode: domain.ModeApply, DestructiveClass: domain.DestructiveBan}
	provider.RecordSecurityEvent(context.Background(), destructive)
	provider.RecordSecurityEvent(context.Background(), destructive)
	if sink.count() != 3 {
		t.Fatalf("destructive alerts = %d", sink.count())
	}
}

func TestApprovalLatencyAndStableDimensions(t *testing.T) {
	spans, reader := testsupport.TelemetryExporters()
	sink := &captureAlertSink{}
	provider, err := New(context.Background(), Options{Writer: &bytes.Buffer{}, Build: buildinfo.Info{}, Redactor: NewRedactor(), SpanExporter: spans, MetricReader: reader, Security: SecurityOptions{AlertSink: sink}})
	if err != nil {
		t.Fatal(err)
	}
	provider.ObserveApprovalLatency(context.Background(), 150*time.Millisecond)
	provider.RecordSecurityEvent(context.Background(), domain.SecurityEvent{Kind: "policy_denial", Reason: domain.PolicyGuildDenied, Mode: domain.ModeDryRun, DestructiveClass: domain.DestructiveNone, CorrelationID: "must-not-be-a-metric-dimension"})
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	serialized := fmt.Sprint(metrics)
	if !containsAll(serialized, "discord.approval.latency", "discord.security.event.count", "security.policy.reason") {
		t.Fatalf("metrics missing stable dimensions: %s", serialized)
	}
	if containsAll(serialized, "must-not-be-a-metric-dimension") {
		t.Fatal("correlation ID became a metric dimension")
	}
}

func containsAll(value string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(value, needle) {
			return false
		}
	}
	return true
}
