package reconcile

import (
	"fmt"
	"sort"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func BuildDAG(input []domain.PlanOperation) ([]domain.PlanOperation, error) {
	operations := append([]domain.PlanOperation(nil), input...)
	for index := range operations {
		operations[index].Dependencies = nil
		operations[index].Risk = ClassifyRisk(operations[index])
		operations[index].RequiresApproval = RequiresApprovalRisk(operations[index].Risk)
		id, err := operationID(operations[index])
		if err != nil {
			return nil, err
		}
		operations[index].ID = id
	}
	seenIDs := make(map[domain.OperationID]struct{}, len(operations))
	for _, operation := range operations {
		if _, duplicate := seenIDs[operation.ID]; duplicate {
			return nil, fmt.Errorf("duplicate operation ID %s", operation.ID)
		}
		seenIDs[operation.ID] = struct{}{}
	}

	for index := range operations {
		dependencies := make(map[domain.OperationID]struct{})
		for candidateIndex := range operations {
			if index == candidateIndex {
				continue
			}
			if operationDependsOn(operations[index], operations[candidateIndex]) {
				dependencies[operations[candidateIndex].ID] = struct{}{}
			}
		}
		for dependency := range dependencies {
			operations[index].Dependencies = append(operations[index].Dependencies, dependency)
		}
		sort.Slice(operations[index].Dependencies, func(i, j int) bool { return operations[index].Dependencies[i] < operations[index].Dependencies[j] })
	}
	return topologicalOrder(operations)
}

func operationID(operation domain.PlanOperation) (domain.OperationID, error) {
	identity := struct {
		Kind        domain.OperationKind     `json:"kind"`
		Resource    domain.StableResourceRef `json:"resource"`
		Fingerprint string                   `json:"payload_fingerprint"`
	}{operation.Kind, operation.Resource, operation.PayloadFingerprint}
	hash, err := HashCanonical(identity)
	if err != nil {
		return "", err
	}
	return domain.OperationID("op_" + hash[:20]), nil
}

func operationDependsOn(operation, candidate domain.PlanOperation) bool {
	if candidate.Kind == domain.OperationDelete {
		if operation.Kind != domain.OperationDelete {
			return false
		}
		if operation.Resource.Kind == domain.ResourceCategory && (candidate.Resource.Kind == domain.ResourceChannel || candidate.Resource.Kind == domain.ResourceWebhook) {
			return true
		}
		if operation.Resource.Kind == domain.ResourceChannel && candidate.Resource.Kind == domain.ResourceWebhook {
			return true
		}
		return false
	}
	if operation.Kind == domain.OperationDelete {
		return false
	}
	candidatePrepares := candidate.Kind == domain.OperationCreate || candidate.Kind == domain.OperationUpdate || candidate.Kind == domain.OperationReplace || candidate.Kind == domain.OperationMove
	if !candidatePrepares {
		return false
	}
	if operation.Kind == domain.OperationReorder {
		return operation.Resource.Kind == candidate.Resource.Kind || (operation.Resource.Kind == domain.ResourceChannel && candidate.Resource.Kind == domain.ResourceCategory)
	}
	if operation.Resource.Kind == domain.ResourceChannel && candidate.Resource.Kind == domain.ResourceCategory {
		return true
	}
	if operation.Kind == domain.OperationOverwrite && (candidate.Resource.Kind == domain.ResourceRole || candidate.Resource == operation.Resource) {
		return true
	}
	if operation.Resource == candidate.Resource && operation.Resource.Kind == domain.ResourceAutoModerationRule && (candidate.Kind == domain.OperationCreate || candidate.Kind == domain.OperationReplace) && operation.Kind == domain.OperationUpdate {
		return true
	}
	if operation.Resource.Kind == domain.ResourceWebhook && candidate.Resource.Kind == domain.ResourceChannel {
		return operation.Payload.Webhook != nil && operation.Payload.Webhook.Channel == candidate.Resource
	}
	if operation.Resource.Kind == domain.ResourceAutoModerationRule && operation.Payload.AutoModerationRule != nil {
		for _, ref := range operation.Payload.AutoModerationRule.ExemptRoles {
			if ref == candidate.Resource {
				return true
			}
		}
		for _, ref := range operation.Payload.AutoModerationRule.ExemptChannels {
			if ref == candidate.Resource {
				return true
			}
		}
		for _, action := range operation.Payload.AutoModerationRule.Actions {
			if action.AlertChannel != nil && *action.AlertChannel == candidate.Resource {
				return true
			}
		}
	}
	if operation.Resource.Kind == domain.ResourceOnboarding && (candidate.Resource.Kind == domain.ResourceChannel || candidate.Resource.Kind == domain.ResourceRole || candidate.Resource.Kind == domain.ResourceCategory) {
		return true
	}
	return false
}

func topologicalOrder(operations []domain.PlanOperation) ([]domain.PlanOperation, error) {
	byID := make(map[domain.OperationID]domain.PlanOperation, len(operations))
	indegree := make(map[domain.OperationID]int, len(operations))
	dependents := make(map[domain.OperationID][]domain.OperationID)
	for _, operation := range operations {
		byID[operation.ID] = operation
		indegree[operation.ID] = len(operation.Dependencies)
		for _, dependency := range operation.Dependencies {
			dependents[dependency] = append(dependents[dependency], operation.ID)
		}
	}
	for _, operation := range operations {
		for _, dependency := range operation.Dependencies {
			if _, exists := byID[dependency]; !exists {
				return nil, fmt.Errorf("operation %s depends on unknown operation %s", operation.ID, dependency)
			}
		}
	}
	ready := []domain.OperationID{}
	for id, degree := range indegree {
		if degree == 0 {
			ready = append(ready, id)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i] < ready[j] })
	ordered := make([]domain.PlanOperation, 0, len(operations))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		ordered = append(ordered, byID[id])
		for _, dependent := range dependents[id] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = append(ready, dependent)
				sort.Slice(ready, func(i, j int) bool { return ready[i] < ready[j] })
			}
		}
	}
	if len(ordered) != len(operations) {
		return nil, fmt.Errorf("operation dependency graph contains a cycle")
	}
	return ordered, nil
}

