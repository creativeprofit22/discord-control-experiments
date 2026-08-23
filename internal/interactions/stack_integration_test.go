package interactions

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const (
	stackApplicationID = "123456789012345678"
	stackGuildID       = domain.GuildID("222")
	stackUserID        = "333"
	stackPlanHash      = domain.PlanHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
)

type stackClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *stackClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *stackClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}

type stackPolicy struct {
	mu       sync.Mutex
	allowed  bool
	err      error
	requests []domain.PolicyRequest
}

func (policy *stackPolicy) Decide(_ context.Context, request domain.PolicyRequest) (domain.PolicyDecision, error) {
	policy.mu.Lock()
	defer policy.mu.Unlock()
	policy.requests = append(policy.requests, request)
	if policy.err != nil {
		return domain.PolicyDecision{}, policy.err
	}
	return domain.PolicyDecision{Allowed: policy.allowed, ActorID: "interaction-actor", EffectiveMode: request.Mode, Reason: domain.PolicyAllowed}, nil
}

type stackJobs struct {
	mu      sync.Mutex
	record  application.JobRecord
	gets    int
	cancels int
	resumes int
}

func (jobs *stackJobs) List(context.Context) ([]application.JobRecord, error) {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	return []application.JobRecord{jobs.record}, nil
}

func (jobs *stackJobs) Get(context.Context, domain.RunID) (application.JobRecord, error) {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	jobs.gets++
	return jobs.record, nil
}

func (jobs *stackJobs) Cancel(context.Context, domain.RunID) (application.JobRecord, error) {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	jobs.cancels++
	return jobs.record, nil
}

func (jobs *stackJobs) Resume(context.Context, application.ResumeRequest) (domain.ApplyResult, error) {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	jobs.resumes++
	return domain.ApplyResult{}, nil
}

func (jobs *stackJobs) counts() (gets, cancels, resumes int) {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	return jobs.gets, jobs.cancels, jobs.resumes
}

type stackCallbackTransport struct {
	mu       sync.Mutex
	status   int
	err      error
	requests int
	bodies   []string
	urls     []string
}

func (transport *stackCallbackTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(request.Body)
	transport.mu.Lock()
	transport.requests++
	transport.bodies = append(transport.bodies, string(body))
	transport.urls = append(transport.urls, request.URL.String())
	status := transport.status
	err := transport.err
	transport.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: request}, nil
}

func (transport *stackCallbackTransport) snapshot() (int, []string, []string) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.requests, append([]string(nil), transport.bodies...), append([]string(nil), transport.urls...)
}

type stackTelemetry struct {
	noopInteractionTelemetry
	mu               sync.Mutex
	callbackOutcomes []string
	spans            []string
}

func (telemetry *stackTelemetry) StartInteractionSpan(ctx context.Context, operation string, correlationID domain.CorrelationID) (context.Context, func(string)) {
	telemetry.mu.Lock()
	telemetry.spans = append(telemetry.spans, operation+":"+string(correlationID))
	telemetry.mu.Unlock()
	return ctx, func(string) {}
}
func (telemetry *stackTelemetry) ObserveCallbackAttempt(context.Context, string, string, time.Duration) {
}
func (telemetry *stackTelemetry) RecordCallback(_ context.Context, outcome string) {
	telemetry.mu.Lock()
	defer telemetry.mu.Unlock()
	telemetry.callbackOutcomes = append(telemetry.callbackOutcomes, outcome)
}
func (telemetry *stackTelemetry) outcomes() []string {
	telemetry.mu.Lock()
	defer telemetry.mu.Unlock()
	return append([]string(nil), telemetry.callbackOutcomes...)
}
func (telemetry *stackTelemetry) recordedSpans() []string {
	telemetry.mu.Lock()
	defer telemetry.mu.Unlock()
	return append([]string(nil), telemetry.spans...)
}

type stackApprovalIssuer struct {
	mu     sync.Mutex
	issues int
}

