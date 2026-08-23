package interactions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type routerPolicyStub struct {
	allow bool
}

func (policy routerPolicyStub) Decide(_ context.Context, request domain.PolicyRequest) (domain.PolicyDecision, error) {
	if !policy.allow {
		return domain.PolicyDecision{}, errors.New("denied")
	}
	return domain.PolicyDecision{Allowed: true, ActorID: "actor", EffectiveMode: request.Mode, Reason: domain.PolicyAllowed}, nil
}

type routerJobsStub struct {
	gets      atomic.Int32
	resumes   atomic.Int32
	record    application.JobRecord
	resumeErr error
}

func (*routerJobsStub) List(context.Context) ([]application.JobRecord, error) {
	return nil, errors.New("unavailable")
}
func (jobs *routerJobsStub) Get(context.Context, domain.RunID) (application.JobRecord, error) {
	jobs.gets.Add(1)
	if jobs.record.RunID != "" {
		return jobs.record, nil
	}
	return application.JobRecord{}, errors.New("unavailable")
}
func (*routerJobsStub) Cancel(context.Context, domain.RunID) (application.JobRecord, error) {
	return application.JobRecord{}, errors.New("unavailable")
}
func (jobs *routerJobsStub) Resume(context.Context, application.ResumeRequest) (domain.ApplyResult, error) {
	jobs.resumes.Add(1)
	return domain.ApplyResult{}, jobs.resumeErr
}

type routerSinkStub struct{ deliveries atomic.Int32 }

func (sink *routerSinkStub) Deliver(context.Context, VerifiedInteraction, DeferredResult) error {
	sink.deliveries.Add(1)
	return nil
}

type approvalTelemetrySpy struct {
	noopInteractionTelemetry
	mu       sync.Mutex
	outcomes []string
}

func (telemetry *approvalTelemetrySpy) RecordApproval(_ context.Context, outcome string) {
	telemetry.mu.Lock()
	defer telemetry.mu.Unlock()
	telemetry.outcomes = append(telemetry.outcomes, outcome)
}

func (telemetry *approvalTelemetrySpy) approvalOutcomes() []string {
	telemetry.mu.Lock()
	defer telemetry.mu.Unlock()
	return append([]string(nil), telemetry.outcomes...)
}

type routerApprovalIssuerStub struct{ issues atomic.Int32 }

func (issuer *routerApprovalIssuerStub) Issue(_ context.Context, grant domain.ApprovalGrant) (domain.ApprovalGrant, error) {
	issuer.issues.Add(1)
	grant.ID = "approval"
	return grant, nil
}

type routerClock struct{ now time.Time }

func (clock routerClock) Now() time.Time { return clock.now }

