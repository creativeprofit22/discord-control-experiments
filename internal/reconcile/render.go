package reconcile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func RenderHuman(plan domain.Plan) string {
	riskCounts := make(map[domain.RiskClass]int)
	kindCounts := make(map[domain.OperationKind]int)
	for _, operation := range plan.Operations {
		riskCounts[operation.Risk]++
		kindCounts[operation.Kind]++
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "Plan %s for guild %s\n", hashPrefix(string(plan.Hash)), plan.Binding.GuildID)
	fmt.Fprintf(&builder, "Operations: %d; conflicts: %d\n", len(plan.Operations), len(plan.Conflicts))
	operationKinds := make([]string, 0, len(kindCounts))
	for kind := range kindCounts {
		operationKinds = append(operationKinds, string(kind))
	}
	sort.Strings(operationKinds)
	for _, kind := range operationKinds {
		fmt.Fprintf(&builder, "  %s: %d\n", kind, kindCounts[domain.OperationKind(kind)])
	}
	risks := make([]string, 0, len(riskCounts))
	for risk := range riskCounts {
		risks = append(risks, string(risk))
	}
	sort.Strings(risks)
	for _, risk := range risks {
		fmt.Fprintf(&builder, "  risk.%s: %d\n", risk, riskCounts[domain.RiskClass(risk)])
	}
	for _, conflict := range plan.Conflicts {
		fmt.Fprintf(&builder, "CONFLICT %s %s/%s\n", conflict.Kind, conflict.Resource.Kind, conflict.Resource.LogicalID)
	}
	return strings.TrimSuffix(builder.String(), "\n")
}

func RenderPlanJSON(plan domain.Plan) ([]byte, error) { return CanonicalJSON(plan) }

func hashPrefix(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}
