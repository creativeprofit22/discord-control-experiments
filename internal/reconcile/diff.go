package reconcile

import (
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type PlanRequest struct {
	Desired   domain.DesiredGuild
	Snapshot  domain.GuildSnapshot
	ActorID   domain.ActorID
	CreatedAt time.Time
}

func BuildPlan(request PlanRequest) (domain.Plan, error) {
	desired := NormalizeDesired(request.Desired)
	if err := ValidateDesired(desired); err != nil {
		return domain.Plan{}, err
	}
	if request.Snapshot.Metadata.GuildID != desired.GuildID {
		return domain.Plan{}, fmt.Errorf("desired guild and snapshot guild differ")
	}
	if desired.ApplicationID != "" && request.Snapshot.Metadata.ApplicationID != desired.ApplicationID {
		return domain.Plan{}, fmt.Errorf("desired application and snapshot application differ")
	}
	desiredHash, err := DesiredHash(desired)
	if err != nil {
		return domain.Plan{}, err
	}
	snapshotHash, err := SnapshotHash(request.Snapshot)
	if err != nil {
		return domain.Plan{}, err
	}
	matches := MatchDesired(desired, request.Snapshot)
	operations, conflicts, err := Diff(desired, request.Snapshot, matches)
	if err != nil {
		return domain.Plan{}, err
	}
	conflicts = append(conflicts, matches.Conflicts...)
	operations, err = BuildDAG(operations)
	if err != nil {
		return domain.Plan{}, err
	}
	plan := domain.Plan{
		SchemaVersion: domain.PlanSchemaV1,
		Binding:       domain.PlanBinding{GuildID: desired.GuildID, ApplicationID: desired.ApplicationID, ActorID: request.ActorID, DesiredHash: desiredHash, SnapshotHash: snapshotHash, AllowDesiredDeletes: desired.Ownership.DeleteOrphans},
		Operations:    operations, Conflicts: conflicts, CreatedAt: request.CreatedAt,
	}
	hash, err := PlanContentHash(plan)
	if err != nil {
		return domain.Plan{}, err
	}
	plan.Hash = domain.PlanHash(hash)
	plan.Summary = RenderHuman(plan)
	return plan, nil
}

func Diff(desired domain.DesiredGuild, snapshot domain.GuildSnapshot, matches MatchResult) ([]domain.PlanOperation, []domain.PlanConflict, error) {
	operations := []domain.PlanOperation{}
	conflicts := []domain.PlanConflict{}
	matched := make(map[domain.StableResourceRef]ResourceMatch, len(matches.Matches))
	for _, match := range matches.Matches {
		matched[match.Ref] = match
	}
	resourcesByID := make(map[domain.Snowflake]domain.SnapshotResource)
	for _, resource := range allSnapshotResources(snapshot) {
		resourcesByID[resource.RemoteID] = resource
	}

	appendOperation := func(operation domain.PlanOperation) error {
		fingerprint, err := HashCanonical(operation.Payload)
		if err != nil {
			return err
		}
		operation.PayloadFingerprint = fingerprint
		operation.Risk = ClassifyRisk(operation)
		operation.RequiresApproval = RequiresApprovalRisk(operation.Risk)
		operation.Preconditions.SnapshotHash = snapshot.Metadata.CanonicalHash
		operations = append(operations, operation)
		return nil
	}

	for index := range desired.Categories {
		category := desired.Categories[index]
		if category.Mode != domain.ManagementModeManaged {
			continue
		}
		ref := domain.StableResourceRef{Kind: domain.ResourceCategory, LogicalID: category.LogicalID}
		match, exists := matched[ref]
		if !exists {
			if conflict := unmanagedNameConflict(ref, category.Name, int(discordCategoryType), snapshot); conflict != nil {
				conflicts = append(conflicts, *conflict)
				continue
			}
			if err := appendOperation(domain.PlanOperation{Kind: domain.OperationCreate, Resource: ref, Preconditions: domain.Preconditions{ExpectedAbsent: true}, Payload: domain.OperationPayload{Category: &category}}); err != nil {
				return nil, nil, err
			}
			continue
		}
		observed := findObservedChannel(snapshot.ObservedChannels, domain.ChannelID(match.RemoteID))
		if observed == nil {
			return nil, nil, fmt.Errorf("matched category %s is absent from observations", ref.LogicalID)
		}
		if observed.Type != discordCategoryType {
			if err := appendReplacement(ref, resourcesByID[match.RemoteID], domain.OperationPayload{Category: &category}, &operations, appendOperation); err != nil {
				return nil, nil, err
			}
			continue
		}
		if stringValue(observed.Name) != category.Name {
			if err := appendOperation(domain.PlanOperation{Kind: domain.OperationUpdate, Resource: ref, Preconditions: resourcePreconditions(match, snapshot), Payload: domain.OperationPayload{Category: &category}}); err != nil {
				return nil, nil, err
			}
		}
		if observed.Position != category.Position {
			if err := appendOperation(domain.PlanOperation{Kind: domain.OperationMove, Resource: ref, Preconditions: resourcePreconditions(match, snapshot), Payload: domain.OperationPayload{Category: &category}}); err != nil {
				return nil, nil, err
			}
		}
	}
	for index := range desired.Roles {
		role := desired.Roles[index]
		if role.Mode != domain.ManagementModeManaged {
			continue
		}
		ref := domain.StableResourceRef{Kind: domain.ResourceRole, LogicalID: role.LogicalID}
		match, exists := matched[ref]
		if !exists {
			if conflict := unmanagedNameConflict(ref, role.Name, 0, snapshot); conflict != nil {
				conflicts = append(conflicts, *conflict)
				continue
			}
			if err := appendOperation(domain.PlanOperation{Kind: domain.OperationCreate, Resource: ref, Preconditions: domain.Preconditions{ExpectedAbsent: true}, Payload: domain.OperationPayload{Role: &role}}); err != nil {
				return nil, nil, err
			}
			continue
		}
		observed := findObservedRole(snapshot.ObservedRoles, domain.RoleID(match.RemoteID))
		if observed == nil {
			return nil, nil, fmt.Errorf("matched role %s is absent from observations", ref.LogicalID)
		}
		if observed.Name != role.Name || observed.Permissions != role.Permissions || observed.Color != role.Color || observed.Hoist != role.Hoist || observed.Mentionable != role.Mentionable {
			if err := appendOperation(domain.PlanOperation{Kind: domain.OperationUpdate, Resource: ref, Preconditions: resourcePreconditions(match, snapshot), Payload: domain.OperationPayload{Role: &role}}); err != nil {
				return nil, nil, err
			}
		}
		if observed.Position != role.Position {
			if err := appendOperation(domain.PlanOperation{Kind: domain.OperationMove, Resource: ref, Preconditions: resourcePreconditions(match, snapshot), Payload: domain.OperationPayload{Role: &role}}); err != nil {
				return nil, nil, err
			}
		}
	}
	for index := range desired.Channels {
		channel := desired.Channels[index]
		if channel.Mode != domain.ManagementModeManaged {
			continue
		}
		ref := domain.StableResourceRef{Kind: domain.ResourceChannel, LogicalID: channel.LogicalID}
		match, exists := matched[ref]
		if !exists {
			if conflict := unmanagedNameConflict(ref, channel.Name, int(channel.Type), snapshot); conflict != nil {
				conflicts = append(conflicts, *conflict)
				continue
			}
			if err := appendOperation(domain.PlanOperation{Kind: domain.OperationCreate, Resource: ref, Preconditions: domain.Preconditions{ExpectedAbsent: true}, Payload: domain.OperationPayload{Channel: &channel}}); err != nil {
				return nil, nil, err
			}
			continue
		}
		observed := findObservedChannel(snapshot.ObservedChannels, domain.ChannelID(match.RemoteID))
		if observed == nil {
			return nil, nil, fmt.Errorf("matched channel %s is absent from observations", ref.LogicalID)
		}
		if observed.Type != channel.Type {
			if err := appendReplacement(ref, resourcesByID[match.RemoteID], domain.OperationPayload{Channel: &channel}, &operations, appendOperation); err != nil {
				return nil, nil, err
			}
			continue
		}
		if channelMutableChanged(channel, *observed) {
			if err := appendOperation(domain.PlanOperation{Kind: domain.OperationUpdate, Resource: ref, Preconditions: resourcePreconditions(match, snapshot), Payload: domain.OperationPayload{Channel: &channel}}); err != nil {
				return nil, nil, err
			}
		}
		expectedParent := domain.ChannelID("")
		if channel.Category != nil {
			if categoryMatch, ok := matched[*channel.Category]; ok {
				expectedParent = domain.ChannelID(categoryMatch.RemoteID)
			}
		}
		actualParent := domain.ChannelID("")
		if observed.ParentID != nil {
			actualParent = *observed.ParentID
		}
		if observed.Position != channel.Position || expectedParent != actualParent {
			if err := appendOperation(domain.PlanOperation{Kind: domain.OperationMove, Resource: ref, Preconditions: resourcePreconditions(match, snapshot), Payload: domain.OperationPayload{Channel: &channel}}); err != nil {
				return nil, nil, err
			}
		}
		if overwritesChanged(channel.Overwrites, observed.PermissionOverwrites, matched) {
			if err := appendOperation(domain.PlanOperation{Kind: domain.OperationOverwrite, Resource: ref, Preconditions: resourcePreconditions(match, snapshot), Payload: domain.OperationPayload{Channel: &channel}}); err != nil {
				return nil, nil, err
			}
		}
	}
	if containsMove(operations, domain.ResourceRole) {
		order := desiredRoleOrder(desired.Roles)
		if err := appendOperation(domain.PlanOperation{Kind: domain.OperationReorder, Resource: domain.StableResourceRef{Kind: domain.ResourceRole, LogicalID: "guild-role-order"}, Payload: domain.OperationPayload{Order: order}}); err != nil {
			return nil, nil, err
		}
	}
	if containsMove(operations, domain.ResourceCategory) || containsMove(operations, domain.ResourceChannel) {
		order := desiredChannelOrder(desired.Categories, desired.Channels)
		if err := appendOperation(domain.PlanOperation{Kind: domain.OperationReorder, Resource: domain.StableResourceRef{Kind: domain.ResourceChannel, LogicalID: "guild-channel-order"}, Payload: domain.OperationPayload{Order: order}}); err != nil {
			return nil, nil, err
		}
	}
	for index := range desired.Webhooks {
		webhook := desired.Webhooks[index]
		if webhook.Mode != domain.ManagementModeManaged {
			continue
		}
		ref := domain.StableResourceRef{Kind: domain.ResourceWebhook, LogicalID: webhook.LogicalID}
		match, exists := matched[ref]
		if !exists {
			if conflict := unmanagedNameConflict(ref, webhook.Name, 1, snapshot); conflict != nil {
				conflicts = append(conflicts, *conflict)
				continue
			}
			if err := appendOperation(domain.PlanOperation{Kind: domain.OperationCreate, Resource: ref, Preconditions: domain.Preconditions{ExpectedAbsent: true}, Payload: domain.OperationPayload{Webhook: &webhook}}); err != nil {
				return nil, nil, err
			}
			continue
		}
		observed := findObservedWebhook(snapshot.ObservedWebhooks, domain.WebhookID(match.RemoteID))
		if observed == nil {
			return nil, nil, fmt.Errorf("matched webhook %s is absent from observations", ref.LogicalID)
		}
		expectedChannel := domain.ChannelID("")
		if channelMatch, ok := matched[webhook.Channel]; ok {
			expectedChannel = domain.ChannelID(channelMatch.RemoteID)
		}
		if stringValue(observed.Name) != webhook.Name || observed.ChannelID == nil || *observed.ChannelID != expectedChannel {
			if err := appendOperation(domain.PlanOperation{Kind: domain.OperationUpdate, Resource: ref, Preconditions: resourcePreconditions(match, snapshot), Payload: domain.OperationPayload{Webhook: &webhook}}); err != nil {
				return nil, nil, err
			}
		}
	}
	for index := range desired.ApplicationCommands {
		command := desired.ApplicationCommands[index]
		if command.Mode != domain.ManagementModeManaged {
			continue
		}
		ref := domain.StableResourceRef{Kind: domain.ResourceApplicationCommand, LogicalID: command.LogicalID}
		match, exists := matched[ref]
		if !exists {
			if err := appendOperation(domain.PlanOperation{Kind: domain.OperationCreate, Resource: ref, Preconditions: domain.Preconditions{ExpectedAbsent: true}, Payload: domain.OperationPayload{ApplicationCommand: &command}}); err != nil {
				return nil, nil, err
			}
			continue
		}
		observed := findObservedCommand(snapshot.ObservedCommands, domain.ApplicationCommandID(match.RemoteID))
		if observed == nil {
			return nil, nil, fmt.Errorf("matched application command %s is absent", ref.LogicalID)
		}
		if commandChanged(command, *observed) {
			if err := appendOperation(domain.PlanOperation{Kind: domain.OperationUpdate, Resource: ref, Preconditions: resourcePreconditions(match, snapshot), Payload: domain.OperationPayload{ApplicationCommand: &command}}); err != nil {
				return nil, nil, err
			}
		}
	}
	if desired.Onboarding != nil {
		ref := domain.StableResourceRef{Kind: domain.ResourceOnboarding, LogicalID: "guild-onboarding"}
		if snapshotCapability(snapshot, domain.ResourceOnboarding) == domain.SupportUnavailable {
			conflicts = append(conflicts, domain.PlanConflict{Kind: domain.ConflictUnsupportedField, Resource: ref, Message: "guild onboarding is unavailable"})
		} else if onboardingChanged(*desired.Onboarding, snapshot.Onboarding, matched) {
			onboarding := *desired.Onboarding
			if err := appendOperation(domain.PlanOperation{Kind: domain.OperationUpdate, Resource: ref, Preconditions: domain.Preconditions{SnapshotHash: snapshot.Metadata.CanonicalHash}, Payload: domain.OperationPayload{Onboarding: &onboarding}}); err != nil {
				return nil, nil, err
			}
		}
	}

	desiredRefs := make(map[domain.StableResourceRef]struct{})
	for _, candidate := range desiredCandidates(desired) {
		desiredRefs[candidate.ref] = struct{}{}
	}
	for _, resource := range allSnapshotResources(snapshot) {
		if !resource.Managed {
			continue
		}
		if _, present := desiredRefs[resource.Ref]; present {
			continue
		}
		if !desired.Ownership.DeleteOrphans {
			continue
		}
		if resource.Protected || desired.Ownership.ProtectDeletes {
			conflicts = append(conflicts, domain.PlanConflict{Kind: domain.ConflictProtectedDelete, Resource: resource.Ref, Message: "managed resource is protected from deletion"})
			continue
		}
		if err := appendOperation(domain.PlanOperation{Kind: domain.OperationDelete, Resource: resource.Ref, Preconditions: domain.Preconditions{SnapshotHash: snapshot.Metadata.CanonicalHash, ResourceRemoteID: resource.RemoteID, ResourceFingerprint: resource.Fingerprint}}); err != nil {
			return nil, nil, err
		}
	}
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Resource != conflicts[j].Resource {
			return refLess(conflicts[i].Resource, conflicts[j].Resource)
		}
		return conflicts[i].Kind < conflicts[j].Kind
	})
	return operations, conflicts, nil
}