func TestRouterConcurrentComponentDuplicateCachesModalCallback(t *testing.T) {
	router, pending, _, _, stop := newRouterFixture(t, 4)
	defer stop()
	now := time.Unix(1_800_000_000, 0)
	customID, err := pending.Put(PendingAction{
		Kind: PendingResumeReview, ActorUserID: "333", ActorID: "actor", GuildID: "222", RunID: "run",
		PlanHash: domain.PlanHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), Recoverable: true, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	interaction := componentInteraction("111", "component-body", customID)
	responses := concurrentResponses(t, router, interaction, 32)
	for index := 1; index < len(responses); index++ {
		if !bytes.Equal(responses[0], responses[index]) {
			t.Fatalf("duplicate %d callback differs: %q != %q", index, responses[index], responses[0])
		}
	}
	if !bytes.Contains(responses[0], []byte(`"type":9`)) {
		t.Fatalf("component callback is not a modal: %s", responses[0])
	}
	pending.mu.Lock()
	resumeModals := 0
	for _, record := range pending.values {
		if record.action.Kind == PendingResumeModal {
			resumeModals++
		}
	}
	pending.mu.Unlock()
	if resumeModals != 1 {
		t.Fatalf("resume modal states = %d, want 1", resumeModals)
	}

	mismatch := interaction
	mismatch.RawBodyDigest = sha256.Sum256([]byte("different-body"))
	callback, err := router.Respond(context.Background(), mismatch)
	if err != nil || !bytes.Equal(callback, genericUnavailableCallback()) {
		t.Fatalf("digest mismatch callback = %q, err=%v", callback, err)
	}
}

func TestRouterConcurrentModalDuplicateClaimsAndQueuesOnce(t *testing.T) {
	router, pending, jobs, sink, stop := newRouterFixture(t, 4)
	defer stop()
	now := time.Unix(1_800_000_000, 0)
	customID, err := pending.Put(PendingAction{
		Kind: PendingResumeModal, ActorUserID: "333", ActorID: "actor", GuildID: "222", RunID: "run",
		PlanHash: domain.PlanHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), Recoverable: true, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	interaction := modalInteraction("112", "modal-body", customID)
	responses := concurrentResponses(t, router, interaction, 32)
	for index := 1; index < len(responses); index++ {
		if !bytes.Equal(responses[0], responses[index]) {
			t.Fatalf("duplicate %d callback differs: %q != %q", index, responses[index], responses[0])
		}
	}
	want, _ := marshalDeferredCallback(CallbackDeferredChannelMessage)
	if !bytes.Equal(responses[0], want) {
		t.Fatalf("modal callback = %q, want %q", responses[0], want)
	}
	if _, outcome := pending.Get(customID, "333", "222"); outcome != PendingReplayed {
		t.Fatalf("modal pending outcome = %s, want replayed", outcome)
	}
	waitFor(t, func() bool { return sink.deliveries.Load() == 1 })
	if jobs.gets.Load() != 1 {
		t.Fatalf("modal work runs = %d, want 1", jobs.gets.Load())
	}
}

func TestRouterQueueFullPreservesComponentAndModalPendingState(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		kind        PendingKind
		interaction func(string) VerifiedInteraction
	}{
		{name: "component", kind: PendingCancel, interaction: func(customID string) VerifiedInteraction {
			return componentInteraction("113", "component-queue-full", customID)
		}},
		{name: "modal", kind: PendingResumeModal, interaction: func(customID string) VerifiedInteraction {
			return modalInteraction("114", "modal-queue-full", customID)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			router, pending, _, _, stop := newRouterFixture(t, 1)
			defer stop()
			release := fillExecutorQueue(t, router.executor)
			now := time.Unix(1_800_000_000, 0)
			action := PendingAction{Kind: testCase.kind, ActorUserID: "333", GuildID: "222", RunID: "run", ExpiresAt: now.Add(time.Minute)}
			if testCase.kind == PendingResumeModal {
				action.ActorID = "actor"
				action.PlanHash = domain.PlanHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
				action.Recoverable = true
			}
			customID, err := pending.Put(action)
			if err != nil {
				t.Fatal(err)
			}
			interaction := testCase.interaction(customID)
			callback, err := router.Respond(context.Background(), interaction)
			if err != nil || !bytes.Equal(callback, genericBusyCallback()) {
				t.Fatalf("queue-full callback = %q, err=%v", callback, err)
			}
			if _, outcome := pending.Get(customID, "333", "222"); outcome != PendingAvailable {
				t.Fatalf("queue-full pending outcome = %s, want available", outcome)
			}

			close(release)
			waitFor(t, func() bool { return router.executor.Depth() == 0 })
			callback, err = router.Respond(context.Background(), interaction)
			if err != nil || bytes.Equal(callback, genericBusyCallback()) {
				t.Fatalf("retry callback = %q, err=%v", callback, err)
			}
			if _, outcome := pending.Get(customID, "333", "222"); outcome != PendingReplayed {
				t.Fatalf("successful retry pending outcome = %s, want replayed", outcome)
			}
		})
	}
}

func TestRouterRecordsPendingApprovalDenialsWithoutChangingResponse(t *testing.T) {
	outcomes := []PendingOutcome{PendingExpired, PendingReplayed, PendingWrongActor, PendingWrongGuild}
	for _, route := range []string{"component", "modal"} {
		for _, wantOutcome := range outcomes {
			t.Run(route+"/"+string(wantOutcome), func(t *testing.T) {
				router, pending, _, _, stop := newRouterFixture(t, 2)
				defer stop()
				telemetry := &approvalTelemetrySpy{}
				router.telemetry = telemetry
				now := time.Unix(1_800_000_000, 0)
				action := PendingAction{Kind: PendingResumeReview, ActorUserID: "333", ActorID: "actor", GuildID: "222", RunID: "run", PlanHash: domain.PlanHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), Recoverable: true, ExpiresAt: now.Add(time.Minute)}
				if route == "modal" {
					action.Kind = PendingResumeModal
				}
				customID, err := pending.Put(action)
				if err != nil {
					t.Fatal(err)
				}
				if wantOutcome == PendingExpired {
					pending.now = func() time.Time { return now.Add(time.Minute) }
				}
				if wantOutcome == PendingReplayed {
					if _, outcome := pending.Claim(customID, "333", "222"); outcome != PendingAvailable {
						t.Fatalf("setup claim outcome = %s", outcome)
					}
				}
				var interaction VerifiedInteraction
				if route == "component" {
					interaction = componentInteraction("pending-denial", "pending-denial-body", customID)
				} else {
					interaction = modalInteraction("pending-denial", "pending-denial-body", customID)
				}
				if wantOutcome == PendingWrongActor {
					interaction.Envelope.Member.User.ID = "444"
				}
				if wantOutcome == PendingWrongGuild {
					interaction.Envelope.GuildID = "555"
				}
				callback, err := router.Respond(context.Background(), interaction)
				if err != nil || !bytes.Equal(callback, genericUnavailableCallback()) {
					t.Fatalf("callback = %q, err=%v", callback, err)
				}
				got := telemetry.approvalOutcomes()
				if len(got) != 1 || got[0] != string(wantOutcome) {
					t.Fatalf("approval outcomes = %v, want [%s]", got, wantOutcome)
				}
			})
		}
	}
}

