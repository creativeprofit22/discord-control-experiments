package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type ModerationIDGenerator interface{ NewModerationID(string) (string, error) }
type TextRedactor interface{ Sanitize(string) (string, bool) }

type ModerationSettings struct {
	Policy                   domain.EscalationPolicy
	Retention                time.Duration
	MaxCasesPerGuild         int
	MaxActionsPerCase        int
	ReasonCodes              map[domain.ReasonCode]struct{}
	QuarantineRoleID         domain.RoleID
	ProtectedOperatorRoleIDs map[domain.RoleID]struct{}
}

type ModerationService struct {
	discord   *DiscordService
	store     ModerationStore
	settings  ModerationSettings
	ids       ModerationIDGenerator
	redactor  TextRedactor
	telemetry ModerationTelemetry
	now       func() time.Time
}

type noopModerationTelemetry struct{}

func (noopModerationTelemetry) ObserveModeration(context.Context, ModerationObservation) {}
func (noopModerationTelemetry) ObserveLockdown(context.Context, LockdownObservation)     {}

type CaseCreateRequest struct {
	Invocation    Invocation
	GuildID       domain.GuildID
	SubjectUserID domain.UserID
	ReasonCode    domain.ReasonCode
	Notes         string
}

type CaseQuery struct {
	Invocation    Invocation
	GuildID       domain.GuildID
	CaseID        domain.CaseID
	SubjectUserID domain.UserID
}
type EvidenceAttachRequest struct {
	Invocation Invocation
	CaseID     domain.CaseID
	Evidence   domain.EvidenceReference
}

type ModerationActionRequest struct {
	Invocation           Invocation
	CaseID               domain.CaseID
	GuildID              domain.GuildID
	SubjectUserID        domain.UserID
	Kind                 domain.ActionKind
	ReasonCode           domain.ReasonCode
	Notes                string
	EvidenceIDs          []domain.EvidenceID
	IdempotencyKey       domain.IdempotencyKey
	Duration             time.Duration
	ChannelID            domain.ChannelID
	MessageIDs           []domain.MessageID
	InviteCode           domain.InviteCode
	DeleteMessageSeconds *int
}

type EscalationEvaluateRequest struct {
	Invocation    Invocation
	GuildID       domain.GuildID
	SubjectUserID domain.UserID
}
type EscalationApplyRequest struct {
	Invocation     Invocation
	GuildID        domain.GuildID
	SubjectUserID  domain.UserID
	CaseID         domain.CaseID
	ReasonCode     domain.ReasonCode
	IdempotencyKey domain.IdempotencyKey
	Recommendation domain.EscalationRecommendation
}
type EscalationResult struct {
	Recommendation domain.EscalationRecommendation `json:"recommendation"`
	Plan           *domain.OperationPlan           `json:"plan,omitempty"`
}

type ModerationActionResult struct {
	Action domain.ModerationAction `json:"action"`
	Plan   *domain.OperationPlan   `json:"plan,omitempty"`
}

func NewModerationService(discord *DiscordService, store ModerationStore, settings ModerationSettings, ids ModerationIDGenerator, redactor TextRedactor, telemetry ModerationTelemetry) (*ModerationService, error) {
	if discord == nil || store == nil || ids == nil || redactor == nil || settings.Retention <= 0 || settings.MaxCasesPerGuild < 1 || settings.MaxActionsPerCase < 1 {
		return nil, errors.New("moderation service dependencies and limits are required")
	}
	if telemetry == nil {
		telemetry = noopModerationTelemetry{}
	}
	return &ModerationService{discord: discord, store: store, settings: settings, ids: ids, redactor: redactor, telemetry: telemetry, now: time.Now}, nil
}

func (s *ModerationService) CreateCase(ctx context.Context, request CaseCreateRequest) (domain.Case, *domain.OperationPlan, error) {
	if request.GuildID == "" || request.SubjectUserID == "" || !s.reasonAllowed(request.ReasonCode) {
		return domain.Case{}, nil, &domain.ValidationError{Field: "case", Rule: "guild, subject, and configured reason are required"}
	}
	notes, _ := s.redactor.Sanitize(request.Notes)
	if len(notes) > domain.MaxModerationNoteLength {
		return domain.Case{}, nil, &domain.ValidationError{Field: "notes", Rule: "too long"}
	}
	finish, plan, err := s.authorizeLocal(ctx, request.Invocation, request.GuildID, domain.CapabilityModerationCaseWrite, "moderation_case_create", domain.ResourceModerationCase, domain.DestructiveNone, map[string]string{"reason_code": string(request.ReasonCode)})
	if err != nil || plan != nil {
		return domain.Case{}, plan, err
	}
	events, err := s.store.Replay(request.GuildID)
	if err != nil {
		return domain.Case{}, nil, err
	}
	if countCases(events) >= s.settings.MaxCasesPerGuild {
		return domain.Case{}, nil, &domain.ConflictError{Reason: "moderation case cap reached"}
	}
	id, err := s.ids.NewModerationID("case")
	if err != nil {
		return domain.Case{}, nil, err
	}
	now := s.now().UTC()
	value := domain.Case{ID: domain.CaseID(id), GuildID: request.GuildID, SubjectUserID: request.SubjectUserID, ActorID: request.Invocation.ActorID, CorrelationID: request.Invocation.CorrelationID, ReasonCode: request.ReasonCode, Status: domain.CaseOpen, Notes: notes, CreatedAt: now, UpdatedAt: now}
	if err := value.Validate(); err != nil {
		return domain.Case{}, nil, err
	}
	_, err = s.store.Append(request.GuildID, domain.ModerationJournalEvent{Type: domain.ModerationCaseUpsert, OccurredAt: now, Case: &value})
	if err != nil {
		_ = finish("error")
		return domain.Case{}, nil, err
	}
	return value, nil, finish("success")
}