func appendReplacement(ref domain.StableResourceRef, resource domain.SnapshotResource, payload domain.OperationPayload, operations *[]domain.PlanOperation, appendOperation func(domain.PlanOperation) error) error {
	if resource.Protected {
		return nil
	}
	return appendOperation(domain.PlanOperation{Kind: domain.OperationReplace, Resource: ref, Preconditions: domain.Preconditions{ResourceRemoteID: resource.RemoteID, ResourceFingerprint: resource.Fingerprint}, Payload: payload})
}

func resourcePreconditions(match ResourceMatch, snapshot domain.GuildSnapshot) domain.Preconditions {
	return domain.Preconditions{SnapshotHash: snapshot.Metadata.CanonicalHash, ResourceRemoteID: match.RemoteID, ResourceFingerprint: match.Fingerprint}
}
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func channelMutableChanged(desired domain.DesiredChannel, observed domain.Channel) bool {
	return stringValue(observed.Name) != desired.Name || !reflect.DeepEqual(observed.Topic, desired.Topic) || !reflect.DeepEqual(observed.NSFW, desired.NSFW) || !reflect.DeepEqual(observed.RateLimitPerUser, desired.RateLimitPerUser) || !reflect.DeepEqual(observed.Bitrate, desired.Bitrate) || !reflect.DeepEqual(observed.UserLimit, desired.UserLimit) || !reflect.DeepEqual(observed.RTCRegion, desired.RTCRegion) || !reflect.DeepEqual(observed.VideoQualityMode, desired.VideoQualityMode) || !reflect.DeepEqual(observed.DefaultAutoArchiveDuration, desired.DefaultAutoArchive) || !reflect.DeepEqual(observed.DefaultThreadRateLimitPerUser, desired.DefaultThreadRateLimit) || forumChanged(desired.Forum, observed)
}
func forumChanged(desired *domain.DesiredForumSettings, observed domain.Channel) bool {
	if desired == nil {
		return false
	}
	return !reflect.DeepEqual(desired.AvailableTags, observed.AvailableTags) || !reflect.DeepEqual(desired.DefaultReactionEmoji, observed.DefaultReactionEmoji) || !reflect.DeepEqual(desired.DefaultSortOrder, observed.DefaultSortOrder) || !reflect.DeepEqual(desired.DefaultForumLayout, observed.DefaultForumLayout)
}
func overwritesChanged(desired []domain.DesiredOverwrite, observed []domain.PermissionOverwrite, matches map[domain.StableResourceRef]ResourceMatch) bool {
	if len(desired) == 0 && len(observed) == 0 {
		return false
	}
	expected := make([]domain.PermissionOverwrite, 0, len(desired))
	for _, overwrite := range desired {
		match, ok := matches[overwrite.Target]
		if !ok {
			return true
		}
		expected = append(expected, domain.PermissionOverwrite{ID: match.RemoteID, Type: overwrite.Type, Allow: overwrite.Allow, Deny: overwrite.Deny})
	}
	sort.Slice(expected, func(i, j int) bool { return expected[i].ID < expected[j].ID })
	actual := append([]domain.PermissionOverwrite(nil), observed...)
	sort.Slice(actual, func(i, j int) bool { return actual[i].ID < actual[j].ID })
	return !reflect.DeepEqual(expected, actual)
}
func commandChanged(desired domain.DesiredApplicationCommand, observed domain.ApplicationCommand) bool {
	return desired.Type != observed.Type || desired.Name != observed.Name || desired.Description != observed.Description || !equalCommandOptions(desired.Options, observed.Options) || !reflect.DeepEqual(desired.DefaultMemberPermissions, observed.DefaultMemberPermissions) || !reflect.DeepEqual(desired.NSFW, observed.NSFW)
}
func equalCommandOptions(left, right []domain.ApplicationCommandOption) bool {
	if len(left) == 0 && len(right) == 0 {
		return true
	}
	return reflect.DeepEqual(left, right)
}
func onboardingChanged(desired domain.DesiredOnboarding, observed *domain.Onboarding, matches map[domain.StableResourceRef]ResourceMatch) bool {
	if observed == nil || desired.Enabled != observed.Enabled || desired.Mode != observed.Mode || len(desired.Prompts) != len(observed.Prompts) {
		return true
	}
	expectedChannels := make([]domain.ChannelID, 0, len(desired.DefaultChannelIDs))
	for _, ref := range desired.DefaultChannelIDs {
		match, ok := matches[ref]
		if !ok {
			return true
		}
		expectedChannels = append(expectedChannels, domain.ChannelID(match.RemoteID))
	}
	sort.Slice(expectedChannels, func(i, j int) bool { return expectedChannels[i] < expectedChannels[j] })
	actual := append([]domain.ChannelID(nil), observed.DefaultChannelIDs...)
	sort.Slice(actual, func(i, j int) bool { return actual[i] < actual[j] })
	return !reflect.DeepEqual(expectedChannels, actual)
}
func snapshotCapability(snapshot domain.GuildSnapshot, kind domain.ResourceKind) domain.SupportStatus {
	for _, capability := range snapshot.Capabilities {
		if capability.Resource == kind {
			return capability.Status
		}
	}
	return domain.SupportUnknown
}