func TestRouterResumeRecordsChangedPlan(t *testing.T) {
	router, _, jobs, _, stop := newRouterFixture(t, 1)
	defer stop()
	telemetry := &approvalTelemetrySpy{}
	router.telemetry = telemetry
	jobs.record = application.JobRecord{RunID: "run", GuildID: "222", Recoverable: true, PlanHash: domain.PlanHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")}
	service, err := application.NewInteractionService(routerPolicyStub{allow: true}, new(application.HealthService), jobs)
	if err != nil {
		t.Fatal(err)
	}
	router.service = service
	action := PendingAction{Kind: PendingResumeModal, ActorUserID: "333", ActorID: "actor", GuildID: "222", RunID: "run", PlanHash: domain.PlanHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), Recoverable: true, ExpiresAt: time.Unix(1_800_000_000, 0).Add(time.Minute)}
	result := router.resume(context.Background(), modalInteraction("changed-plan", "changed-plan-body", "resume_modal.aaaaaaaaaaaaaaaaaaaaaaaa"), action)
	if result.Outcome != "not_available" {
		t.Fatalf("resume outcome = %q", result.Outcome)
	}
	got := telemetry.approvalOutcomes()
	if len(got) != 1 || got[0] != "changed_plan" {
		t.Fatalf("approval outcomes = %v, want [changed_plan]", got)
	}
}

