package application

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type operationExecution struct {
	remoteID  domain.Snowflake
	attempts  int
	uncertain bool
}

func (s *ReconciliationService) runOperation(ctx context.Context, journal ReconciliationJournal, runID domain.RunID, plan domain.Plan, operation domain.PlanOperation, bindings map[domain.StableResourceRef]domain.ResourceBinding, invocation Invocation) (domain.ApplyStepResult, error) {
	started := s.clock.Now()
	resource := operation.Resource
	if _, err := journal.Append(domain.JournalEvent{RunID: runID, Type: domain.JournalStepStarted, OperationID: operation.ID, Resource: &resource, State: domain.StepStarted, OccurredAt: started}); err != nil {
		return domain.ApplyStepResult{OperationID: operation.ID, Resource: resource, State: domain.StepFailed}, err
	}
	execution, err := s.executeOperation(ctx, plan, operation, bindings, invocation)
	step := domain.ApplyStepResult{OperationID: operation.ID, Resource: resource, State: domain.StepCompleted, RemoteID: execution.remoteID, Attempt: execution.attempts}
	if err != nil {
		state := domain.StepFailed
		if execution.uncertain {
			state = domain.StepUncertain
		}
		step.State, step.ErrorCode = state, "discord_step_failed"
		_, journalErr := journal.Append(domain.JournalEvent{RunID: runID, Type: domain.JournalStepFailed, OperationID: operation.ID, Resource: &resource, State: state, OccurredAt: s.clock.Now(), ErrorCode: step.ErrorCode})
		if journalErr != nil {
			return step, journalErr
		}
		s.telemetry.ObserveReconciliation(ctx, ReconciliationObservation{CorrelationID: invocation.CorrelationID, PlanHashPrefix: prefix(string(plan.Hash)), RunID: runID, OperationID: operation.ID, ResourceKind: operation.Resource.Kind, Risk: operation.Risk, Attempt: execution.attempts, Outcome: "failed", Partial: execution.uncertain, Latency: s.clock.Now().Sub(started)})
		return step, err
	}
	if operation.Kind == domain.OperationDelete {
		delete(bindings, operation.Resource)
	} else if execution.remoteID != "" {
		bindings[operation.Resource] = domain.ResourceBinding{Ref: operation.Resource, RemoteID: execution.remoteID, Fingerprint: operation.PayloadFingerprint}
	}
	if _, err := journal.Append(domain.JournalEvent{RunID: runID, Type: domain.JournalStepComplete, OperationID: operation.ID, Resource: &resource, State: domain.StepCompleted, RemoteID: execution.remoteID, Fingerprint: operation.PayloadFingerprint, OccurredAt: s.clock.Now()}); err != nil {
		step.State = domain.StepUncertain
		return step, err
	}
	if err := s.saveBindingMap(plan.Binding.GuildID, bindings); err != nil {
		step.State = domain.StepUncertain
		return step, err
	}
	s.telemetry.ObserveReconciliation(ctx, ReconciliationObservation{CorrelationID: invocation.CorrelationID, PlanHashPrefix: prefix(string(plan.Hash)), RunID: runID, OperationID: operation.ID, ResourceKind: operation.Resource.Kind, Risk: operation.Risk, Attempt: execution.attempts, Outcome: "completed", Retries: max(0, execution.attempts-1), Latency: s.clock.Now().Sub(started)})
	return step, nil
}