func containsMove(operations []domain.PlanOperation, kind domain.ResourceKind) bool {
	for _, operation := range operations {
		if operation.Kind == domain.OperationMove && operation.Resource.Kind == kind {
			return true
		}
	}
	return false
}
func desiredRoleOrder(roles []domain.DesiredRole) []domain.StableResourceRef {
	values := append([]domain.DesiredRole(nil), roles...)
	sort.Slice(values, func(i, j int) bool {
		if values[i].Position != values[j].Position {
			return values[i].Position < values[j].Position
		}
		return values[i].LogicalID < values[j].LogicalID
	})
	result := make([]domain.StableResourceRef, 0, len(values))
	for _, role := range values {
		result = append(result, domain.StableResourceRef{Kind: domain.ResourceRole, LogicalID: role.LogicalID})
	}
	return result
}
func desiredChannelOrder(categories []domain.DesiredCategory, channels []domain.DesiredChannel) []domain.StableResourceRef {
	type ordered struct {
		position int
		ref      domain.StableResourceRef
	}
	values := make([]ordered, 0, len(categories)+len(channels))
	for _, category := range categories {
		values = append(values, ordered{category.Position, domain.StableResourceRef{Kind: domain.ResourceCategory, LogicalID: category.LogicalID}})
	}
	for _, channel := range channels {
		values = append(values, ordered{channel.Position, domain.StableResourceRef{Kind: domain.ResourceChannel, LogicalID: channel.LogicalID}})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].position != values[j].position {
			return values[i].position < values[j].position
		}
		return refLess(values[i].ref, values[j].ref)
	})
	result := make([]domain.StableResourceRef, 0, len(values))
	for _, value := range values {
		result = append(result, value.ref)
	}
	return result
}

