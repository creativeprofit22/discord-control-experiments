package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type scriptedPort struct {
	mu      sync.Mutex
	calls   []domain.Command
	results []DiscordPortResult
	errors  []error
}

func (p *scriptedPort) ExecuteDiscord(_ context.Context, c domain.Command) (DiscordPortResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, c)
	i := len(p.calls) - 1
	var result DiscordPortResult
	if i < len(p.results) {
		result = p.results[i]
	}
	var err error
	if i < len(p.errors) {
		err = p.errors[i]
	}
	return result, err
}
func (p *scriptedPort) count() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.calls) }

type allowTestPolicy struct{}

func (allowTestPolicy) Decide(_ context.Context, request domain.PolicyRequest) (domain.PolicyDecision, error) {
	return domain.PolicyDecision{Allowed: true, ActorID: "test-actor", EffectiveMode: request.Mode, Reason: domain.PolicyAllowed}, nil
}

type errorTestPolicy struct{}

func (errorTestPolicy) Decide(context.Context, domain.PolicyRequest) (domain.PolicyDecision, error) {
	return domain.PolicyDecision{}, errors.New("evaluator unavailable")
}

type panicTestPolicy struct{}

func (panicTestPolicy) Decide(context.Context, domain.PolicyRequest) (domain.PolicyDecision, error) {
	panic("untrusted policy panic value")
}

type memoryTestAudit struct {
	mu     sync.Mutex
	events []domain.AuditEvent
}

func (a *memoryTestAudit) Append(_ context.Context, event domain.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
	return nil
}

type allowTestApproval struct{}

func (allowTestApproval) Consume(_ context.Context, id domain.ApprovalID, actor domain.ActorID, hash string, now time.Time) (domain.ApprovalGrant, error) {
	return domain.ApprovalGrant{ID: id, ActorID: actor, OperationHash: hash, ApproverID: "human", IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute)}, nil
}

func newTestDiscordService(port DiscordPort, idempotency IdempotencyStore, ownership ResourceOwnershipStore, telemetry DiscordTelemetry) *DiscordService {
	service, err := NewSecuredDiscordService(port, idempotency, ownership, telemetry, SecurityDependencies{Policy: allowTestPolicy{}, Approvals: allowTestApproval{}, Audit: &memoryTestAudit{}, DefaultInvocation: Invocation{Principal: domain.Principal{Adapter: "test", Subject: "suite", AuthenticationMethod: "in-process"}, RequestedMode: domain.ModeApply, CorrelationID: "test-correlation", ApprovalID: "approved"}})
	if err != nil {
		panic(err)
	}
	return service
}

type captureTestPolicy struct {
	mu       sync.Mutex
	requests []domain.PolicyRequest
}

func (p *captureTestPolicy) Decide(_ context.Context, request domain.PolicyRequest) (domain.PolicyDecision, error) {
	p.mu.Lock()
	p.requests = append(p.requests, request)
	p.mu.Unlock()
	return domain.PolicyDecision{Allowed: true, ActorID: "test-actor", EffectiveMode: request.Mode, Reason: domain.PolicyAllowed}, nil
}

func securedServiceWithPolicy(port DiscordPort, evaluator PolicyEvaluator, mode domain.ExecutionMode) (*DiscordService, *memoryTestAudit) {
	audit := &memoryTestAudit{}
	service, err := NewSecuredDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil, SecurityDependencies{Policy: evaluator, Approvals: allowTestApproval{}, Audit: audit, DefaultInvocation: Invocation{Principal: domain.Principal{Adapter: "test", Subject: "suite", AuthenticationMethod: "in-process"}, RequestedMode: mode, CorrelationID: "correlation-1", ApprovalID: "approved"}})
	if err != nil {
		panic(err)
	}
	return service, audit
}

type failingTestAudit struct {
	failAt int
	calls  int
}

