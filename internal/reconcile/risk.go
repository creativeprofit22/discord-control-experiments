package reconcile

import (
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func ClassifyRisk(operation domain.PlanOperation) domain.RiskClass {
	switch operation.Kind {
	case domain.OperationNoop:
		return domain.RiskReadOnly
	case domain.OperationCreate:
		if operation.Resource.Kind == domain.ResourcePermissionOverwrite || operation.Resource.Kind == domain.ResourceAutoModerationRule {
			return domain.RiskPermissionSensitive
		}
		return domain.RiskAdditive
	case domain.OperationOverwrite:
		return domain.RiskPermissionSensitive
	case domain.OperationReplace:
		return domain.RiskReplacement
	case domain.OperationDelete:
		return domain.RiskDestructive
	case domain.OperationUpdate, domain.OperationMove, domain.OperationReorder:
		if operation.Resource.Kind == domain.ResourceAutoModerationRule {
			return domain.RiskPermissionSensitive
		}
		return domain.RiskMutating
	default:
		return domain.RiskMutating
	}
}

func IsDestructiveRisk(risk domain.RiskClass) bool {
	return risk == domain.RiskDestructive || risk == domain.RiskReplacement
}

func RequiresApprovalRisk(risk domain.RiskClass) bool {
	return IsDestructiveRisk(risk) || risk == domain.RiskPermissionSensitive
}
