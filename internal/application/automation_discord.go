package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type CoreAutomationExecutor struct {
	Discord   *DiscordService
	Snapshots AutomationSnapshot
	Now       func() time.Time
}

func (e CoreAutomationExecutor) ExecuteAutomationAction(ctx context.Context, invocation Invocation, guild domain.GuildID, action domain.AutomationAction, content, stepID string) (AutomationActionResult, error) {
	if e.Discord == nil {
		return AutomationActionResult{}, errors.New("Discord service is unavailable")
	}
	mentions := &domain.AllowedMentions{Parse: []string{}, Roles: []domain.RoleID{}, Users: []domain.UserID{}}
	commandContext := domain.CommandContext{GuildID: guild, IdempotencyKey: domain.IdempotencyKey(stepID), ReconciliationKey: domain.ReconciliationKey(stepID)}
	switch action.Kind {
	case domain.ActionMessage, domain.ActionWelcome:
		execution, err := ExecuteAs(ctx, e.Discord, invocation, domain.CreateMessage{CommandContext: commandContext, ChannelID: domain.ChannelID(action.ChannelID), Content: &content, AllowedMentions: mentions})
		return automationExecutionError(execution.Partial, err)
	case domain.ActionForumThread, domain.ActionMediaThread:
		execution, err := ExecuteAs(ctx, e.Discord, invocation, domain.StartForumThread{CommandContext: commandContext, ChannelID: domain.ChannelID(action.ChannelID), Name: action.Title, Content: content, AllowedMentions: mentions})
		return automationExecutionError(execution.Partial, err)
	case domain.ActionPoll:
		answers := make([]domain.PollAnswer, len(action.Choices))
		for i, choice := range action.Choices {
			answers[i] = domain.PollAnswer{Text: choice}
		}
		execution, err := ExecuteAs(ctx, e.Discord, invocation, domain.CreatePoll{CommandContext: commandContext, ChannelID: domain.ChannelID(action.ChannelID), Content: &content, Question: action.Title, Answers: answers, DurationHours: 24, AllowedMentions: mentions})
		return automationExecutionError(execution.Partial, err)
	case domain.ActionWebhook:
		execution, err := ExecuteAs(ctx, e.Discord, invocation, domain.ExecuteConfiguredWebhook{CommandContext: commandContext, Alias: action.Webhook, Content: content, AllowedMentions: mentions})
		return automationExecutionError(execution.Partial, err)
	case domain.ActionRoleAdd:
		execution, err := ExecuteAs(ctx, e.Discord, invocation, domain.AddMemberRole{CommandContext: commandContext, UserID: domain.UserID(action.UserID), RoleID: domain.RoleID(action.RoleID)})
		return automationExecutionError(execution.Partial, err)
	case domain.ActionRoleRemove:
		execution, err := ExecuteAs(ctx, e.Discord, invocation, domain.RemoveMemberRole{CommandContext: commandContext, UserID: domain.UserID(action.UserID), RoleID: domain.RoleID(action.RoleID)})
		return automationExecutionError(execution.Partial, err)
	case domain.ActionCleanup:
		return e.cleanup(ctx, invocation, commandContext, action, content)
	case domain.ActionSnapshot:
		if e.Snapshots == nil {
			return AutomationActionResult{}, errors.New("snapshot service is unavailable")
		}
		return automationActionError(e.Snapshots.Snapshot(ctx, guild))
	default:
		return AutomationActionResult{}, errors.New("unsupported automation action")
	}
}

func (e CoreAutomationExecutor) PreviewAutomationAction(ctx context.Context, invocation Invocation, guild domain.GuildID, action domain.AutomationAction) (string, error) {
	if action.Kind != domain.ActionCleanup {
		return "", errors.New("automation action has no preview")
	}
	commandContext := domain.CommandContext{GuildID: guild}
	messages, err := e.cleanupCandidates(ctx, invocation, commandContext, action)
	if err != nil {
		return "", err
	}
	return cleanupPreview(messages), nil
}

