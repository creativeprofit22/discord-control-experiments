package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type moderationMemoryStore struct {
	events  []domain.ModerationJournalEvent
	exports domain.AppealExport
}

func (s *moderationMemoryStore) Append(_ domain.GuildID, event domain.ModerationJournalEvent) (domain.ModerationJournalEvent, error) {
	event.Sequence = uint64(len(s.events) + 1)
	s.events = append(s.events, event)
	return event, nil
}
func (s *moderationMemoryStore) Replay(domain.GuildID) ([]domain.ModerationJournalEvent, error) {
	return append([]domain.ModerationJournalEvent(nil), s.events...), nil
}
func (*moderationMemoryStore) Compact(domain.GuildID, time.Duration, time.Time) error { return nil }
func (s *moderationMemoryStore) AppealExport(domain.GuildID, domain.UserID, time.Time) (domain.AppealExport, error) {
	return s.exports, nil
}

type moderationIDs int

func (i *moderationIDs) NewModerationID(prefix string) (string, error) {
	*i++
	return prefix + string(rune('0'+*i)), nil
}

type moderationRedactor struct{}

func (moderationRedactor) Sanitize(value string) (string, bool) {
	if value == "secret" {
		return "[REDACTED]", true
	}
	return value, false
}

type moderationPolicy struct{ deny domain.Capability }

func (p moderationPolicy) Decide(_ context.Context, request domain.PolicyRequest) (domain.PolicyDecision, error) {
	if request.Capability == p.deny {
		return domain.PolicyDecision{Reason: domain.PolicyCapabilityDenied}, nil
	}
	return domain.PolicyDecision{Allowed: true, Reason: domain.PolicyAllowed, ActorID: "actor", EffectiveMode: request.Mode}, nil
}

type moderationAudit struct{}

func (moderationAudit) Append(context.Context, domain.AuditEvent) error { return nil }

type noDiscordPort struct{ calls int }

func (p *noDiscordPort) ExecuteDiscord(context.Context, domain.Command) (DiscordPortResult, error) {
	p.calls++
	return DiscordPortResult{}, errors.New("unexpected Discord call")
}

func TestModerationHierarchyFailsClosedForOwnerEqualProtectedAndManagedTargets(t *testing.T) {
	tests := []struct {
		name                        string
		subject                     domain.UserID
		targetRoles                 []domain.RoleID
		botPosition, targetPosition int
		protected                   bool
		kind                        domain.ActionKind
		managedQuarantine           bool
	}{
		{name: "owner", subject: "99", targetRoles: []domain.RoleID{"30"}, botPosition: 10, targetPosition: 1, kind: domain.ActionKick},
		{name: "equal", subject: "2", targetRoles: []domain.RoleID{"30"}, botPosition: 5, targetPosition: 5, kind: domain.ActionKick},
		{name: "protected", subject: "2", targetRoles: []domain.RoleID{"30"}, botPosition: 10, targetPosition: 5, protected: true, kind: domain.ActionKick},
		{name: "managed quarantine", subject: "2", targetRoles: []domain.RoleID{"30"}, botPosition: 10, targetPosition: 5, kind: domain.ActionQuarantineAdd, managedQuarantine: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			roles := []domain.Role{{ID: "20", Position: test.botPosition}, {ID: "30", Position: test.targetPosition}, {ID: "40", Position: 2, Managed: test.managedQuarantine}}
			results := []DiscordPortResult{{Value: domain.User{ID: "10"}}, {Value: domain.GuildInspection{Guild: domain.Guild{ID: "1", OwnerID: "99"}, Roles: roles}}}
			if test.subject != "99" {
				results = append(results, DiscordPortResult{Value: domain.Member{User: domain.User{ID: test.subject}, Roles: test.targetRoles}}, DiscordPortResult{Value: domain.Member{User: domain.User{ID: "10"}, Roles: []domain.RoleID{"20"}}}, DiscordPortResult{Value: roles})
			}
			port := &scriptedPort{results: results}
			discord, err := NewSecuredDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil, SecurityDependencies{Policy: moderationPolicy{}, Audit: moderationAudit{}})
			if err != nil {
				t.Fatal(err)
			}
			protected := map[domain.RoleID]struct{}{}
			if test.protected {
				protected["30"] = struct{}{}
			}
			service := &ModerationService{discord: discord, settings: ModerationSettings{ProtectedOperatorRoleIDs: protected, QuarantineRoleID: "40"}, telemetry: noopModerationTelemetry{}, now: time.Now}
			err = service.checkHierarchy(context.Background(), ModerationActionRequest{Invocation: moderationInvocation(domain.ModeApply), GuildID: "1", SubjectUserID: test.subject, Kind: test.kind}, domain.CapabilityModerationKick)
			if err == nil {
				t.Fatal("hierarchy allowed")
			}
		})
	}
}

