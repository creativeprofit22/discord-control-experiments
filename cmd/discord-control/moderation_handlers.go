package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/config"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"github.com/ken-kai/discord-control-experiments/internal/observability"
	"github.com/ken-kai/discord-control-experiments/internal/state"
)

func buildModerationServices(discord *application.DiscordService, paths state.Paths, configured config.Moderation, redactor *observability.Redactor, telemetry *observability.Provider) (*application.ModerationService, *application.LockdownService, error) {
	retention, err := time.ParseDuration(configured.Retention)
	if err != nil {
		return nil, nil, err
	}
	policy := domain.EscalationPolicy{Version: configured.PolicyVersion, MaxActionsPerSubjectWindow: configured.MaxActionsPerSubjectWindow, MaxActionsPerGuildWindow: configured.MaxActionsPerGuildWindow, Rules: make([]domain.EscalationRule, 0, len(configured.EscalationRules))}
	for _, rule := range configured.EscalationRules {
		lookback, _ := time.ParseDuration(rule.Lookback)
		duration, _ := optionalConfiguredDuration(rule.Duration)
		deleteWindow, _ := optionalConfiguredDuration(rule.DeleteWindow)
		prior := make([]domain.ActionKind, len(rule.EligiblePriorKinds))
		for i, value := range rule.EligiblePriorKinds {
			prior[i] = domain.ActionKind(value)
		}
		policy.Rules = append(policy.Rules, domain.EscalationRule{ID: rule.ID, Priority: rule.Priority, EligiblePriorKinds: prior, Lookback: lookback, MinimumCount: rule.MinimumCount, Action: domain.ActionKind(rule.Action), Duration: duration, DeleteWindow: deleteWindow, MaximumApplications: rule.MaximumApplications})
	}
	reasons := map[domain.ReasonCode]struct{}{}
	for _, value := range configured.ReasonCodes {
		reasons[domain.ReasonCode(value)] = struct{}{}
	}
	protected := map[domain.RoleID]struct{}{}
	protectedList := make([]domain.RoleID, len(configured.ProtectedOperatorRoleIDs))
	for i, value := range configured.ProtectedOperatorRoleIDs {
		protectedList[i] = domain.RoleID(value)
		protected[domain.RoleID(value)] = struct{}{}
	}
	moderation, err := application.NewModerationService(discord, state.NewModerationStore(paths), application.ModerationSettings{Policy: policy, Retention: retention, MaxCasesPerGuild: configured.MaxCasesPerGuild, MaxActionsPerCase: configured.MaxActionsPerCase, ReasonCodes: reasons, QuarantineRoleID: domain.RoleID(configured.QuarantineRoleID), ProtectedOperatorRoleIDs: protected}, correlationIDGenerator{}, redactor, telemetry)
	if err != nil {
		return nil, nil, err
	}
	if len(protectedList) == 0 {
		return moderation, nil, nil
	}
	ttl, _ := time.ParseDuration(configured.LockdownPlanTTL)
	lockdown, err := application.NewLockdownService(discord, state.NewLockdownStore(paths), application.LockdownSettings{ProtectedRoleIDs: protectedList, MaxOperations: configured.MaxLockdownOperations, MaxDestructiveOperations: configured.MaxLockdownDestructiveOperations, PlanTTL: ttl}, telemetry)
	return moderation, lockdown, err
}

func optionalConfiguredDuration(value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	return time.ParseDuration(value)
}

