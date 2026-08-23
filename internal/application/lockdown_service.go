package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const lockdownDenyPermissions uint64 = 1<<0 | 1<<6 | 1<<11 | 1<<20 | 1<<21 | 1<<35 | 1<<36 | 1<<38
const lockdownViewPermission uint64 = 1 << 10
const lockdownManageChannels uint64 = 1 << 4
const lockdownAdministrator uint64 = 1 << 3

type LockdownSettings struct {
	ProtectedRoleIDs         []domain.RoleID
	ProtectedChannelIDs      map[domain.ChannelID]struct{}
	MaxOperations            int
	MaxDestructiveOperations int
	PlanTTL                  time.Duration
}

type LockdownService struct {
	discord   *DiscordService
	store     LockdownStore
	settings  LockdownSettings
	now       func() time.Time
	telemetry LockdownTelemetry
}

type BuildLockdownRequest struct {
	Invocation Invocation
	GuildID    domain.GuildID
	Scope      domain.LockdownScope
}
type ApplyLockdownRequest struct {
	Invocation Invocation
	GuildID    domain.GuildID
	PlanHash   domain.LockdownPlanHash
	ApprovalID domain.ApprovalID
}
type LockdownApplyResult struct {
	PlanHash  domain.LockdownPlanHash      `json:"plan_hash"`
	Completed []domain.LockdownOperationID `json:"completed"`
	Pending   []domain.LockdownOperationID `json:"pending"`
	Plan      *domain.OperationPlan        `json:"plan,omitempty"`
}

func NewLockdownService(discord *DiscordService, store LockdownStore, settings LockdownSettings, telemetry ...LockdownTelemetry) (*LockdownService, error) {
	if discord == nil || store == nil || len(settings.ProtectedRoleIDs) == 0 || settings.MaxOperations < 1 || settings.MaxDestructiveOperations < 1 || settings.MaxDestructiveOperations > settings.MaxOperations || settings.PlanTTL < time.Minute || settings.PlanTTL > time.Hour {
		return nil, errors.New("lockdown dependencies, protected roles, caps, and TTL are required")
	}
	if settings.ProtectedChannelIDs == nil {
		settings.ProtectedChannelIDs = map[domain.ChannelID]struct{}{}
	}
	var observer LockdownTelemetry = noopModerationTelemetry{}
	if len(telemetry) > 0 && telemetry[0] != nil {
		observer = telemetry[0]
	}
	return &LockdownService{discord: discord, store: store, settings: settings, now: time.Now, telemetry: observer}, nil
}

