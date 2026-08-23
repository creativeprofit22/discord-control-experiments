package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type lockdownMemoryStore struct {
	plans  map[domain.LockdownPlanHash]domain.LockdownPlan
	events map[domain.LockdownPlanHash][]domain.LockdownJournalEvent
}

func (s *lockdownMemoryStore) SavePlan(plan domain.LockdownPlan) error {
	if s.plans == nil {
		s.plans = map[domain.LockdownPlanHash]domain.LockdownPlan{}
	}
	if _, ok := s.plans[plan.Hash]; ok {
		return errors.New("exists")
	}
	s.plans[plan.Hash] = plan
	return nil
}
func (s *lockdownMemoryStore) LoadPlan(guild domain.GuildID, hash domain.LockdownPlanHash) (domain.LockdownPlan, error) {
	plan, ok := s.plans[hash]
	if !ok || plan.GuildID != guild {
		return plan, errors.New("not found")
	}
	return plan, plan.VerifyHash()
}
func (s *lockdownMemoryStore) Append(_ domain.GuildID, hash domain.LockdownPlanHash, event domain.LockdownJournalEvent) (domain.LockdownJournalEvent, error) {
	if s.events == nil {
		s.events = map[domain.LockdownPlanHash][]domain.LockdownJournalEvent{}
	}
	s.events[hash] = append(s.events[hash], event)
	return event, nil
}
func (s *lockdownMemoryStore) Replay(_ domain.GuildID, hash domain.LockdownPlanHash) ([]domain.LockdownJournalEvent, error) {
	return append([]domain.LockdownJournalEvent(nil), s.events[hash]...), nil
}

type lockdownApproval struct{ consumes int }

func (*lockdownApproval) ConsumeOperation(context.Context, domain.ApprovalID, domain.ActorID, domain.GuildID, string, time.Time) (domain.ApprovalGrant, error) {
	return domain.ApprovalGrant{}, errors.New("unexpected operation approval")
}
func (*lockdownApproval) ConsumeReconciliationPlan(context.Context, domain.ApprovalID, domain.ActorID, domain.GuildID, string, time.Time) (domain.ApprovalGrant, error) {
	return domain.ApprovalGrant{}, errors.New("unexpected reconciliation approval")
}
func (a *lockdownApproval) ConsumeLockdownPlan(_ context.Context, id domain.ApprovalID, actor domain.ActorID, guild domain.GuildID, hash string, now time.Time) (domain.ApprovalGrant, error) {
	a.consumes++
	if id == "" {
		return domain.ApprovalGrant{}, errors.New("missing")
	}
	return domain.ApprovalGrant{ID: id, ActorID: actor, GuildID: guild, ScopeKind: domain.ApprovalScopeLockdownPlan, ScopeHash: hash, IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute)}, nil
}

