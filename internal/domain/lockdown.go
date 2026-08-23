package domain

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

const LockdownPlanSchemaV1 = "discord-control/lockdown-plan-v1"

type LockdownPlanHash string
type LockdownOperationID string
type LockdownPlanKind string

const (
	LockdownApplyPlan    LockdownPlanKind = "apply"
	LockdownRecoveryPlan LockdownPlanKind = "recovery"
	MaxLockdownScope                      = 500
)

type LockdownScope struct {
	ChannelIDs  []ChannelID `json:"channel_ids,omitempty"`
	CategoryIDs []ChannelID `json:"category_ids,omitempty"`
}

type LockdownOverwriteChange struct {
	ChannelID ChannelID            `json:"channel_id"`
	TargetID  Snowflake            `json:"target_id"`
	Type      OverwriteType        `json:"type"`
	Before    *PermissionOverwrite `json:"before,omitempty"`
	After     *PermissionOverwrite `json:"after,omitempty"`
}

type LockdownJournalEventType string

const (
	LockdownPlanPersisted      LockdownJournalEventType = "plan_persisted"
	LockdownOperationStarted   LockdownJournalEventType = "operation_started"
	LockdownOperationCompleted LockdownJournalEventType = "operation_completed"
	LockdownOperationFailed    LockdownJournalEventType = "operation_failed"
)

type LockdownJournalEvent struct {
	Sequence     uint64                   `json:"sequence"`
	PreviousHash string                   `json:"previous_hash,omitempty"`
	RecordHash   string                   `json:"record_hash"`
	GuildID      GuildID                  `json:"guild_id"`
	PlanHash     LockdownPlanHash         `json:"plan_hash"`
	Type         LockdownJournalEventType `json:"type"`
	OperationID  LockdownOperationID      `json:"operation_id,omitempty"`
	OccurredAt   time.Time                `json:"occurred_at"`
	Result       string                   `json:"result,omitempty"`
}

type LockdownOperation struct {
	ID           LockdownOperationID       `json:"id"`
	ChannelID    ChannelID                 `json:"channel_id"`
	Changes      []LockdownOverwriteChange `json:"changes"`
	Dependencies []LockdownOperationID     `json:"dependencies,omitempty"`
}

type LockdownPlan struct {
	SchemaVersion    string              `json:"schema_version"`
	Kind             LockdownPlanKind    `json:"kind"`
	Hash             LockdownPlanHash    `json:"hash"`
	GuildID          GuildID             `json:"guild_id"`
	ActorID          ActorID             `json:"actor_id"`
	SnapshotHash     string              `json:"snapshot_hash"`
	ProtectedRoleIDs []RoleID            `json:"protected_role_ids"`
	Scope            LockdownScope       `json:"scope"`
	Operations       []LockdownOperation `json:"operations"`
	CreatedAt        time.Time           `json:"created_at"`
	ExpiresAt        time.Time           `json:"expires_at"`
	PairedPlanHash   LockdownPlanHash    `json:"paired_plan_hash,omitempty"`
}