func (s *LockdownService) BuildLockdownPlan(ctx context.Context, request BuildLockdownRequest) (domain.LockdownPlan, domain.LockdownPlan, error) {
	if request.GuildID == "" || len(request.Scope.ChannelIDs)+len(request.Scope.CategoryIDs) == 0 || len(request.Scope.ChannelIDs)+len(request.Scope.CategoryIDs) > domain.MaxLockdownScope {
		return domain.LockdownPlan{}, domain.LockdownPlan{}, &domain.ValidationError{Field: "scope", Rule: "explicit bounded scope is required"}
	}
	decision, err := s.discord.policy.Decide(ctx, domain.PolicyRequest{Principal: request.Invocation.Principal, ActorID: request.Invocation.ActorID, GuildID: request.GuildID, Operation: "moderation_lockdown_plan", Capability: domain.CapabilityModerationLockdownPlan, Resources: []domain.ResourceRef{{Kind: domain.ResourceLockdownPlan, ID: string(request.GuildID)}}, Mode: request.Invocation.RequestedMode, DestructiveClass: domain.DestructiveNone, CorrelationID: request.Invocation.CorrelationID})
	if err != nil || !decision.Allowed {
		return domain.LockdownPlan{}, domain.LockdownPlan{}, &domain.AuthorizationError{Reason: domain.PolicyCapabilityDenied}
	}
	invocation := request.Invocation
	invocation.RequestedMode = domain.ModeApply
	inspectionExecution, err := dispatchAs[domain.GuildInspection](ctx, s.discord, invocation, domain.InspectGuild{CommandContext: domain.CommandContext{GuildID: request.GuildID}}, domain.CapabilityModerationLockdownPlan)
	if err != nil {
		return domain.LockdownPlan{}, domain.LockdownPlan{}, err
	}
	currentUserExecution, err := dispatchAs[domain.User](ctx, s.discord, invocation, domain.GetCurrentUser{CommandContext: domain.CommandContext{GuildID: request.GuildID}}, domain.CapabilityModerationLockdownPlan)
	if err != nil || currentUserExecution.Result.ID == "" {
		return domain.LockdownPlan{}, domain.LockdownPlan{}, errors.New("current bot identity is unavailable")
	}
	botExecution, err := dispatchAs[domain.Member](ctx, s.discord, invocation, domain.GetMember{CommandContext: domain.CommandContext{GuildID: request.GuildID}, UserID: currentUserExecution.Result.ID}, domain.CapabilityModerationLockdownPlan)
	if err != nil {
		return domain.LockdownPlan{}, domain.LockdownPlan{}, err
	}
	rolesExecution, err := dispatchAs[[]domain.Role](ctx, s.discord, invocation, domain.ListGuildRoles{CommandContext: domain.CommandContext{GuildID: request.GuildID}}, domain.CapabilityModerationLockdownPlan)
	if err != nil {
		return domain.LockdownPlan{}, domain.LockdownPlan{}, err
	}
	channels, roles := inspectionExecution.Result.Channels, rolesExecution.Result
	if err := validateProtectedRoles(s.settings.ProtectedRoleIDs, roles, botExecution.Result); err != nil {
		return domain.LockdownPlan{}, domain.LockdownPlan{}, err
	}
	operations, err := s.lockdownOperations(request.GuildID, request.Scope, channels)
	if err != nil {
		return domain.LockdownPlan{}, domain.LockdownPlan{}, err
	}
	if len(operations) == 0 || len(operations) > s.settings.MaxOperations || len(operations) > s.settings.MaxDestructiveOperations {
		return domain.LockdownPlan{}, domain.LockdownPlan{}, &domain.ConflictError{Reason: "lockdown operation cap exceeded"}
	}
	now := s.now().UTC()
	snapshotHash, err := lockdownSnapshotHash(inspectionExecution.Result.Guild, channels, roles, botExecution.Result)
	if err != nil {
		return domain.LockdownPlan{}, domain.LockdownPlan{}, err
	}
	recoveryOperations := make([]domain.LockdownOperation, len(operations))
	for i, operation := range operations {
		recoveryOperations[i] = operation
		recoveryOperations[i].Changes = append([]domain.LockdownOverwriteChange(nil), operation.Changes...)
		recoveryOperations[i].Changes[0].Before, recoveryOperations[i].Changes[0].After = operation.Changes[0].After, operation.Changes[0].Before
	}
	recovery := domain.LockdownPlan{SchemaVersion: domain.LockdownPlanSchemaV1, Kind: domain.LockdownRecoveryPlan, GuildID: request.GuildID, ActorID: decision.ActorID, SnapshotHash: snapshotHash, ProtectedRoleIDs: append([]domain.RoleID(nil), s.settings.ProtectedRoleIDs...), Scope: request.Scope, Operations: recoveryOperations, CreatedAt: now, ExpiresAt: now.Add(s.settings.PlanTTL)}
	recovery.Hash, err = recovery.CanonicalHash()
	if err != nil {
		return domain.LockdownPlan{}, domain.LockdownPlan{}, err
	}
	plan := domain.LockdownPlan{SchemaVersion: domain.LockdownPlanSchemaV1, Kind: domain.LockdownApplyPlan, GuildID: request.GuildID, ActorID: decision.ActorID, SnapshotHash: snapshotHash, ProtectedRoleIDs: append([]domain.RoleID(nil), s.settings.ProtectedRoleIDs...), Scope: request.Scope, Operations: operations, CreatedAt: now, ExpiresAt: now.Add(s.settings.PlanTTL), PairedPlanHash: recovery.Hash}
	plan.Hash, err = plan.CanonicalHash()
	if err != nil {
		return domain.LockdownPlan{}, domain.LockdownPlan{}, err
	}
	if err := s.store.SavePlan(recovery); err != nil {
		return domain.LockdownPlan{}, domain.LockdownPlan{}, err
	}
	if err := s.store.SavePlan(plan); err != nil {
		return domain.LockdownPlan{}, domain.LockdownPlan{}, err
	}
	_, err = s.store.Append(request.GuildID, plan.Hash, domain.LockdownJournalEvent{Type: domain.LockdownPlanPersisted, OccurredAt: now})
	if err == nil {
		s.telemetry.ObserveLockdown(ctx, LockdownObservation{Operation: "plan", Outcome: "success", OperationCount: len(plan.Operations)})
	}
	return plan, recovery, err
}

