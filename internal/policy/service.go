package policy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

const maxIdentityLength = 256

var ErrInvalidRules = errors.New("invalid policy rules")

type ActorRule struct {
	ActorID               domain.ActorID
	Principals            []domain.Principal
	Guilds                []domain.GuildID
	Capabilities          []domain.Capability
	Mode                  domain.ExecutionMode
	PreapprovedOperations []domain.OperationName
}

type principalKey struct {
	adapter, subject, method string
}

type compiledActor struct {
	id           domain.ActorID
	guilds       map[domain.GuildID]struct{}
	capabilities map[domain.Capability]struct{}
	mode         domain.ExecutionMode
	preapproved  map[domain.OperationName]struct{}
}

type Service struct {
	ceiling    domain.ExecutionMode
	principals map[principalKey]compiledActor
}

// New returns a deny-all, read-only evaluator for compatibility and safe defaults.
func New() *Service {
	service, _ := NewWithRules(domain.ModeReadOnly, nil)
	return service
}

func NewWithRules(ceiling domain.ExecutionMode, rules []ActorRule) (*Service, error) {
	if !domain.ValidExecutionMode(ceiling) {
		return nil, fmt.Errorf("%w: unknown mode", ErrInvalidRules)
	}
	service := &Service{ceiling: ceiling, principals: make(map[principalKey]compiledActor)}
	actors := make(map[domain.ActorID]struct{}, len(rules))
	for _, rule := range rules {
		if !validIdentity(string(rule.ActorID)) || strings.Contains(string(rule.ActorID), "*") {
			return nil, fmt.Errorf("%w: malformed actor", ErrInvalidRules)
		}
		if _, duplicate := actors[rule.ActorID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate actor", ErrInvalidRules)
		}
		actors[rule.ActorID] = struct{}{}
		if !domain.ValidExecutionMode(rule.Mode) || modeRank(rule.Mode) > modeRank(ceiling) {
			return nil, fmt.Errorf("%w: actor mode exceeds ceiling", ErrInvalidRules)
		}
		actor := compiledActor{id: rule.ActorID, guilds: make(map[domain.GuildID]struct{}), capabilities: make(map[domain.Capability]struct{}), mode: rule.Mode, preapproved: make(map[domain.OperationName]struct{})}
		for _, guild := range rule.Guilds {
			if !validGrant(string(guild)) {
				return nil, fmt.Errorf("%w: malformed guild grant", ErrInvalidRules)
			}
			if _, duplicate := actor.guilds[guild]; duplicate {
				return nil, fmt.Errorf("%w: duplicate guild grant", ErrInvalidRules)
			}
			actor.guilds[guild] = struct{}{}
		}
		for _, capability := range rule.Capabilities {
			if !domain.ValidCapability(capability) || strings.Contains(string(capability), "*") {
				return nil, fmt.Errorf("%w: unknown capability", ErrInvalidRules)
			}
			if _, duplicate := actor.capabilities[capability]; duplicate {
				return nil, fmt.Errorf("%w: duplicate capability grant", ErrInvalidRules)
			}
			actor.capabilities[capability] = struct{}{}
		}
		for _, operation := range rule.PreapprovedOperations {
			if !validGrant(string(operation)) {
				return nil, fmt.Errorf("%w: malformed preapproved operation", ErrInvalidRules)
			}
			if _, duplicate := actor.preapproved[operation]; duplicate {
				return nil, fmt.Errorf("%w: duplicate preapproved operation", ErrInvalidRules)
			}
			actor.preapproved[operation] = struct{}{}
		}
		for _, principal := range rule.Principals {
			key, ok := normalizedPrincipal(principal)
			if !ok {
				return nil, fmt.Errorf("%w: malformed principal", ErrInvalidRules)
			}
			if _, duplicate := service.principals[key]; duplicate {
				return nil, fmt.Errorf("%w: ambiguous principal", ErrInvalidRules)
			}
			service.principals[key] = actor
		}
	}
	return service, nil
}

func (s *Service) Decide(_ context.Context, request domain.PolicyRequest) (domain.PolicyDecision, error) {
	deny := func(reason domain.PolicyReason) (domain.PolicyDecision, error) {
		return domain.PolicyDecision{Reason: reason}, nil
	}
	if s == nil {
		return deny(domain.PolicyError)
	}
	key, valid := normalizedPrincipal(request.Principal)
	if !valid {
		return deny(domain.PolicyMalformedPrincipal)
	}
	actor, known := s.principals[key]
	if !known {
		return deny(domain.PolicyUnknownPrincipal)
	}
	if request.ActorID != "" && request.ActorID != actor.id {
		return deny(domain.PolicyUnknownPrincipal)
	}
	if request.GuildID == "" {
		return deny(domain.PolicyGuildDenied)
	}
	if _, allowed := actor.guilds[request.GuildID]; !allowed {
		return deny(domain.PolicyGuildDenied)
	}
	if !domain.ValidCapability(request.Capability) {
		return deny(domain.PolicyCapabilityDenied)
	}
	if _, allowed := actor.capabilities[request.Capability]; !allowed {
		return deny(domain.PolicyCapabilityDenied)
	}
	if !domain.ValidExecutionMode(request.Mode) || modeRank(request.Mode) > modeRank(actor.mode) || modeRank(request.Mode) > modeRank(s.ceiling) {
		return deny(domain.PolicyModeDenied)
	}
	if !domain.ValidDestructiveClass(request.DestructiveClass) {
		return deny(domain.PolicyDestructiveDenied)
	}
	for _, resource := range request.Resources {
		if resource.GuildID != "" && resource.GuildID != request.GuildID {
			return deny(domain.PolicyResourceDenied)
		}
	}
	_, preapproved := actor.preapproved[request.Operation]
	return domain.PolicyDecision{Allowed: true, ActorID: actor.id, EffectiveMode: request.Mode, Reason: domain.PolicyAllowed, Preapproved: preapproved}, nil
}

func normalizedPrincipal(principal domain.Principal) (principalKey, bool) {
	if !validIdentity(string(principal.Adapter)) || !validIdentity(principal.Subject) || !validIdentity(principal.AuthenticationMethod) {
		return principalKey{}, false
	}
	return principalKey{adapter: string(principal.Adapter), subject: principal.Subject, method: principal.AuthenticationMethod}, true
}

func validIdentity(value string) bool {
	return value != "" && len(value) <= maxIdentityLength && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00")
}

func validGrant(value string) bool { return validIdentity(value) && !strings.Contains(value, "*") }

func modeRank(mode domain.ExecutionMode) int {
	switch mode {
	case domain.ModeReadOnly:
		return 1
	case domain.ModeDryRun:
		return 2
	case domain.ModeApply:
		return 3
	default:
		return 99
	}
}