func TestRouterResumeRecordsIssuedAndAcceptedOnce(t *testing.T) {
	router, _, jobs, _, stop := newRouterFixture(t, 1)
	defer stop()
	now := time.Unix(1_800_000_000, 0)
	planHash := domain.PlanHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	jobs.record = application.JobRecord{RunID: "run", GuildID: "222", Recoverable: true, PlanHash: planHash}
	service, err := application.NewInteractionService(routerPolicyStub{allow: true}, new(application.HealthService), jobs)
	if err != nil {
		t.Fatal(err)
	}
	issuer := &routerApprovalIssuerStub{}
	coordinator, err := application.NewApprovalCoordinator(issuer, routerClock{now: now}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	telemetry := &approvalTelemetrySpy{}
	router.service = service
	router.approvals = coordinator
	router.telemetry = telemetry
	action := PendingAction{Kind: PendingResumeModal, ActorUserID: "333", ActorID: "actor", GuildID: "222", RunID: "run", PlanHash: planHash, Recoverable: true, ExpiresAt: now.Add(time.Minute)}
	result := router.resume(context.Background(), modalInteraction("accepted", "accepted-body", "resume_modal.aaaaaaaaaaaaaaaaaaaaaaaa"), action)
	if result.Outcome != "ok" {
		t.Fatalf("resume outcome = %q", result.Outcome)
	}
	got := telemetry.approvalOutcomes()
	if len(got) != 2 || got[0] != "issued" || got[1] != "accepted" {
		t.Fatalf("approval outcomes = %v, want [issued accepted]", got)
	}
	if issuer.issues.Load() != 1 || jobs.resumes.Load() != 1 {
		t.Fatalf("issues=%d resumes=%d, want 1 each", issuer.issues.Load(), jobs.resumes.Load())
	}
}

func newRouterFixture(t *testing.T, queueCapacity int) (*Router, *PendingStore, *routerJobsStub, *routerSinkStub, func()) {
	t.Helper()
	now := time.Unix(1_800_000_000, 0)
	jobs := &routerJobsStub{}
	service, err := application.NewInteractionService(routerPolicyStub{}, new(application.HealthService), jobs)
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
	executor, err := NewExecutor(queueCapacity, 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	sink := &routerSinkStub{}
	router, err := NewRouter(service, new(application.ApprovalCoordinator), store, pending, executor, sink, noopInteractionTelemetry{}, time.Minute, func() time.Time { return now })
	if err != nil {
		executor.Stop()
		t.Fatal(err)
	}
	return router, pending, jobs, sink, executor.Stop
}

func concurrentResponses(t *testing.T, router *Router, interaction VerifiedInteraction, count int) [][]byte {
	t.Helper()
	responses := make([][]byte, count)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index := range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			callback, err := router.Respond(context.Background(), interaction)
			if err != nil {
				t.Errorf("respond: %v", err)
				return
			}
			responses[index] = callback
		}()
	}
	close(start)
	wait.Wait()
	return responses
}

func componentInteraction(id, body, customID string) VerifiedInteraction {
	return VerifiedInteraction{
		Envelope:  Envelope{ID: id, Type: InteractionMessageComponent, GuildID: "222", Member: Member{User: User{ID: "333"}}},
		Component: &ComponentData{CustomID: customID}, RawBodyDigest: sha256.Sum256([]byte(body)),
		CorrelationID: "correlation", TraceContext: context.Background(),
	}
}

func modalInteraction(id, body, customID string) VerifiedInteraction {
	return VerifiedInteraction{
		Envelope:      Envelope{ID: id, Type: InteractionModalSubmit, GuildID: "222", Member: Member{User: User{ID: "333"}}},
		Modal:         &ModalData{CustomID: customID, Components: []ModalComponent{{Type: 1, Components: []ModalComponent{{Type: 4, CustomID: "confirmation", Value: "aaaaaaaaaaaa"}}}}},
		RawBodyDigest: sha256.Sum256([]byte(body)),
		CorrelationID: "correlation", TraceContext: context.Background(),
	}
}

func fillExecutorQueue(t *testing.T, executor *Executor) chan struct{} {
	t.Helper()
	started := make(chan struct{})
	release := make(chan struct{})
	if !executor.Enqueue(WorkItem{InteractionID: "blocking", Run: func(context.Context) {
		close(started)
		<-release
	}}) {
		t.Fatal("failed to enqueue blocking work")
	}
	<-started
	if !executor.Enqueue(WorkItem{InteractionID: "queued", Run: func(context.Context) {}}) {
		t.Fatal("failed to fill executor queue")
	}
	return release
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition was not met before timeout")
		}
		time.Sleep(time.Millisecond)
	}
}
