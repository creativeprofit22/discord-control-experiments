package application

import (
	"context"
	"errors"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type DiscoveryContext struct {
	Principal     domain.Principal
	GuildID       domain.GuildID
	Mode          domain.ExecutionMode
	CorrelationID domain.CorrelationID
}

// CommandCatalogFor returns only policy-authorized metadata. Any classifier error fails closed.
func CommandCatalogFor(ctx context.Context, evaluator PolicyEvaluator, discovery DiscoveryContext) (catalog []CommandSpec, err error) {
	if evaluator == nil || discovery.Principal.Adapter == "" || discovery.Principal.Subject == "" || discovery.Principal.AuthenticationMethod == "" || discovery.GuildID == "" || !domain.ValidExecutionMode(discovery.Mode) || discovery.CorrelationID == "" {
		return []CommandSpec{}, errors.New("invalid discovery context")
	}
	defer func() {
		if recover() != nil {
			catalog = []CommandSpec{}
			err = errors.New("policy discovery panic")
		}
	}()
	for _, spec := range CommandCatalog() {
		if discovery.Mode == domain.ModeReadOnly && spec.MutationExposed {
			continue
		}
		request := domain.PolicyRequest{Principal: discovery.Principal, GuildID: discovery.GuildID, Operation: domain.OperationName(spec.Name), Capability: spec.Capability, Resources: []domain.ResourceRef{{Kind: domain.ResourceGuild, ID: string(discovery.GuildID), GuildID: discovery.GuildID, Owned: true}}, Mode: discovery.Mode, DestructiveClass: spec.DestructiveClass, CorrelationID: discovery.CorrelationID}
		decision, decisionErr := evaluator.Decide(ctx, request)
		if decisionErr != nil {
			return []CommandSpec{}, errors.New("policy discovery failed")
		}
		if decision.Allowed {
			catalog = append(catalog, spec)
		}
	}
	return catalog, nil
}