func (issuer *stackApprovalIssuer) Issue(_ context.Context, grant domain.ApprovalGrant) (domain.ApprovalGrant, error) {
	issuer.mu.Lock()
	defer issuer.mu.Unlock()
	issuer.issues++
	grant.ID = "interaction-approval"
	return grant, nil
}

func (issuer *stackApprovalIssuer) count() int {
	issuer.mu.Lock()
	defer issuer.mu.Unlock()
	return issuer.issues
}

type realStackFixture struct {
	server      *Server
	executor    *Executor
	pending     *PendingStore
	policy      *stackPolicy
	jobs        *stackJobs
	callbacks   *stackCallbackTransport
	telemetry   *stackTelemetry
	clock       *stackClock
	privateKey  ed25519.PrivateKey
	ackDeadline time.Duration
	issuer      *stackApprovalIssuer
}

func newRealStackFixture(t *testing.T, queueCapacity int) *realStackFixture {
	t.Helper()
	clock := &stackClock{now: time.Unix(1_800_000_000, 0)}
	policy := &stackPolicy{allowed: true}
	jobs := &stackJobs{record: application.JobRecord{RunID: "run-1", GuildID: stackGuildID, Status: application.JobRunning, PlanHash: stackPlanHash, PlanHashPrefix: "aaaaaaaaaaaa", Recoverable: true, TotalSteps: 2, CompletedSteps: 1}}
	service, err := application.NewInteractionService(policy, new(application.HealthService), jobs)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewInteractionStore(64, 15*time.Minute, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := NewPendingStore(64, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewExecutor(queueCapacity, 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(executor.Stop)
	telemetry := &stackTelemetry{}
	callbackTransport := &stackCallbackTransport{}
	callbackClient, err := NewCallbackClient(&http.Client{Transport: callbackTransport}, stackApplicationID, time.Second, 4096, 4096, telemetry)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := NewRenderer(callbackClient)
	if err != nil {
		t.Fatal(err)
	}
	issuer := &stackApprovalIssuer{}
	approvals, err := application.NewApprovalCoordinator(issuer, clock, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(service, approvals, store, pending, executor, renderer, telemetry, time.Minute, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey := testSigningKey(t)
	verifier, err := NewVerifier([]string{hex.EncodeToString(publicKey)}, time.Minute, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	ackDeadline := 500 * time.Millisecond
	server, err := NewServer(ServerOptions{
		ListenAddress: "127.0.0.1:0", Path: "/interactions", ApplicationID: stackApplicationID,
		Verifier: verifier, Responder: router, MaxBodyBytes: 8192, MaxConcurrentRequests: 4,
		AckDeadline: ackDeadline, TLSMode: "direct", TLSCertificate: &tls.Certificate{}, Telemetry: telemetry,
		CorrelationID: func() (domain.CorrelationID, error) { return "stack-correlation", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return &realStackFixture{server: server, executor: executor, pending: pending, policy: policy, jobs: jobs, callbacks: callbackTransport, telemetry: telemetry, clock: clock, privateKey: privateKey, ackDeadline: ackDeadline, issuer: issuer}
}

func (fixture *realStackFixture) request(t *testing.T, body string) (*httptest.ResponseRecorder, time.Duration) {
	t.Helper()
	timestamp := "1800000000"
	request := httptest.NewRequest(http.MethodPost, "https://example.test/interactions", strings.NewReader(body))
	request.TLS = &tls.ConnectionState{}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Signature-Timestamp", timestamp)
	request.Header.Set("X-Signature-Ed25519", signInteraction(fixture.privateKey, timestamp, []byte(body)))
	response := httptest.NewRecorder()
	started := time.Now()
	fixture.server.ServeHTTP(response, request)
	return response, time.Since(started)
}

func TestRealSignedStackRoutesAllInteractionTypesAndDeduplicatesDeferredWork(t *testing.T) {
	fixture := newRealStackFixture(t, 8)

	ping, _ := fixture.request(t, `{"id":"100","application_id":"123456789012345678","type":1,"token":"ping-token"}`)
	if ping.Code != http.StatusOK || ping.Body.String() != `{"type":1}` {
		t.Fatalf("PING response = %d %s", ping.Code, ping.Body.String())
	}

	autocomplete, _ := fixture.request(t, `{"id":"101","application_id":"123456789012345678","type":4,"token":"autocomplete-token","guild_id":"222","member":{"user":{"id":"333"}},"data":{"id":"444","name":"discord-control","type":1,"options":[{"name":"health","type":1,"focused":true}]}}`)
	if autocomplete.Code != http.StatusOK || !strings.Contains(autocomplete.Body.String(), `"type":8`) {
		t.Fatalf("autocomplete response = %d %s", autocomplete.Code, autocomplete.Body.String())
	}

	slashBody := `{"id":"102","application_id":"123456789012345678","type":2,"token":"slash-token","guild_id":"222","member":{"user":{"id":"333"}},"data":{"id":"444","name":"discord-control","type":1,"options":[{"name":"jobs","type":2,"options":[{"name":"status","type":1,"options":[{"name":"run_id","type":3,"value":"run-1"}]}]}]}}`
	slash, elapsed := fixture.request(t, slashBody)
	duplicate, _ := fixture.request(t, slashBody)
	if slash.Code != http.StatusOK || slash.Body.String() != `{"type":5,"data":{"flags":64}}` || duplicate.Body.String() != slash.Body.String() {
		t.Fatalf("slash responses = %d %s; duplicate=%s", slash.Code, slash.Body.String(), duplicate.Body.String())
	}
	if elapsed >= fixture.ackDeadline {
		t.Fatalf("slash acknowledgement took %s, deadline %s", elapsed, fixture.ackDeadline)
	}
	waitFor(t, func() bool { requests, _, _ := fixture.callbacks.snapshot(); return requests == 1 })
	gets, _, _ := fixture.jobs.counts()
	if gets != 1 {
		t.Fatalf("duplicate slash work executions = %d, want 1", gets)
	}

	cancelID, err := fixture.pending.Put(PendingAction{Kind: PendingCancel, ActorUserID: stackUserID, GuildID: stackGuildID, RunID: "run-1", ExpiresAt: fixture.clock.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	componentBody := `{"id":"103","application_id":"123456789012345678","type":3,"token":"component-token","guild_id":"222","member":{"user":{"id":"333"}},"data":{"custom_id":"` + cancelID + `","component_type":2}}`
	component, elapsed := fixture.request(t, componentBody)
	if component.Body.String() != `{"type":6}` || elapsed >= fixture.ackDeadline {
		t.Fatalf("component acknowledgement = %s in %s", component.Body.String(), elapsed)
	}
	waitFor(t, func() bool { _, cancels, _ := fixture.jobs.counts(); return cancels == 1 })

	modalID, err := fixture.pending.Put(PendingAction{Kind: PendingResumeModal, ActorUserID: stackUserID, ActorID: "interaction-actor", GuildID: stackGuildID, RunID: "run-1", PlanHash: stackPlanHash, Recoverable: true, ExpiresAt: fixture.clock.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	modalBody := `{"id":"104","application_id":"123456789012345678","type":5,"token":"modal-token","guild_id":"222","member":{"user":{"id":"333"}},"data":{"custom_id":"` + modalID + `","components":[{"type":1,"components":[{"type":4,"custom_id":"confirmation","value":"aaaaaaaaaaaa"}]}]}}`
	modal, elapsed := fixture.request(t, modalBody)
	if modal.Body.String() != `{"type":5,"data":{"flags":64}}` || elapsed >= fixture.ackDeadline {
		t.Fatalf("modal acknowledgement = %s in %s", modal.Body.String(), elapsed)
	}
	waitFor(t, func() bool { _, _, resumes := fixture.jobs.counts(); return resumes == 1 })
	if fixture.issuer.count() != 1 {
		t.Fatalf("modal approvals issued = %d, want 1", fixture.issuer.count())
	}
	waitFor(t, func() bool { requests, _, _ := fixture.callbacks.snapshot(); return requests == 3 })
	callbackSpans := 0
	for _, span := range fixture.telemetry.recordedSpans() {
		if strings.HasPrefix(span, "callback:") {
			callbackSpans++
			if span != "callback:stack-correlation" {
				t.Fatalf("deferred callback correlation = %q", span)
			}
		}
	}
	if callbackSpans != 3 {
		t.Fatalf("callback spans = %d, want 3", callbackSpans)
	}
}

func TestRealSignedStackDeniesGenericallyAndPreservesPendingState(t *testing.T) {
	fixture := newRealStackFixture(t, 4)
	fixture.policy.allowed = false
	fixture.policy.err = errors.New("policy backend leaked-secret-canary")
	body := `{"id":"200","application_id":"123456789012345678","type":2,"token":"denied-token","guild_id":"222","member":{"user":{"id":"333"}},"data":{"id":"444","name":"discord-control","type":1,"options":[{"name":"jobs","type":2,"options":[{"name":"status","type":1,"options":[{"name":"run_id","type":3,"value":"run-1"}]}]}]}}`
	response, _ := fixture.request(t, body)
	if response.Body.String() != `{"type":5,"data":{"flags":64}}` || strings.Contains(response.Body.String(), "leaked-secret-canary") {
		t.Fatalf("denied initial response leaked details: %s", response.Body.String())
	}
	waitFor(t, func() bool {
		requests, bodies, _ := fixture.callbacks.snapshot()
		return requests == 1 && len(bodies) == 1
	})
	_, callbackBodies, _ := fixture.callbacks.snapshot()
	if callbackBodies[0] != `{"content":"This operation is not available.","flags":64}` || strings.Contains(callbackBodies[0], "leaked-secret-canary") {
		t.Fatalf("denied callback = %s", callbackBodies[0])
	}

	fixture.policy.err = nil
	fixture.policy.allowed = true
	pendingID, err := fixture.pending.Put(PendingAction{Kind: PendingCancel, ActorUserID: stackUserID, GuildID: stackGuildID, RunID: "run-1", ExpiresAt: fixture.clock.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	wrongGuild := `{"id":"201","application_id":"123456789012345678","type":3,"token":"wrong-guild-token","guild_id":"999","member":{"user":{"id":"333"}},"data":{"custom_id":"` + pendingID + `","component_type":2}}`
	wrongGuildResponse, _ := fixture.request(t, wrongGuild)
	if !bytes.Equal(wrongGuildResponse.Body.Bytes(), genericUnavailableCallback()) {
		t.Fatalf("wrong-guild response = %s", wrongGuildResponse.Body.String())
	}
	if _, outcome := fixture.pending.Get(pendingID, stackUserID, stackGuildID); outcome != PendingAvailable {
		t.Fatalf("wrong-guild request consumed pending action: %s", outcome)
	}

	valid := `{"id":"202","application_id":"123456789012345678","type":3,"token":"valid-token","guild_id":"222","member":{"user":{"id":"333"}},"data":{"custom_id":"` + pendingID + `","component_type":2}}`
	validResponse, _ := fixture.request(t, valid)
	if validResponse.Body.String() != `{"type":6}` {
		t.Fatalf("valid component response = %s", validResponse.Body.String())
	}
	waitFor(t, func() bool { _, cancels, _ := fixture.jobs.counts(); return cancels == 1 })
	replay := `{"id":"203","application_id":"123456789012345678","type":3,"token":"replay-token","guild_id":"222","member":{"user":{"id":"333"}},"data":{"custom_id":"` + pendingID + `","component_type":2}}`
	replayResponse, _ := fixture.request(t, replay)
	if !bytes.Equal(replayResponse.Body.Bytes(), genericUnavailableCallback()) {
		t.Fatalf("replay response = %s", replayResponse.Body.String())
	}
	if _, cancels, _ := fixture.jobs.counts(); cancels != 1 {
		t.Fatalf("replayed component executions = %d, want 1", cancels)
	}

	expiredID, err := fixture.pending.Put(PendingAction{Kind: PendingCancel, ActorUserID: stackUserID, GuildID: stackGuildID, RunID: "run-1", ExpiresAt: fixture.clock.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	fixture.clock.Advance(time.Minute)
	expired := `{"id":"204","application_id":"123456789012345678","type":3,"token":"expired-token","guild_id":"222","member":{"user":{"id":"333"}},"data":{"custom_id":"` + expiredID + `","component_type":2}}`
	expiredResponse, _ := fixture.request(t, expired)
	if !bytes.Equal(expiredResponse.Body.Bytes(), genericUnavailableCallback()) {
		t.Fatalf("expired response = %s", expiredResponse.Body.String())
	}
}

func TestRealSignedStackQueueSaturationHasNoExecutionOrCallback(t *testing.T) {
	fixture := newRealStackFixture(t, 1)
	release := fillExecutorQueue(t, fixture.executor)
	defer close(release)
	body := `{"id":"300","application_id":"123456789012345678","type":2,"token":"busy-token","guild_id":"222","member":{"user":{"id":"333"}},"data":{"id":"444","name":"discord-control","type":1,"options":[{"name":"jobs","type":2,"options":[{"name":"status","type":1,"options":[{"name":"run_id","type":3,"value":"run-1"}]}]}]}}`
	response, elapsed := fixture.request(t, body)
	if !bytes.Equal(response.Body.Bytes(), genericBusyCallback()) || elapsed >= fixture.ackDeadline {
		t.Fatalf("saturated response = %s in %s", response.Body.String(), elapsed)
	}
	gets, cancels, resumes := fixture.jobs.counts()
	requests, _, _ := fixture.callbacks.snapshot()
	if gets != 0 || cancels != 0 || resumes != 0 || requests != 0 {
		t.Fatalf("saturated side effects: gets=%d cancels=%d resumes=%d callbacks=%d", gets, cancels, resumes, requests)
	}
}

func TestRealSignedStackCallbackOutageIsContainedAndRedacted(t *testing.T) {
	fixture := newRealStackFixture(t, 4)
	fixture.callbacks.err = errors.New("transport failure containing callback-outage-secret")
	body := `{"id":"400","application_id":"123456789012345678","type":2,"token":"outage-token","guild_id":"222","member":{"user":{"id":"333"}},"data":{"id":"444","name":"discord-control","type":1,"options":[{"name":"jobs","type":2,"options":[{"name":"status","type":1,"options":[{"name":"run_id","type":3,"value":"run-1"}]}]}]}}`
	response, _ := fixture.request(t, body)
	if response.Body.String() != `{"type":5,"data":{"flags":64}}` || strings.Contains(response.Body.String(), "callback-outage-secret") {
		t.Fatalf("callback outage changed acknowledgement: %s", response.Body.String())
	}
	waitFor(t, func() bool { outcomes := fixture.telemetry.outcomes(); return len(outcomes) == 1 })
	outcomes := fixture.telemetry.outcomes()
	if len(outcomes) != 1 || outcomes[0] != "failure" {
		t.Fatalf("callback outcomes = %v", outcomes)
	}
	requests, _, urls := fixture.callbacks.snapshot()
	if requests != 2 {
		t.Fatalf("idempotent callback attempts = %d, want 2", requests)
	}
	for _, value := range append(urls, response.Body.String()) {
		if strings.Contains(value, "callback-outage-secret") {
			t.Fatalf("callback transport error leaked: %s", value)
		}
	}
}
