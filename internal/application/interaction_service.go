package application

import (
	"context"
	"errors"
	"sort"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

var ErrInteractionNotAvailable = errors.New("interaction operation not available")

type InteractionIdentity struct {
	UserID        string
	GuildID       domain.GuildID
	CorrelationID domain.CorrelationID
}

type InteractionPolicyView struct {
	Mode       domain.ExecutionMode
	Capability domain.Capability
	Allowed    bool
}

type InteractionJobView struct {
	RunID          domain.RunID
	Status         JobStatus
	PlanHashPrefix string
	CompletedSteps int
	TotalSteps     int
	Recoverable    bool
}

type InteractionResumeReview struct {
	Job      InteractionJobView
	ActorID  domain.ActorID
	PlanHash domain.PlanHash
}

type InteractionHealthView struct {
	LocalStatus   string
	DiscordStatus string
}

type interactionJobs interface {
	List(context.Context) ([]JobRecord, error)
	Get(context.Context, domain.RunID) (JobRecord, error)
	Cancel(context.Context, domain.RunID) (JobRecord, error)
	Resume(context.Context, ResumeRequest) (domain.ApplyResult, error)
}

type InteractionService struct {
	policy     PolicyEvaluator
	health     *HealthService
	jobs       interactionJobs
	moderation *ModerationService
	lockdown   *LockdownService
}

func NewInteractionService(policy PolicyEvaluator, health *HealthService, jobs interactionJobs, extras ...any) (*InteractionService, error) {
	if policy == nil || health == nil || jobs == nil {
		return nil, errors.New("interaction service dependencies are required")
	}
	service := &InteractionService{policy: policy, health: health, jobs: jobs}
	for _, extra := range extras {
		switch value := extra.(type) {
		case *ModerationService:
			service.moderation = value
		case *LockdownService:
			service.lockdown = value
		}
	}
	return service, nil
}

func (s *InteractionService) Moderate(ctx context.Context, identity InteractionIdentity, request ModerationActionRequest) (ModerationActionResult, error) {
	if s.moderation == nil {
		return ModerationActionResult{}, ErrInteractionNotAvailable
	}
	request.Invocation = interactionInvocation(identity, domain.ModeApply)
	request.GuildID = identity.GuildID
	return s.moderation.ExecuteAction(ctx, request)
}
func (s *InteractionService) ModerationCase(ctx context.Context, identity InteractionIdentity, caseID domain.CaseID) (domain.Case, error) {
	if s.moderation == nil {
		return domain.Case{}, ErrInteractionNotAvailable
	}
	return s.moderation.GetCase(ctx, CaseQuery{Invocation: interactionInvocation(identity, domain.ModeReadOnly), GuildID: identity.GuildID, CaseID: caseID})
}
func (s *InteractionService) Escalation(ctx context.Context, identity InteractionIdentity, subject domain.UserID) (EscalationResult, error) {
	if s.moderation == nil {
		return EscalationResult{}, ErrInteractionNotAvailable
	}
	return s.moderation.EvaluateEscalation(ctx, EscalationEvaluateRequest{Invocation: interactionInvocation(identity, domain.ModeReadOnly), GuildID: identity.GuildID, SubjectUserID: subject})
}
func (s *InteractionService) BuildLockdown(ctx context.Context, identity InteractionIdentity, scope domain.LockdownScope) (domain.LockdownPlan, domain.LockdownPlan, error) {
	if s.lockdown == nil {
		return domain.LockdownPlan{}, domain.LockdownPlan{}, ErrInteractionNotAvailable
	}
	return s.lockdown.BuildLockdownPlan(ctx, BuildLockdownRequest{Invocation: interactionInvocation(identity, domain.ModeApply), GuildID: identity.GuildID, Scope: scope})
}
func (s *InteractionService) ApplyLockdown(ctx context.Context, identity InteractionIdentity, hash domain.LockdownPlanHash, approval domain.ApprovalID, recover bool) (LockdownApplyResult, error) {
	if s.lockdown == nil {
		return LockdownApplyResult{}, ErrInteractionNotAvailable
	}
	request := ApplyLockdownRequest{Invocation: interactionInvocation(identity, domain.ModeApply), GuildID: identity.GuildID, PlanHash: hash, ApprovalID: approval}
	if recover {
		return s.lockdown.RecoverLockdown(ctx, request)
	}
	return s.lockdown.ApplyLockdown(ctx, request)
}
func interactionInvocation(identity InteractionIdentity, mode domain.ExecutionMode) Invocation {
	return Invocation{Principal: interactionPrincipal(identity.UserID), RequestedMode: mode, CorrelationID: identity.CorrelationID}
}

func interactionPrincipal(userID string) domain.Principal {
	return domain.Principal{Adapter: domain.Adapter("interactions"), Subject: userID, AuthenticationMethod: "discord-ed25519"}
}

func (s *InteractionService) authorize(ctx context.Context, identity InteractionIdentity, capability domain.Capability, mode domain.ExecutionMode, operation domain.OperationName) (domain.ActorID, error) {
	if identity.UserID == "" || identity.GuildID == "" || identity.CorrelationID == "" {
		return "", ErrInteractionNotAvailable
	}
	decision, err := s.policy.Decide(ctx, domain.PolicyRequest{Principal: interactionPrincipal(identity.UserID), GuildID: identity.GuildID, Capability: capability, Mode: mode, DestructiveClass: domain.DestructiveNone, Operation: operation, CorrelationID: identity.CorrelationID})
	if err != nil || !decision.Allowed || decision.ActorID == "" {
		return "", ErrInteractionNotAvailable
	}
	return decision.ActorID, nil
}

func (s *InteractionService) Policy(ctx context.Context, identity InteractionIdentity, capability domain.Capability) (InteractionPolicyView, error) {
	if !domain.ValidCapability(capability) {
		return InteractionPolicyView{}, ErrInteractionNotAvailable
	}
	decision, err := s.policy.Decide(ctx, domain.PolicyRequest{Principal: interactionPrincipal(identity.UserID), GuildID: identity.GuildID, Capability: capability, Mode: domain.ModeReadOnly, DestructiveClass: domain.DestructiveNone, Operation: "interaction_policy", CorrelationID: identity.CorrelationID})
	if err != nil || decision.ActorID == "" {
		return InteractionPolicyView{}, ErrInteractionNotAvailable
	}
	return InteractionPolicyView{Mode: decision.EffectiveMode, Capability: capability, Allowed: decision.Allowed}, nil
}

func (s *InteractionService) Health(ctx context.Context, identity InteractionIdentity) (InteractionHealthView, error) {
	actor, err := s.authorize(ctx, identity, domain.CapabilityGuildInspect, domain.ModeReadOnly, "interaction_health")
	if err != nil {
		return InteractionHealthView{}, err
	}
	local := s.health.Local(ctx)
	discord, err := s.health.Discord(ctx, Invocation{Principal: interactionPrincipal(identity.UserID), ActorID: actor, RequestedMode: domain.ModeReadOnly, CorrelationID: identity.CorrelationID}, identity.GuildID)
	if err != nil {
		return InteractionHealthView{LocalStatus: local.Status, DiscordStatus: "unavailable"}, nil
	}
	return InteractionHealthView{LocalStatus: local.Status, DiscordStatus: discord.Status}, nil
}

func (s *InteractionService) ListJobs(ctx context.Context, identity InteractionIdentity) ([]InteractionJobView, error) {
	if _, err := s.authorize(ctx, identity, domain.CapabilityJobRead, domain.ModeReadOnly, "interaction_jobs_list"); err != nil {
		return nil, err
	}
	records, err := s.jobs.List(ctx)
	if err != nil {
		return nil, ErrInteractionNotAvailable
	}
	views := make([]InteractionJobView, 0, len(records))
	for _, record := range records {
		if record.GuildID == identity.GuildID {
			views = append(views, interactionJobView(record))
		}
	}
	sort.Slice(views, func(i, j int) bool { return views[i].RunID < views[j].RunID })
	return views, nil
}

func (s *InteractionService) Job(ctx context.Context, identity InteractionIdentity, runID domain.RunID) (InteractionJobView, error) {
	record, actor, err := s.authorizedJob(ctx, identity, runID, domain.CapabilityJobRead, domain.ModeReadOnly, "interaction_job_status")
	_ = actor
	if err != nil {
		return InteractionJobView{}, err
	}
	return interactionJobView(record), nil
}

func (s *InteractionService) CancelJob(ctx context.Context, identity InteractionIdentity, runID domain.RunID) (InteractionJobView, error) {
	_, _, err := s.authorizedJob(ctx, identity, runID, domain.CapabilityJobCancel, domain.ModeApply, "interaction_job_cancel")
	if err != nil {
		return InteractionJobView{}, err
	}
	record, err := s.jobs.Cancel(ctx, runID)
	if err != nil || record.GuildID != identity.GuildID {
		return InteractionJobView{}, ErrInteractionNotAvailable
	}
	return interactionJobView(record), nil
}

func (s *InteractionService) ReviewResume(ctx context.Context, identity InteractionIdentity, runID domain.RunID) (InteractionResumeReview, error) {
	record, actor, err := s.authorizedJob(ctx, identity, runID, domain.CapabilityJobResume, domain.ModeApply, "interaction_job_resume_review")
	if err != nil || !record.Recoverable || len(record.PlanHash) != 64 {
		return InteractionResumeReview{}, ErrInteractionNotAvailable
	}
	view := interactionJobView(record)
	view.PlanHashPrefix = PlanHashPrefix(record.PlanHash)
	return InteractionResumeReview{Job: view, ActorID: actor, PlanHash: record.PlanHash}, nil
}

func (s *InteractionService) ResumeJob(ctx context.Context, identity InteractionIdentity, runID domain.RunID, approvalID domain.ApprovalID) (domain.ApplyResult, error) {
	record, actor, err := s.authorizedJob(ctx, identity, runID, domain.CapabilityJobResume, domain.ModeApply, "interaction_job_resume")
	if err != nil || !record.Recoverable || approvalID == "" {
		return domain.ApplyResult{}, ErrInteractionNotAvailable
	}
	result, err := s.jobs.Resume(ctx, ResumeRequest{GuildID: identity.GuildID, RunID: runID, PlanHash: record.PlanHash, ActorID: actor, Principal: interactionPrincipal(identity.UserID), ApprovalID: approvalID, AllowDelete: false, CorrelationID: identity.CorrelationID})
	if err != nil {
		return domain.ApplyResult{}, ErrInteractionNotAvailable
	}
	return result, nil
}

func (s *InteractionService) authorizedJob(ctx context.Context, identity InteractionIdentity, runID domain.RunID, capability domain.Capability, mode domain.ExecutionMode, operation domain.OperationName) (JobRecord, domain.ActorID, error) {
	record, err := s.jobs.Get(ctx, runID)
	if err != nil || record.GuildID != identity.GuildID {
		return JobRecord{}, "", ErrInteractionNotAvailable
	}
	actor, err := s.authorize(ctx, identity, capability, mode, operation)
	if err != nil {
		return JobRecord{}, "", err
	}
	return record, actor, nil
}

func interactionJobView(record JobRecord) InteractionJobView {
	return InteractionJobView{RunID: record.RunID, Status: record.Status, PlanHashPrefix: record.PlanHashPrefix, CompletedSteps: record.CompletedSteps, TotalSteps: record.TotalSteps, Recoverable: record.Recoverable}
}
