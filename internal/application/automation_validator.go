package application

import (
	"errors"
	"fmt"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func ValidateAutomationWorkflow(workflow domain.AutomationWorkflow, catalog AutomationCatalog) error {
	if err := workflow.Validate(catalog.MaxSteps, catalog.MaxGuilds); err != nil {
		return err
	}
	if workflow.Trigger.Kind == domain.TriggerNews {
		if _, ok := catalog.Sources[workflow.Trigger.Source]; !ok {
			return errors.New("workflow references unknown configured source")
		}
	}
	actions, destructive := 0, 0
	for _, guildID := range workflow.Targets.GuildIDs {
		binding, ok := catalog.Guilds[guildID]
		if !ok {
			return fmt.Errorf("workflow guild %s is not configured", guildID)
		}
		for _, step := range workflow.Steps {
			actions++
			action := step.Action
			for _, condition := range step.Conditions {
				if condition.Kind == domain.ConditionMemberRole {
					if _, ok := binding.Roles[condition.RoleID]; !ok {
						return errors.New("workflow condition references role outside configured guild bindings")
					}
				}
			}
			if destructiveAutomationAction(action.Kind) {
				destructive++
			}
			if action.ChannelID != "" {
				if _, ok := binding.Channels[action.ChannelID]; !ok {
					return errors.New("workflow references channel outside configured guild bindings")
				}
			}
			if action.RoleID != "" {
				if _, ok := binding.Roles[action.RoleID]; !ok {
					return errors.New("workflow references role outside configured guild bindings")
				}
				if action.UserID == "" && workflow.Trigger.Kind != domain.TriggerMemberScan {
					return errors.New("dynamic role member requires member_scan trigger")
				}
			}
			if action.Kind == domain.ActionWebhook {
				webhook, ok := catalog.Webhooks[action.Webhook]
				if !ok || webhook.GuildID != guildID {
					return errors.New("workflow webhook is not bound to target guild")
				}
			}
			if action.Kind == domain.ActionRoleRemove && !action.RequireReview {
				return errors.New("role removal requires review")
			}
			if action.Kind == domain.ActionCleanup {
				if _, ok := catalog.CleanupChannels[guildID+":"+action.ChannelID]; !ok {
					return errors.New("cleanup channel is not allowlisted")
				}
				if !action.RequireReview {
					return errors.New("cleanup requires review")
				}
			}
			for _, transform := range step.Transforms {
				if transform.Kind == domain.TransformSummarize && !action.RequireReview {
					return errors.New("AI summary publication requires review")
				}
			}
		}
	}
	if actions > catalog.MaxActions || destructive > catalog.MaxDestructive {
		return errors.New("workflow cross-guild action scope exceeds configured cap")
	}
	return nil
}
