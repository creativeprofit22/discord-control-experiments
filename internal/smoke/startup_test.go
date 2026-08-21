package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/buildinfo"
	"github.com/ken-kai/discord-control-experiments/internal/config"
	"github.com/ken-kai/discord-control-experiments/internal/observability"
	"github.com/ken-kai/discord-control-experiments/internal/testsupport"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const configJSON = `{"version":1,"discord":{"token":{"source":"env","name":"DISCORD_BOT_TOKEN"}},"mcp":{"enabled":true},"observability":{"exporter":"none"}}`
const canary = "smoke-canary-token-83fd"

type resolver struct{}

func (resolver) ResolveEnvironment(string) (string, bool) { return canary, true }

func TestStartupWithFakeTransportsEmitsTelemetryWithoutSecrets(t *testing.T) {
	resolved, err := config.Load(strings.NewReader(configJSON), resolver{})
	if err != nil {
		t.Fatal(err)
	}
	var redactor *observability.Redactor
	_ = resolved.DiscordToken.Use(func(value string) error { redactor = observability.NewRedactor(value); return nil })
	spans, reader := testsupport.TelemetryExporters()
	var logs bytes.Buffer
	build := buildinfo.Info{Version: "1.2.3", Commit: "abc123", GoVersion: "go1.27.0", Dependencies: map[string]string{}}
	provider, err := observability.New(context.Background(), observability.Options{Writer: &logs, Build: build, Redactor: redactor, SpanExporter: spans, MetricReader: reader})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown telemetry: %v", err)
		}
	})
	discord := testsupport.NewFakeTransport("discord")
	discord.FailStarts = 1
	discord.Failure = errors.New("temporary " + canary)
	mcp := testsupport.NewFakeMCP()
	app, err := application.New(discord, mcp, provider, testsupport.FixedClock{Time: time.Now()}, observability.CorrelationID)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	for _, started := range []<-chan struct{}{discord.Started, mcp.Started} {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("transport did not start")
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	startCalls, stopCalls := discord.Calls()
	if startCalls != 2 || stopCalls != 1 {
		t.Fatalf("Discord calls = %d/%d, want 2/1", startCalls, stopCalls)
	}
	if len(spans.GetSpans()) < 5 {
		t.Fatalf("got %d spans, want startup retry and shutdown spans", len(spans.GetSpans()))
	}

	var resourceMetrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &resourceMetrics); err != nil {
		t.Fatal(err)
	}
	metricPoints := 0
	for _, scope := range resourceMetrics.ScopeMetrics {
		for _, item := range scope.Metrics {
			if sum, ok := item.Data.(metricdata.Sum[int64]); ok {
				metricPoints += len(sum.DataPoints)
			}
		}
	}
	if metricPoints < 5 {
		t.Fatalf("got %d operation metric points, want at least 5", metricPoints)
	}

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) < 5 {
		t.Fatalf("got %d log records", len(lines))
	}
	correlation := ""
	seenRetryError, seenRetrySuccess := false, false
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"operation", "adapter", "correlation_id", "result", "duration_ms", "retry_count", "build.version", "build.commit"} {
			if _, ok := record[field]; !ok {
				t.Fatalf("missing %s in %s", field, line)
			}
		}
		id, _ := record["correlation_id"].(string)
		if correlation == "" {
			correlation = id
		} else if id != correlation {
			t.Fatalf("correlation IDs differ: %s and %s", correlation, id)
		}
		if record["adapter"] == "discord" && record["operation"] == "startup" && record["retry_count"] == float64(0) && record["result"] == "error" {
			seenRetryError = true
		}
		if record["adapter"] == "discord" && record["operation"] == "startup" && record["retry_count"] == float64(1) && record["result"] == "ok" {
			seenRetrySuccess = true
		}
	}
	if correlation == "" || !seenRetryError || !seenRetrySuccess {
		t.Fatalf("missing correlation or retry evidence")
	}
	serialized := logs.String() + fmt.Sprint(spans.GetSpans()) + fmt.Sprint(resourceMetrics) + fmt.Sprint(resolved.Diagnostics())
	if strings.Contains(serialized, canary) {
		t.Fatal("canary appeared in telemetry or diagnostics")
	}
}