func (s *LockdownService) ApplyLockdown(ctx context.Context, request ApplyLockdownRequest) (LockdownApplyResult, error) {
	return s.apply(ctx, request, domain.LockdownApplyPlan, domain.CapabilityModerationLockdownApply)
}
func (s *LockdownService) RecoverLockdown(ctx context.Context, request ApplyLockdownRequest) (LockdownApplyResult, error) {
	return s.apply(ctx, request, domain.LockdownRecoveryPlan, domain.CapabilityModerationLockdownRecover)
}

func (s *LockdownService) apply(ctx context.Context, request ApplyLockdownRequest, kind domain.LockdownPlanKind, capability domain.Capability) (LockdownApplyResult, error) {
	started := s.now()
	plan, err := s.store.LoadPlan(request.GuildID, request.PlanHash)
	if err != nil {
		return LockdownApplyResult{}, err
	}
	if plan.Kind != kind || plan.GuildID != request.GuildID || plan.ExpiresAt.Before(s.now()) {
		return LockdownApplyResult{}, &domain.ConflictError{Reason: "lockdown plan binding, kind, or expiry mismatch"}
	}
	decision, err := s.discord.policy.Decide(ctx, domain.PolicyRequest{Principal: request.Invocation.Principal, ActorID: request.Invocation.ActorID, GuildID: request.GuildID, Operation: "moderation_lockdown_" + domain.OperationName(kind), Capability: capability, Resources: []domain.ResourceRef{{Kind: domain.ResourceLockdownPlan, ID: string(plan.Hash)}}, Mode: request.Invocation.RequestedMode, DestructiveClass: domain.DestructiveLockdown, CorrelationID: request.Invocation.CorrelationID})
	if err != nil || !decision.Allowed {
		return LockdownApplyResult{}, &domain.AuthorizationError{Reason: domain.PolicyCapabilityDenied}
	}
	if decision.ActorID != plan.ActorID {
		return LockdownApplyResult{}, &domain.AuthorizationError{Reason: domain.PolicyResourceDenied}
	}
	if decision.EffectiveMode == domain.ModeDryRun {
		s.telemetry.ObserveLockdown(ctx, LockdownObservation{Operation: string(kind), Outcome: "dry_run", OperationCount: len(plan.Operations), Latency: s.now().Sub(started)})
		return LockdownApplyResult{PlanHash: plan.Hash, Plan: &domain.OperationPlan{GuildID: plan.GuildID, Operation: domain.OperationName("moderation_lockdown_" + string(kind)), Capability: capability, Mode: domain.ModeDryRun, DestructiveClass: domain.DestructiveLockdown, ResourceKinds: []domain.ResourceKind{domain.ResourceLockdownPlan}, Intent: map[string]string{"plan_hash": string(plan.Hash), "operation_count": fmt.Sprint(len(plan.Operations))}, OperationHash: string(plan.Hash)}}, nil
	}
	if decision.EffectiveMode != domain.ModeApply {
		return LockdownApplyResult{}, &domain.AuthorizationError{Reason: domain.PolicyModeDenied}
	}
	events, err := s.store.Replay(plan.GuildID, plan.Hash)
	if err != nil {
		return LockdownApplyResult{}, err
	}
	completed := completedLockdownOperations(events)
	if len(completed) == 0 {
		if s.discord.approvals == nil || request.ApprovalID == "" {
			return LockdownApplyResult{}, &domain.AuthorizationError{Reason: domain.PolicyApprovalRequired}
		}
		if _, err := s.discord.approvals.ConsumeLockdownPlan(ctx, request.ApprovalID, plan.ActorID, plan.GuildID, string(plan.Hash), s.now()); err != nil {
			return LockdownApplyResult{}, &domain.AuthorizationError{Reason: domain.PolicyApprovalInvalid}
		}
	}
	invocation := request.Invocation
	invocation.RequestedMode, invocation.ActorID, invocation.ApprovalID = domain.ModeApply, plan.ActorID, ""
	invocation.lockdown = newLockdownAuthorization(plan.ActorID, plan.GuildID, plan.Hash, plan.Operations)
	result := LockdownApplyResult{PlanHash: plan.Hash, Completed: []domain.LockdownOperationID{}, Pending: []domain.LockdownOperationID{}}
	for _, operation := range plan.Operations {
		if _, done := completed[operation.ID]; done {
			result.Completed = append(result.Completed, operation.ID)
			continue
		}
		if len(operation.Changes) != 1 {
			return result, &domain.InvariantError{Detail: "lockdown operation must contain one overwrite change"}
		}
		change := operation.Changes[0]
		current, err := s.currentOverwrite(ctx, invocation, plan.GuildID, change.ChannelID, change.TargetID, capability)
		if err != nil {
			return result, err
		}
		if !equalOverwrite(current, change.Before) {
			result.Pending = pendingLockdownOperations(plan.Operations, completed)
			return result, &domain.ConflictError{Reason: "lockdown overwrite drift"}
		}
		_, err = s.store.Append(plan.GuildID, plan.Hash, domain.LockdownJournalEvent{Type: domain.LockdownOperationStarted, OperationID: operation.ID, OccurredAt: s.now().UTC()})
		if err != nil {
			return result, err
		}
		base := domain.CommandContext{GuildID: plan.GuildID, IdempotencyKey: domain.IdempotencyKey(operation.ID), ReconciliationKey: domain.ReconciliationKey(plan.Hash)}
		var execution untypedExecution
		if change.After == nil {
			execution, err = s.discord.dispatchDiscordWithCapability(ctx, invocation, domain.DeleteChannelPermission{CommandContext: base, ChannelID: change.ChannelID, OverwriteID: change.TargetID}, capability)
		} else {
			execution, err = s.discord.dispatchDiscordWithCapability(ctx, invocation, domain.SetChannelPermission{CommandContext: base, ChannelID: change.ChannelID, Overwrite: *change.After}, capability)
		}
		if err != nil {
			status := "failed"
			if execution.Partial != nil {
				status = "operator_review"
			}
			_, _ = s.store.Append(plan.GuildID, plan.Hash, domain.LockdownJournalEvent{Type: domain.LockdownOperationFailed, OperationID: operation.ID, OccurredAt: s.now().UTC(), Result: status})
			result.Pending = pendingLockdownOperations(plan.Operations, completed)
			s.telemetry.ObserveLockdown(ctx, LockdownObservation{Operation: string(kind), Outcome: "partial_failure", OperationCount: len(plan.Operations), Partial: true, Latency: s.now().Sub(started)})
			s.discord.securityTelemetry.RecordSecurityEvent(ctx, domain.SecurityEvent{Kind: "lockdown_partial_failure", Reason: domain.PolicyError, Mode: domain.ModeApply, DestructiveClass: domain.DestructiveLockdown, CorrelationID: request.Invocation.CorrelationID, OccurredAt: s.now()})
			return result, err
		}
		completed[operation.ID] = struct{}{}
		result.Completed = append(result.Completed, operation.ID)
		if _, err := s.store.Append(plan.GuildID, plan.Hash, domain.LockdownJournalEvent{Type: domain.LockdownOperationCompleted, OperationID: operation.ID, OccurredAt: s.now().UTC(), Result: "applied"}); err != nil {
			return result, err
		}
	}
	s.telemetry.ObserveLockdown(ctx, LockdownObservation{Operation: string(kind), Outcome: "success", OperationCount: len(plan.Operations), Latency: s.now().Sub(started)})
	return result, nil
}