func (s *ModerationService) GetCase(ctx context.Context, query CaseQuery) (domain.Case, error) {
	finish, plan, err := s.authorizeLocal(ctx, query.Invocation, query.GuildID, domain.CapabilityModerationCaseRead, "moderation_case_get", domain.ResourceModerationCase, domain.DestructiveNone, nil)
	if err != nil {
		return domain.Case{}, err
	}
	if plan != nil {
		return domain.Case{}, &domain.AuthorizationError{Reason: domain.PolicyModeDenied}
	}
	events, err := s.store.Replay(query.GuildID)
	if err != nil {
		return domain.Case{}, err
	}
	cases, _, _ := materializeModeration(events)
	value, ok := cases[query.CaseID]
	if !ok || query.SubjectUserID != "" && value.SubjectUserID != query.SubjectUserID {
		return domain.Case{}, &domain.ConflictError{Reason: "moderation case not found"}
	}
	return value, finish("success")
}

func (s *ModerationService) ListCases(ctx context.Context, query CaseQuery) ([]domain.Case, error) {
	finish, plan, err := s.authorizeLocal(ctx, query.Invocation, query.GuildID, domain.CapabilityModerationCaseRead, "moderation_case_list", domain.ResourceModerationCase, domain.DestructiveNone, nil)
	if err != nil {
		return nil, err
	}
	if plan != nil {
		return nil, &domain.AuthorizationError{Reason: domain.PolicyModeDenied}
	}
	events, err := s.store.Replay(query.GuildID)
	if err != nil {
		return nil, err
	}
	cases, _, _ := materializeModeration(events)
	result := []domain.Case{}
	for _, value := range cases {
		if query.SubjectUserID == "" || value.SubjectUserID == query.SubjectUserID {
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, finish("success")
}

func (s *ModerationService) AttachEvidence(ctx context.Context, request EvidenceAttachRequest) (*domain.OperationPlan, error) {
	caseValue, err := s.caseWithoutAuthorization(request.Evidence.GuildID, request.CaseID)
	if err != nil {
		return nil, err
	}
	if request.Evidence.GuildID != caseValue.GuildID || request.Evidence.CaseID != request.CaseID || request.Evidence.SubjectUserID != caseValue.SubjectUserID || request.Evidence.ActorID != request.Invocation.ActorID || request.Evidence.CorrelationID != request.Invocation.CorrelationID {
		return nil, &domain.ValidationError{Field: "evidence", Rule: "case, guild, subject, actor, and correlation bindings must match"}
	}
	if err := request.Evidence.Validate(); err != nil {
		return nil, err
	}
	finish, plan, err := s.authorizeLocal(ctx, request.Invocation, caseValue.GuildID, domain.CapabilityModerationEvidenceWrite, "moderation_evidence_attach", domain.ResourceModerationEvidence, domain.DestructiveNone, map[string]string{"evidence_kind": string(request.Evidence.Kind)})
	if err != nil || plan != nil {
		return plan, err
	}
	if _, err := s.store.Append(caseValue.GuildID, domain.ModerationJournalEvent{Type: domain.ModerationEvidenceAttach, OccurredAt: s.now().UTC(), Evidence: &request.Evidence}); err != nil {
		return nil, err
	}
	return nil, finish("success")
}

func (s *ModerationService) AppealExport(ctx context.Context, query CaseQuery) (domain.AppealExport, error) {
	finish, plan, err := s.authorizeLocal(ctx, query.Invocation, query.GuildID, domain.CapabilityModerationEvidenceExport, "moderation_appeal_export", domain.ResourceModerationEvidence, domain.DestructiveNone, nil)
	if err != nil {
		return domain.AppealExport{}, err
	}
	if plan != nil {
		return domain.AppealExport{}, &domain.AuthorizationError{Reason: domain.PolicyModeDenied}
	}
	result, err := s.store.AppealExport(query.GuildID, query.SubjectUserID, s.now().UTC())
	if err != nil {
		return result, err
	}
	return result, finish("success")
}

func (s *ModerationService) EvaluateEscalation(ctx context.Context, request EscalationEvaluateRequest) (EscalationResult, error) {
	if request.GuildID == "" || request.SubjectUserID == "" {
		return EscalationResult{}, &domain.ValidationError{Field: "escalation", Rule: "guild and subject are required"}
	}
	finish, plan, err := s.authorizeLocal(ctx, request.Invocation, request.GuildID, domain.CapabilityModerationEscalation, "moderation_escalation_evaluate", domain.ResourceModerationCase, domain.DestructiveNone, map[string]string{"policy_version": s.settings.Policy.Version})
	if err != nil || plan != nil {
		return EscalationResult{Plan: plan}, err
	}
	events, err := s.store.Replay(request.GuildID)
	if err != nil {
		return EscalationResult{}, err
	}
	recommendation, err := domain.EvaluateEscalation(s.settings.Policy, subjectActionHistory(events, request.SubjectUserID), s.now().UTC())
	if err != nil {
		_ = finish("error")
		return EscalationResult{}, err
	}
	outcome := "no_match"
	if recommendation.RuleID != "" {
		outcome = "recommended"
	}
	s.telemetry.ObserveModeration(ctx, ModerationObservation{Outcome: outcome, Reason: recommendation.Reason, PolicyVersion: s.settings.Policy.Version})
	return EscalationResult{Recommendation: recommendation}, finish(outcome)
}

func (s *ModerationService) ApplyEscalation(ctx context.Context, request EscalationApplyRequest) (ModerationActionResult, error) {
	if request.Recommendation.RuleID == "" {
		return ModerationActionResult{}, &domain.ValidationError{Field: "recommendation", Rule: "matching escalation recommendation is required"}
	}
	events, err := s.store.Replay(request.GuildID)
	if err != nil {
		return ModerationActionResult{}, err
	}
	current, err := domain.EvaluateEscalation(s.settings.Policy, subjectActionHistory(events, request.SubjectUserID), s.now().UTC())
	if err != nil {
		return ModerationActionResult{}, err
	}
	if !equalEscalationRecommendation(current, request.Recommendation) {
		return ModerationActionResult{}, &domain.ConflictError{Reason: "stale escalation recommendation"}
	}
	action := ModerationActionRequest{Invocation: request.Invocation, CaseID: request.CaseID, GuildID: request.GuildID, SubjectUserID: request.SubjectUserID, Kind: current.Action, ReasonCode: request.ReasonCode, IdempotencyKey: request.IdempotencyKey, Duration: current.Duration}
	if current.DeleteWindow > 0 {
		seconds := int(current.DeleteWindow.Seconds())
		action.DeleteMessageSeconds = &seconds
	}
	return s.ExecuteAction(ctx, action)
}

func equalEscalationRecommendation(left, right domain.EscalationRecommendation) bool {
	encodedLeft, _ := json.Marshal(left)
	encodedRight, _ := json.Marshal(right)
	return string(encodedLeft) == string(encodedRight)
}

func subjectActionHistory(events []domain.ModerationJournalEvent, subject domain.UserID) []domain.ModerationAction {
	_, actions, _ := materializeModeration(events)
	result := make([]domain.ModerationAction, 0, len(actions))
	for _, action := range actions {
		if action.SubjectUserID == subject {
			result = append(result, action)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if !result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].CreatedAt.Before(result[j].CreatedAt)
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func (s *ModerationService) ExecuteAction(ctx context.Context, request ModerationActionRequest) (ModerationActionResult, error) {
	started := s.now()
	capability, destructive, err := moderationActionPolicy(request.Kind)
	if err != nil {
		return ModerationActionResult{}, err
	}
	caseValue, err := s.caseWithoutAuthorization(request.GuildID, request.CaseID)
	if err != nil {
		return ModerationActionResult{}, err
	}
	if caseValue.SubjectUserID != request.SubjectUserID || caseValue.Status != domain.CaseOpen || !s.reasonAllowed(request.ReasonCode) || request.IdempotencyKey == "" {
		return ModerationActionResult{}, &domain.ValidationError{Field: "action", Rule: "open case, matching subject, configured reason, and idempotency key are required"}
	}
	notes, _ := s.redactor.Sanitize(request.Notes)
	if len(notes) > domain.MaxModerationNoteLength {
		return ModerationActionResult{}, &domain.ValidationError{Field: "notes", Rule: "too long"}
	}
	events, err := s.store.Replay(request.GuildID)
	if err != nil {
		return ModerationActionResult{}, err
	}
	if existing, conflict := duplicateAction(events, request); existing != nil || conflict != nil {
		if conflict != nil {
			return ModerationActionResult{}, conflict
		}
		return ModerationActionResult{Action: *existing}, nil
	}
	if err := validateActionEvidence(events, request.CaseID, request.GuildID, request.EvidenceIDs); err != nil {
		return ModerationActionResult{}, err
	}
	if countCaseActions(events, request.CaseID) >= s.settings.MaxActionsPerCase {
		return ModerationActionResult{}, &domain.ConflictError{Reason: "moderation action cap reached"}
	}
	if request.Kind == domain.ActionTimeoutRemove && !activeTimeout(events, request.SubjectUserID, s.now()) {
		return s.recordNoop(ctx, request, capability, notes, "timeout_not_active")
	}
	if hierarchyRequired(request.Kind) {
		if err := s.checkHierarchy(ctx, request, capability); err != nil {
			s.telemetry.ObserveModeration(ctx, ModerationObservation{ActionKind: request.Kind, Outcome: "denied", Reason: "hierarchy", PolicyVersion: s.settings.Policy.Version})
			s.discord.securityTelemetry.RecordSecurityEvent(ctx, domain.SecurityEvent{Kind: "moderation_hierarchy_failure", Reason: domain.PolicyResourceDenied, Mode: request.Invocation.RequestedMode, DestructiveClass: destructive, CorrelationID: request.Invocation.CorrelationID, OccurredAt: s.now()})
			return ModerationActionResult{}, err
		}
	}
	command, localOnly, err := s.actionCommand(request)
	if err != nil {
		return ModerationActionResult{}, err
	}
	if localOnly {
		return s.executeLocalAction(ctx, request, capability, destructive, notes)
	}
	decision, err := s.discord.policy.Decide(ctx, domain.PolicyRequest{Principal: request.Invocation.Principal, ActorID: request.Invocation.ActorID, GuildID: request.GuildID, Operation: domain.OperationName("moderation_" + string(request.Kind)), Capability: capability, Resources: []domain.ResourceRef{{Kind: domain.ResourceModerationCase, ID: string(request.CaseID)}}, Mode: request.Invocation.RequestedMode, DestructiveClass: destructive, CorrelationID: request.Invocation.CorrelationID})
	if err != nil {
		return ModerationActionResult{}, &domain.AuthorizationError{Reason: domain.PolicyError}
	}
	var action domain.ModerationAction
	if decision.Allowed && decision.EffectiveMode == domain.ModeApply {
		id, idErr := s.ids.NewModerationID("action")
		if idErr != nil {
			return ModerationActionResult{}, idErr
		}
		action = s.newAction(request, domain.ModerationActionID(id), notes)
		if _, appendErr := s.store.Append(request.GuildID, domain.ModerationJournalEvent{Type: domain.ModerationActionAppend, OccurredAt: action.CreatedAt, Action: &action}); appendErr != nil {
			return ModerationActionResult{}, appendErr
		}
	}
	execution, err := s.discord.dispatchDiscordWithCapability(ctx, request.Invocation, command, capability)
	if execution.Plan != nil {
		return ModerationActionResult{Plan: execution.Plan}, err
	}
	if action.ID != "" {
		action.Status, action.Result = domain.ActionCompleted, "applied"
		completed := s.now().UTC()
		action.CompletedAt = &completed
		if err != nil {
			action.Status, action.Result = domain.ActionFailed, "retryable_failure"
			if execution.Partial != nil {
				action.Status, action.Result = domain.ActionOperatorReview, "uncertain_mutation"
			}
		}
		if _, appendErr := s.store.Append(request.GuildID, domain.ModerationJournalEvent{Type: domain.ModerationActionAppend, OccurredAt: completed, Action: &action}); appendErr != nil {
			return ModerationActionResult{}, appendErr
		}
	}
	outcome := action.Result
	if outcome == "" {
		outcome = "denied"
	}
	s.telemetry.ObserveModeration(ctx, ModerationObservation{ActionKind: request.Kind, Outcome: outcome, PolicyVersion: s.settings.Policy.Version, Latency: s.now().Sub(started)})
	if action.Status == domain.ActionCompleted && domain.RequiresApproval(destructive) {
		s.discord.securityTelemetry.RecordSecurityEvent(ctx, domain.SecurityEvent{Kind: "moderation_destructive_apply", Reason: domain.PolicyAllowed, Mode: domain.ModeApply, DestructiveClass: destructive, CorrelationID: request.Invocation.CorrelationID, OccurredAt: s.now()})
	}
	return ModerationActionResult{Action: action}, err
}

func (s *ModerationService) Warn(ctx context.Context, request ModerationActionRequest) (ModerationActionResult, error) {
	request.Kind = domain.ActionWarn
	return s.ExecuteAction(ctx, request)
}
func (s *ModerationService) Timeout(ctx context.Context, request ModerationActionRequest) (ModerationActionResult, error) {
	request.Kind = domain.ActionTimeout
	return s.ExecuteAction(ctx, request)
}
func (s *ModerationService) RemoveTimeout(ctx context.Context, request ModerationActionRequest) (ModerationActionResult, error) {
	request.Kind = domain.ActionTimeoutRemove
	return s.ExecuteAction(ctx, request)
}
func (s *ModerationService) Kick(ctx context.Context, request ModerationActionRequest) (ModerationActionResult, error) {
	request.Kind = domain.ActionKick
	return s.ExecuteAction(ctx, request)
}
func (s *ModerationService) Ban(ctx context.Context, request ModerationActionRequest) (ModerationActionResult, error) {
	request.Kind = domain.ActionBan
	return s.ExecuteAction(ctx, request)
}
func (s *ModerationService) Unban(ctx context.Context, request ModerationActionRequest) (ModerationActionResult, error) {
	request.Kind = domain.ActionUnban
	return s.ExecuteAction(ctx, request)
}
func (s *ModerationService) DeleteMessage(ctx context.Context, request ModerationActionRequest) (ModerationActionResult, error) {
	request.Kind = domain.ActionMessageDelete
	return s.ExecuteAction(ctx, request)
}
func (s *ModerationService) BulkDeleteMessages(ctx context.Context, request ModerationActionRequest) (ModerationActionResult, error) {
	request.Kind = domain.ActionBulkMessageDelete
	return s.ExecuteAction(ctx, request)
}
func (s *ModerationService) AddQuarantine(ctx context.Context, request ModerationActionRequest) (ModerationActionResult, error) {
	request.Kind = domain.ActionQuarantineAdd
	return s.ExecuteAction(ctx, request)
}
func (s *ModerationService) RemoveQuarantine(ctx context.Context, request ModerationActionRequest) (ModerationActionResult, error) {
	request.Kind = domain.ActionQuarantineRemove
	return s.ExecuteAction(ctx, request)
}
func (s *ModerationService) CreateInvite(ctx context.Context, request ModerationActionRequest) (ModerationActionResult, error) {
	request.Kind = domain.ActionInviteCreate
	return s.ExecuteAction(ctx, request)
}
func (s *ModerationService) RevokeInvite(ctx context.Context, request ModerationActionRequest) (ModerationActionResult, error) {
	request.Kind = domain.ActionInviteRevoke
	return s.ExecuteAction(ctx, request)
}

func (s *ModerationService) ListInvites(ctx context.Context, invocation Invocation, guildID domain.GuildID) (domain.Execution[[]domain.Invite], error) {
	execution, err := s.discord.dispatchDiscordWithCapability(ctx, invocation, domain.ListGuildInvites{CommandContext: domain.CommandContext{GuildID: guildID}}, domain.CapabilityModerationInviteControl)
	if err != nil {
		return domain.Execution[[]domain.Invite]{Plan: execution.Plan}, err
	}
	if execution.Plan != nil {
		return domain.Execution[[]domain.Invite]{Plan: execution.Plan}, nil
	}
	value, ok := execution.Result.([]domain.Invite)
	if !ok {
		return domain.Execution[[]domain.Invite]{}, &domain.InvariantError{Detail: "invite result type"}
	}
	return domain.Execution[[]domain.Invite]{Result: value, Attempts: execution.Attempts}, nil
}

func (s *ModerationService) executeLocalAction(ctx context.Context, request ModerationActionRequest, capability domain.Capability, destructive domain.DestructiveClass, notes string) (ModerationActionResult, error) {
	finish, plan, err := s.authorizeLocal(ctx, request.Invocation, request.GuildID, capability, "moderation_"+string(request.Kind), domain.ResourceModerationCase, destructive, map[string]string{"case_id": string(request.CaseID), "action_kind": string(request.Kind), "reason_code": string(request.ReasonCode)})
	if err != nil || plan != nil {
		return ModerationActionResult{Plan: plan}, err
	}
	id, err := s.ids.NewModerationID("action")
	if err != nil {
		return ModerationActionResult{}, err
	}
	action := s.newAction(request, domain.ModerationActionID(id), notes)
	if _, err := s.store.Append(request.GuildID, domain.ModerationJournalEvent{Type: domain.ModerationActionAppend, OccurredAt: action.CreatedAt, Action: &action}); err != nil {
		return ModerationActionResult{}, err
	}
	action.Status, action.Result = domain.ActionCompleted, "recorded"
	completed := s.now().UTC()
	action.CompletedAt = &completed
	if _, err := s.store.Append(request.GuildID, domain.ModerationJournalEvent{Type: domain.ModerationActionAppend, OccurredAt: completed, Action: &action}); err != nil {
		return ModerationActionResult{}, err
	}
	return ModerationActionResult{Action: action}, finish("success")
}

func (s *ModerationService) recordNoop(ctx context.Context, request ModerationActionRequest, capability domain.Capability, notes, result string) (ModerationActionResult, error) {
	finish, plan, err := s.authorizeLocal(ctx, request.Invocation, request.GuildID, capability, "moderation_"+string(request.Kind), domain.ResourceModerationCase, domain.DestructiveNone, map[string]string{"action_kind": string(request.Kind)})
	if err != nil || plan != nil {
		return ModerationActionResult{Plan: plan}, err
	}
	id, err := s.ids.NewModerationID("action")
	if err != nil {
		return ModerationActionResult{}, err
	}
	action := s.newAction(request, domain.ModerationActionID(id), notes)
	action.Status, action.Result = domain.ActionNoop, result
	completed := s.now().UTC()
	action.CompletedAt = &completed
	_, err = s.store.Append(request.GuildID, domain.ModerationJournalEvent{Type: domain.ModerationActionAppend, OccurredAt: completed, Action: &action})
	if err == nil {
		err = finish("noop")
	}
	return ModerationActionResult{Action: action}, err
}

func (s *ModerationService) actionCommand(request ModerationActionRequest) (domain.Command, bool, error) {
	auditReason := string(request.ReasonCode)
	base := domain.CommandContext{GuildID: request.GuildID, AuditReason: &auditReason, IdempotencyKey: request.IdempotencyKey, ReconciliationKey: domain.ReconciliationKey(request.IdempotencyKey)}
	switch request.Kind {
	case domain.ActionWarn:
		return nil, true, nil
	case domain.ActionTimeout:
		if request.Duration <= 0 || request.Duration > domain.MaxDiscordTimeout {
			return nil, false, &domain.ValidationError{Field: "duration", Rule: "must be within 28 days"}
		}
		until := s.now().UTC().Add(request.Duration)
		return domain.ModifyMember{CommandContext: base, UserID: request.SubjectUserID, CommunicationDisabledUntil: &until}, false, nil
	case domain.ActionTimeoutRemove:
		return domain.ModifyMember{CommandContext: base, UserID: request.SubjectUserID, RemoveCommunicationDisabled: boolPointer(true)}, false, nil
	case domain.ActionKick:
		return domain.RemoveMember{CommandContext: base, UserID: request.SubjectUserID}, false, nil
	case domain.ActionBan:
		return domain.CreateMemberBan{CommandContext: base, UserID: request.SubjectUserID, DeleteMessageSeconds: request.DeleteMessageSeconds}, false, nil
	case domain.ActionUnban:
		return domain.DeleteMemberBan{CommandContext: base, UserID: request.SubjectUserID}, false, nil
	case domain.ActionMessageDelete:
		if request.ChannelID == "" || len(request.MessageIDs) != 1 {
			return nil, false, &domain.ValidationError{Field: "message_ids", Rule: "single delete requires one message"}
		}
		return domain.DeleteMessage{CommandContext: base, ChannelID: request.ChannelID, MessageID: request.MessageIDs[0]}, false, nil
	case domain.ActionBulkMessageDelete:
		if request.ChannelID == "" || len(request.MessageIDs) < 2 || len(request.MessageIDs) > MaxBulkDeleteMessages {
			return nil, false, &domain.ValidationError{Field: "message_ids", Rule: "bulk delete requires 2-100 messages"}
		}
		return domain.BulkDeleteMessages{CommandContext: base, ChannelID: request.ChannelID, MessageIDs: request.MessageIDs}, false, nil
	case domain.ActionQuarantineAdd:
		if s.settings.QuarantineRoleID == "" {
			return nil, false, &domain.ConflictError{Reason: "quarantine role is not configured"}
		}
		return domain.AddMemberRole{CommandContext: base, UserID: request.SubjectUserID, RoleID: s.settings.QuarantineRoleID}, false, nil
	case domain.ActionQuarantineRemove:
		if s.settings.QuarantineRoleID == "" {
			return nil, false, &domain.ConflictError{Reason: "quarantine role is not configured"}
		}
		return domain.RemoveMemberRole{CommandContext: base, UserID: request.SubjectUserID, RoleID: s.settings.QuarantineRoleID}, false, nil
	case domain.ActionInviteCreate:
		if request.ChannelID == "" {
			return nil, false, &domain.ValidationError{Field: "channel_id", Rule: "required"}
		}
		return domain.CreateInvite{CommandContext: base, ChannelID: request.ChannelID}, false, nil
	case domain.ActionInviteRevoke:
		if request.InviteCode == "" {
			return nil, false, &domain.ValidationError{Field: "invite_code", Rule: "required"}
		}
		return domain.DeleteInvite{CommandContext: base, Code: request.InviteCode}, false, nil
	default:
		return nil, false, &domain.ValidationError{Field: "kind", Rule: "unsupported moderation action"}
	}
}

func (s *ModerationService) checkHierarchy(ctx context.Context, request ModerationActionRequest, capability domain.Capability) error {
	base := domain.CommandContext{GuildID: request.GuildID}
	current, err := s.discord.dispatchDiscordWithCapability(ctx, request.Invocation, domain.GetCurrentUser{CommandContext: base}, capability)
	if err != nil {
		return err
	}
	bot, ok := current.Result.(domain.User)
	if !ok || bot.ID == "" {
		return &domain.ConflictError{Reason: "current bot identity is unknown"}
	}
	inspectionResult, err := s.discord.dispatchDiscordWithCapability(ctx, request.Invocation, domain.InspectGuild{CommandContext: base}, capability)
	if err != nil {
		return err
	}
	inspection, ok := inspectionResult.Result.(domain.GuildInspection)
	if !ok || inspection.Guild.OwnerID == "" {
		return &domain.ConflictError{Reason: "guild owner is unknown"}
	}
	if request.SubjectUserID == inspection.Guild.OwnerID {
		return &domain.ConflictError{Reason: "guild owner cannot be moderated"}
	}
	targetResult, err := s.discord.dispatchDiscordWithCapability(ctx, request.Invocation, domain.GetMember{CommandContext: base, UserID: request.SubjectUserID}, capability)
	if err != nil {
		return err
	}
	target, ok := targetResult.Result.(domain.Member)
	if !ok {
		return &domain.ConflictError{Reason: "target member is unknown"}
	}
	botResult, err := s.discord.dispatchDiscordWithCapability(ctx, request.Invocation, domain.GetMember{CommandContext: base, UserID: bot.ID}, capability)
	if err != nil {
		return err
	}
	botMember, ok := botResult.Result.(domain.Member)
	if !ok {
		return &domain.ConflictError{Reason: "bot member is unknown"}
	}
	rolesResult, err := s.discord.dispatchDiscordWithCapability(ctx, request.Invocation, domain.ListGuildRoles{CommandContext: base}, capability)
	if err != nil {
		return err
	}
	roles, ok := rolesResult.Result.([]domain.Role)
	if !ok || len(roles) == 0 {
		return &domain.ConflictError{Reason: "guild role hierarchy is unknown"}
	}
	positions := map[domain.RoleID]int{}
	managed := map[domain.RoleID]bool{}
	for _, role := range roles {
		positions[role.ID], managed[role.ID] = role.Position, role.Managed
	}
	highest := func(ids []domain.RoleID) (int, bool) {
		value, found := 0, false
		for _, id := range ids {
			position, ok := positions[id]
			if !ok {
				return 0, false
			}
			if !found || position > value {
				value, found = position, true
			}
		}
		return value, found
	}
	for _, roleID := range target.Roles {
		if _, protected := s.settings.ProtectedOperatorRoleIDs[roleID]; protected {
			return &domain.ConflictError{Reason: "target has a protected operator role"}
		}
	}
	targetPosition, targetKnown := highest(target.Roles)
	botPosition, botKnown := highest(botMember.Roles)
	if !targetKnown || !botKnown || botPosition <= targetPosition {
		return &domain.ConflictError{Reason: "bot hierarchy does not exceed target"}
	}
	if request.Kind == domain.ActionQuarantineAdd || request.Kind == domain.ActionQuarantineRemove {
		position, exists := positions[s.settings.QuarantineRoleID]
		if !exists || managed[s.settings.QuarantineRoleID] || position >= botPosition {
			return &domain.ConflictError{Reason: "quarantine role is managed, unknown, or above the bot"}
		}
	}
	return nil
}

func (s *ModerationService) authorizeLocal(ctx context.Context, invocation Invocation, guildID domain.GuildID, capability domain.Capability, operation string, resource domain.ResourceKind, destructive domain.DestructiveClass, intent map[string]string) (func(string) error, *domain.OperationPlan, error) {
	if !invocation.valid() {
		return nil, nil, &domain.AuthorizationError{Reason: domain.PolicyMalformedPrincipal}
	}
	request := domain.PolicyRequest{Principal: invocation.Principal, ActorID: invocation.ActorID, GuildID: guildID, Operation: domain.OperationName(operation), Capability: capability, Resources: []domain.ResourceRef{{Kind: resource}}, Mode: invocation.RequestedMode, DestructiveClass: destructive, CorrelationID: invocation.CorrelationID}
	decision, err := s.discord.evaluatePolicy(ctx, request)
	if err != nil || !decision.Allowed {
		reason := decision.Reason
		if reason == "" {
			reason = domain.PolicyError
		}
		return nil, nil, &domain.AuthorizationError{Reason: reason}
	}
	if decision.EffectiveMode == domain.ModeReadOnly && operation != "moderation_case_get" && operation != "moderation_case_list" && operation != "moderation_appeal_export" {
		return nil, nil, &domain.AuthorizationError{Reason: domain.PolicyModeDenied}
	}
	encoded, _ := json.Marshal(struct {
		Request domain.PolicyRequest
		Intent  map[string]string
	}{request, intent})
	sum := sha256.Sum256(encoded)
	hash := hex.EncodeToString(sum[:])
	event := domain.AuditEvent{Kind: domain.AuditEventKind("decision"), Timestamp: s.now(), ActorID: decision.ActorID, Adapter: invocation.Principal.Adapter, GuildID: guildID, Operation: request.Operation, Capability: capability, ResourceKinds: []domain.ResourceKind{resource}, Intent: intent, Decision: true, Reason: decision.Reason, Mode: decision.EffectiveMode, DestructiveClass: destructive, Result: "pending", CorrelationID: invocation.CorrelationID, OperationHash: hash}
	if err := s.discord.audit.Append(ctx, event); err != nil {
		return nil, nil, &domain.AuditError{Stage: "precommit"}
	}
	finish := func(result string) error {
		completion := event
		completion.Kind, completion.Timestamp, completion.Result = domain.AuditEventKind("completion"), s.now(), result
		if err := s.discord.audit.Append(context.WithoutCancel(ctx), completion); err != nil {
			return &domain.AuditError{Stage: "completion"}
		}
		return nil
	}
	if decision.EffectiveMode == domain.ModeDryRun {
		plan := &domain.OperationPlan{GuildID: guildID, Operation: request.Operation, Capability: capability, Mode: decision.EffectiveMode, DestructiveClass: destructive, ResourceKinds: []domain.ResourceKind{resource}, Intent: intent, OperationHash: hash}
		return finish, plan, finish("dry_run")
	}
	return finish, nil, nil
}

func boolPointer(value bool) *bool { return &value }

func (s *ModerationService) newAction(request ModerationActionRequest, id domain.ModerationActionID, notes string) domain.ModerationAction {
	now := s.now().UTC()
	action := domain.ModerationAction{ID: id, CaseID: request.CaseID, GuildID: request.GuildID, SubjectUserID: request.SubjectUserID, ActorID: request.Invocation.ActorID, CorrelationID: request.Invocation.CorrelationID, IdempotencyKey: request.IdempotencyKey, ReasonCode: request.ReasonCode, Kind: request.Kind, Status: domain.ActionPending, EvidenceIDs: append([]domain.EvidenceID(nil), request.EvidenceIDs...), Notes: notes, CreatedAt: now}
	if request.Kind == domain.ActionTimeout {
		expiry := now.Add(request.Duration)
		action.ExpiresAt = &expiry
	}
	return action
}
func (s *ModerationService) reasonAllowed(reason domain.ReasonCode) bool {
	_, ok := s.settings.ReasonCodes[reason]
	return ok
}
func (s *ModerationService) caseWithoutAuthorization(guildID domain.GuildID, caseID domain.CaseID) (domain.Case, error) {
	events, err := s.store.Replay(guildID)
	if err != nil {
		return domain.Case{}, err
	}
	cases, _, _ := materializeModeration(events)
	value, ok := cases[caseID]
	if !ok {
		return value, &domain.ConflictError{Reason: "moderation case not found"}
	}
	return value, nil
}

func materializeModeration(events []domain.ModerationJournalEvent) (map[domain.CaseID]domain.Case, map[domain.ModerationActionID]domain.ModerationAction, map[domain.EvidenceID]domain.EvidenceReference) {
	cases := map[domain.CaseID]domain.Case{}
	actions := map[domain.ModerationActionID]domain.ModerationAction{}
	evidence := map[domain.EvidenceID]domain.EvidenceReference{}
	for _, event := range events {
		if event.Case != nil {
			cases[event.Case.ID] = *event.Case
		}
		if event.Action != nil {
			actions[event.Action.ID] = *event.Action
		}
		if event.Evidence != nil {
			evidence[event.Evidence.ID] = *event.Evidence
		}
	}
	return cases, actions, evidence
}
func countCases(events []domain.ModerationJournalEvent) int {
	cases, _, _ := materializeModeration(events)
	return len(cases)
}
func validateActionEvidence(events []domain.ModerationJournalEvent, caseID domain.CaseID, guildID domain.GuildID, ids []domain.EvidenceID) error {
	_, _, evidence := materializeModeration(events)
	seen := map[domain.EvidenceID]struct{}{}
	for _, id := range ids {
		value, ok := evidence[id]
		if !ok || value.CaseID != caseID || value.GuildID != guildID {
			return &domain.ValidationError{Field: "evidence_ids", Rule: "all evidence must belong to the case and guild"}
		}
		if _, duplicate := seen[id]; duplicate {
			return &domain.ValidationError{Field: "evidence_ids", Rule: "duplicates are forbidden"}
		}
		seen[id] = struct{}{}
	}
	return nil
}

func countCaseActions(events []domain.ModerationJournalEvent, caseID domain.CaseID) int {
	_, actions, _ := materializeModeration(events)
	count := 0
	for _, action := range actions {
		if action.CaseID == caseID {
			count++
		}
	}
	return count
}
func duplicateAction(events []domain.ModerationJournalEvent, request ModerationActionRequest) (*domain.ModerationAction, error) {
	_, actions, _ := materializeModeration(events)
	for _, action := range actions {
		if action.IdempotencyKey == request.IdempotencyKey {
			if action.CaseID != request.CaseID || action.SubjectUserID != request.SubjectUserID || action.Kind != request.Kind || action.ReasonCode != request.ReasonCode {
				return nil, &domain.IdempotencyConflictError{}
			}
			value := action
			return &value, nil
		}
	}
	return nil, nil
}
func activeTimeout(events []domain.ModerationJournalEvent, subject domain.UserID, now time.Time) bool {
	_, actions, _ := materializeModeration(events)
	for _, action := range actions {
		if action.SubjectUserID == subject && action.Kind == domain.ActionTimeout && action.Status == domain.ActionCompleted && action.ExpiresAt != nil && action.ExpiresAt.After(now) {
			return true
		}
	}
	return false
}
func hierarchyRequired(kind domain.ActionKind) bool {
	return kind == domain.ActionTimeout || kind == domain.ActionTimeoutRemove || kind == domain.ActionKick || kind == domain.ActionBan || kind == domain.ActionQuarantineAdd || kind == domain.ActionQuarantineRemove
}
func moderationActionPolicy(kind domain.ActionKind) (domain.Capability, domain.DestructiveClass, error) {
	switch kind {
	case domain.ActionWarn:
		return domain.CapabilityModerationWarn, domain.DestructiveNone, nil
	case domain.ActionTimeout, domain.ActionTimeoutRemove:
		return domain.CapabilityModerationTimeout, domain.DestructiveNone, nil
	case domain.ActionKick:
		return domain.CapabilityModerationKick, domain.DestructiveKick, nil
	case domain.ActionBan, domain.ActionUnban:
		return domain.CapabilityModerationBan, domain.DestructiveBan, nil
	case domain.ActionMessageDelete:
		return domain.CapabilityModerationMessageDelete, domain.DestructiveDelete, nil
	case domain.ActionBulkMessageDelete:
		return domain.CapabilityModerationMessageDelete, domain.DestructiveBulkDelete, nil
	case domain.ActionQuarantineAdd, domain.ActionQuarantineRemove:
		return domain.CapabilityModerationQuarantine, domain.DestructiveRoleEscalation, nil
	case domain.ActionInviteCreate:
		return domain.CapabilityModerationInviteControl, domain.DestructiveInviteCreate, nil
	case domain.ActionInviteRevoke:
		return domain.CapabilityModerationInviteControl, domain.DestructiveDelete, nil
	default:
		return "", "", &domain.ValidationError{Field: "kind", Rule: "unsupported moderation action"}
	}
}
