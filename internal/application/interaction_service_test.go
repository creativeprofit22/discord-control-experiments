package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type interactionPolicyFixture struct {
	mu       sync.Mutex
	decision domain.PolicyDecision
	err      error
	requests []domain.PolicyRequest
}

func (policy *interactionPolicyFixture) Decide(_ context.Context, request domain.PolicyRequest) (domain.PolicyDecision, error) {
	policy.mu.Lock()
	defer policy.mu.Unlock()
	policy.requests = append(policy.requests, request)
	if request.Principal.Adapter == "" || request.Principal.Subject == "" || request.Principal.AuthenticationMethod == "" || request.GuildID == "" || request.Operation == "" || request.CorrelationID == "" || !domain.ValidCapability(request.Capability) || !domain.ValidExecutionMode(request.Mode) || !domain.ValidDestructiveClass(request.DestructiveClass) {
		return domain.PolicyDecision{}, errors.New("structurally invalid policy request")
	}
	return policy.decision, policy.err
}

func (policy *interactionPolicyFixture) count() int {
	policy.mu.Lock()
	defer policy.mu.Unlock()
	return len(policy.requests)
}

func (policy *interactionPolicyFixture) requestSnapshot() []domain.PolicyRequest {
	policy.mu.Lock()
	defer policy.mu.Unlock()
	return append([]domain.PolicyRequest(nil), policy.requests...)
}

type interactionJobsFixture struct {
	mu             sync.Mutex
	records        []JobRecord
	cancels        int
	resumes        int
	resumeRequests []ResumeRequest
	resumeErr      error
}

func (jobs *interactionJobsFixture) List(context.Context) ([]JobRecord, error) {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	return append([]JobRecord(nil), jobs.records...), nil
}

func (jobs *interactionJobsFixture) Get(_ context.Context, runID domain.RunID) (JobRecord, error) {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	for _, record := range jobs.records {
		if record.RunID == runID {
			return record, nil
		}
	}
	return JobRecord{}, errors.New("job lookup detail must not escape")
}

func (jobs *interactionJobsFixture) Cancel(_ context.Context, runID domain.RunID) (JobRecord, error) {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	jobs.cancels++
	for _, record := range jobs.records {
		if record.RunID == runID {
			return record, nil
		}
	}
	return JobRecord{}, errors.New("job cancellation detail must not escape")
}

func (jobs *interactionJobsFixture) Resume(_ context.Context, request ResumeRequest) (domain.ApplyResult, error) {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	jobs.resumes++
	jobs.resumeRequests = append(jobs.resumeRequests, request)
	return domain.ApplyResult{}, jobs.resumeErr
}

func (jobs *interactionJobsFixture) mutationCounts() (int, int) {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	return jobs.cancels, jobs.resumes
}

func (jobs *interactionJobsFixture) resumeRequestSnapshot() []ResumeRequest {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	return append([]ResumeRequest(nil), jobs.resumeRequests...)
}

type interactionChangingJobStore struct {
	mu      sync.Mutex
	records []JobRecord
	gets    int
}

func (store *interactionChangingJobStore) ListJobRecords(context.Context) ([]JobRecord, error) {
	return nil, nil
}

func (store *interactionChangingJobStore) GetJobRecord(context.Context, domain.RunID) (JobRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.gets >= len(store.records) {
		return JobRecord{}, errors.New("unexpected job lookup")
	}
	record := store.records[store.gets]
	store.gets++
	return record, nil
}

func (*interactionChangingJobStore) SaveJobCancellation(context.Context, domain.RunID, time.Time) error {
	return nil
}

type interactionRecoveryFixture struct {
	mu       sync.Mutex
	requests []ResumeRequest
}

func (recovery *interactionRecoveryFixture) Resume(_ context.Context, request ResumeRequest) (domain.ApplyResult, error) {
	recovery.mu.Lock()
	defer recovery.mu.Unlock()
	recovery.requests = append(recovery.requests, request)
	return domain.ApplyResult{}, nil
}

func (recovery *interactionRecoveryFixture) count() int {
	recovery.mu.Lock()
	defer recovery.mu.Unlock()
	return len(recovery.requests)
}

type interactionClock struct{}