func PlanContentHash(plan domain.Plan) (string, error) {
	content := struct {
		SchemaVersion string                 `json:"schema_version"`
		Binding       domain.PlanBinding     `json:"binding"`
		Operations    []domain.PlanOperation `json:"operations"`
	}{plan.SchemaVersion, plan.Binding, plan.Operations}
	return HashCanonical(content)
}

func ValidatePlan(plan domain.Plan) error {
	if plan.SchemaVersion != domain.PlanSchemaV1 {
		return fmt.Errorf("unsupported plan schema %q", plan.SchemaVersion)
	}
	if plan.Binding.GuildID == "" || plan.Binding.ActorID == "" || plan.Binding.DesiredHash == "" || plan.Binding.SnapshotHash == "" {
		return fmt.Errorf("plan binding is incomplete")
	}
	rebuilt, err := BuildDAG(plan.Operations)
	if err != nil {
		return err
	}
	if !reflectOperations(rebuilt, plan.Operations) {
		return fmt.Errorf("plan operation IDs, risks, approvals, or dependencies are not canonical")
	}
	hash, err := PlanContentHash(plan)
	if err != nil {
		return err
	}
	if plan.Hash != domain.PlanHash(hash) {
		return &domain.StalePlanError{Reason: "plan content hash does not match"}
	}
	if len(plan.Conflicts) > 0 {
		return &domain.ConflictError{Reason: "plan contains unresolved conflicts", Conflicts: plan.Conflicts}
	}
	return nil
}

func reflectOperations(left, right []domain.PlanOperation) bool {
	leftJSON, err := CanonicalJSON(left)
	if err != nil {
		return false
	}
	rightJSON, err := CanonicalJSON(right)
	if err != nil {
		return false
	}
	return string(leftJSON) == string(rightJSON)
}
