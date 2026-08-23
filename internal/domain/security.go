package domain

import (
	"fmt"
	"time"
)

// Principal is authentication evidence supplied by an adapter. It never carries grants.
type Principal struct {
	Adapter              Adapter
	Subject              string
	AuthenticationMethod string
}

type ActorID string
type Capability string
type ExecutionMode string
type DestructiveClass string
type ResourceKind string
type PolicyReason string
type ApprovalID string
type ApprovalScopeKind string
type AuditEventKind string
type SecurityEventKind string

const (
	ModeReadOnly ExecutionMode = "read_only"
	ModeDryRun   ExecutionMode = "dry_run"
	ModeApply    ExecutionMode = "apply"
)

const (
	CapabilityGuildInspect             Capability = "guild.inspect"
	CapabilityChannelRead              Capability = "channel.read"
	CapabilityChannelManage            Capability = "channel.manage"
	CapabilityRoleRead                 Capability = "role.read"
	CapabilityRoleManage               Capability = "role.manage"
	CapabilityMemberRead               Capability = "member.read"
	CapabilityMemberModerate           Capability = "member.moderate"
	CapabilityMessageRead              Capability = "message.read"
	CapabilityMessageManage            Capability = "message.manage"
	CapabilityWebhookRead              Capability = "webhook.read"
	CapabilityWebhookManage            Capability = "webhook.manage"
	CapabilityInviteRead               Capability = "invite.read"
	CapabilityInviteManage             Capability = "invite.manage"
	CapabilityApplicationCommandRead   Capability = "application_command.read"
	CapabilityApplicationCommandManage Capability = "application_command.manage"
	CapabilityOnboardingRead           Capability = "onboarding.read"
	CapabilityOnboardingManage         Capability = "onboarding.manage"
	CapabilityScheduledEventRead       Capability = "scheduled_event.read"
	CapabilityScheduledEventManage     Capability = "scheduled_event.manage"
	CapabilityAutoModerationRead       Capability = "auto_moderation.read"
	CapabilityAutoModerationManage     Capability = "auto_moderation.manage"
	CapabilityJobRead                  Capability = "job.read"
	CapabilityJobCancel                Capability = "job.cancel"
	CapabilityJobResume                Capability = "job.resume"
)

const (
	DestructiveNone             DestructiveClass = "none"
	DestructiveDelete           DestructiveClass = "delete"
	DestructiveBulkDelete       DestructiveClass = "bulk_delete"
	DestructiveBan              DestructiveClass = "ban"
	DestructiveKick             DestructiveClass = "kick"
	DestructiveRoleEscalation   DestructiveClass = "role_escalation"
	DestructivePermissionChange DestructiveClass = "permission_change"
	DestructiveWebhookCreate    DestructiveClass = "webhook_create"
	DestructiveInviteCreate     DestructiveClass = "invite_create"
	DestructiveBulkOverwrite    DestructiveClass = "bulk_overwrite"
	DestructiveLockdown         DestructiveClass = "lockdown"
)

const (
	ResourceGuild               ResourceKind = "guild"
	ResourceChannel             ResourceKind = "channel"
	ResourceRole                ResourceKind = "role"
	ResourceMember              ResourceKind = "member"
	ResourceMessage             ResourceKind = "message"
	ResourceWebhook             ResourceKind = "webhook"
	ResourceInvite              ResourceKind = "invite"
	ResourceApplication         ResourceKind = "application"
	ResourceApplicationCommand  ResourceKind = "application_command"
	ResourceOnboarding          ResourceKind = "onboarding"
	ResourceCategory            ResourceKind = "category"
	ResourcePermissionOverwrite ResourceKind = "permission_overwrite"
	ResourceScheduledEvent      ResourceKind = "scheduled_event"
)

const (
	PolicyAllowed            PolicyReason = "allowed"
	PolicyUnknownPrincipal   PolicyReason = "unknown_principal"
	PolicyAmbiguousPrincipal PolicyReason = "ambiguous_principal"
	PolicyMalformedPrincipal PolicyReason = "malformed_principal"
	PolicyGuildDenied        PolicyReason = "guild_denied"
	PolicyCapabilityDenied   PolicyReason = "capability_denied"
	PolicyModeDenied         PolicyReason = "mode_denied"
	PolicyDestructiveDenied  PolicyReason = "destructive_denied"
	PolicyResourceDenied     PolicyReason = "resource_denied"
	PolicyUnknownCommand     PolicyReason = "unknown_command"
	PolicyError              PolicyReason = "policy_error"
	PolicyApprovalRequired   PolicyReason = "approval_required"
	PolicyApprovalInvalid    PolicyReason = "approval_invalid"
	PolicyAuditUnavailable   PolicyReason = "audit_unavailable"
)