func TestModerationActionCapabilityMappingIsSpecific(t *testing.T) {
	tests := map[domain.ActionKind]domain.Capability{domain.ActionWarn: domain.CapabilityModerationWarn, domain.ActionTimeout: domain.CapabilityModerationTimeout, domain.ActionTimeoutRemove: domain.CapabilityModerationTimeout, domain.ActionKick: domain.CapabilityModerationKick, domain.ActionBan: domain.CapabilityModerationBan, domain.ActionUnban: domain.CapabilityModerationBan, domain.ActionMessageDelete: domain.CapabilityModerationMessageDelete, domain.ActionBulkMessageDelete: domain.CapabilityModerationMessageDelete, domain.ActionQuarantineAdd: domain.CapabilityModerationQuarantine, domain.ActionQuarantineRemove: domain.CapabilityModerationQuarantine, domain.ActionInviteCreate: domain.CapabilityModerationInviteControl, domain.ActionInviteRevoke: domain.CapabilityModerationInviteControl}
	for kind, want := range tests {
		got, _, err := moderationActionPolicy(kind)
		if err != nil || got != want {
			t.Fatalf("%s capability=%s err=%v", kind, got, err)
		}
	}
}

func TestModerationWarningRedactionDuplicateAndExpiredTimeoutRemoval(t *testing.T) {
	service, store, port := newModerationTestService(t, moderationPolicy{})
	service.now = func() time.Time { return time.Unix(1000, 0).UTC() }
	invocation := moderationInvocation(domain.ModeApply)
	caseValue, _, err := service.CreateCase(context.Background(), CaseCreateRequest{Invocation: invocation, GuildID: "1", SubjectUserID: "2", ReasonCode: "spam", Notes: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if caseValue.Notes != "[REDACTED]" {
		t.Fatalf("notes = %q", caseValue.Notes)
	}
	request := ModerationActionRequest{Invocation: invocation, CaseID: caseValue.ID, GuildID: "1", SubjectUserID: "2", Kind: domain.ActionWarn, ReasonCode: "spam", Notes: "secret", IdempotencyKey: "idem"}
	first, err := service.ExecuteAction(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.ExecuteAction(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Action.ID != second.Action.ID || first.Action.Notes != "[REDACTED]" {
		t.Fatalf("duplicate result = %#v %#v", first, second)
	}
	changed := request
	changed.ReasonCode = "abuse"
	if _, err := service.ExecuteAction(context.Background(), changed); err == nil {
		t.Fatal("mismatched idempotency payload accepted")
	}
	expired := domain.ModerationAction{ID: "timeout", CaseID: caseValue.ID, GuildID: "1", SubjectUserID: "2", ActorID: "actor", CorrelationID: "corr", IdempotencyKey: "old", ReasonCode: "spam", Kind: domain.ActionTimeout, Status: domain.ActionCompleted, CreatedAt: time.Unix(1, 0), ExpiresAt: timePointer(time.Unix(2, 0))}
	store.events = append(store.events, domain.ModerationJournalEvent{Type: domain.ModerationActionAppend, Action: &expired})
	remove := request
	remove.Kind, remove.IdempotencyKey = domain.ActionTimeoutRemove, "remove"
	result, err := service.ExecuteAction(context.Background(), remove)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action.Status != domain.ActionNoop || port.calls != 0 {
		t.Fatalf("expired timeout removal = %#v calls=%d", result, port.calls)
	}
}

func TestModerationEscalationRejectsStaleRecommendationAndAppliesFreshDecision(t *testing.T) {
	service, store, _ := newModerationTestService(t, moderationPolicy{})
	now := time.Unix(1000, 0).UTC()
	service.now = func() time.Time { return now }
	service.settings.Policy = domain.EscalationPolicy{Version: "p1", Rules: []domain.EscalationRule{{ID: "warn-again", Priority: 1, EligiblePriorKinds: []domain.ActionKind{domain.ActionWarn}, Lookback: time.Hour, MinimumCount: 1, Action: domain.ActionWarn, MaximumApplications: 10}}}
	invocation := moderationInvocation(domain.ModeApply)
	caseValue, _, err := service.CreateCase(context.Background(), CaseCreateRequest{Invocation: invocation, GuildID: "1", SubjectUserID: "2", ReasonCode: "spam"})
	if err != nil {
		t.Fatal(err)
	}
	prior := domain.ModerationAction{ID: "prior", CaseID: caseValue.ID, GuildID: "1", SubjectUserID: "2", ActorID: "actor", CorrelationID: "corr", IdempotencyKey: "prior", ReasonCode: "spam", Kind: domain.ActionWarn, Status: domain.ActionCompleted, CreatedAt: now.Add(-time.Minute)}
	store.events = append(store.events, domain.ModerationJournalEvent{Type: domain.ModerationActionAppend, Action: &prior})
	evaluated, err := service.EvaluateEscalation(context.Background(), EscalationEvaluateRequest{Invocation: invocation, GuildID: "1", SubjectUserID: "2"})
	if err != nil {
		t.Fatal(err)
	}
	stale := evaluated.Recommendation
	stale.PolicyHash = "tampered"
	apply := EscalationApplyRequest{Invocation: invocation, GuildID: "1", SubjectUserID: "2", CaseID: caseValue.ID, ReasonCode: "spam", IdempotencyKey: "escalated", Recommendation: stale}
	if _, err := service.ApplyEscalation(context.Background(), apply); err == nil {
		t.Fatal("stale escalation accepted")
	}
	apply.Recommendation = evaluated.Recommendation
	result, err := service.ApplyEscalation(context.Background(), apply)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action.Kind != domain.ActionWarn || result.Action.Status != domain.ActionCompleted {
		t.Fatalf("apply = %#v", result)
	}
}

func TestModerationEvidenceGuildBindingAndCapabilityDenial(t *testing.T) {
	service, _, _ := newModerationTestService(t, moderationPolicy{deny: domain.CapabilityModerationCaseWrite})
	if _, _, err := service.CreateCase(context.Background(), CaseCreateRequest{Invocation: moderationInvocation(domain.ModeApply), GuildID: "1", SubjectUserID: "2", ReasonCode: "spam"}); err == nil {
		t.Fatal("capability denial allowed")
	}
	service, _, _ = newModerationTestService(t, moderationPolicy{})
	caseValue, _, err := service.CreateCase(context.Background(), CaseCreateRequest{Invocation: moderationInvocation(domain.ModeApply), GuildID: "1", SubjectUserID: "2", ReasonCode: "spam"})
	if err != nil {
		t.Fatal(err)
	}
	evidence := domain.EvidenceReference{ID: "e", CaseID: caseValue.ID, GuildID: "9", SubjectUserID: "2", ActorID: "actor", CorrelationID: "corr", Kind: domain.EvidenceDiscordMessage, ChannelID: "3", MessageID: "4", CreatedAt: time.Now()}
	if _, err := service.AttachEvidence(context.Background(), EvidenceAttachRequest{Invocation: moderationInvocation(domain.ModeApply), CaseID: caseValue.ID, Evidence: evidence}); err == nil {
		t.Fatal("wrong-guild evidence accepted")
	}
}

func newModerationTestService(t *testing.T, policy PolicyEvaluator) (*ModerationService, *moderationMemoryStore, *noDiscordPort) {
	t.Helper()
	port := &noDiscordPort{}
	discord, err := NewSecuredDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil, SecurityDependencies{Policy: policy, Audit: moderationAudit{}})
	if err != nil {
		t.Fatal(err)
	}
	store := &moderationMemoryStore{}
	ids := moderationIDs(0)
	service, err := NewModerationService(discord, store, ModerationSettings{Retention: time.Hour, MaxCasesPerGuild: 10, MaxActionsPerCase: 10, ReasonCodes: map[domain.ReasonCode]struct{}{"spam": {}, "abuse": {}}}, &ids, moderationRedactor{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return service, store, port
}

func moderationInvocation(mode domain.ExecutionMode) Invocation {
	return Invocation{Principal: domain.Principal{Adapter: "cli", Subject: "local", AuthenticationMethod: "local"}, RequestedMode: mode, CorrelationID: "corr", ActorID: "actor"}
}
func timePointer(value time.Time) *time.Time { return &value }