func (s *ReconciliationService) executeOperation(ctx context.Context, plan domain.Plan, operation domain.PlanOperation, bindings map[domain.StableResourceRef]domain.ResourceBinding, invocation Invocation) (operationExecution, error) {
	reason := "reconciliation " + prefix(string(plan.Hash)) + " step " + string(operation.ID)
	commandContext := domain.CommandContext{GuildID: plan.Binding.GuildID, AuditReason: &reason, ReconciliationKey: domain.ReconciliationKey(plan.Hash), IdempotencyKey: domain.IdempotencyKey(operation.ID)}
	remoteID := func(ref domain.StableResourceRef) (domain.Snowflake, error) {
		binding, ok := bindings[ref]
		if !ok || binding.RemoteID == "" {
			return "", fmt.Errorf("missing binding for %s/%s", ref.Kind, ref.LogicalID)
		}
		return binding.RemoteID, nil
	}
	currentID := domain.Snowflake("")
	if operation.Kind != domain.OperationCreate && operation.Kind != domain.OperationReorder && operation.Resource.Kind != domain.ResourceOnboarding {
		var err error
		currentID, err = remoteID(operation.Resource)
		if err != nil {
			if operation.Kind == domain.OperationDelete && operation.Preconditions.ResourceRemoteID != "" {
				currentID = operation.Preconditions.ResourceRemoteID
			} else {
				return operationExecution{}, err
			}
		}
	}

	switch operation.Kind {
	case domain.OperationNoop:
		return operationExecution{}, nil
	case domain.OperationCreate:
		return s.executeCreate(ctx, plan, operation, bindings, invocation, commandContext)
	case domain.OperationReplace:
		deleted, err := s.executeDelete(ctx, plan, operation, currentID, invocation, commandContext)
		if err != nil {
			return deleted, err
		}
		created, err := s.executeCreate(ctx, plan, operation, bindings, invocation, commandContext)
		created.attempts += deleted.attempts
		return created, err
	case domain.OperationDelete:
		return s.executeDelete(ctx, plan, operation, currentID, invocation, commandContext)
	case domain.OperationReorder:
		if operation.Resource.Kind == domain.ResourceRole {
			positions := make([]domain.RolePosition, 0, len(operation.Payload.Order))
			for position, ref := range operation.Payload.Order {
				id, err := remoteID(ref)
				if err != nil {
					return operationExecution{}, err
				}
				positions = append(positions, domain.RolePosition{ID: domain.RoleID(id), Position: position})
			}
			result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.ReorderRoles{CommandContext: commandContext, Positions: positions})
			_ = result
			return operationExecution{attempts: attempts, uncertain: uncertain}, err
		}
		positions := make([]domain.ChannelPosition, 0, len(operation.Payload.Order))
		for position, ref := range operation.Payload.Order {
			id, err := remoteID(ref)
			if err != nil {
				return operationExecution{}, err
			}
			value := position
			positions = append(positions, domain.ChannelPosition{ID: domain.ChannelID(id), Position: &value})
		}
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.ReorderChannels{CommandContext: commandContext, Positions: positions})
		_ = result
		return operationExecution{attempts: attempts, uncertain: uncertain}, err
	case domain.OperationMove:
		if operation.Payload.Role != nil {
			position := operation.Payload.Role.Position
			result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.ReorderRoles{CommandContext: commandContext, Positions: []domain.RolePosition{{ID: domain.RoleID(currentID), Position: position}}})
			_ = result
			return operationExecution{remoteID: currentID, attempts: attempts, uncertain: uncertain}, err
		}
		position := 0
		var parentID *domain.ChannelID
		if operation.Payload.Category != nil {
			position = operation.Payload.Category.Position
		}
		if operation.Payload.Channel != nil {
			position = operation.Payload.Channel.Position
			if operation.Payload.Channel.Category != nil {
				id, err := remoteID(*operation.Payload.Channel.Category)
				if err != nil {
					return operationExecution{}, err
				}
				value := domain.ChannelID(id)
				parentID = &value
			}
		}
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.ReorderChannels{CommandContext: commandContext, Positions: []domain.ChannelPosition{{ID: domain.ChannelID(currentID), Position: &position, ParentID: parentID}}})
		_ = result
		return operationExecution{remoteID: currentID, attempts: attempts, uncertain: uncertain}, err
	case domain.OperationOverwrite:
		if operation.Payload.Channel == nil {
			return operationExecution{}, fmt.Errorf("overwrite operation is missing channel payload")
		}
		overwrites, err := resolveOverwrites(operation.Payload.Channel.Overwrites, bindings)
		if err != nil {
			return operationExecution{}, err
		}
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.ModifyChannel{CommandContext: commandContext, ChannelID: domain.ChannelID(currentID), PermissionOverwrites: &overwrites})
		return operationExecution{remoteID: domain.Snowflake(result.ID), attempts: attempts, uncertain: uncertain}, err
	case domain.OperationUpdate:
		return s.executeUpdate(ctx, plan, operation, currentID, bindings, invocation, commandContext)
	default:
		return operationExecution{}, fmt.Errorf("unsupported reconciliation operation %s", operation.Kind)
	}
}