func (e CoreAutomationExecutor) cleanup(ctx context.Context, invocation Invocation, commandContext domain.CommandContext, action domain.AutomationAction, reviewed string) (AutomationActionResult, error) {
	messages, err := e.cleanupCandidates(ctx, invocation, commandContext, action)
	if err != nil {
		return automationActionError(err)
	}
	if reviewed != cleanupPreview(messages) {
		return AutomationActionResult{ProvenUnsent: true, ReviewChanged: true, Rendered: cleanupPreview(messages)}, errors.New("cleanup candidate set changed; review again")
	}
	for index, message := range messages {
		deleteContext := commandContext
		deleteContext.IdempotencyKey = domain.IdempotencyKey(string(commandContext.IdempotencyKey) + "-" + string(message.ID))
		deleteContext.ReconciliationKey = domain.ReconciliationKey(string(commandContext.ReconciliationKey) + "-" + string(message.ID))
		execution, err := ExecuteAs(ctx, e.Discord, invocation, domain.DeleteMessage{CommandContext: deleteContext, ChannelID: domain.ChannelID(action.ChannelID), MessageID: message.ID})
		if err != nil {
			if index > 0 {
				return AutomationActionResult{Uncertain: true}, err
			}
			return automationExecutionError(execution.Partial, err)
		}
	}
	return AutomationActionResult{}, nil
}

func cleanupPreview(messages []domain.Message) string {
	hash := sha256.New()
	for _, message := range messages {
		_, _ = hash.Write([]byte(message.ID))
		_, _ = hash.Write([]byte{0})
	}
	return fmt.Sprintf("cleanup-candidates:%d;set:%s", len(messages), hex.EncodeToString(hash.Sum(nil)))
}

func (e CoreAutomationExecutor) cleanupCandidates(ctx context.Context, invocation Invocation, commandContext domain.CommandContext, action domain.AutomationAction) ([]domain.Message, error) {
	limit := action.Limit
	messages, err := ExecuteAs(ctx, e.Discord, invocation, domain.ListMessages{CommandContext: commandContext, ChannelID: domain.ChannelID(action.ChannelID), Limit: &limit})
	if err != nil {
		var retryExhausted *domain.RetryExhaustedError
		var transport *domain.TransportError
		var cancellation *domain.CancellationError
		if errors.As(err, &retryExhausted) || errors.As(err, &transport) || errors.As(err, &cancellation) {
			return nil, &automationRetryableReadError{err}
		}
		return nil, err
	}
	age, err := time.ParseDuration(action.Age)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if e.Now != nil {
		now = e.Now()
	}
	eligible := make([]domain.Message, 0, action.Limit)
	for _, message := range messages.Result {
		if len(eligible) == action.Limit || message.Pinned || message.Timestamp.After(now.Add(-age)) {
			continue
		}
		eligible = append(eligible, message)
	}
	return eligible, nil
}

type automationRetryableReadError struct{ error }

func automationExecutionError(partial *domain.PartialResult, err error) (AutomationActionResult, error) {
	if err == nil {
		return AutomationActionResult{}, nil
	}
	if partial != nil {
		switch partial.Outcome {
		case domain.PartialNotSent:
			return AutomationActionResult{ProvenUnsent: true, Retryable: true}, err
		case domain.PartialSentUnknown:
			return AutomationActionResult{Uncertain: true}, err
		}
	}
	return automationActionError(err)
}

func automationActionError(err error) (AutomationActionResult, error) {
	if err == nil {
		return AutomationActionResult{}, nil
	}
	var transport *domain.TransportError
	if errors.As(err, &transport) {
		return AutomationActionResult{Uncertain: transport.Uncertain, ProvenUnsent: !transport.Uncertain, Retryable: !transport.Uncertain}, err
	}
	var cancellation *domain.CancellationError
	if errors.As(err, &cancellation) {
		return AutomationActionResult{Uncertain: true}, err
	}
	return AutomationActionResult{}, err
}
