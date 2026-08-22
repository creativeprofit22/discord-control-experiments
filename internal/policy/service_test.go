package policy

import (
	"context"
	"testing"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func leastPrivilegeService(t *testing.T) *Service {
	t.Helper()
	service, err := NewWithRules(domain.ModeApply, []ActorRule{{
		ActorID:      "operator-1",
		Principals:   []domain.Principal{{Adapter: "mcp", Subject: "local-user", AuthenticationMethod: "os-process"}},
		Guilds:       []domain.GuildID{"100"},
		Capabilities: []domain.Capability{domain.CapabilityGuildInspect, domain.CapabilityChannelRead, domain.CapabilityMessageManage},
		Mode:         domain.ModeApply,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestPolicyTruthTable(t *testing.T) {
	service := leastPrivilegeService(t)
	base := domain.PolicyRequest{Principal: domain.Principal{Adapter: "mcp", Subject: "local-user", AuthenticationMethod: "os-process"}, GuildID: "100", Capability: domain.CapabilityChannelRead, Mode: domain.ModeApply, DestructiveClass: domain.DestructiveNone}
	tests := []struct {
		name    string
		mutate  func(*domain.PolicyRequest)
		allowed bool
		reason  domain.PolicyReason
	}{
		{"allow", func(*domain.PolicyRequest) {}, true, domain.PolicyAllowed},
		{"forged actor", func(r *domain.PolicyRequest) { r.ActorID = "admin" }, false, domain.PolicyUnknownPrincipal},
		{"adapter substitution", func(r *domain.PolicyRequest) { r.Principal.Adapter = "cli" }, false, domain.PolicyUnknownPrincipal},
		{"guild denied", func(r *domain.PolicyRequest) { r.GuildID = "200" }, false, domain.PolicyGuildDenied},
		{"capability denied", func(r *domain.PolicyRequest) { r.Capability = domain.CapabilityRoleManage }, false, domain.PolicyCapabilityDenied},
		{"unknown capability", func(r *domain.PolicyRequest) { r.Capability = "future.superuser" }, false, domain.PolicyCapabilityDenied},
		{"unknown mode", func(r *domain.PolicyRequest) { r.Mode = "unlimited" }, false, domain.PolicyModeDenied},
		{"unknown destructive", func(r *domain.PolicyRequest) { r.DestructiveClass = "erase_everything" }, false, domain.PolicyDestructiveDenied},
		{"cross guild resource", func(r *domain.PolicyRequest) {
			r.Resources = []domain.ResourceRef{{Kind: domain.ResourceChannel, ID: "2", GuildID: "200"}}
		}, false, domain.PolicyResourceDenied},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := base
			test.mutate(&request)
			decision, err := service.Decide(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Allowed != test.allowed || decision.Reason != test.reason {
				t.Fatalf("decision = %#v", decision)
			}
		})
	}
}

func TestPolicyConfigurationRejectsAmbiguityWildcardsAndDuplicates(t *testing.T) {
	principal := domain.Principal{Adapter: "mcp", Subject: "user", AuthenticationMethod: "local"}
	tests := []struct {
		name  string
		rules []ActorRule
	}{
		{"duplicate principal", []ActorRule{{ActorID: "a", Principals: []domain.Principal{principal}, Mode: domain.ModeReadOnly}, {ActorID: "b", Principals: []domain.Principal{principal}, Mode: domain.ModeReadOnly}}},
		{"wildcard guild", []ActorRule{{ActorID: "a", Principals: []domain.Principal{principal}, Guilds: []domain.GuildID{"*"}, Mode: domain.ModeReadOnly}}},
		{"wildcard capability", []ActorRule{{ActorID: "a", Principals: []domain.Principal{principal}, Capabilities: []domain.Capability{"*"}, Mode: domain.ModeReadOnly}}},
		{"duplicate actor", []ActorRule{{ActorID: "a", Mode: domain.ModeReadOnly}, {ActorID: "a", Mode: domain.ModeReadOnly}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewWithRules(domain.ModeApply, test.rules); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func FuzzPolicyPrincipal(f *testing.F) {
	f.Add("mcp", "local-user", "os-process", "100", "channel.read")
	evaluator, err := NewWithRules(domain.ModeApply, []ActorRule{{ActorID: "operator-1", Principals: []domain.Principal{{Adapter: "mcp", Subject: "local-user", AuthenticationMethod: "os-process"}}, Guilds: []domain.GuildID{"100"}, Capabilities: []domain.Capability{domain.CapabilityChannelRead}, Mode: domain.ModeApply}})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, adapter, subject, method, guild, capability string) {
		if len(adapter)+len(subject)+len(method)+len(guild)+len(capability) > 2048 {
			return
		}
		_, _ = evaluator.Decide(context.Background(), domain.PolicyRequest{Principal: domain.Principal{Adapter: domain.Adapter(adapter), Subject: subject, AuthenticationMethod: method}, GuildID: domain.GuildID(guild), Capability: domain.Capability(capability), Mode: domain.ModeReadOnly, DestructiveClass: domain.DestructiveNone})
	})
}

func TestDenyAllDefaultAndModeCeiling(t *testing.T) {
	decision, err := New().Decide(context.Background(), domain.PolicyRequest{Principal: domain.Principal{Adapter: "mcp", Subject: "x", AuthenticationMethod: "local"}, GuildID: "1", Capability: domain.CapabilityGuildInspect, Mode: domain.ModeReadOnly, DestructiveClass: domain.DestructiveNone})
	if err != nil || decision.Allowed {
		t.Fatalf("default decision = %#v, %v", decision, err)
	}
	if _, err := NewWithRules(domain.ModeReadOnly, []ActorRule{{ActorID: "a", Mode: domain.ModeApply}}); err == nil {
		t.Fatal("actor exceeded ceiling")
	}
}