func TestLockdownProtectedAccessPartialApplyResumeAndDriftRefusal(t *testing.T) {
	guildID := domain.GuildID("1")
	channelID := domain.ChannelID("40")
	protected := domain.RoleID("30")
	botRole := domain.RoleID("20")
	botID := domain.UserID("10")
	channel := domain.Channel{ID: channelID, GuildID: &guildID, Type: 0, PermissionOverwrites: []domain.PermissionOverwrite{}}
	roles := []domain.Role{{ID: botRole, Position: 10, Permissions: domain.PermissionBits("16")}, {ID: protected, Position: 5, Permissions: "0"}}
	inspection := domain.GuildInspection{Guild: domain.Guild{ID: guildID, OwnerID: "99"}, Channels: []domain.Channel{channel}, Roles: roles}
	port := &scriptedPort{results: []DiscordPortResult{{Value: inspection}, {Value: domain.User{ID: botID}}, {Value: domain.Member{User: domain.User{ID: botID}, Roles: []domain.RoleID{botRole}}}, {Value: roles},
		{Value: channel}, {Value: domain.EmptyResult{}}, {Value: channel}, {Value: domain.EmptyResult{}, Sent: true, Uncertain: true},
		{Value: channel}, {Value: domain.EmptyResult{}}}}
	port.errors = []error{nil, nil, nil, nil, nil, nil, nil, errors.New("uncertain"), nil, nil}
	approval := &lockdownApproval{}
	discord, err := NewSecuredDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil, SecurityDependencies{Policy: moderationPolicy{}, Approvals: approval, Audit: moderationAudit{}})
	if err != nil {
		t.Fatal(err)
	}
	store := &lockdownMemoryStore{}
	service, err := NewLockdownService(discord, store, LockdownSettings{ProtectedRoleIDs: []domain.RoleID{protected}, MaxOperations: 10, MaxDestructiveOperations: 10, PlanTTL: 15 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Unix(100, 0) }
	invocation := moderationInvocation(domain.ModeApply)
	plan, recovery, err := service.BuildLockdownPlan(context.Background(), BuildLockdownRequest{Invocation: invocation, GuildID: guildID, Scope: domain.LockdownScope{ChannelIDs: []domain.ChannelID{channelID}}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.PairedPlanHash != recovery.Hash || len(plan.Operations) != 2 {
		t.Fatalf("plans = %#v %#v", plan, recovery)
	}
	protectedAfter := plan.Operations[1].Changes[0].After
	if permissionUint(protectedAfter.Allow)&lockdownViewPermission == 0 || permissionUint(protectedAfter.Deny)&lockdownDenyPermissions != 0 {
		t.Fatalf("protected overwrite = %#v", protectedAfter)
	}
	partial, err := service.ApplyLockdown(context.Background(), ApplyLockdownRequest{Invocation: invocation, GuildID: guildID, PlanHash: plan.Hash, ApprovalID: "approved"})
	if err == nil || len(partial.Completed) != 1 || len(partial.Pending) != 1 {
		t.Fatalf("partial=%#v err=%v", partial, err)
	}
	resumed, err := service.ApplyLockdown(context.Background(), ApplyLockdownRequest{Invocation: invocation, GuildID: guildID, PlanHash: plan.Hash})
	if err != nil || len(resumed.Completed) != 2 || approval.consumes != 1 {
		t.Fatalf("resume=%#v consumes=%d err=%v", resumed, approval.consumes, err)
	}
	lockedChannel := channel
	for _, operation := range plan.Operations {
		lockedChannel.PermissionOverwrites = append(lockedChannel.PermissionOverwrites, *operation.Changes[0].After)
	}
	port.results = append(port.results, DiscordPortResult{Value: lockedChannel}, DiscordPortResult{Value: domain.EmptyResult{}}, DiscordPortResult{Value: lockedChannel}, DiscordPortResult{Value: domain.EmptyResult{}})
	port.errors = append(port.errors, nil, nil, nil, nil)
	recovered, recoverErr := service.RecoverLockdown(context.Background(), ApplyLockdownRequest{Invocation: invocation, GuildID: guildID, PlanHash: recovery.Hash, ApprovalID: "recover-approved"})
	if recoverErr != nil || len(recovered.Completed) != 2 || approval.consumes != 2 {
		t.Fatalf("recover=%#v consumes=%d err=%v", recovered, approval.consumes, recoverErr)
	}

	store.plans[plan.Hash] = func() domain.LockdownPlan {
		tampered := plan
		tampered.Operations[0].Changes[0].After.Deny = "0"
		return tampered
	}()
	if _, err := service.ApplyLockdown(context.Background(), ApplyLockdownRequest{Invocation: invocation, GuildID: guildID, PlanHash: plan.Hash}); err == nil {
		t.Fatal("tampered plan accepted")
	}
}

func TestLockdownRejectsProtectedResourceAndExpiredPlan(t *testing.T) {
	service, _, _ := lockdownBuildFixture(t, map[domain.ChannelID]struct{}{"40": {}})
	_, _, err := service.BuildLockdownPlan(context.Background(), BuildLockdownRequest{Invocation: moderationInvocation(domain.ModeApply), GuildID: "1", Scope: domain.LockdownScope{ChannelIDs: []domain.ChannelID{"40"}}})
	if err == nil {
		t.Fatal("protected channel accepted")
	}
}

func lockdownBuildFixture(t *testing.T, protectedChannels map[domain.ChannelID]struct{}) (*LockdownService, *lockdownMemoryStore, *scriptedPort) {
	t.Helper()
	guild := domain.GuildID("1")
	channel := domain.Channel{ID: "40", GuildID: &guild, Type: 0}
	roles := []domain.Role{{ID: "20", Position: 10, Permissions: "16"}, {ID: "30", Position: 5, Permissions: "0"}}
	port := &scriptedPort{results: []DiscordPortResult{{Value: domain.GuildInspection{Guild: domain.Guild{ID: guild}, Channels: []domain.Channel{channel}, Roles: roles}}, {Value: domain.User{ID: "10"}}, {Value: domain.Member{User: domain.User{ID: "10"}, Roles: []domain.RoleID{"20"}}}, {Value: roles}}}
	discord, err := NewSecuredDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil, SecurityDependencies{Policy: moderationPolicy{}, Approvals: &lockdownApproval{}, Audit: moderationAudit{}})
	if err != nil {
		t.Fatal(err)
	}
	store := &lockdownMemoryStore{}
	service, err := NewLockdownService(discord, store, LockdownSettings{ProtectedRoleIDs: []domain.RoleID{"30"}, ProtectedChannelIDs: protectedChannels, MaxOperations: 10, MaxDestructiveOperations: 10, PlanTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Unix(1, 0) }
	return service, store, port
}