func addModerationOperatorHandlers(handlers map[string]application.OperatorHandler, moderation *application.ModerationService, lockdown *application.LockdownService) {
	handlers["moderation.case.create"] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
		var input struct {
			SubjectUserID domain.UserID     `json:"subject_user_id"`
			ReasonCode    domain.ReasonCode `json:"reason_code"`
			Notes         string            `json:"notes,omitempty"`
		}
		if err := decodeOperatorPayload(request.Payload, &input); err != nil {
			return application.OperatorResult{}, err
		}
		value, plan, err := moderation.CreateCase(ctx, application.CaseCreateRequest{Invocation: request.Invocation, GuildID: guildValue(request), SubjectUserID: input.SubjectUserID, ReasonCode: input.ReasonCode, Notes: input.Notes})
		return application.OperatorResult{Kind: "ModerationCase", Value: value, Plan: plan}, err
	}
	handlers["moderation.case.get"] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
		var input struct {
			CaseID        domain.CaseID `json:"case_id"`
			SubjectUserID domain.UserID `json:"subject_user_id,omitempty"`
		}
		if err := decodeOperatorPayload(request.Payload, &input); err != nil {
			return application.OperatorResult{}, err
		}
		value, err := moderation.GetCase(ctx, application.CaseQuery{Invocation: request.Invocation, GuildID: guildValue(request), CaseID: input.CaseID, SubjectUserID: input.SubjectUserID})
		return application.OperatorResult{Kind: "ModerationCase", Value: value}, err
	}
	handlers["moderation.case.list"] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
		var input struct {
			SubjectUserID domain.UserID `json:"subject_user_id,omitempty"`
		}
		if err := decodeOperatorPayload(request.Payload, &input); err != nil {
			return application.OperatorResult{}, err
		}
		value, err := moderation.ListCases(ctx, application.CaseQuery{Invocation: request.Invocation, GuildID: guildValue(request), SubjectUserID: input.SubjectUserID})
		return application.OperatorResult{Kind: "ModerationCases", Value: value}, err
	}
	handlers["moderation.evidence.attach"] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
		var input struct {
			CaseID   domain.CaseID            `json:"case_id"`
			Evidence domain.EvidenceReference `json:"evidence"`
		}
		if err := decodeOperatorPayload(request.Payload, &input); err != nil {
			return application.OperatorResult{}, err
		}
		input.Evidence.CaseID, input.Evidence.GuildID, input.Evidence.ActorID, input.Evidence.CorrelationID = input.CaseID, guildValue(request), request.Invocation.ActorID, request.Invocation.CorrelationID
		if input.Evidence.CreatedAt.IsZero() {
			input.Evidence.CreatedAt = time.Now().UTC()
		}
		plan, err := moderation.AttachEvidence(ctx, application.EvidenceAttachRequest{Invocation: request.Invocation, CaseID: input.CaseID, Evidence: input.Evidence})
		return application.OperatorResult{Kind: "ModerationEvidence", Value: input.Evidence, Plan: plan}, err
	}
	handlers["moderation.appeal.export"] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
		var input struct {
			SubjectUserID domain.UserID `json:"subject_user_id"`
		}
		if err := decodeOperatorPayload(request.Payload, &input); err != nil {
			return application.OperatorResult{}, err
		}
		value, err := moderation.AppealExport(ctx, application.CaseQuery{Invocation: request.Invocation, GuildID: guildValue(request), SubjectUserID: input.SubjectUserID})
		return application.OperatorResult{Kind: "AppealExport", Value: value}, err
	}
	handlers["moderation.escalation.evaluate"] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
		var input struct {
			SubjectUserID domain.UserID `json:"subject_user_id"`
		}
		if err := decodeOperatorPayload(request.Payload, &input); err != nil {
			return application.OperatorResult{}, err
		}
		value, err := moderation.EvaluateEscalation(ctx, application.EscalationEvaluateRequest{Invocation: request.Invocation, GuildID: guildValue(request), SubjectUserID: input.SubjectUserID})
		return application.OperatorResult{Kind: "Escalation", Value: value, Plan: value.Plan}, err
	}
	handlers["moderation.escalation.apply"] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
		var input struct {
			SubjectUserID  domain.UserID                   `json:"subject_user_id"`
			CaseID         domain.CaseID                   `json:"case_id"`
			ReasonCode     domain.ReasonCode               `json:"reason_code"`
			IdempotencyKey domain.IdempotencyKey           `json:"idempotency_key"`
			Recommendation domain.EscalationRecommendation `json:"recommendation"`
		}
		if err := decodeOperatorPayload(request.Payload, &input); err != nil {
			return application.OperatorResult{}, err
		}
		value, err := moderation.ApplyEscalation(ctx, application.EscalationApplyRequest{Invocation: request.Invocation, GuildID: guildValue(request), SubjectUserID: input.SubjectUserID, CaseID: input.CaseID, ReasonCode: input.ReasonCode, IdempotencyKey: input.IdempotencyKey, Recommendation: input.Recommendation})
		return application.OperatorResult{Kind: "ModerationAction", Value: value, Plan: value.Plan}, err
	}

	actions := map[string]domain.ActionKind{"moderation.warn": domain.ActionWarn, "moderation.timeout": domain.ActionTimeout, "moderation.timeout_remove": domain.ActionTimeoutRemove, "moderation.kick": domain.ActionKick, "moderation.ban": domain.ActionBan, "moderation.unban": domain.ActionUnban, "moderation.message_delete": domain.ActionMessageDelete, "moderation.bulk_message_delete": domain.ActionBulkMessageDelete, "moderation.quarantine_add": domain.ActionQuarantineAdd, "moderation.quarantine_remove": domain.ActionQuarantineRemove, "moderation.invite_create": domain.ActionInviteCreate, "moderation.invite_revoke": domain.ActionInviteRevoke}
	for handler, kind := range actions {
		kind := kind
		handlers[handler] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
			var input struct {
				CaseID               domain.CaseID         `json:"case_id"`
				SubjectUserID        domain.UserID         `json:"subject_user_id"`
				ReasonCode           domain.ReasonCode     `json:"reason_code"`
				Notes                string                `json:"notes,omitempty"`
				EvidenceIDs          []domain.EvidenceID   `json:"evidence_ids,omitempty"`
				IdempotencyKey       domain.IdempotencyKey `json:"idempotency_key"`
				Duration             string                `json:"duration,omitempty"`
				ChannelID            domain.ChannelID      `json:"channel_id,omitempty"`
				MessageIDs           []domain.MessageID    `json:"message_ids,omitempty"`
				InviteCode           domain.InviteCode     `json:"invite_code,omitempty"`
				DeleteMessageSeconds *int                  `json:"delete_message_seconds,omitempty"`
			}
			if err := decodeOperatorPayload(request.Payload, &input); err != nil {
				return application.OperatorResult{}, err
			}
			duration, err := optionalConfiguredDuration(input.Duration)
			if err != nil {
				return application.OperatorResult{}, err
			}
			value, err := moderation.ExecuteAction(ctx, application.ModerationActionRequest{Invocation: request.Invocation, CaseID: input.CaseID, GuildID: guildValue(request), SubjectUserID: input.SubjectUserID, Kind: kind, ReasonCode: input.ReasonCode, Notes: input.Notes, EvidenceIDs: input.EvidenceIDs, IdempotencyKey: input.IdempotencyKey, Duration: duration, ChannelID: input.ChannelID, MessageIDs: input.MessageIDs, InviteCode: input.InviteCode, DeleteMessageSeconds: input.DeleteMessageSeconds})
			return application.OperatorResult{Kind: "ModerationAction", Value: value, Plan: value.Plan}, err
		}
	}
	handlers["moderation.invite_list"] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
		value, err := moderation.ListInvites(ctx, request.Invocation, guildValue(request))
		return application.OperatorResult{Kind: "DiscordResult", Value: value.Result, Plan: value.Plan, Attempts: value.Attempts}, err
	}
	handlers["moderation.lockdown.plan"] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
		if lockdown == nil {
			return application.OperatorResult{}, errors.New("lockdown requires protectedOperatorRoleIds configuration")
		}
		var input struct {
			Scope domain.LockdownScope `json:"scope"`
		}
		if err := decodeOperatorPayload(request.Payload, &input); err != nil {
			return application.OperatorResult{}, err
		}
		plan, recovery, err := lockdown.BuildLockdownPlan(ctx, application.BuildLockdownRequest{Invocation: request.Invocation, GuildID: guildValue(request), Scope: input.Scope})
		return application.OperatorResult{Kind: "LockdownPlan", Value: map[string]any{"plan": plan, "recovery": recovery}}, err
	}
	for handler, recovering := range map[string]bool{"moderation.lockdown.apply": false, "moderation.lockdown.recover": true} {
		recovering := recovering
		handlers[handler] = func(ctx context.Context, request application.OperatorRequest) (application.OperatorResult, error) {
			if lockdown == nil {
				return application.OperatorResult{}, errors.New("lockdown requires protectedOperatorRoleIds configuration")
			}
			hash := domain.LockdownPlanHash(request.Values["--plan-hash"])
			approval := domain.ApprovalID(request.Values["--approval-id"])
			input := application.ApplyLockdownRequest{Invocation: request.Invocation, GuildID: guildValue(request), PlanHash: hash, ApprovalID: approval}
			var value application.LockdownApplyResult
			var err error
			if recovering {
				value, err = lockdown.RecoverLockdown(ctx, input)
			} else {
				value, err = lockdown.ApplyLockdown(ctx, input)
			}
			return application.OperatorResult{Kind: "LockdownApply", Value: value, Plan: value.Plan}, err
		}
	}
}

func decodeOperatorPayload(payload json.RawMessage, target any) error {
	if len(payload) == 0 {
		return errors.New("structured input is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode moderation input: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("moderation input contains trailing data")
	}
	return nil
}
func guildValue(request application.OperatorRequest) domain.GuildID {
	return domain.GuildID(request.Values["--guild"])
}
