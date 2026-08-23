package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type CaseID string
type ModerationActionID string
type EvidenceID string
type ReasonCode string
type CaseStatus string
type ActionKind string
type ActionStatus string
type EvidenceKind string

const (
	CaseOpen   CaseStatus = "open"
	CaseClosed CaseStatus = "closed"

	ActionWarn              ActionKind = "warn"
	ActionTimeout           ActionKind = "timeout"
	ActionTimeoutRemove     ActionKind = "timeout_remove"
	ActionKick              ActionKind = "kick"
	ActionBan               ActionKind = "ban"
	ActionUnban             ActionKind = "unban"
	ActionMessageDelete     ActionKind = "message_delete"
	ActionBulkMessageDelete ActionKind = "bulk_message_delete"
	ActionQuarantineAdd     ActionKind = "quarantine_add"
	ActionQuarantineRemove  ActionKind = "quarantine_remove"
	ActionInviteCreate      ActionKind = "invite_create"
	ActionInviteRevoke      ActionKind = "invite_revoke"

	ActionPending        ActionStatus = "pending"
	ActionCompleted      ActionStatus = "completed"
	ActionFailed         ActionStatus = "failed"
	ActionNoop           ActionStatus = "noop"
	ActionOperatorReview ActionStatus = "operator_review"

	EvidenceDiscordMessage    EvidenceKind = "discord_message"
	EvidenceDiscordAuditEvent EvidenceKind = "discord_audit_event"
	EvidenceExternalReference EvidenceKind = "external_reference"
)

const (
	MaxModerationNoteLength    = 1000
	MaxEvidenceReferenceLength = 256
	MaxReasonCodeLength        = 64
	MaxEscalationRules         = 100
	MaxDiscordTimeout          = 28 * 24 * time.Hour
)

type Case struct {
	ID            CaseID             `json:"id"`
	GuildID       GuildID            `json:"guild_id"`
	SubjectUserID UserID             `json:"subject_user_id"`
	ActorID       ActorID            `json:"actor_id"`
	CorrelationID CorrelationID      `json:"correlation_id"`
	ReasonCode    ReasonCode         `json:"reason_code"`
	Status        CaseStatus         `json:"status"`
	Notes         string             `json:"notes,omitempty"`
	EvidenceIDs   []EvidenceID       `json:"evidence_ids,omitempty"`
	Actions       []ModerationAction `json:"actions,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
	ClosedAt      *time.Time         `json:"closed_at,omitempty"`
}

type ModerationAction struct {
	ID             ModerationActionID `json:"id"`
	CaseID         CaseID             `json:"case_id"`
	GuildID        GuildID            `json:"guild_id"`
	SubjectUserID  UserID             `json:"subject_user_id"`
	ActorID        ActorID            `json:"actor_id"`
	CorrelationID  CorrelationID      `json:"correlation_id"`
	IdempotencyKey IdempotencyKey     `json:"idempotency_key"`
	ReasonCode     ReasonCode         `json:"reason_code"`
	Kind           ActionKind         `json:"kind"`
	Status         ActionStatus       `json:"status"`
	EvidenceIDs    []EvidenceID       `json:"evidence_ids,omitempty"`
	Notes          string             `json:"notes,omitempty"`
	ExpiresAt      *time.Time         `json:"expires_at,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
	CompletedAt    *time.Time         `json:"completed_at,omitempty"`
	Result         string             `json:"result,omitempty"`
}