func (s *LockdownService) lockdownOperations(guildID domain.GuildID, scope domain.LockdownScope, channels []domain.Channel) ([]domain.LockdownOperation, error) {
	byID := map[domain.ChannelID]domain.Channel{}
	for _, channel := range channels {
		if channel.GuildID == nil || *channel.GuildID != guildID {
			return nil, errors.New("channel guild ownership is unknown")
		}
		byID[channel.ID] = channel
	}
	selected := append([]domain.ChannelID(nil), scope.ChannelIDs...)
	for _, categoryID := range scope.CategoryIDs {
		category, ok := byID[categoryID]
		if !ok || category.Type != 4 {
			return nil, &domain.ValidationError{Field: "scope", Rule: "unknown category"}
		}
		selected = append(selected, categoryID)
		for _, channel := range channels {
			if channel.ParentID != nil && *channel.ParentID == categoryID {
				selected = append(selected, channel.ID)
			}
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i] < selected[j] })
	seen := map[domain.ChannelID]struct{}{}
	operations := []domain.LockdownOperation{}
	for _, channelID := range selected {
		if _, duplicate := seen[channelID]; duplicate {
			continue
		}
		seen[channelID] = struct{}{}
		if _, protected := s.settings.ProtectedChannelIDs[channelID]; protected {
			return nil, &domain.ConflictError{Reason: "lockdown scope includes protected channel"}
		}
		channel, ok := byID[channelID]
		if !ok || !supportedLockdownChannel(channel.Type) {
			return nil, &domain.ValidationError{Field: "scope", Rule: "unknown or unsupported channel"}
		}
		targets := append([]domain.RoleID{domain.RoleID(guildID)}, s.settings.ProtectedRoleIDs...)
		for _, roleID := range targets {
			before := findOverwrite(channel.PermissionOverwrites, domain.Snowflake(roleID))
			after, err := lockdownAfterOverwrite(before, domain.Snowflake(roleID), roleID != domain.RoleID(guildID))
			if err != nil {
				return nil, err
			}
			change := domain.LockdownOverwriteChange{ChannelID: channelID, TargetID: domain.Snowflake(roleID), Type: 0, Before: before, After: &after}
			encoded, _ := json.Marshal(change)
			sum := sha256.Sum256(encoded)
			id := domain.LockdownOperationID(hex.EncodeToString(sum[:16]))
			operations = append(operations, domain.LockdownOperation{ID: id, ChannelID: channelID, Changes: []domain.LockdownOverwriteChange{change}})
		}
	}
	return operations, nil
}