func (p LockdownPlan) Validate() error {
	if p.SchemaVersion != LockdownPlanSchemaV1 || (p.Kind != LockdownApplyPlan && p.Kind != LockdownRecoveryPlan) {
		return errors.New("unsupported lockdown plan schema or kind")
	}
	if p.GuildID == "" || p.ActorID == "" || p.SnapshotHash == "" || p.CreatedAt.IsZero() || !p.ExpiresAt.After(p.CreatedAt) {
		return errors.New("lockdown plan bindings and expiry are required")
	}
	if len(p.ProtectedRoleIDs) == 0 || len(p.Scope.ChannelIDs)+len(p.Scope.CategoryIDs) == 0 || len(p.Scope.ChannelIDs)+len(p.Scope.CategoryIDs) > MaxLockdownScope {
		return errors.New("lockdown scope and protected roles must be bounded and non-empty")
	}
	if hasDuplicateSnowflakes(roleSnowflakes(p.ProtectedRoleIDs)) || hasDuplicateSnowflakes(channelSnowflakes(append(append([]ChannelID(nil), p.Scope.ChannelIDs...), p.Scope.CategoryIDs...))) {
		return errors.New("lockdown roles and scope must not contain duplicates")
	}
	operationIDs := make(map[LockdownOperationID]struct{}, len(p.Operations))
	scoped := make(map[ChannelID]struct{}, len(p.Scope.ChannelIDs)+len(p.Scope.CategoryIDs))
	for _, id := range p.Scope.ChannelIDs {
		scoped[id] = struct{}{}
	}
	for _, id := range p.Scope.CategoryIDs {
		scoped[id] = struct{}{}
	}
	for _, operation := range p.Operations {
		if operation.ID == "" || operation.ChannelID == "" || len(operation.Changes) == 0 {
			return errors.New("lockdown operation identity and changes are required")
		}
		if _, ok := scoped[operation.ChannelID]; !ok {
			return fmt.Errorf("lockdown operation %q is outside scope", operation.ID)
		}
		if _, duplicate := operationIDs[operation.ID]; duplicate {
			return fmt.Errorf("duplicate lockdown operation %q", operation.ID)
		}
		operationIDs[operation.ID] = struct{}{}
		for _, change := range operation.Changes {
			if change.ChannelID != operation.ChannelID || change.TargetID == "" || change.Before == nil && change.After == nil {
				return fmt.Errorf("invalid overwrite change in operation %q", operation.ID)
			}
			if change.Before != nil && (change.Before.ID != change.TargetID || change.Before.Type != change.Type) {
				return errors.New("before overwrite binding mismatch")
			}
			if change.After != nil && (change.After.ID != change.TargetID || change.After.Type != change.Type) {
				return errors.New("after overwrite binding mismatch")
			}
		}
	}
	for _, operation := range p.Operations {
		for _, dependency := range operation.Dependencies {
			if dependency == operation.ID {
				return errors.New("lockdown operation cannot depend on itself")
			}
			if _, ok := operationIDs[dependency]; !ok {
				return fmt.Errorf("unknown lockdown dependency %q", dependency)
			}
		}
	}
	return nil
}

func (p LockdownPlan) CanonicalHash() (LockdownPlanHash, error) {
	copyPlan := p
	copyPlan.Hash = ""
	copyPlan.ProtectedRoleIDs = append([]RoleID(nil), p.ProtectedRoleIDs...)
	copyPlan.Scope.ChannelIDs = append([]ChannelID(nil), p.Scope.ChannelIDs...)
	copyPlan.Scope.CategoryIDs = append([]ChannelID(nil), p.Scope.CategoryIDs...)
	copyPlan.Operations = append([]LockdownOperation(nil), p.Operations...)
	for index := range copyPlan.Operations {
		copyPlan.Operations[index].Changes = append([]LockdownOverwriteChange(nil), p.Operations[index].Changes...)
		copyPlan.Operations[index].Dependencies = append([]LockdownOperationID(nil), p.Operations[index].Dependencies...)
		sort.Slice(copyPlan.Operations[index].Dependencies, func(i, j int) bool {
			return copyPlan.Operations[index].Dependencies[i] < copyPlan.Operations[index].Dependencies[j]
		})
	}
	sort.Slice(copyPlan.ProtectedRoleIDs, func(i, j int) bool { return copyPlan.ProtectedRoleIDs[i] < copyPlan.ProtectedRoleIDs[j] })
	sort.Slice(copyPlan.Scope.ChannelIDs, func(i, j int) bool { return copyPlan.Scope.ChannelIDs[i] < copyPlan.Scope.ChannelIDs[j] })
	sort.Slice(copyPlan.Scope.CategoryIDs, func(i, j int) bool { return copyPlan.Scope.CategoryIDs[i] < copyPlan.Scope.CategoryIDs[j] })
	sort.Slice(copyPlan.Operations, func(i, j int) bool { return copyPlan.Operations[i].ID < copyPlan.Operations[j].ID })
	hash, err := canonicalHash(copyPlan)
	return LockdownPlanHash(hash), err
}

func (p LockdownPlan) VerifyHash() error {
	if err := p.Validate(); err != nil {
		return err
	}
	hash, err := p.CanonicalHash()
	if err != nil {
		return err
	}
	if p.Hash == "" || p.Hash != hash {
		return errors.New("lockdown plan hash mismatch")
	}
	return nil
}

func roleSnowflakes(values []RoleID) []Snowflake {
	result := make([]Snowflake, len(values))
	for i, value := range values {
		result[i] = Snowflake(value)
	}
	return result
}

func channelSnowflakes(values []ChannelID) []Snowflake {
	result := make([]Snowflake, len(values))
	for i, value := range values {
		result[i] = Snowflake(value)
	}
	return result
}

func hasDuplicateSnowflakes(values []Snowflake) bool {
	seen := make(map[Snowflake]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return true
		}
		if _, ok := seen[value]; ok {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}