type ResourceRef struct {
	Kind    ResourceKind
	ID      string
	GuildID GuildID
	Owned   bool
}

type PolicyRequest struct {
	Principal        Principal
	ActorID          ActorID
	GuildID          GuildID
	Operation        OperationName
	Capability       Capability
	Resources        []ResourceRef
	Mode             ExecutionMode
	DestructiveClass DestructiveClass
	CorrelationID    CorrelationID
}

type PolicyDecision struct {
	Allowed       bool
	ActorID       ActorID
	EffectiveMode ExecutionMode
	Reason        PolicyReason
	Preapproved   bool
}

const (
	ApprovalScopeOperation          ApprovalScopeKind = "operation"
	ApprovalScopeReconciliationPlan ApprovalScopeKind = "reconciliation_plan"
)

type ApprovalGrant struct {
	ID         ApprovalID
	ActorID    ActorID
	GuildID    GuildID
	ScopeKind  ApprovalScopeKind
	ScopeHash  string
	ApproverID ActorID
	IssuedAt   time.Time
	ExpiresAt  time.Time
	ConsumedAt time.Time
}

type AuditEvent struct {
	Kind                AuditEventKind
	Timestamp           time.Time
	ActorID             ActorID
	ApproverID          ActorID
	Adapter             Adapter
	GuildID             GuildID
	Operation           OperationName
	Capability          Capability
	ResourceKinds       []ResourceKind
	Intent              map[string]string
	Decision            bool
	Reason              PolicyReason
	ApprovalID          ApprovalID
	Mode                ExecutionMode
	DestructiveClass    DestructiveClass
	Result              string
	CorrelationID       CorrelationID
	ParentCorrelationID CorrelationID
	AuditReason         string
	OperationHash       string
}

type SecurityEvent struct {
	Kind             SecurityEventKind
	Reason           PolicyReason
	Mode             ExecutionMode
	DestructiveClass DestructiveClass
	CorrelationID    CorrelationID
	OccurredAt       time.Time
}

type AuthorizationError struct{ Reason PolicyReason }

func (e *AuthorizationError) Error() string {
	return fmt.Sprintf("Discord authorization denied: %s", e.Reason)
}

type ApprovalError struct{ Reason string }

func (e *ApprovalError) Error() string { return "Discord approval denied: " + e.Reason }

type AuditError struct{ Stage string }

func (e *AuditError) Error() string { return "Discord audit append failed at " + e.Stage }

type OperationPlan struct {
	GuildID          GuildID           `json:"guildId"`
	Operation        OperationName     `json:"operation"`
	Capability       Capability        `json:"capability"`
	Mode             ExecutionMode     `json:"mode"`
	DestructiveClass DestructiveClass  `json:"destructiveClass"`
	ResourceKinds    []ResourceKind    `json:"resourceKinds"`
	Intent           map[string]string `json:"intent"`
	OperationHash    string            `json:"operationHash"`
	Preapproved      bool              `json:"preapproved,omitempty"`
}

func ValidExecutionMode(mode ExecutionMode) bool {
	return mode == ModeReadOnly || mode == ModeDryRun || mode == ModeApply
}

func ValidCapability(capability Capability) bool {
	switch capability {
	case CapabilityGuildInspect, CapabilityChannelRead, CapabilityChannelManage, CapabilityRoleRead, CapabilityRoleManage,
		CapabilityMemberRead, CapabilityMemberModerate, CapabilityMessageRead, CapabilityMessageManage, CapabilityWebhookRead,
		CapabilityWebhookManage, CapabilityInviteRead, CapabilityInviteManage, CapabilityApplicationCommandRead,
		CapabilityApplicationCommandManage, CapabilityOnboardingRead, CapabilityOnboardingManage,
		CapabilityScheduledEventRead, CapabilityScheduledEventManage, CapabilityAutoModerationRead, CapabilityAutoModerationManage,
		CapabilityJobRead, CapabilityJobCancel, CapabilityJobResume:
		return true
	default:
		return false
	}
}

func ValidDestructiveClass(class DestructiveClass) bool {
	switch class {
	case DestructiveNone, DestructiveDelete, DestructiveBulkDelete, DestructiveBan, DestructiveKick,
		DestructiveRoleEscalation, DestructivePermissionChange, DestructiveWebhookCreate,
		DestructiveInviteCreate, DestructiveBulkOverwrite, DestructiveLockdown:
		return true
	default:
		return false
	}
}

func RequiresApproval(class DestructiveClass) bool { return class != "" && class != DestructiveNone }