type EvidenceReference struct {
	ID             EvidenceID    `json:"id"`
	CaseID         CaseID        `json:"case_id"`
	GuildID        GuildID       `json:"guild_id"`
	SubjectUserID  UserID        `json:"subject_user_id"`
	ActorID        ActorID       `json:"actor_id"`
	CorrelationID  CorrelationID `json:"correlation_id"`
	Kind           EvidenceKind  `json:"kind"`
	ChannelID      ChannelID     `json:"channel_id,omitempty"`
	MessageID      MessageID     `json:"message_id,omitempty"`
	AuditEventID   Snowflake     `json:"audit_event_id,omitempty"`
	ExternalSystem string        `json:"external_system,omitempty"`
	ReferenceID    string        `json:"reference_id,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	ExpiresAt      *time.Time    `json:"expires_at,omitempty"`
}

type AppealExport struct {
	SchemaVersion string       `json:"schema_version"`
	GuildID       GuildID      `json:"guild_id"`
	SubjectUserID UserID       `json:"subject_user_id"`
	GeneratedAt   time.Time    `json:"generated_at"`
	Cases         []AppealCase `json:"cases"`
}

type AppealCase struct {
	ID         CaseID              `json:"id"`
	ReasonCode ReasonCode          `json:"reason_code"`
	Status     CaseStatus          `json:"status"`
	CreatedAt  time.Time           `json:"created_at"`
	ClosedAt   *time.Time          `json:"closed_at,omitempty"`
	Actions    []AppealAction      `json:"actions"`
	Evidence   []EvidenceReference `json:"evidence,omitempty"`
}

type ModerationTombstone struct {
	CaseID         CaseID             `json:"case_id"`
	ActionID       ModerationActionID `json:"action_id,omitempty"`
	ActionKind     ActionKind         `json:"action_kind,omitempty"`
	IdempotencyKey IdempotencyKey     `json:"idempotency_key,omitempty"`
	IntentHash     string             `json:"intent_hash,omitempty"`
	Result         string             `json:"result"`
	CorrelationID  CorrelationID      `json:"correlation_id"`
	RetainedAt     time.Time          `json:"retained_at"`
}

type ModerationJournalEventType string

const (
	ModerationCaseUpsert         ModerationJournalEventType = "case_upsert"
	ModerationEvidenceAttach     ModerationJournalEventType = "evidence_attach"
	ModerationActionAppend       ModerationJournalEventType = "action_append"
	ModerationRetentionTombstone ModerationJournalEventType = "retention_tombstone"
)

type ModerationJournalEvent struct {
	Sequence     uint64                     `json:"sequence"`
	PreviousHash string                     `json:"previous_hash,omitempty"`
	RecordHash   string                     `json:"record_hash"`
	GuildID      GuildID                    `json:"guild_id"`
	Type         ModerationJournalEventType `json:"type"`
	OccurredAt   time.Time                  `json:"occurred_at"`
	Case         *Case                      `json:"case,omitempty"`
	Evidence     *EvidenceReference         `json:"evidence,omitempty"`
	Action       *ModerationAction          `json:"action,omitempty"`
	Tombstone    *ModerationTombstone       `json:"tombstone,omitempty"`
}

type AppealAction struct {
	Kind       ActionKind   `json:"kind"`
	Status     ActionStatus `json:"status"`
	ReasonCode ReasonCode   `json:"reason_code"`
	CreatedAt  time.Time    `json:"created_at"`
	ExpiresAt  *time.Time   `json:"expires_at,omitempty"`
}

func (e EvidenceReference) Validate() error {
	if e.ID == "" || e.CaseID == "" || e.GuildID == "" || e.SubjectUserID == "" || e.ActorID == "" || e.CorrelationID == "" || e.CreatedAt.IsZero() {
		return errors.New("evidence binding fields are required")
	}
	switch e.Kind {
	case EvidenceDiscordMessage:
		if e.ChannelID == "" || e.MessageID == "" || e.AuditEventID != "" || e.ExternalSystem != "" || e.ReferenceID != "" {
			return errors.New("discord message evidence requires only channel_id and message_id")
		}
	case EvidenceDiscordAuditEvent:
		if e.AuditEventID == "" || e.ChannelID != "" || e.MessageID != "" || e.ExternalSystem != "" || e.ReferenceID != "" {
			return errors.New("discord audit evidence requires only audit_event_id")
		}
	case EvidenceExternalReference:
		if !validOpaque(e.ExternalSystem, 64) || !validOpaque(e.ReferenceID, MaxEvidenceReferenceLength) || e.ChannelID != "" || e.MessageID != "" || e.AuditEventID != "" {
			return errors.New("external evidence requires bounded opaque system and reference identifiers")
		}
	default:
		return errors.New("unsupported evidence kind")
	}
	return nil
}

func validOpaque(value string, maximum int) bool {
	if value == "" || len(value) > maximum || strings.ContainsAny(value, ":/?#\\\x00\r\n\t") {
		return false
	}
	for _, r := range value {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

func (c Case) Validate() error {
	if c.ID == "" || c.GuildID == "" || c.SubjectUserID == "" || c.ActorID == "" || c.CorrelationID == "" || c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() {
		return errors.New("case binding fields are required")
	}
	if !ValidReasonCode(c.ReasonCode) || len(c.Notes) > MaxModerationNoteLength {
		return errors.New("case reason or notes are invalid")
	}
	if c.Status != CaseOpen && c.Status != CaseClosed {
		return errors.New("unsupported case status")
	}
	if (c.Status == CaseClosed) != (c.ClosedAt != nil) {
		return errors.New("closed cases require closed_at and open cases forbid it")
	}
	return nil
}

func (a ModerationAction) Validate() error {
	if a.ID == "" || a.CaseID == "" || a.GuildID == "" || a.SubjectUserID == "" || a.ActorID == "" || a.CorrelationID == "" || a.IdempotencyKey == "" || a.CreatedAt.IsZero() {
		return errors.New("action binding fields are required")
	}
	if !ValidReasonCode(a.ReasonCode) || len(a.Notes) > MaxModerationNoteLength || !ValidActionKind(a.Kind) || !ValidActionStatus(a.Status) {
		return errors.New("action kind, status, reason, or notes are invalid")
	}
	if a.Kind == ActionTimeout && (a.ExpiresAt == nil || !a.ExpiresAt.After(a.CreatedAt) || a.ExpiresAt.Sub(a.CreatedAt) > MaxDiscordTimeout) {
		return errors.New("timeout expiry must be within Discord's 28-day limit")
	}
	return nil
}

func ValidReasonCode(code ReasonCode) bool {
	if code == "" || len(code) > MaxReasonCodeLength {
		return false
	}
	for _, r := range code {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

func ValidActionKind(kind ActionKind) bool {
	switch kind {
	case ActionWarn, ActionTimeout, ActionTimeoutRemove, ActionKick, ActionBan, ActionUnban, ActionMessageDelete, ActionBulkMessageDelete, ActionQuarantineAdd, ActionQuarantineRemove, ActionInviteCreate, ActionInviteRevoke:
		return true
	default:
		return false
	}
}

func ValidActionStatus(status ActionStatus) bool {
	return status == ActionPending || status == ActionCompleted || status == ActionFailed || status == ActionNoop || status == ActionOperatorReview
}

type EscalationPolicy struct {
	Version                    string           `json:"version"`
	MaxActionsPerSubjectWindow int              `json:"max_actions_per_subject_window"`
	MaxActionsPerGuildWindow   int              `json:"max_actions_per_guild_window"`
	Rules                      []EscalationRule `json:"rules"`
}

type EscalationRule struct {
	ID                  string        `json:"id"`
	Priority            int           `json:"priority"`
	EligiblePriorKinds  []ActionKind  `json:"eligible_prior_kinds"`
	Lookback            time.Duration `json:"lookback"`
	MinimumCount        int           `json:"minimum_count"`
	Action              ActionKind    `json:"action"`
	Duration            time.Duration `json:"duration,omitempty"`
	DeleteWindow        time.Duration `json:"delete_window,omitempty"`
	MaximumApplications int           `json:"maximum_applications"`
}

type EscalationRecommendation struct {
	PolicyVersion string        `json:"policy_version"`
	PolicyHash    string        `json:"policy_hash"`
	RuleID        string        `json:"rule_id,omitempty"`
	Action        ActionKind    `json:"action,omitempty"`
	Duration      time.Duration `json:"duration,omitempty"`
	DeleteWindow  time.Duration `json:"delete_window,omitempty"`
	Reason        string        `json:"reason"`
	HistoryHash   string        `json:"history_hash"`
	EvaluatedAt   time.Time     `json:"evaluated_at"`
}

func (p EscalationPolicy) Validate() error {
	if !validOpaque(p.Version, 64) || len(p.Rules) > MaxEscalationRules || p.MaxActionsPerSubjectWindow < 0 || p.MaxActionsPerGuildWindow < 0 {
		return errors.New("invalid escalation policy bounds")
	}
	ids := make(map[string]struct{}, len(p.Rules))
	for _, rule := range p.Rules {
		if !validOpaque(rule.ID, 64) || rule.Lookback <= 0 || rule.MinimumCount <= 0 || rule.MaximumApplications <= 0 || !ValidActionKind(rule.Action) {
			return fmt.Errorf("invalid escalation rule %q", rule.ID)
		}
		if rule.Duration < 0 || rule.Duration > MaxDiscordTimeout || rule.DeleteWindow < 0 || rule.DeleteWindow > 14*24*time.Hour {
			return fmt.Errorf("invalid escalation duration for %q", rule.ID)
		}
		if _, duplicate := ids[rule.ID]; duplicate {
			return fmt.Errorf("duplicate escalation rule %q", rule.ID)
		}
		ids[rule.ID] = struct{}{}
		for _, kind := range rule.EligiblePriorKinds {
			if !ValidActionKind(kind) {
				return fmt.Errorf("invalid prior action kind in %q", rule.ID)
			}
		}
	}
	return nil
}

func EvaluateEscalation(policy EscalationPolicy, history []ModerationAction, now time.Time) (EscalationRecommendation, error) {
	if err := policy.Validate(); err != nil {
		return EscalationRecommendation{}, err
	}
	if now.IsZero() {
		return EscalationRecommendation{}, errors.New("evaluation time is required")
	}
	policyHash, err := canonicalHash(policy)
	if err != nil {
		return EscalationRecommendation{}, err
	}
	ordered := append([]ModerationAction(nil), history...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
	})
	historyHash, err := canonicalHash(ordered)
	if err != nil {
		return EscalationRecommendation{}, err
	}
	result := EscalationRecommendation{PolicyVersion: policy.Version, PolicyHash: policyHash, HistoryHash: historyHash, EvaluatedAt: now.UTC(), Reason: "no_rule_matched"}
	rules := append([]EscalationRule(nil), policy.Rules...)
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Priority == rules[j].Priority {
			return rules[i].ID < rules[j].ID
		}
		return rules[i].Priority < rules[j].Priority
	})
	for _, rule := range rules {
		eligible := make(map[ActionKind]struct{}, len(rule.EligiblePriorKinds))
		for _, kind := range rule.EligiblePriorKinds {
			eligible[kind] = struct{}{}
		}
		count, applications := 0, 0
		cutoff := now.Add(-rule.Lookback)
		for _, action := range ordered {
			if action.CreatedAt.Before(cutoff) || action.CreatedAt.After(now) || (action.Status != ActionCompleted && action.Status != ActionNoop) {
				continue
			}
			if _, ok := eligible[action.Kind]; ok {
				count++
			}
			if action.Kind == rule.Action {
				applications++
			}
		}
		if count >= rule.MinimumCount && applications < rule.MaximumApplications {
			result.RuleID, result.Action, result.Duration, result.DeleteWindow, result.Reason = rule.ID, rule.Action, rule.Duration, rule.DeleteWindow, "rule_matched"
			break
		}
	}
	return result, nil
}

func canonicalHash(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
