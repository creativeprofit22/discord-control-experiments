package interactions

import (
	"context"
	"fmt"
	"strings"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type Renderer struct{ callbacks *CallbackClient }

func NewRenderer(callbacks *CallbackClient) (*Renderer, error) {
	if callbacks == nil {
		return nil, fmt.Errorf("callback client is required")
	}
	return &Renderer{callbacks: callbacks}, nil
}
func (r *Renderer) Deliver(ctx context.Context, interaction VerifiedInteraction, result DeferredResult) error {
	return r.callbacks.EditOriginal(ctx, interaction.CorrelationID, interaction.Envelope.Token, r.Render(result))
}
func (r *Renderer) Render(result DeferredResult) CallbackData {
	unavailable := CallbackData{Content: "This operation is not available.", Flags: EphemeralFlag}
	if result.Outcome != "ok" {
		return unavailable
	}
	switch result.Kind {
	case "health":
		value, ok := result.Value.(application.InteractionHealthView)
		if !ok {
			return unavailable
		}
		return CallbackData{Content: bounded(fmt.Sprintf("Local: %s\nDiscord: %s", value.LocalStatus, value.DiscordStatus), 500), Flags: EphemeralFlag}
	case "policy":
		value, ok := result.Value.(application.InteractionPolicyView)
		if !ok {
			return unavailable
		}
		decision := "denied"
		if value.Allowed {
			decision = "allowed"
		}
		return CallbackData{Content: bounded(fmt.Sprintf("Capability `%s`: %s in `%s` mode.", value.Capability, decision, value.Mode), 500), Flags: EphemeralFlag}
	case "moderation_case":
		value, ok := result.Value.(domain.Case)
		if !ok {
			return unavailable
		}
		return CallbackData{Content: bounded(fmt.Sprintf("Case `%s`: %s (%s).", value.ID, value.Status, value.ReasonCode), 700), Flags: EphemeralFlag}
	case "moderation_action":
		value, ok := result.Value.(application.ModerationActionResult)
		if !ok {
			return unavailable
		}
		return CallbackData{Content: bounded(fmt.Sprintf("Action `%s`: %s.", value.Action.Kind, value.Action.Status), 700), Flags: EphemeralFlag}
	case "escalation":
		value, ok := result.Value.(application.EscalationResult)
		if !ok {
			return unavailable
		}
		if value.Recommendation.RuleID == "" {
			return CallbackData{Content: "No escalation rule currently matches.", Flags: EphemeralFlag}
		}
		return CallbackData{Content: bounded(fmt.Sprintf("Recommendation: `%s` via rule `%s` (policy `%s`).", value.Recommendation.Action, value.Recommendation.RuleID, value.Recommendation.PolicyVersion), 900), Flags: EphemeralFlag}
	case "lockdown_plan":
		value, ok := result.Value.(map[string]any)
		if !ok {
			return unavailable
		}
		plan, ok := value["plan"].(domain.LockdownPlan)
		if !ok {
			return unavailable
		}
		return CallbackData{Content: bounded(fmt.Sprintf("Lockdown plan `%s` has %d operations. Recovery is persisted.", plan.Hash, len(plan.Operations)), 1000), Flags: EphemeralFlag}
	case "lockdown_apply":
		value, ok := result.Value.(application.LockdownApplyResult)
		if !ok {
			return unavailable
		}
		return CallbackData{Content: bounded(fmt.Sprintf("Lockdown `%s`: %d completed, %d pending.", value.PlanHash, len(value.Completed), len(value.Pending)), 1000), Flags: EphemeralFlag}
	case "jobs_list":
		value, ok := result.Value.(JobListResult)
		if !ok {
			return unavailable
		}
		if len(value.Jobs) == 0 {
			return CallbackData{Content: "No jobs are available in this server.", Flags: EphemeralFlag}
		}
		lines := make([]string, 0, min(len(value.Jobs), 20))
		for index, job := range value.Jobs {
			if index == 20 {
				break
			}
			lines = append(lines, fmt.Sprintf("`%s` — %s (%d/%d)", bounded(string(job.RunID), 64), job.Status, job.CompletedSteps, job.TotalSteps))
		}
		data := CallbackData{Content: bounded(strings.Join(lines, "\n"), 1900), Flags: EphemeralFlag}
		if opaqueCustomID.MatchString(value.SelectCustomID) && len(value.Options) > 0 {
			options := make([]SelectOption, 0, len(value.Options))
			for _, option := range value.Options {
				if !opaqueCustomID.MatchString(option.Value) {
					return unavailable
				}
				options = append(options, SelectOption{Label: bounded(string(option.Job.RunID), 100), Value: option.Value, Description: bounded(string(option.Job.Status), 100)})
			}
			data.Components = []Component{{Type: 1, Components: []Component{{Type: 3, CustomID: value.SelectCustomID, Placeholder: "Inspect a job", MinValues: 1, MaxValues: 1, Options: options}}}}
		}
		return data
	case "job_resumed":
		return CallbackData{Content: "The recoverable job resume was accepted.", Flags: EphemeralFlag}
	case "job", "job_cancelled":
		job, ok := result.Value.(application.InteractionJobView)
		if !ok {
			return unavailable
		}
		return CallbackData{Content: bounded(fmt.Sprintf("Job `%s`: %s (%d/%d).", job.RunID, job.Status, job.CompletedSteps, job.TotalSteps), 1000), Flags: EphemeralFlag}
	case "cancel_review":
		value, ok := result.Value.(map[string]any)
		if !ok {
			return unavailable
		}
		job, ok1 := value["job"].(application.InteractionJobView)
		customID, ok2 := value["custom_id"].(string)
		if !ok1 || !ok2 || !opaqueCustomID.MatchString(customID) {
			return unavailable
		}
		return CallbackData{Content: bounded(fmt.Sprintf("Cancel job `%s`? This confirmation is single-use.", job.RunID), 1000), Flags: EphemeralFlag, Components: []Component{{Type: 1, Components: []Component{{Type: 2, Style: 4, Label: "Confirm cancellation", CustomID: customID}}}}}
	case "resume_review":
		value, ok := result.Value.(map[string]any)
		if !ok {
			return unavailable
		}
		job, ok1 := value["job"].(application.InteractionJobView)
		customID, ok2 := value["custom_id"].(string)
		if !ok1 || !ok2 || !opaqueCustomID.MatchString(customID) {
			return unavailable
		}
		return CallbackData{Content: bounded(fmt.Sprintf("Review recoverable job `%s` (plan `%s`).", job.RunID, job.PlanHashPrefix), 1000), Flags: EphemeralFlag, Components: []Component{{Type: 1, Components: []Component{{Type: 2, Style: 3, Label: "Continue to confirmation", CustomID: customID}}}}}
	default:
		return unavailable
	}
}
func bounded(value string, maximum int) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}