func (s *ReconciliationService) executeCreate(ctx context.Context, plan domain.Plan, operation domain.PlanOperation, bindings map[domain.StableResourceRef]domain.ResourceBinding, invocation Invocation, commandContext domain.CommandContext) (operationExecution, error) {
	if category := operation.Payload.Category; category != nil {
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.CreateChannel{CommandContext: commandContext, Name: category.Name, Type: 4})
		return operationExecution{remoteID: domain.Snowflake(result.ID), attempts: attempts, uncertain: uncertain}, err
	}
	if channel := operation.Payload.Channel; channel != nil {
		var parent *domain.ChannelID
		if channel.Category != nil {
			binding, ok := bindings[*channel.Category]
			if !ok {
				return operationExecution{}, fmt.Errorf("missing category binding")
			}
			value := domain.ChannelID(binding.RemoteID)
			parent = &value
		}
		overwrites, err := resolveOverwrites(channel.Overwrites, bindings)
		if err != nil {
			return operationExecution{}, err
		}
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.CreateChannel{CommandContext: commandContext, Name: channel.Name, Type: channel.Type, Topic: channel.Topic, ParentID: parent, NSFW: channel.NSFW, RateLimitPerUser: channel.RateLimitPerUser, Bitrate: channel.Bitrate, UserLimit: channel.UserLimit, RTCRegion: channel.RTCRegion, VideoQualityMode: channel.VideoQualityMode, DefaultAutoArchiveDuration: channel.DefaultAutoArchive, DefaultThreadRateLimitPerUser: channel.DefaultThreadRateLimit, AvailableTags: forumTags(channel), DefaultReactionEmoji: forumReaction(channel), DefaultSortOrder: forumSort(channel), DefaultForumLayout: forumLayout(channel), PermissionOverwrites: overwrites})
		return operationExecution{remoteID: domain.Snowflake(result.ID), attempts: attempts, uncertain: uncertain}, err
	}
	if role := operation.Payload.Role; role != nil {
		permissions, color, hoist, mentionable := role.Permissions, role.Color, role.Hoist, role.Mentionable
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.CreateRole{CommandContext: commandContext, Name: role.Name, Permissions: &permissions, Color: &color, Hoist: &hoist, Mentionable: &mentionable})
		return operationExecution{remoteID: domain.Snowflake(result.ID), attempts: attempts, uncertain: uncertain}, err
	}
	if webhook := operation.Payload.Webhook; webhook != nil {
		channel, ok := bindings[webhook.Channel]
		if !ok {
			return operationExecution{}, fmt.Errorf("missing webhook channel binding")
		}
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.CreateWebhook{CommandContext: commandContext, ChannelID: domain.ChannelID(channel.RemoteID), Name: webhook.Name})
		return operationExecution{remoteID: domain.Snowflake(result.ID), attempts: attempts, uncertain: uncertain}, err
	}
	if command := operation.Payload.ApplicationCommand; command != nil {
		input := applicationCommandInput(*command)
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.CreateGuildApplicationCommand{CommandContext: commandContext, ApplicationID: plan.Binding.ApplicationID, Command: input})
		return operationExecution{remoteID: domain.Snowflake(result.ID), attempts: attempts, uncertain: uncertain}, err
	}
	if onboarding := operation.Payload.Onboarding; onboarding != nil {
		command, err := onboardingCommand(commandContext, *onboarding, bindings)
		if err != nil {
			return operationExecution{}, err
		}
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, command)
		return operationExecution{remoteID: domain.Snowflake(result.GuildID), attempts: attempts, uncertain: uncertain}, err
	}
	return operationExecution{}, fmt.Errorf("create operation has no typed payload")
}