func (interactionClock) Now() time.Time { return time.Unix(1, 0) }

func TestInteractionServiceFiltersJobsAndRejectsCrossGuildLookupBeforePolicy(t *testing.T) {
	policy := &interactionPolicyFixture{decision: domain.PolicyDecision{Allowed: true, ActorID: "interaction-actor", EffectiveMode: domain.ModeReadOnly, Reason: domain.PolicyAllowed}}
	jobs := &interactionJobsFixture{records: []JobRecord{
		{RunID: "same-guild", GuildID: "222", Status: JobRunning},
		{RunID: "other-guild", GuildID: "999", Status: JobRunning},
	}}
	service, err := NewInteractionService(policy, new(HealthService), jobs)
	if err != nil {
		t.Fatal(err)
	}
	identity := InteractionIdentity{UserID: "333", GuildID: "222", CorrelationID: "interaction-correlation"}

	listed, err := service.ListJobs(context.Background(), identity)
	if err != nil || len(listed) != 1 || listed[0].RunID != "same-guild" {
		t.Fatalf("guild-filtered jobs = %#v, err=%v", listed, err)
	}
	policyCalls := policy.count()
	if _, err := service.Job(context.Background(), identity, "other-guild"); !errors.Is(err, ErrInteractionNotAvailable) {
		t.Fatalf("cross-guild lookup error = %v", err)
	}
	if policy.count() != policyCalls {
		t.Fatal("cross-guild job reached policy authorization")
	}
	if cancels, resumes := jobs.mutationCounts(); cancels != 0 || resumes != 0 {
		t.Fatalf("cross-guild side effects: cancels=%d resumes=%d", cancels, resumes)
	}
}

func TestInteractionServiceReadAndCancelPolicyRequestsAreStructurallyValid(t *testing.T) {
	identity := InteractionIdentity{UserID: "333", GuildID: "222", CorrelationID: "interaction-correlation"}
	tests := []struct {
		name       string
		capability domain.Capability
		mode       domain.ExecutionMode
		operation  domain.OperationName
		invoke     func(context.Context, *InteractionService) error
	}{
		{
			name:       "read",
			capability: domain.CapabilityJobRead,
			mode:       domain.ModeReadOnly,
			operation:  "interaction_jobs_list",
			invoke: func(ctx context.Context, service *InteractionService) error {
				_, err := service.ListJobs(ctx, identity)
				return err
			},
		},
		{
			name:       "cancel",
			capability: domain.CapabilityJobCancel,
			mode:       domain.ModeApply,
			operation:  "interaction_job_cancel",
			invoke: func(ctx context.Context, service *InteractionService) error {
				_, err := service.CancelJob(ctx, identity, "run-1")
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := &interactionPolicyFixture{decision: domain.PolicyDecision{Allowed: true, ActorID: "interaction-actor", EffectiveMode: test.mode, Reason: domain.PolicyAllowed}}
			jobs := &interactionJobsFixture{records: []JobRecord{{RunID: "run-1", GuildID: identity.GuildID, Status: JobRunning}}}
			service, err := NewInteractionService(policy, new(HealthService), jobs)
			if err != nil {
				t.Fatal(err)
			}

			if err := test.invoke(context.Background(), service); err != nil {
				t.Fatalf("invoke: %v", err)
			}
			requests := policy.requestSnapshot()
			if len(requests) != 1 {
				t.Fatalf("policy requests = %d, want 1", len(requests))
			}
			request := requests[0]
			wantPrincipal := domain.Principal{Adapter: "interactions", Subject: identity.UserID, AuthenticationMethod: "discord-ed25519"}
			if request.Principal != wantPrincipal || request.GuildID != identity.GuildID || request.CorrelationID != identity.CorrelationID || request.Capability != test.capability || request.Mode != test.mode || request.DestructiveClass != domain.DestructiveNone || request.Operation != test.operation {
				t.Fatalf("policy request = %#v", request)
			}
		})
	}
}

