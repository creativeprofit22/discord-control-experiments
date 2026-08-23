package interactions

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type callbackRoundTripper struct {
	mu       sync.Mutex
	requests []*http.Request
	statuses []int
}

func (r *callbackRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, request.Clone(request.Context()))
	status := http.StatusOK
	if len(r.statuses) > 0 {
		status = r.statuses[0]
		r.statuses = r.statuses[1:]
	}
	headers := http.Header{}
	if status == 429 {
		headers.Set("Retry-After", "0")
	}
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(`{}`)), Request: request}, nil
}

type callbackObservation struct {
	operation   string
	statusClass string
	duration    time.Duration
}

type callbackTelemetryRecorder struct {
	observations []callbackObservation
}

func (r *callbackTelemetryRecorder) StartInteractionSpan(ctx context.Context, _ string, _ domain.CorrelationID) (context.Context, func(string)) {
	return ctx, func(string) {}
}

func (r *callbackTelemetryRecorder) ObserveCallbackAttempt(_ context.Context, operation, statusClass string, duration time.Duration) {
	r.observations = append(r.observations, callbackObservation{operation: operation, statusClass: statusClass, duration: duration})
}

func TestCallbackClientFixedOriginAndRetrySafety(t *testing.T) {
	roundTripper := &callbackRoundTripper{statuses: []int{500, 200, 500}}
	telemetry := &callbackTelemetryRecorder{}
	client, err := NewCallbackClient(&http.Client{Transport: roundTripper}, "123456789012345678", time.Second, 4096, 4096, telemetry)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.EditOriginal(context.Background(), "correlation", "abc.DEF_123", CallbackData{Content: "ok", Flags: EphemeralFlag}); err != nil {
		t.Fatal(err)
	}
	if len(roundTripper.requests) != 2 {
		t.Fatalf("PATCH attempts = %d", len(roundTripper.requests))
	}
	request := roundTripper.requests[0]
	if request.Method != http.MethodPatch || request.URL.String() != "https://discord.com/api/v10/webhooks/123456789012345678/abc.DEF_123/messages/@original" || request.Header.Get("Authorization") != "" {
		t.Fatalf("unsafe callback request: %s %s", request.Method, request.URL)
	}
	if err := client.Followup(context.Background(), "correlation", "abc.DEF_123", CallbackData{Content: "one"}); err == nil {
		t.Fatal("failed follow-up reported success")
	}
	if len(roundTripper.requests) != 3 {
		t.Fatalf("POST was retried: %d requests", len(roundTripper.requests))
	}
	wantObservations := []callbackObservation{
		{operation: "edit_original", statusClass: "5xx"},
		{operation: "edit_original", statusClass: "2xx"},
		{operation: "followup", statusClass: "5xx"},
	}
	if len(telemetry.observations) != len(wantObservations) {
		t.Fatalf("callback observations = %#v", telemetry.observations)
	}
	for index, want := range wantObservations {
		got := telemetry.observations[index]
		if got.operation != want.operation || got.statusClass != want.statusClass || got.duration < 0 {
			t.Errorf("callback observation %d = %#v, want operation=%q status=%q and non-negative duration", index, got, want.operation, want.statusClass)
		}
	}
	if err := client.EditOriginal(context.Background(), "correlation", "../../token", CallbackData{}); err == nil {
		t.Fatal("unsafe token accepted")
	}
}
