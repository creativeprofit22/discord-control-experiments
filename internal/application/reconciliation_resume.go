package application

import (
	"fmt"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func reconcileUncertainCreate(operation domain.PlanOperation, snapshot domain.GuildSnapshot) (*domain.ResourceBinding, error) {
	name, resourceType, ok := operationDesiredIdentity(operation)
	if !ok {
		return nil, &domain.ConflictError{Reason: "uncertain create has no discoverable typed identity"}
	}
	matches := []domain.SnapshotResource{}
	for _, resource := range snapshotResources(snapshot) {
		if !resource.Managed && resource.Ref.Kind == operation.Resource.Kind && resource.Name == name && resource.Type == resourceType {
			matches = append(matches, resource)
		}
	}
	if len(matches) != 1 {
		return nil, &domain.ConflictError{Reason: fmt.Sprintf("uncertain create discovery found %d candidates", len(matches)), Conflicts: []domain.PlanConflict{{Kind: domain.ConflictAmbiguousMatch, Resource: operation.Resource, Message: "uncertain create requires exactly one matching remote resource"}}}
	}
	return &domain.ResourceBinding{Ref: operation.Resource, RemoteID: matches[0].RemoteID, Fingerprint: operation.PayloadFingerprint}, nil
}

func operationDesiredIdentity(operation domain.PlanOperation) (string, int, bool) {
	if operation.Payload.Category != nil {
		return operation.Payload.Category.Name, 4, true
	}
	if operation.Payload.Channel != nil {
		return operation.Payload.Channel.Name, int(operation.Payload.Channel.Type), true
	}
	if operation.Payload.Role != nil {
		return operation.Payload.Role.Name, 0, true
	}
	if operation.Payload.Webhook != nil {
		return operation.Payload.Webhook.Name, 1, true
	}
	if operation.Payload.ApplicationCommand != nil {
		return operation.Payload.ApplicationCommand.Name, int(operation.Payload.ApplicationCommand.Type), true
	}
	return "", 0, false
}

func operationPreconditionHolds(operation domain.PlanOperation, snapshot domain.GuildSnapshot) bool {
	if operation.Preconditions.ResourceRemoteID == "" {
		return operation.Preconditions.ExpectedAbsent
	}
	for _, resource := range snapshotResources(snapshot) {
		if resource.RemoteID == operation.Preconditions.ResourceRemoteID {
			return operation.Preconditions.ResourceFingerprint == "" || resource.Fingerprint == operation.Preconditions.ResourceFingerprint
		}
	}
	return false
}
func resourcePresent(remoteID domain.Snowflake, snapshot domain.GuildSnapshot) bool {
	for _, resource := range snapshotResources(snapshot) {
		if resource.RemoteID == remoteID {
			return true
		}
	}
	return false
}
func snapshotResources(snapshot domain.GuildSnapshot) []domain.SnapshotResource {
	result := make([]domain.SnapshotResource, 0, len(snapshot.Categories)+len(snapshot.Channels)+len(snapshot.Roles)+len(snapshot.Webhooks)+len(snapshot.ApplicationCommands))
	result = append(result, snapshot.Categories...)
	result = append(result, snapshot.Channels...)
	result = append(result, snapshot.Roles...)
	result = append(result, snapshot.Webhooks...)
	result = append(result, snapshot.ApplicationCommands...)
	return result
}
