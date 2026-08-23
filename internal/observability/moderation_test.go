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

func TestModerationMetricsExistWithoutSensitiveIdentifiersOrContent(t *testing.T) {
	spans, reader := testsupport.TelemetryExporters()
	var logs bytes.Buffer
	provider, err := New(context.Background(), Options{Writer: &logs, Build: buildinfo.Info{}, Redactor: NewRedactor("secret-note"), SpanExporter: spans, MetricReader: reader})
	if err != nil {
		t.Fatal(err)
	}
	provider.ObserveModeration(context.Background(), application.ModerationObservation{ActionKind: domain.ActionBan, Outcome: "applied", PolicyVersion: "policy-1", Latency: time.Millisecond})
	provider.ObserveModeration(context.Background(), application.ModerationObservation{Outcome: "recommended", Reason: "escalation_rule", PolicyVersion: "policy-1"})
	provider.ObserveModeration(context.Background(), application.ModerationObservation{ActionKind: domain.ActionTimeout, Outcome: "denied", Reason: "hierarchy", PolicyVersion: "policy-1"})
	provider.ObserveLockdown(context.Background(), application.LockdownObservation{Operation: "apply", Outcome: "partial_failure", OperationCount: 2, Partial: true})
	provider.ObserveReconciliation(context.Background(), application.ReconciliationObservation{ResourceKind: domain.ResourceAutoModerationRule, Outcome: "drift", ManagedDrift: 1})
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, scope := range metrics.ScopeMetrics {
		for _, item := range scope.Metrics {
			names[item.Name] = true
		}
	}
	for _, name := range []string{"discord.moderation.action.count", "discord.moderation.action.duration", "discord.moderation.hierarchy_failure.count", "discord.moderation.escalation.count", "discord.moderation.lockdown.count", "discord.moderation.lockdown.operation_count", "discord.automod.drift.count"} {
		if !names[name] {
			t.Fatalf("missing metric %s", name)
		}
	}
	serialized := fmt.Sprint(metrics) + logs.String() + fmt.Sprint(spans.GetSpans())
	for _, forbidden := range []string{"guild-123", "subject-456", "case-789", "evidence-012", "secret-note", "keyword-content", "regex-content", "message-content"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("forbidden telemetry value %q found", forbidden)
		}
	}
}

type moderationAlertSink struct{ alerts []SecurityAlert }

func (s *moderationAlertSink) Alert(_ context.Context, alert SecurityAlert) {
	s.alerts = append(s.alerts, alert)
}

func TestModerationDestructiveAndLockdownPartialThresholdAlerts(t *testing.T) {
	now := time.Unix(1, 0)
	sink := &moderationAlertSink{}
	provider, err := New(context.Background(), Options{Writer: &bytes.Buffer{}, Redactor: NewRedactor(), Security: SecurityOptions{ModerationDestructiveThreshold: 2, ModerationDestructiveWindow: time.Minute, LockdownPartialFailureThreshold: 1, LockdownPartialFailureWindow: time.Minute, AlertSink: sink, Now: func() time.Time { return now }}})
	if err != nil {
		t.Fatal(err)
	}
	provider.RecordSecurityEvent(context.Background(), domain.SecurityEvent{Kind: "moderation_destructive_apply", DestructiveClass: domain.DestructiveBan})
	provider.RecordSecurityEvent(context.Background(), domain.SecurityEvent{Kind: "moderation_destructive_apply", DestructiveClass: domain.DestructiveBan})
	provider.RecordSecurityEvent(context.Background(), domain.SecurityEvent{Kind: "lockdown_partial_failure", DestructiveClass: domain.DestructiveLockdown})
	if len(sink.alerts) != 2 || !sink.alerts[0].Threshold || !sink.alerts[1].Threshold {
		t.Fatalf("alerts=%#v", sink.alerts)
	}
}