func (s *ReconciliationService) executeUpdate(ctx context.Context, plan domain.Plan, operation domain.PlanOperation, currentID domain.Snowflake, bindings map[domain.StableResourceRef]domain.ResourceBinding, invocation Invocation, commandContext domain.CommandContext) (operationExecution, error) {
	if category := operation.Payload.Category; category != nil {
		name := category.Name
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.ModifyChannel{CommandContext: commandContext, ChannelID: domain.ChannelID(currentID), Name: &name})
		return operationExecution{remoteID: domain.Snowflake(result.ID), attempts: attempts, uncertain: uncertain}, err
	}
	if channel := operation.Payload.Channel; channel != nil {
		name, channelType := channel.Name, channel.Type
		tags := forumTags(channel)
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.ModifyChannel{CommandContext: commandContext, ChannelID: domain.ChannelID(currentID), Name: &name, Type: &channelType, Topic: channel.Topic, NSFW: channel.NSFW, RateLimitPerUser: channel.RateLimitPerUser, Bitrate: channel.Bitrate, UserLimit: channel.UserLimit, RTCRegion: channel.RTCRegion, VideoQualityMode: channel.VideoQualityMode, DefaultAutoArchiveDuration: channel.DefaultAutoArchive, DefaultThreadRateLimitPerUser: channel.DefaultThreadRateLimit, AvailableTags: &tags, DefaultReactionEmoji: forumReaction(channel), DefaultSortOrder: forumSort(channel), DefaultForumLayout: forumLayout(channel)})
		return operationExecution{remoteID: domain.Snowflake(result.ID), attempts: attempts, uncertain: uncertain}, err
	}
	if role := operation.Payload.Role; role != nil {
		name, permissions, color, hoist, mentionable := role.Name, role.Permissions, role.Color, role.Hoist, role.Mentionable
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.ModifyRole{CommandContext: commandContext, RoleID: domain.RoleID(currentID), Name: &name, Permissions: &permissions, Color: &color, Hoist: &hoist, Mentionable: &mentionable})
		return operationExecution{remoteID: domain.Snowflake(result.ID), attempts: attempts, uncertain: uncertain}, err
	}
	if webhook := operation.Payload.Webhook; webhook != nil {
		name := webhook.Name
		channelBinding, ok := bindings[webhook.Channel]
		if !ok {
			return operationExecution{}, fmt.Errorf("missing webhook channel binding")
		}
		channelID := domain.ChannelID(channelBinding.RemoteID)
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.ModifyWebhook{CommandContext: commandContext, WebhookID: domain.WebhookID(currentID), Name: &name, ChannelID: &channelID})
		return operationExecution{remoteID: domain.Snowflake(result.ID), attempts: attempts, uncertain: uncertain}, err
	}
	if command := operation.Payload.ApplicationCommand; command != nil {
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.ModifyGuildApplicationCommand{CommandContext: commandContext, ApplicationID: plan.Binding.ApplicationID, ApplicationCommandID: domain.ApplicationCommandID(currentID), Command: applicationCommandInput(*command)})
		return operationExecution{remoteID: domain.Snowflake(result.ID), attempts: attempts, uncertain: uncertain}, err
	}
	if onboarding := operation.Payload.Onboarding; onboarding != nil {
		command, err := onboardingCommand(commandContext, *onboarding, bindings)
		if err != nil {
			return operationExecution{}, err
		}
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, command)
		return operationExecution{remoteID: domain.Snowflake(result.GuildID), attempts: attempts, uncertain: uncertain}, err
	}
	return operationExecution{}, fmt.Errorf("update operation has no typed payload")
}