func unmanagedNameConflict(ref domain.StableResourceRef, name string, resourceType int, snapshot domain.GuildSnapshot) *domain.PlanConflict {
	for _, resource := range allSnapshotResources(snapshot) {
		if !resource.Managed && resource.Ref.Kind == ref.Kind && resource.Name == name && resource.Type == resourceType {
			return &domain.PlanConflict{Kind: domain.ConflictUnmanagedResource, Resource: ref, Message: "an unmanaged resource already has the desired identity"}
		}
	}
	return nil
}
func findObservedChannel(values []domain.Channel, id domain.ChannelID) *domain.Channel {
	for index := range values {
		if values[index].ID == id {
			return &values[index]
		}
	}
	return nil
}
func findObservedRole(values []domain.Role, id domain.RoleID) *domain.Role {
	for index := range values {
		if values[index].ID == id {
			return &values[index]
		}
	}
	return nil
}
func findObservedWebhook(values []domain.Webhook, id domain.WebhookID) *domain.Webhook {
	for index := range values {
		if values[index].ID == id {
			return &values[index]
		}
	}
	return nil
}
func findObservedCommand(values []domain.ApplicationCommand, id domain.ApplicationCommandID) *domain.ApplicationCommand {
	for index := range values {
		if values[index].ID == id {
			return &values[index]
		}
	}
	return nil
}