func TestInteractionServiceReviewResumeReturnsRecoverableFullHashAndExactPolicyRequest(t *testing.T) {
	const planHash = domain.PlanHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	identity := InteractionIdentity{UserID: "333", GuildID: "222", CorrelationID: "interaction-correlation"}
	policy := &interactionPolicyFixture{decision: domain.PolicyDecision{Allowed: true, ActorID: "interaction-actor", EffectiveMode: domain.ModeApply, Reason: domain.PolicyAllowed}}
	jobs := &interactionJobsFixture{records: []JobRecord{{RunID: "run-1", GuildID: identity.GuildID, Status: JobPartial, Recoverable: true, PlanHash: planHash}}}
	service, err := NewInteractionService(policy, new(HealthService), jobs)
	if err != nil {
		t.Fatal(err)
	}

	review, err := service.ReviewResume(context.Background(), identity, "run-1")
	if err != nil {
		t.Fatalf("review resume: %v", err)
	}
	if !review.Job.Recoverable || review.Job.PlanHashPrefix != "aaaaaaaaaaaa" || review.PlanHash != planHash || review.ActorID != "interaction-actor" {
		t.Fatalf("resume review = %#v", review)
	}
	requests := policy.requestSnapshot()
	if len(requests) != 1 {
		t.Fatalf("policy requests = %d, want 1", len(requests))
	}
	request := requests[0]
	wantPrincipal := domain.Principal{Adapter: "interactions", Subject: identity.UserID, AuthenticationMethod: "discord-ed25519"}
	if request.Principal != wantPrincipal || request.GuildID != identity.GuildID || request.CorrelationID != identity.CorrelationID || request.Capability != domain.CapabilityJobResume || request.Mode != domain.ModeApply || request.DestructiveClass != domain.DestructiveNone || request.Operation != "interaction_job_resume_review" {
		t.Fatalf("policy request = %#v", request)
	}
}

func TestInteractionServiceResumeJobPropagatesCompleteBoundRequest(t *testing.T) {
	const planHash = domain.PlanHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	identity := InteractionIdentity{UserID: "333", GuildID: "222", CorrelationID: "interaction-correlation"}
	policy := &interactionPolicyFixture{decision: domain.PolicyDecision{Allowed: true, ActorID: "interaction-actor", EffectiveMode: domain.ModeApply, Reason: domain.PolicyAllowed}}
	jobs := &interactionJobsFixture{records: []JobRecord{{RunID: "run-1", GuildID: identity.GuildID, Status: JobPartial, Recoverable: true, PlanHash: planHash}}}
	service, err := NewInteractionService(policy, new(HealthService), jobs)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.ResumeJob(context.Background(), identity, "run-1", "approval-1"); err != nil {
		t.Fatalf("resume job: %v", err)
	}
	policyRequests := policy.requestSnapshot()
	if len(policyRequests) != 1 {
		t.Fatalf("policy requests = %d, want 1", len(policyRequests))
	}
	policyRequest := policyRequests[0]
	wantPrincipal := domain.Principal{Adapter: "interactions", Subject: identity.UserID, AuthenticationMethod: "discord-ed25519"}
	if policyRequest.Principal != wantPrincipal || policyRequest.GuildID != identity.GuildID || policyRequest.CorrelationID != identity.CorrelationID || policyRequest.Capability != domain.CapabilityJobResume || policyRequest.Mode != domain.ModeApply || policyRequest.DestructiveClass != domain.DestructiveNone || policyRequest.Operation != "interaction_job_resume" {
		t.Fatalf("policy request = %#v", policyRequest)
	}
	requests := jobs.resumeRequestSnapshot()
	if len(requests) != 1 {
		t.Fatalf("resume requests = %d, want 1", len(requests))
	}
	want := ResumeRequest{
		GuildID:       identity.GuildID,
		RunID:         "run-1",
		PlanHash:      planHash,
		ActorID:       "interaction-actor",
		Principal:     wantPrincipal,
		ApprovalID:    "approval-1",
		AllowDelete:   false,
		CorrelationID: identity.CorrelationID,
	}
	if requests[0] != want {
		t.Fatalf("resume request = %#v, want %#v", requests[0], want)
	}
}