func (s *ReconciliationService) executeDelete(ctx context.Context, plan domain.Plan, operation domain.PlanOperation, currentID domain.Snowflake, invocation Invocation, commandContext domain.CommandContext) (operationExecution, error) {
	switch operation.Resource.Kind {
	case domain.ResourceCategory, domain.ResourceChannel:
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.DeleteChannel{CommandContext: commandContext, ChannelID: domain.ChannelID(currentID)})
		return operationExecution{remoteID: domain.Snowflake(result.ID), attempts: attempts, uncertain: uncertain}, err
	case domain.ResourceRole:
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.DeleteRole{CommandContext: commandContext, RoleID: domain.RoleID(currentID)})
		_ = result
		return operationExecution{remoteID: currentID, attempts: attempts, uncertain: uncertain}, err
	case domain.ResourceWebhook:
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.DeleteWebhook{CommandContext: commandContext, WebhookID: domain.WebhookID(currentID)})
		_ = result
		return operationExecution{remoteID: currentID, attempts: attempts, uncertain: uncertain}, err
	case domain.ResourceApplicationCommand:
		result, attempts, uncertain, err := executeReconciliationCommand(ctx, s.discord, invocation, domain.DeleteGuildApplicationCommand{CommandContext: commandContext, ApplicationID: plan.Binding.ApplicationID, ApplicationCommandID: domain.ApplicationCommandID(currentID)})
		_ = result
		return operationExecution{remoteID: currentID, attempts: attempts, uncertain: uncertain}, err
	default:
		return operationExecution{}, fmt.Errorf("unsupported delete resource %s", operation.Resource.Kind)
	}
}

func executeReconciliationCommand[R any](ctx context.Context, discord *DiscordService, invocation Invocation, command domain.DiscordCommand[R]) (R, int, bool, error) {
	execution, err := ExecuteAs(ctx, discord, invocation, command)
	uncertain := execution.Partial != nil && execution.Partial.Outcome == domain.PartialSentUnknown
	return execution.Result, execution.Attempts, uncertain, err
}

