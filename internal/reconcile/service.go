package reconcile

import (
	"context"
	"reflect"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type Service struct{}

func New() *Service { return &Service{} }

func (s *Service) Reconcile(_ context.Context, desired, current any) (domain.Result, error) {
	changed := !reflect.DeepEqual(desired, current)
	return domain.Result{Changed: changed, Summary: map[bool]string{true: "change required", false: "already converged"}[changed]}, nil
}