func TestInteractionServiceResumeJobPreAuthorizationFailuresDoNotCallResume(t *testing.T) {
	const planHash = domain.PlanHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	identity := InteractionIdentity{UserID: "333", GuildID: "222", CorrelationID: "interaction-correlation"}
	tests := []struct {
		name       string
		record     JobRecord
		approvalID domain.ApprovalID
		decision   domain.PolicyDecision
	}{
		{
			name:       "cross guild",
			record:     JobRecord{RunID: "run-1", GuildID: "999", Status: JobPartial, Recoverable: true, PlanHash: planHash},
			approvalID: "approval-1",
			decision:   domain.PolicyDecision{Allowed: true, ActorID: "interaction-actor", EffectiveMode: domain.ModeApply, Reason: domain.PolicyAllowed},
		},
		{
			name:     "missing approval",
			record:   JobRecord{RunID: "run-1", GuildID: identity.GuildID, Status: JobPartial, Recoverable: true, PlanHash: planHash},
			decision: domain.PolicyDecision{Allowed: true, ActorID: "interaction-actor", EffectiveMode: domain.ModeApply, Reason: domain.PolicyAllowed},
		},
		{
			name:       "policy denied",
			record:     JobRecord{RunID: "run-1", GuildID: identity.GuildID, Status: JobPartial, Recoverable: true, PlanHash: planHash},
			approvalID: "approval-1",
			decision:   domain.PolicyDecision{Allowed: false, Reason: domain.PolicyCapabilityDenied},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := &interactionPolicyFixture{decision: test.decision}
			jobs := &interactionJobsFixture{records: []JobRecord{test.record}}
			service, err := NewInteractionService(policy, new(HealthService), jobs)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := service.ResumeJob(context.Background(), identity, "run-1", test.approvalID); !errors.Is(err, ErrInteractionNotAvailable) {
				t.Fatalf("resume error = %v", err)
			}
			if _, resumes := jobs.mutationCounts(); resumes != 0 {
				t.Fatalf("resume calls = %d, want 0", resumes)
			}
		})
	}
}

func TestInteractionServiceResumeJobChangedHashDoesNotReachReconciliation(t *testing.T) {
	const reviewedHash = domain.PlanHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	const changedHash = domain.PlanHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	identity := InteractionIdentity{UserID: "333", GuildID: "222", CorrelationID: "interaction-correlation"}
	store := &interactionChangingJobStore{records: []JobRecord{
		{RunID: "run-1", GuildID: identity.GuildID, Status: JobPartial, Recoverable: true, PlanHash: reviewedHash},
		{RunID: "run-1", GuildID: identity.GuildID, Status: JobPartial, Recoverable: true, PlanHash: changedHash},
	}}
	recovery := new(interactionRecoveryFixture)
	jobs, err := NewJobService(store, recovery, interactionClock{})
	if err != nil {
		t.Fatal(err)
	}
	policy := &interactionPolicyFixture{decision: domain.PolicyDecision{Allowed: true, ActorID: "interaction-actor", EffectiveMode: domain.ModeApply, Reason: domain.PolicyAllowed}}
	service, err := NewInteractionService(policy, new(HealthService), jobs)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.ResumeJob(context.Background(), identity, "run-1", "approval-1"); !errors.Is(err, ErrInteractionNotAvailable) {
		t.Fatalf("changed-hash resume error = %v", err)
	}
	if recovery.count() != 0 {
		t.Fatalf("reconciliation calls = %d, want 0", recovery.count())
	}
}

func TestInteractionServicePolicyFailureIsGenericAndCannotMutate(t *testing.T) {
	policy := &interactionPolicyFixture{err: errors.New("policy-secret-canary")}
	jobs := &interactionJobsFixture{records: []JobRecord{{RunID: "run-1", GuildID: "222", Status: JobRunning}}}
	service, err := NewInteractionService(policy, new(HealthService), jobs)
	if err != nil {
		t.Fatal(err)
	}
	identity := InteractionIdentity{UserID: "333", GuildID: "222", CorrelationID: "interaction-correlation"}

	_, err = service.CancelJob(context.Background(), identity, "run-1")
	if !errors.Is(err, ErrInteractionNotAvailable) || err.Error() != ErrInteractionNotAvailable.Error() {
		t.Fatalf("policy denial error leaked details: %v", err)
	}
	if cancels, resumes := jobs.mutationCounts(); cancels != 0 || resumes != 0 {
		t.Fatalf("denied mutation side effects: cancels=%d resumes=%d", cancels, resumes)
	}
}