func validateProtectedRoles(protected []domain.RoleID, roles []domain.Role, bot domain.Member) error {
	byID := map[domain.RoleID]domain.Role{}
	for _, role := range roles {
		byID[role.ID] = role
	}
	botHighest := highestRolePosition(bot.Roles, byID)
	if bot.User.ID == "" || botHighest < 0 {
		return errors.New("bot hierarchy is unknown")
	}
	canManage := false
	for _, roleID := range bot.Roles {
		if role, ok := byID[roleID]; ok {
			permissions := permissionUint(role.Permissions)
			if permissions&lockdownAdministrator != 0 || permissions&lockdownManageChannels != 0 {
				canManage = true
			}
		}
	}
	if !canManage {
		return errors.New("bot lacks channel management permission")
	}
	for _, id := range protected {
		role, ok := byID[id]
		if !ok || role.Managed || role.Position >= botHighest {
			return errors.New("protected role is unknown, managed, or outside bot hierarchy")
		}
	}
	return nil
}

func supportedLockdownChannel(kind domain.ChannelType) bool {
	switch kind {
	case 0, 2, 4, 5, 13, 15, 16:
		return true
	default:
		return false
	}
}
func findOverwrite(values []domain.PermissionOverwrite, target domain.Snowflake) *domain.PermissionOverwrite {
	for _, value := range values {
		if value.ID == target && value.Type == 0 {
			copyValue := value
			return &copyValue
		}
	}
	return nil
}
func lockdownAfterOverwrite(before *domain.PermissionOverwrite, target domain.Snowflake, protected bool) (domain.PermissionOverwrite, error) {
	value := domain.PermissionOverwrite{ID: target, Type: 0, Allow: "0", Deny: "0"}
	if before != nil {
		value = *before
	}
	allow, allowOK := parsePermission(value.Allow)
	deny, denyOK := parsePermission(value.Deny)
	if !allowOK || !denyOK {
		return domain.PermissionOverwrite{}, errors.New("channel overwrite permissions are invalid")
	}
	if protected {
		allow |= lockdownDenyPermissions | lockdownViewPermission
		deny &^= lockdownDenyPermissions | lockdownViewPermission
	} else {
		deny |= lockdownDenyPermissions
		allow &^= lockdownDenyPermissions
	}
	value.Allow, value.Deny = domain.PermissionBits(fmt.Sprint(allow)), domain.PermissionBits(fmt.Sprint(deny))
	return value, nil
}
func parsePermission(value domain.PermissionBits) (uint64, bool) {
	parsed, ok := new(big.Int).SetString(string(value), 10)
	if !ok || !parsed.IsUint64() {
		return 0, false
	}
	return parsed.Uint64(), true
}
func permissionUint(value domain.PermissionBits) uint64 {
	parsed, _ := parsePermission(value)
	return parsed
}
func highestRolePosition(ids []domain.RoleID, roles map[domain.RoleID]domain.Role) int {
	highest := -1
	for _, id := range ids {
		if role, ok := roles[id]; ok && role.Position > highest {
			highest = role.Position
		}
	}
	return highest
}
func lockdownSnapshotHash(guild domain.Guild, channels []domain.Channel, roles []domain.Role, bot domain.Member) (string, error) {
	sort.Slice(channels, func(i, j int) bool { return channels[i].ID < channels[j].ID })
	sort.Slice(roles, func(i, j int) bool { return roles[i].ID < roles[j].ID })
	encoded, err := json.Marshal(struct {
		Guild    domain.Guild
		Channels []domain.Channel
		Roles    []domain.Role
		Bot      domain.Member
	}{guild, channels, roles, bot})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
func completedLockdownOperations(events []domain.LockdownJournalEvent) map[domain.LockdownOperationID]struct{} {
	result := map[domain.LockdownOperationID]struct{}{}
	for _, event := range events {
		if event.Type == domain.LockdownOperationCompleted {
			result[event.OperationID] = struct{}{}
		}
	}
	return result
}
func pendingLockdownOperations(operations []domain.LockdownOperation, completed map[domain.LockdownOperationID]struct{}) []domain.LockdownOperationID {
	result := []domain.LockdownOperationID{}
	for _, op := range operations {
		if _, ok := completed[op.ID]; !ok {
			result = append(result, op.ID)
		}
	}
	return result
}
func equalOverwrite(left, right *domain.PermissionOverwrite) bool {
	encodedLeft, _ := json.Marshal(left)
	encodedRight, _ := json.Marshal(right)
	return string(encodedLeft) == string(encodedRight)
}
func (s *LockdownService) currentOverwrite(ctx context.Context, invocation Invocation, guild domain.GuildID, channelID domain.ChannelID, target domain.Snowflake, capability domain.Capability) (*domain.PermissionOverwrite, error) {
	execution, err := dispatchAs[domain.Channel](ctx, s.discord, invocation, domain.GetChannel{CommandContext: domain.CommandContext{GuildID: guild}, ChannelID: channelID}, capability)
	if err != nil {
		return nil, err
	}
	return findOverwrite(execution.Result.PermissionOverwrites, target), nil
}
func dispatchAs[R any](ctx context.Context, service *DiscordService, invocation Invocation, command domain.DiscordCommand[R], capability domain.Capability) (domain.Execution[R], error) {
	raw, err := service.dispatchDiscordWithCapability(ctx, invocation, command, capability)
	if err != nil {
		return domain.Execution[R]{Plan: raw.Plan, Attempts: raw.Attempts, Partial: raw.Partial}, err
	}
	if raw.Plan != nil {
		return domain.Execution[R]{Plan: raw.Plan}, nil
	}
	value, ok := raw.Result.(R)
	if !ok {
		return domain.Execution[R]{}, &domain.InvariantError{Detail: "Discord result type"}
	}
	return domain.Execution[R]{Result: value, Attempts: raw.Attempts}, nil
}
