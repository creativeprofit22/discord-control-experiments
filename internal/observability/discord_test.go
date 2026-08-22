package observability

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/buildinfo"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/testsupport"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestSecretCanariesAbsentFromEveryExporter(t *testing.T) {
	bot, webhook := "bot-token-export-canary", "https://discord.com/api/webhooks/export-canary"
	spans, reader := testsupport.TelemetryExporters()
	var logs bytes.Buffer
	redactor := NewRedactor()
	redactor.RegisterSecret(bot)
	redactor.RegisterSecret(webhook)
	provider, err := New(context.Background(), Options{Writer: &logs, Build: buildinfo.Info{}, Redactor: redactor, SpanExporter: spans, MetricReader: reader})
	if err != nil {
		t.Fatal(err)
	}
	provider.Logger.Error("secret surface "+bot, slog.Group("nested", slog.String("webhook", webhook)), slog.String("error", bot))
	err = provider.Observe(context.Background(), domain.Operation{Name: "canary.operation", Adapter: "test", CorrelationID: "safe"}, func(context.Context) error { return errors.New(bot + " " + webhook) })
	if err == nil {
		t.Fatal("expected observed error")
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	serialized := fmt.Sprint(spans.GetSpans()) + fmt.Sprint(metrics) + logs.String()
	if strings.Contains(serialized, bot) || strings.Contains(serialized, webhook) {
		t.Fatalf("secret leaked from exporter: %s", serialized)
	}
	if redactor.Interventions() < 3 {
		t.Fatalf("redaction interventions = %d", redactor.Interventions())
	}
}

func TestDiscordTelemetryUsesOnlySanitizedLowCardinalityFields(t *testing.T) {
	const canary = "token-content-webhook-canary"
	spans, reader := testsupport.TelemetryExporters()
	var logs bytes.Buffer
	provider, err := New(context.Background(), Options{Writer: &logs, Build: buildinfo.Info{}, Redactor: NewRedactor(canary), SpanExporter: spans, MetricReader: reader})
	if err != nil {
		t.Fatal(err)
	}
	observation := &application.DiscordObservation{CommandName: "create_message", RouteTemplate: "/channels/{channel.id}/messages", Method: "POST", Attempts: 2, StatusClass: "5xx", RetryReason: "server_error", Partial: &domain.PartialResult{Outcome: domain.PartialSentUnknown}}
	err = provider.ObserveDiscordCommand(context.Background(), observation, func(context.Context) error { return errors.New("failed " + canary) })
	if err == nil {
		t.Fatal("expected observed error")
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	serialized := fmt.Sprint(spans.GetSpans()) + fmt.Sprint(metrics) + logs.String()
	if strings.Contains(serialized, canary) {
		t.Fatal("canary leaked into telemetry")
	}
	for _, forbidden := range []string{"channel.id=", "message.content", "webhook_url", "filename", "audit_reason", "Authorization"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("forbidden telemetry field %q", forbidden)
		}
	}
	names := map[string]bool{}
	for _, span := range spans.GetSpans() {
		names[span.Name] = true
	}
	if !names["discord.command.create_message"] || !names["discord.rest.attempt"] {
		t.Fatalf("missing Discord spans: %v", names)
	}
	metricNames := map[string]bool{}
	for _, scope := range metrics.ScopeMetrics {
		for _, metric := range scope.Metrics {
			metricNames[metric.Name] = true
		}
	}
	for _, name := range []string{"discord.rest.request.count", "discord.rest.request.duration", "discord.rest.ratelimit.wait", "discord.rest.retry.count", "discord.command.partial.count"} {
		if !metricNames[name] {
			t.Fatalf("missing metric %s", name)
		}
	}
}
