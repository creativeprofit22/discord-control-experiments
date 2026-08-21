package policy

import (
	"context"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type Service struct{}

func New() *Service { return &Service{} }

func (s *Service) Decide(context.Context, string) domain.PolicyDecision {
	return domain.PolicyDecision{Allowed: false, Reason: "no allow policy configured"}
}
