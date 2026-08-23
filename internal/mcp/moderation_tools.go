package mcp

import (
	"reflect"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type moderationCaseCreateInput struct {
	SubjectUserID domain.UserID     `json:"subject_user_id"`
	ReasonCode    domain.ReasonCode `json:"reason_code"`
	Notes         string            `json:"notes,omitempty"`
}
type moderationCaseQueryInput struct {
	CaseID        domain.CaseID `json:"case_id,omitempty"`
	SubjectUserID domain.UserID `json:"subject_user_id,omitempty"`
}
type moderationEvidenceInput struct {
	CaseID   domain.CaseID            `json:"case_id"`
	Evidence domain.EvidenceReference `json:"evidence"`
}
type moderationSubjectInput struct {
	SubjectUserID domain.UserID `json:"subject_user_id"`
}
type moderationEscalationApplyInput struct {
	SubjectUserID  domain.UserID                   `json:"subject_user_id"`
	CaseID         domain.CaseID                   `json:"case_id"`
	ReasonCode     domain.ReasonCode               `json:"reason_code"`
	IdempotencyKey domain.IdempotencyKey           `json:"idempotency_key"`
	Recommendation domain.EscalationRecommendation `json:"recommendation"`
	ApprovalID     domain.ApprovalID               `json:"approval_id,omitempty"`
}
type moderationActionInput struct {
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
	ApprovalID           domain.ApprovalID     `json:"approval_id,omitempty"`
}
type lockdownPlanInput struct {
	Scope domain.LockdownScope `json:"scope"`
}
type lockdownApplyInput struct {
	PlanHash   domain.LockdownPlanHash `json:"plan_hash"`
	ApprovalID domain.ApprovalID       `json:"approval_id"`
}
type escalationOutput struct {
	Recommendation domain.EscalationRecommendation `json:"recommendation"`
}
type moderationActionOutput struct {
	Action domain.ModerationAction `json:"action"`
}
type lockdownApplyOutput struct {
	PlanHash  domain.LockdownPlanHash      `json:"plan_hash"`
	Completed []domain.LockdownOperationID `json:"completed"`
	Pending   []domain.LockdownOperationID `json:"pending"`
}
type lockdownPlanOutput struct {
	Plan     domain.LockdownPlan `json:"plan"`
	Recovery domain.LockdownPlan `json:"recovery"`
}

type localModerationTool struct {
	contract application.CommandContract
	handler  string
}

func localModerationTools() []localModerationTool {
	read := func(name, handler, description string, input, result any, capability domain.Capability) localModerationTool {
		return localTool(name, handler, description, input, result, capability, false, domain.DestructiveNone)
	}
	write := func(name, handler, description string, input, result any, capability domain.Capability, destructive domain.DestructiveClass) localModerationTool {
		return localTool(name, handler, description, input, result, capability, true, destructive)
	}
	tools := []localModerationTool{
		read("moderation_case_get", "moderation.case.get", "Get a bounded moderation case", moderationCaseQueryInput{}, domain.Case{}, domain.CapabilityModerationCaseRead),
		read("moderation_case_list", "moderation.case.list", "List bounded moderation cases", moderationCaseQueryInput{}, []domain.Case{}, domain.CapabilityModerationCaseRead),
		read("moderation_appeal_export", "moderation.appeal.export", "Export subject-visible appeal data", moderationSubjectInput{}, domain.AppealExport{}, domain.CapabilityModerationEvidenceExport),
		read("moderation_escalation_evaluate", "moderation.escalation.evaluate", "Evaluate deterministic escalation", moderationSubjectInput{}, escalationOutput{}, domain.CapabilityModerationEscalation),
		read("moderation_lockdown_plan", "moderation.lockdown.plan", "Build reviewed lockdown and recovery plans", lockdownPlanInput{}, lockdownPlanOutput{}, domain.CapabilityModerationLockdownPlan),
		write("moderation_case_create", "moderation.case.create", "Create a moderation case", moderationCaseCreateInput{}, domain.Case{}, domain.CapabilityModerationCaseWrite, domain.DestructiveNone),
		write("moderation_evidence_attach", "moderation.evidence.attach", "Attach a structured evidence locator", moderationEvidenceInput{}, domain.EvidenceReference{}, domain.CapabilityModerationEvidenceWrite, domain.DestructiveNone),
		write("moderation_escalation_apply", "moderation.escalation.apply", "Apply a current escalation recommendation", moderationEscalationApplyInput{}, moderationActionOutput{}, domain.CapabilityModerationWarn, domain.DestructiveKick),
		write("moderation_lockdown_apply", "moderation.lockdown.apply", "Apply an approved lockdown plan", lockdownApplyInput{}, lockdownApplyOutput{}, domain.CapabilityModerationLockdownApply, domain.DestructiveLockdown),
		write("moderation_lockdown_recover", "moderation.lockdown.recover", "Apply exact approved lockdown recovery", lockdownApplyInput{}, lockdownApplyOutput{}, domain.CapabilityModerationLockdownRecover, domain.DestructiveLockdown),
	}
	actions := map[string]struct {
		handler string
		cap     domain.Capability
		class   domain.DestructiveClass
	}{"warn": {"moderation.warn", domain.CapabilityModerationWarn, domain.DestructiveNone}, "timeout": {"moderation.timeout", domain.CapabilityModerationTimeout, domain.DestructiveNone}, "timeout_remove": {"moderation.timeout_remove", domain.CapabilityModerationTimeout, domain.DestructiveNone}, "kick": {"moderation.kick", domain.CapabilityModerationKick, domain.DestructiveKick}, "ban": {"moderation.ban", domain.CapabilityModerationBan, domain.DestructiveBan}, "unban": {"moderation.unban", domain.CapabilityModerationBan, domain.DestructiveNone}, "message_delete": {"moderation.message_delete", domain.CapabilityModerationMessageDelete, domain.DestructiveDelete}, "bulk_message_delete": {"moderation.bulk_message_delete", domain.CapabilityModerationMessageDelete, domain.DestructiveBulkDelete}, "quarantine_add": {"moderation.quarantine_add", domain.CapabilityModerationQuarantine, domain.DestructiveRoleEscalation}, "quarantine_remove": {"moderation.quarantine_remove", domain.CapabilityModerationQuarantine, domain.DestructiveRoleEscalation}, "invite_create": {"moderation.invite_create", domain.CapabilityModerationInviteControl, domain.DestructiveInviteCreate}, "invite_revoke": {"moderation.invite_revoke", domain.CapabilityModerationInviteControl, domain.DestructiveDelete}}
	for name, item := range actions {
		tools = append(tools, write("moderation_"+name, item.handler, "Execute typed moderation "+name, moderationActionInput{}, moderationActionOutput{}, item.cap, item.class))
	}
	return tools
}

func localTool(name, handler, description string, input, result any, capability domain.Capability, mutation bool, destructive domain.DestructiveClass) localModerationTool {
	return localModerationTool{handler: handler, contract: application.CommandContract{Name: name, Toolsets: []application.Toolset{application.ToolsetModeration}, Description: description, InputType: reflect.TypeOf(input), ResultType: reflect.TypeOf(result), Spec: application.CommandSpec{Name: name, RouteTemplate: "local://moderation", Mutation: map[bool]application.MutationClass{true: application.MutationWrite, false: application.MutationRead}[mutation], Retry: application.RetrySafe, Idempotency: application.IdempotencyRequired, Capability: capability, MutationExposed: mutation, DestructiveClass: destructive}}}
}
