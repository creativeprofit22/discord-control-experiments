package interactions

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/buildinfo"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/observability"
	"github.com/ken-kai/discord-control-experiments/internal/testsupport"
	"go.opentelemetry.io/otel/trace"
)

type tracingPolicy struct {
	observed chan tracingObservation
}

type tracingObservation struct {
	spanContext   trace.SpanContext
	correlationID domain.CorrelationID
}

func (p tracingPolicy) Decide(ctx context.Context, request domain.PolicyRequest) (domain.PolicyDecision, error) {
	p.observed <- tracingObservation{spanContext: trace.SpanContextFromContext(ctx), correlationID: request.CorrelationID}
	return domain.PolicyDecision{Allowed: true, ActorID: "interaction-actor", EffectiveMode: domain.ModeReadOnly, Reason: domain.PolicyAllowed}, nil
}

type tracingCallbackTransport struct {
	observed chan trace.SpanContext
}

func (transport tracingCallbackTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.observed <- trace.SpanContextFromContext(request.Context())
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil)), Request: request}, nil
}

func TestSignedInteractionCarriesOneTraceAndCorrelationThroughCallback(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	spanExporter, metricReader := testsupport.TelemetryExporters()
	telemetry, err := observability.New(context.Background(), observability.Options{
		Writer:       &bytes.Buffer{},
		Build:        buildinfo.Info{},
		Redactor:     observability.NewRedactor(),
		SpanExporter: spanExporter,
		MetricReader: metricReader,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = telemetry.Shutdown(context.Background()) }()

	policyObserved := make(chan tracingObservation, 1)
	service, err := application.NewInteractionService(tracingPolicy{observed: policyObserved}, new(application.HealthService), &routerJobsStub{})
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewInteractionStore(64, 15*time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	pending, err := NewPendingStore(64, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewExecutor(8, 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer executor.Stop()

	callbackObserved := make(chan trace.SpanContext, 1)
	callbacks, err := NewCallbackClient(&http.Client{Transport: tracingCallbackTransport{observed: callbackObserved}}, "123456789012345678", time.Second, 4096, 4096, telemetry)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := NewRenderer(callbacks)
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(service, new(application.ApprovalCoordinator), store, pending, executor, renderer, telemetry, time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	publicKey, privateKey := testSigningKey(t)
	verifier, err := NewVerifier([]string{hex.EncodeToString(publicKey)}, time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	const correlationID domain.CorrelationID = "0123456789abcdef0123456789abcdef"
	server, err := NewServer(ServerOptions{
		ListenAddress: "127.0.0.1:0", Path: "/interactions", ApplicationID: "123456789012345678",
		Verifier: verifier, Responder: router, MaxBodyBytes: 4096, MaxConcurrentRequests: 2,
		AckDeadline: 2500 * time.Millisecond, TLSMode: "direct", TLSCertificate: &tls.Certificate{}, Telemetry: telemetry,
		CorrelationID: func() (domain.CorrelationID, error) { return correlationID, nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"id":"111","application_id":"123456789012345678","type":2,"token":"callback-token","guild_id":"222","member":{"user":{"id":"333"}},"data":{"id":"444","name":"discord-control","type":1,"options":[{"name":"policy","type":1,"options":[{"name":"capability","type":3,"value":"guild.inspect"}]}]}}`)
	timestamp := "1800000000"
	request := httptest.NewRequest(http.MethodPost, "https://example.test/interactions", bytes.NewReader(body))
	request.TLS = &tls.ConnectionState{}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Signature-Timestamp", timestamp)
	request.Header.Set("X-Signature-Ed25519", signInteraction(ed25519.PrivateKey(privateKey), timestamp, body))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("interaction response status = %d", response.Code)
	}

	policy := receiveTracingObservation(t, policyObserved)
	callbackSpanContext := receiveSpanContext(t, callbackObserved)
	if policy.correlationID != correlationID {
		t.Fatalf("policy correlation = %q, want %q", policy.correlationID, correlationID)
	}
	if !policy.spanContext.IsValid() || policy.spanContext.TraceID() != callbackSpanContext.TraceID() {
		t.Fatalf("policy trace %s and callback trace %s do not match", policy.spanContext.TraceID(), callbackSpanContext.TraceID())
	}

	deadline := time.Now().Add(time.Second)
	for len(spanExporter.GetSpans()) < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	spans := spanExporter.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("interaction spans = %d, want 3", len(spans))
	}
	stages := map[string]bool{}
	for _, span := range spans {
		if span.SpanContext.TraceID() != policy.spanContext.TraceID() {
			t.Fatalf("span %q trace = %s, want %s", span.Name, span.SpanContext.TraceID(), policy.spanContext.TraceID())
		}
		for _, item := range span.Attributes {
			switch string(item.Key) {
			case "interaction.stage":
				stages[item.Value.AsString()] = true
			case "interaction.outcome":
			case "correlation_id":
				if item.Value.AsString() != string(correlationID) {
					t.Fatalf("span correlation = %q, want %q", item.Value.AsString(), correlationID)
				}
			default:
				t.Fatalf("unbounded interaction span attribute: %q", item.Key)
			}
		}
	}
	for _, stage := range []string{"inbound", "deferred", "callback"} {
		if !stages[stage] {
			t.Errorf("missing %s interaction span", stage)
		}
	}
}

func receiveTracingObservation(t *testing.T, values <-chan tracingObservation) tracingObservation {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for policy observation")
		return tracingObservation{}
	}
}

func receiveSpanContext(t *testing.T, values <-chan trace.SpanContext) trace.SpanContext {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for callback observation")
		return trace.SpanContext{}
	}
}