func resolveOverwrites(desired []domain.DesiredOverwrite, bindings map[domain.StableResourceRef]domain.ResourceBinding) ([]domain.PermissionOverwrite, error) {
	result := make([]domain.PermissionOverwrite, 0, len(desired))
	for _, overwrite := range desired {
		binding, ok := bindings[overwrite.Target]
		if !ok {
			return nil, fmt.Errorf("missing overwrite target binding")
		}
		result = append(result, domain.PermissionOverwrite{ID: binding.RemoteID, Type: overwrite.Type, Allow: overwrite.Allow, Deny: overwrite.Deny})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
func forumTags(channel *domain.DesiredChannel) []domain.ForumTag {
	if channel.Forum == nil {
		return []domain.ForumTag{}
	}
	tags := make([]domain.ForumTag, 0, len(channel.Forum.AvailableTags))
	for _, tag := range channel.Forum.AvailableTags {
		tags = append(tags, domain.ForumTag{Name: tag.Name, Moderated: tag.Moderated, EmojiName: tag.EmojiName})
	}
	return tags
}
func forumReaction(channel *domain.DesiredChannel) *domain.DefaultReaction {
	if channel.Forum == nil {
		return nil
	}
	return channel.Forum.DefaultReactionEmoji
}
func forumSort(channel *domain.DesiredChannel) *int {
	if channel.Forum == nil {
		return nil
	}
	return channel.Forum.DefaultSortOrder
}
func forumLayout(channel *domain.DesiredChannel) *int {
	if channel.Forum == nil {
		return nil
	}
	return channel.Forum.DefaultForumLayout
}
func applicationCommandInput(command domain.DesiredApplicationCommand) domain.ApplicationCommandInput {
	commandType := command.Type
	return domain.ApplicationCommandInput{Type: &commandType, Name: command.Name, Description: command.Description, Options: command.Options, DefaultMemberPermissions: command.DefaultMemberPermissions, NSFW: command.NSFW}
}
func onboardingCommand(context domain.CommandContext, desired domain.DesiredOnboarding, bindings map[domain.StableResourceRef]domain.ResourceBinding) (domain.ModifyGuildOnboarding, error) {
	result := domain.ModifyGuildOnboarding{CommandContext: context, Enabled: desired.Enabled, Mode: desired.Mode, Prompts: make([]domain.OnboardingPromptInput, 0, len(desired.Prompts)), DefaultChannelIDs: make([]domain.ChannelID, 0, len(desired.DefaultChannelIDs))}
	for _, ref := range desired.DefaultChannelIDs {
		binding, ok := bindings[ref]
		if !ok {
			return result, fmt.Errorf("missing onboarding channel binding")
		}
		result.DefaultChannelIDs = append(result.DefaultChannelIDs, domain.ChannelID(binding.RemoteID))
	}
	for _, prompt := range desired.Prompts {
		converted := domain.OnboardingPromptInput{Type: prompt.Type, Title: prompt.Title, SingleSelect: prompt.Single, Required: prompt.Required, InOnboarding: prompt.InOnboard, Options: make([]domain.OnboardingOptionInput, 0, len(prompt.Options))}
		for _, option := range prompt.Options {
			value := domain.OnboardingOptionInput{Title: option.Title, Description: option.Description, EmojiID: option.EmojiID, EmojiName: option.EmojiName, ChannelIDs: []domain.ChannelID{}, RoleIDs: []domain.RoleID{}}
			for _, ref := range option.Channels {
				binding, ok := bindings[ref]
				if !ok {
					return result, fmt.Errorf("missing onboarding channel binding")
				}
				value.ChannelIDs = append(value.ChannelIDs, domain.ChannelID(binding.RemoteID))
			}
			for _, ref := range option.Roles {
				binding, ok := bindings[ref]
				if !ok {
					return result, fmt.Errorf("missing onboarding role binding")
				}
				value.RoleIDs = append(value.RoleIDs, domain.RoleID(binding.RemoteID))
			}
			converted.Options = append(converted.Options, value)
		}
		result.Prompts = append(result.Prompts, converted)
	}
	return result, nil
}

func (s *ReconciliationService) saveBindingMap(guildID domain.GuildID, values map[domain.StableResourceRef]domain.ResourceBinding) error {
	bindings := make([]domain.ResourceBinding, 0, len(values))
	for _, binding := range values {
		bindings = append(bindings, binding)
	}
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].Ref.Kind != bindings[j].Ref.Kind {
			return bindings[i].Ref.Kind < bindings[j].Ref.Kind
		}
		return bindings[i].Ref.LogicalID < bindings[j].Ref.LogicalID
	})
	return s.state.SaveBindings(guildID, bindings)
}
func bindingMap(bindings []domain.ResourceBinding) map[domain.StableResourceRef]domain.ResourceBinding {
	result := make(map[domain.StableResourceRef]domain.ResourceBinding, len(bindings))
	for _, binding := range bindings {
		result[binding.Ref] = binding
	}
	return result
}
func replayStepStates(events []domain.JournalEvent) (map[domain.OperationID]bool, map[domain.OperationID]bool) {
	completed, started := map[domain.OperationID]bool{}, map[domain.OperationID]bool{}
	for _, event := range events {
		switch event.Type {
		case domain.JournalStepStarted:
			started[event.OperationID] = true
		case domain.JournalStepComplete:
			completed[event.OperationID] = true
			delete(started, event.OperationID)
		case domain.JournalStepFailed:
			if event.State != domain.StepUncertain {
				delete(started, event.OperationID)
			}
		}
	}
	return completed, started
}
func findOperation(operations []domain.PlanOperation, id domain.OperationID) *domain.PlanOperation {
	for index := range operations {
		if operations[index].ID == id {
			return &operations[index]
		}
	}
	return nil
}
func dependenciesComplete(operation domain.PlanOperation, completed map[domain.OperationID]bool) bool {
	for _, dependency := range operation.Dependencies {
		if !completed[dependency] {
			return false
		}
	}
	return true
}
func firstRunTime(events []domain.JournalEvent) time.Time {
	for _, event := range events {
		if event.Type == domain.JournalRunStarted {
			return event.OccurredAt
		}
	}
	return time.Time{}
}
func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