func (a *failingTestAudit) Append(_ context.Context, _ domain.AuditEvent) error {
	a.calls++
	if a.calls >= a.failAt {
		return errors.New("audit unavailable")
	}
	return nil
}

type orderingTestPort struct {
	audit  *memoryTestAudit
	called bool
}

func (p *orderingTestPort) ExecuteDiscord(_ context.Context, _ domain.Command) (DiscordPortResult, error) {
	p.audit.mu.Lock()
	defer p.audit.mu.Unlock()
	if len(p.audit.events) != 1 || p.audit.events[0].Kind != "decision" {
		return DiscordPortResult{}, errors.New("network before precommit")
	}
	p.called = true
	return DiscordPortResult{Value: domain.GuildInspection{Guild: domain.Guild{ID: "1"}}, Attempts: 1}, nil
}

func TestAuditPrecommitAndCompletionContract(t *testing.T) {
	audit := &memoryTestAudit{}
	port := &orderingTestPort{audit: audit}
	service, err := NewSecuredDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil, SecurityDependencies{Policy: allowTestPolicy{}, Audit: audit, DefaultInvocation: Invocation{Principal: domain.Principal{Adapter: "test", Subject: "suite", AuthenticationMethod: "in-process"}, RequestedMode: domain.ModeApply, CorrelationID: "audit-order"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(context.Background(), service, domain.InspectGuild{CommandContext: domain.CommandContext{GuildID: "1"}}); err != nil {
		t.Fatal(err)
	}
	if !port.called || len(audit.events) != 2 || audit.events[1].Kind != "completion" || audit.events[1].Result != "success" {
		t.Fatalf("events = %#v", audit.events)
	}
}

func TestAuditFailurePreventsNetwork(t *testing.T) {
	port := &scriptedPort{}
	sink := &failingTestAudit{failAt: 1}
	service, err := NewSecuredDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil, SecurityDependencies{Policy: allowTestPolicy{}, Audit: sink, DefaultInvocation: Invocation{Principal: domain.Principal{Adapter: "test", Subject: "suite", AuthenticationMethod: "in-process"}, RequestedMode: domain.ModeApply, CorrelationID: "audit-failure"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(context.Background(), service, domain.InspectGuild{CommandContext: domain.CommandContext{GuildID: "1"}}); err == nil {
		t.Fatal("expected audit failure")
	}
	if port.count() != 0 {
		t.Fatalf("transport calls = %d", port.count())
	}
}

func TestReadOnlyStructurallyRemovesMutations(t *testing.T) {
	catalog, err := CommandCatalogFor(context.Background(), allowTestPolicy{}, DiscoveryContext{Principal: domain.Principal{Adapter: "mcp", Subject: "local", AuthenticationMethod: "os-process"}, GuildID: "1", Mode: domain.ModeReadOnly, CorrelationID: "discovery"})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) == 0 {
		t.Fatal("expected readable catalog")
	}
	previous := ""
	for _, spec := range catalog {
		if spec.MutationExposed {
			t.Fatalf("mutation exposed: %s", spec.Name)
		}
		if spec.Name < previous {
			t.Fatalf("catalog not sorted: %s before %s", previous, spec.Name)
		}
		previous = spec.Name
	}
}

func TestDryRunHasNoSideEffects(t *testing.T) {
	port := &scriptedPort{}
	service, audit := securedServiceWithPolicy(port, allowTestPolicy{}, domain.ModeDryRun)
	command := domain.CreateChannel{CommandContext: domain.CommandContext{GuildID: "1", IdempotencyKey: "dry", ReconciliationKey: "dry-reconcile"}, Name: "not-sent", Type: 0}
	execution, err := Execute(context.Background(), service, command)
	if err != nil {
		t.Fatal(err)
	}
	if execution.Plan == nil || execution.Attempts != 0 || port.count() != 0 {
		t.Fatalf("execution=%#v transport=%d", execution, port.count())
	}
	if len(audit.events) != 2 || audit.events[1].Result != "dry_run" {
		t.Fatalf("audit events = %#v", audit.events)
	}
	if _, ok, _ := service.idempotency.Load(context.Background(), "1:create_channel:dry"); ok {
		t.Fatal("dry run claimed idempotency")
	}
}

func TestDestructiveClassesRequireApproval(t *testing.T) {
	required := map[string]domain.DestructiveClass{"delete_channel": domain.DestructiveDelete, "bulk_delete_messages": domain.DestructiveBulkDelete, "create_member_ban": domain.DestructiveBan, "remove_member": domain.DestructiveKick, "set_channel_permission": domain.DestructivePermissionChange, "create_webhook": domain.DestructiveWebhookCreate, "create_invite": domain.DestructiveInviteCreate, "bulk_overwrite_guild_application_commands": domain.DestructiveBulkOverwrite}
	for name, class := range required {
		spec, ok := LookupCommandSpec(name)
		if !ok || spec.DestructiveClass != class || !domain.RequiresApproval(spec.DestructiveClass) {
			t.Fatalf("%s classification = %#v", name, spec)
		}
	}
	roleSpec, _ := LookupCommandSpec("create_role")
	permissions := domain.PermissionBits("1")
	if DestructiveClassForCommand(domain.CreateRole{CommandContext: domain.CommandContext{GuildID: "1"}, Name: "plain"}, roleSpec) != domain.DestructiveNone || DestructiveClassForCommand(domain.CreateRole{CommandContext: domain.CommandContext{GuildID: "1"}, Name: "privileged", Permissions: &permissions}, roleSpec) != domain.DestructiveRoleEscalation {
		t.Fatal("permission-bearing role creation must be the only escalated role-create form")
	}
	if !domain.RequiresApproval(domain.DestructiveLockdown) {
		t.Fatal("lockdown must require approval")
	}
	port := &scriptedPort{}
	audit := &memoryTestAudit{}
	service, err := NewSecuredDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil, SecurityDependencies{Policy: allowTestPolicy{}, Audit: audit, DefaultInvocation: Invocation{Principal: domain.Principal{Adapter: "test", Subject: "suite", AuthenticationMethod: "in-process"}, RequestedMode: domain.ModeApply, CorrelationID: "destructive"}})
	if err != nil {
		t.Fatal(err)
	}
	service.ownership.Record(ResourceChannel, "2", "1")
	if _, err := Execute(context.Background(), service, domain.DeleteChannel{CommandContext: domain.CommandContext{GuildID: "1"}, ChannelID: "2"}); err == nil {
		t.Fatal("destructive operation bypassed approval")
	}
	if port.count() != 0 {
		t.Fatalf("transport calls = %d", port.count())
	}
}

func TestEveryDiscordDispatchPassesTypedPolicy(t *testing.T) {
	port := &scriptedPort{results: []DiscordPortResult{{Value: domain.GuildInspection{Guild: domain.Guild{ID: "1"}}, Attempts: 1}}}
	policy := &captureTestPolicy{}
	service, _ := securedServiceWithPolicy(port, policy, domain.ModeApply)
	if _, err := Execute(context.Background(), service, domain.InspectGuild{CommandContext: domain.CommandContext{GuildID: "1"}}); err != nil {
		t.Fatal(err)
	}
	if len(policy.requests) != 1 || port.count() != 1 {
		t.Fatalf("policy=%d transport=%d", len(policy.requests), port.count())
	}
}

func TestInternalDiscoveryUsesPolicyChokepoint(t *testing.T) {
	port := &scriptedPort{results: []DiscordPortResult{{Value: []domain.Channel{}, Attempts: 1}, {Value: domain.Channel{ID: "2"}, Attempts: 1}}}
	policy := &captureTestPolicy{}
	service, _ := securedServiceWithPolicy(port, policy, domain.ModeApply)
	command := domain.CreateChannel{CommandContext: domain.CommandContext{GuildID: "1", IdempotencyKey: "idem", ReconciliationKey: "reconcile"}, Name: "safe", Type: 0}
	if _, err := Execute(context.Background(), service, command); err != nil {
		t.Fatal(err)
	}
	if len(policy.requests) != 2 || policy.requests[0].Operation != "create_channel" || policy.requests[1].Operation != "list_guild_channels" {
		t.Fatalf("requests = %#v", policy.requests)
	}
}

func TestPolicyRequestIncludesAllDimensions(t *testing.T) {
	port := &scriptedPort{results: []DiscordPortResult{{Value: domain.Channel{ID: "2"}, Attempts: 1}}}
	policy := &captureTestPolicy{}
	service, _ := securedServiceWithPolicy(port, policy, domain.ModeApply)
	service.ownership.Record(ResourceChannel, "2", "1")
	if _, err := Execute(context.Background(), service, domain.GetChannel{CommandContext: domain.CommandContext{GuildID: "1"}, ChannelID: "2"}); err != nil {
		t.Fatal(err)
	}
	request := policy.requests[0]
	if request.Principal.Adapter != "test" || request.GuildID != "1" || request.Operation != "get_channel" || request.Capability != domain.CapabilityChannelRead || request.Mode != domain.ModeApply || request.DestructiveClass != domain.DestructiveNone || request.CorrelationID == "" || len(request.Resources) < 2 {
		t.Fatalf("incomplete request: %#v", request)
	}
}

func TestAuthorizationFailsClosedBeforeNetwork(t *testing.T) {
	port := &scriptedPort{}
	service, err := NewSecuredDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil, SecurityDependencies{Policy: denyPolicy{}, Audit: &memoryTestAudit{}, DefaultInvocation: Invocation{Principal: domain.Principal{Adapter: "mcp", Subject: "unknown", AuthenticationMethod: "os-process"}, RequestedMode: domain.ModeApply, CorrelationID: "deny-all"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(context.Background(), service, domain.InspectGuild{CommandContext: domain.CommandContext{GuildID: "1"}}); err == nil {
		t.Fatal("deny-all policy allowed transport")
	}
	if port.count() != 0 {
		t.Fatalf("transport calls = %d", port.count())
	}

	name := "bounded"
	cases := []struct {
		kind    ResourceKind
		id      string
		command domain.Command
		setup   func(*DiscordService)
	}{
		{ResourceRole, "20", domain.ModifyRole{CommandContext: domain.CommandContext{GuildID: "1", IdempotencyKey: "i", ReconciliationKey: "r"}, RoleID: "20", Name: &name}, nil},
		{ResourceMessage, "30", domain.GetMessage{CommandContext: domain.CommandContext{GuildID: "1"}, ChannelID: "10", MessageID: "30"}, func(s *DiscordService) { s.ownership.Record(ResourceChannel, "10", "1") }},
		{ResourceWebhook, "40", domain.GetWebhook{CommandContext: domain.CommandContext{GuildID: "1"}, WebhookID: "40"}, nil},
		{ResourceInvite, "invite", domain.GetInvite{CommandContext: domain.CommandContext{GuildID: "1"}, Code: "invite"}, nil},
		{ResourceApplicationCommand, "60", domain.GetGuildApplicationCommand{CommandContext: domain.CommandContext{GuildID: "1"}, ApplicationID: "50", ApplicationCommandID: "60"}, nil},
		{ResourceScheduledEvent, "70", domain.GetScheduledEvent{CommandContext: domain.CommandContext{GuildID: "1"}, ScheduledEventID: "70"}, nil},
	}
	for _, test := range cases {
		t.Run(string(test.kind), func(t *testing.T) {
			p := &scriptedPort{}
			s := newTestDiscordService(p, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil)
			s.ownership.Record(test.kind, test.id, "2")
			if test.setup != nil {
				test.setup(s)
			}
			if _, err := s.execute(context.Background(), test.command); err == nil {
				t.Fatal("cross-guild resource allowed")
			}
			if p.count() != 0 {
				t.Fatalf("transport calls = %d", p.count())
			}
		})
	}
}

func TestForgedPrincipalAndPolicyErrorsDeny(t *testing.T) {
	for name, evaluator := range map[string]PolicyEvaluator{"error": errorTestPolicy{}, "panic": panicTestPolicy{}} {
		t.Run(name, func(t *testing.T) {
			port := &scriptedPort{}
			service, err := NewSecuredDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil, SecurityDependencies{Policy: evaluator, Audit: &memoryTestAudit{}, DefaultInvocation: Invocation{Principal: domain.Principal{Adapter: "mcp", Subject: "forged-admin", AuthenticationMethod: "model-claim"}, RequestedMode: domain.ModeApply, CorrelationID: "policy-error"}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Execute(context.Background(), service, domain.InspectGuild{CommandContext: domain.CommandContext{GuildID: "1"}}); err == nil {
				t.Fatal("policy failure allowed")
			}
			if port.count() != 0 {
				t.Fatalf("transport calls = %d", port.count())
			}
		})
	}
}

func TestRegistryCompleteness(t *testing.T) {
	catalog := CommandCatalog()
	if len(catalog) != 59 {
		t.Fatalf("catalog has %d entries, want 59", len(catalog))
	}
	seen := map[string]bool{}
	for _, spec := range catalog {
		if spec.Name == "" || spec.Method == "" || spec.RouteTemplate == "" || spec.MajorResource == "" || spec.Ownership == "" || spec.Mutation == "" || spec.Retry == "" || spec.Idempotency == "" || spec.RequestCodec == "" || spec.ResponseCodec == "" || spec.Reconciliation == "" || spec.Capability == "" || spec.DestructiveClass == "" || spec.ResourceExtractor == nil || spec.IntentSanitizer == nil {
			t.Fatalf("incomplete registry entry: %#v", spec)
		}
		if seen[spec.Name] {
			t.Fatalf("duplicate command %s", spec.Name)
		}
		seen[spec.Name] = true
	}
}

func TestRegistryResourceExtractionAndIntentSanitization(t *testing.T) {
	spec, ok := LookupCommandSpec("modify_message")
	if !ok {
		t.Fatal("missing modify_message")
	}
	content := "canary-secret-message"
	command := domain.ModifyMessage{CommandContext: domain.CommandContext{GuildID: "1"}, ChannelID: "2", MessageID: "3", Content: &content}
	refs := spec.ResourceExtractor(command)
	if len(refs) != 3 {
		t.Fatalf("resource refs = %#v", refs)
	}
	intent := spec.IntentSanitizer(command)
	if strings.Contains(fmt.Sprint(intent), content) {
		t.Fatalf("sanitized intent leaked content: %#v", intent)
	}
}
func TestValidationAndOwnershipRejectBeforeTransport(t *testing.T) {
	port := &scriptedPort{}
	service := newTestDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil)
	_, err := Execute(context.Background(), service, domain.ListGuildChannels{CommandContext: domain.CommandContext{GuildID: " 1"}})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	_, err = Execute(context.Background(), service, domain.GetChannel{CommandContext: domain.CommandContext{GuildID: "1"}, ChannelID: "2"})
	var ownership *domain.OwnershipError
	if !errors.As(err, &ownership) {
		t.Fatalf("expected ownership error, got %v", err)
	}
	if port.count() != 0 {
		t.Fatalf("transport called %d times", port.count())
	}
}
func TestCommandValidationCoversLengthsEnumsPermissionsPayloadsAndOwnership(t *testing.T) {
	port := &scriptedPort{}
	service := newTestDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil)
	base := domain.CommandContext{GuildID: "1"}
	commands := []domain.Command{
		domain.CreateRole{CommandContext: domain.CommandContext{GuildID: "1", IdempotencyKey: "a", ReconciliationKey: "a"}, Name: strings.Repeat("x", 101)},
		domain.CreateChannel{CommandContext: domain.CommandContext{GuildID: "1", IdempotencyKey: "b", ReconciliationKey: "b"}, Name: "x", Type: 99},
		domain.SetChannelPermission{CommandContext: base, ChannelID: "2", Overwrite: domain.PermissionOverwrite{ID: "3", Type: 0, Allow: "1", Deny: "1"}},
		domain.CreateMessage{CommandContext: domain.CommandContext{GuildID: "1", IdempotencyKey: "c", ReconciliationKey: "c"}, ChannelID: "2", Content: stringPointerForApplication("x"), Uploads: []domain.Upload{{Filename: strings.Repeat("x", int(MaxJSONRequestBytes)), Size: 1, Reader: bytes.NewReader([]byte("x"))}}},
	}
	for _, command := range commands {
		_, err := service.execute(context.Background(), command)
		var validation *domain.ValidationError
		if !errors.As(err, &validation) {
			t.Errorf("%s: expected validation error, got %v", command.CommandName(), err)
		}
	}
	if port.count() != 0 {
		t.Fatalf("invalid commands made %d network calls", port.count())
	}
}
func stringPointerForApplication(value string) *string { return &value }

func TestCreateReplayDoesNotDuplicate(t *testing.T) {
	port := &scriptedPort{results: []DiscordPortResult{{Value: []domain.Role{}, Attempts: 1}, {Value: domain.Role{ID: "9", Name: "operator", Permissions: "0"}, Attempts: 1}}}
	service := newTestDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil)
	command := domain.CreateRole{CommandContext: domain.CommandContext{GuildID: "1", IdempotencyKey: "same", ReconciliationKey: "role-operator"}, Name: "operator"}
	first, err := Execute(context.Background(), service, command)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Execute(context.Background(), service, command)
	if err != nil {
		t.Fatal(err)
	}
	if first.Result.ID != "9" || second.Result.ID != "9" || second.Replay != domain.ReplaySucceeded {
		t.Fatalf("unexpected executions: %#v %#v", first, second)
	}
	if port.count() != 2 {
		t.Fatalf("baseline plus one create expected, got %d calls", port.count())
	}
}
func TestUncertainCreateReconcilesWithoutDuplicateAndReturnsPartial(t *testing.T) {
	port := &scriptedPort{results: []DiscordPortResult{{Value: []domain.Role{}, Attempts: 1}, {Attempts: 1, Sent: true, Uncertain: true}, {Value: []domain.Role{{ID: "9", Name: "operator", Permissions: "0"}}, Attempts: 1}}, errors: []error{nil, &domain.TransportError{Uncertain: true, Err: errors.New("connection closed")}}}
	service := newTestDiscordService(port, NewInMemoryIdempotencyStore(), NewInMemoryResourceOwnershipStore(), nil)
	command := domain.CreateRole{CommandContext: domain.CommandContext{GuildID: "1", IdempotencyKey: "uncertain", ReconciliationKey: "role-operator"}, Name: "operator"}
	failed, err := Execute(context.Background(), service, command)
	if err == nil || failed.Partial == nil || failed.Partial.Outcome != domain.PartialSentUnknown || !failed.Partial.Recoverable {
		t.Fatalf("missing recoverable partial result: %#v, %v", failed, err)
	}
	resumed, err := Execute(context.Background(), service, command)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Result.ID != "9" || resumed.Replay != domain.ReplayCoalesced || port.count() != 3 {
		t.Fatalf("unexpected reconciliation: %#v calls=%d", resumed, port.count())
	}
}

func FuzzValidateSnowflake(f *testing.F) {
	for _, seed := range []string{"1", "0", "18446744073709551615", " 1", "+1", "18446744073709551616", "abc"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) { _ = validateSnowflake(value) })
}
