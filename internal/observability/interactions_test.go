package observability

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/buildinfo"
	"github.com/ken-kai/discord-control-experiments/internal/testsupport"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestInteractionLoadMetricsAreGaugesWithCurrentValues(t *testing.T) {
	_, reader := testsupport.TelemetryExporters()
	provider, err := New(context.Background(), Options{
		Writer:       &bytes.Buffer{},
		Build:        buildinfo.Info{},
		Redactor:     NewRedactor(),
		MetricReader: reader,
	})
	if err != nil {
		t.Fatal(err)
	}

	provider.ObserveQueueDepth(context.Background(), 2)
	provider.ObserveQueueDepth(context.Background(), 7)
	provider.ObserveHandlerActive(context.Background(), 4)
	provider.ObserveHandlerActive(context.Background(), 1)

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}

	assertInt64GaugeValue(t, metrics, "discord.interaction.queue.depth", 7)
	assertInt64GaugeValue(t, metrics, "discord.interaction.handler.active", 1)
}

func TestInteractionLoadMetricsIgnoreInvalidValues(t *testing.T) {
	_, reader := testsupport.TelemetryExporters()
	provider, err := New(context.Background(), Options{
		Writer:       &bytes.Buffer{},
		Build:        buildinfo.Info{},
		Redactor:     NewRedactor(),
		MetricReader: reader,
	})
	if err != nil {
		t.Fatal(err)
	}

	provider.ObserveQueueDepth(context.Background(), 3)
	provider.ObserveQueueDepth(context.Background(), -1)
	provider.ObserveHandlerActive(context.Background(), 2)
	provider.ObserveHandlerActive(context.Background(), -1)

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}

	assertInt64GaugeValue(t, metrics, "discord.interaction.queue.depth", 3)
	assertInt64GaugeValue(t, metrics, "discord.interaction.handler.active", 2)
}

func TestCallbackDurationUsesOnlyBoundedAttemptAttributes(t *testing.T) {
	_, reader := testsupport.TelemetryExporters()
	provider, err := New(context.Background(), Options{
		Writer:       &bytes.Buffer{},
		Build:        buildinfo.Info{},
		Redactor:     NewRedactor(),
		MetricReader: reader,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider.ObserveCallbackAttempt(context.Background(), "edit_original", "5xx", 12*time.Millisecond)
	provider.ObserveCallbackAttempt(context.Background(), "edit_original", "2xx", 8*time.Millisecond)
	provider.ObserveCallbackAttempt(context.Background(), "followup", "transport_error", 3*time.Millisecond)
	provider.ObserveCallbackAttempt(context.Background(), "unbounded-operation", "2xx", time.Millisecond)
	provider.ObserveCallbackAttempt(context.Background(), "followup", "418", time.Millisecond)
	provider.ObserveCallbackAttempt(context.Background(), "followup", "4xx", -time.Millisecond)

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	for _, scope := range metrics.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != "discord.interaction.callback.duration" {
				continue
			}
			histogram, ok := candidate.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("callback duration data type = %T", candidate.Data)
			}
			var count uint64
			for _, point := range histogram.DataPoints {
				count += point.Count
				attributes := point.Attributes.ToSlice()
				if len(attributes) != 2 {
					t.Fatalf("callback duration attributes = %#v", attributes)
				}
				for _, item := range attributes {
					key := string(item.Key)
					if key != "interaction.operation" && key != "http.response.status_class" {
						t.Fatalf("unexpected callback duration attribute %q", key)
					}
				}
			}
			if count != 3 {
				t.Fatalf("callback duration count = %d, want 3", count)
			}
			return
		}
	}
	t.Fatal("callback duration metric not found")
}

func TestApprovalMetricsUseOnlyClosedOutcomeAttributes(t *testing.T) {
	_, reader := testsupport.TelemetryExporters()
	provider, err := New(context.Background(), Options{
		Writer:       &bytes.Buffer{},
		Build:        buildinfo.Info{},
		Redactor:     NewRedactor(),
		MetricReader: reader,
	})
	if err != nil {
		t.Fatal(err)
	}
	allowed := []string{"accepted", "denied", "expired", "replayed", "wrong_actor", "wrong_guild", "changed_plan", "issued"}
	for _, outcome := range allowed {
		provider.RecordApproval(context.Background(), outcome)
	}
	provider.RecordApproval(context.Background(), "user-333@guild-222")

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	for _, scope := range metrics.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != "discord.interaction.approval.count" {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("approval metric data type = %T", candidate.Data)
			}
			got := make(map[string]int64, len(sum.DataPoints))
			for _, point := range sum.DataPoints {
				attributes := point.Attributes.ToSlice()
				if len(attributes) != 1 || string(attributes[0].Key) != "interaction.outcome" {
					t.Fatalf("approval attributes = %#v, want only interaction.outcome", attributes)
				}
				got[attributes[0].Value.AsString()] = point.Value
			}
			if len(got) != len(allowed) {
				t.Fatalf("approval outcomes = %v, want %v", got, allowed)
			}
			for _, outcome := range allowed {
				if got[outcome] != 1 {
					t.Fatalf("approval outcome %q count = %d, want 1", outcome, got[outcome])
				}
			}
			return
		}
	}
	t.Fatal("approval metric not found")
}

func assertInt64GaugeValue(t *testing.T, metrics metricdata.ResourceMetrics, name string, want int64) {
	t.Helper()
	for _, scope := range metrics.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != name {
				continue
			}
			gauge, ok := candidate.Data.(metricdata.Gauge[int64])
			if !ok {
				t.Fatalf("metric %s data type = %T, want metricdata.Gauge[int64]", name, candidate.Data)
			}
			if len(gauge.DataPoints) != 1 {
				t.Fatalf("metric %s data points = %d, want 1", name, len(gauge.DataPoints))
			}
			if got := gauge.DataPoints[0].Value; got != want {
				t.Fatalf("metric %s value = %d, want %d", name, got, want)
			}
			return
		}
	}
	t.Fatalf("metric %s not found", name)
}
