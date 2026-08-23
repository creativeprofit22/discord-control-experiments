package interactions

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type JobListOption struct {
	Job   application.InteractionJobView
	Value string
}
type JobListResult struct {
	Jobs           []application.InteractionJobView
	SelectCustomID string
	Options        []JobListOption
}

type DeferredResult struct {
	Kind    string
	Value   any
	Outcome string
}
type DeferredSink interface {
	Deliver(context.Context, VerifiedInteraction, DeferredResult) error
}
type Router struct {
	service    *application.InteractionService
	approvals  *application.ApprovalCoordinator
	store      *InteractionStore
	pending    *PendingStore
	executor   *Executor
	sink       DeferredSink
	telemetry  InteractionTelemetry
	pendingTTL time.Duration
	now        func() time.Time
}

func NewRouter(service *application.InteractionService, approvals *application.ApprovalCoordinator, store *InteractionStore, pending *PendingStore, executor *Executor, sink DeferredSink, telemetry InteractionTelemetry, pendingTTL time.Duration, now func() time.Time) (*Router, error) {
	if service == nil || approvals == nil || store == nil || pending == nil || executor == nil || sink == nil || telemetry == nil || pendingTTL <= 0 || pendingTTL > 15*time.Minute || now == nil {
		return nil, errors.New("router dependencies are required")
	}
	return &Router{service: service, approvals: approvals, store: store, pending: pending, executor: executor, sink: sink, telemetry: telemetry, pendingTTL: pendingTTL, now: now}, nil
}

func (r *Router) Respond(ctx context.Context, interaction VerifiedInteraction) (callback []byte, err error) {
	outcome, cached := r.store.Claim(interaction.Envelope.ID, interaction.RawBodyDigest)
	switch outcome {
	case ClaimCached, ClaimInFlight:
		r.telemetry.RecordDuplicate(context.Background(), string(outcome))
		return cached, nil
	case ClaimDigestMismatch:
		r.telemetry.RecordDuplicate(context.Background(), string(outcome))
		return genericUnavailableCallback(), nil
	case ClaimCapacity:
		r.telemetry.RecordSaturation(context.Background())
		return genericBusyCallback(), nil
	}
	cache := true
	defer func() {
		if recover() != nil {
			callback = genericUnavailableCallback()
			err = nil
			cache = true
		}
		if cache {
			r.store.Complete(interaction.Envelope.ID, interaction.RawBodyDigest, callback)
		} else {
			r.store.Abandon(interaction.Envelope.ID, interaction.RawBodyDigest)
		}
	}()
	callback, cache, err = r.respondOwned(ctx, interaction)
	return callback, err
}

func (r *Router) respondOwned(ctx context.Context, interaction VerifiedInteraction) ([]byte, bool, error) {
	switch interaction.Envelope.Type {
	case InteractionPing:
		callback, err := marshalCallback(Callback{Type: CallbackPong})
		return callback, true, err
	case InteractionAutocomplete:
		callback, err := r.autocomplete(interaction)
		return callback, true, err
	case InteractionApplicationCommand:
		return r.deferWork(interaction, CallbackDeferredChannelMessage, func(work context.Context) DeferredResult { return r.runCommand(work, interaction) })
	case InteractionMessageComponent:
		return r.component(ctx, interaction)
	case InteractionModalSubmit:
		return r.modal(ctx, interaction)
	default:
		return genericUnavailableCallback(), true, nil
	}
}

func (r *Router) deferWork(interaction VerifiedInteraction, callbackType CallbackType, work func(context.Context) DeferredResult) ([]byte, bool, error) {
	callback, reservation, ok := r.prepareDeferred(callbackType)
	if !ok {
		return callback, false, nil
	}
	if !r.enqueueDeferred(reservation, interaction, work) {
		return genericBusyCallback(), false, nil
	}
	return callback, true, nil
}

func (r *Router) prepareDeferred(callbackType CallbackType) ([]byte, *WorkReservation, bool) {
	callback, _ := marshalDeferredCallback(callbackType)
	reservation := r.executor.Reserve()
	if reservation == nil {
		r.telemetry.RecordSaturation(context.Background())
		return genericBusyCallback(), nil, false
	}
	return callback, reservation, true
}

func (r *Router) enqueueDeferred(reservation *WorkReservation, interaction VerifiedInteraction, work func(context.Context) DeferredResult) bool {
	if !reservation.Commit(WorkItem{InteractionID: interaction.Envelope.ID, CorrelationID: interaction.CorrelationID, TraceContext: interaction.TraceContext, Run: func(ctx context.Context) {
		ctx, endSpan := r.telemetry.StartInteractionSpan(ctx, "deferred", interaction.CorrelationID)
		started := time.Now()
		result := work(ctx)
		outcome := result.Outcome
		callbackOutcome := "success"
		if err := r.sink.Deliver(ctx, interaction, result); err != nil {
			callbackOutcome = "failure"
		}
		r.telemetry.RecordCallback(ctx, callbackOutcome)
		r.telemetry.ObserveDeferred(ctx, time.Since(started), outcome)
		endSpan(outcome)
	}}) {
		r.telemetry.RecordSaturation(context.Background())
		return false
	}
	r.telemetry.ObserveQueueDepth(context.Background(), r.executor.Depth())
	return true
}

func (r *Router) runCommand(ctx context.Context, interaction VerifiedInteraction) DeferredResult {
	identity, err := r.identity(interaction)
	if err != nil {
		return unavailableResult()
	}
	route, value, ok := parseCommand(interaction.Command)
	if !ok {
		return unavailableResult()
	}
	switch route {
	case "health":
		result, err := r.service.Health(ctx, identity)
		return deferred("health", result, err)
	case "policy":
		result, err := r.service.Policy(ctx, identity, domain.Capability(value))
		return deferred("policy", result, err)
	case "jobs.list":
		jobs, err := r.service.ListJobs(ctx, identity)
		if err != nil {
			return unavailableResult()
		}
		if len(jobs) == 0 {
			return DeferredResult{Kind: "jobs_list", Value: JobListResult{Jobs: jobs}, Outcome: "ok"}
		}
		selectorID, err := r.pending.Put(PendingAction{Kind: PendingJobSelect, ActorUserID: identity.UserID, GuildID: identity.GuildID, RunID: "selector", ExpiresAt: r.now().Add(r.pendingTTL)})
		if err != nil {
			return unavailableResult()
		}
		result := JobListResult{Jobs: jobs, SelectCustomID: selectorID}
		for index, job := range jobs {
			if index == 25 {
				break
			}
			value, putErr := r.pending.Put(PendingAction{Kind: PendingJobSelect, ActorUserID: identity.UserID, GuildID: identity.GuildID, RunID: job.RunID, ExpiresAt: r.now().Add(r.pendingTTL)})
			if putErr != nil {
				return unavailableResult()
			}
			result.Options = append(result.Options, JobListOption{Job: job, Value: value})
		}
		return DeferredResult{Kind: "jobs_list", Value: result, Outcome: "ok"}
	case "jobs.status":
		result, err := r.service.Job(ctx, identity, domain.RunID(value))
		return deferred("job", result, err)
	case "jobs.cancel":
		job, err := r.service.Job(ctx, identity, domain.RunID(value))
		if err != nil {
			return unavailableResult()
		}
		customID, err := r.pending.Put(PendingAction{Kind: PendingCancel, ActorUserID: identity.UserID, GuildID: identity.GuildID, RunID: job.RunID, ExpiresAt: r.now().Add(r.pendingTTL)})
		if err != nil {
			return unavailableResult()
		}
		return DeferredResult{Kind: "cancel_review", Value: map[string]any{"job": job, "custom_id": customID}, Outcome: "ok"}
	case "jobs.resume":
		review, err := r.service.ReviewResume(ctx, identity, domain.RunID(value))
		if err != nil || !review.Job.Recoverable {
			return unavailableResult()
		}
		customID, err := r.pending.Put(PendingAction{Kind: PendingResumeReview, ActorUserID: identity.UserID, ActorID: review.ActorID, GuildID: identity.GuildID, RunID: review.Job.RunID, PlanHash: review.PlanHash, Recoverable: true, ExpiresAt: r.now().Add(r.pendingTTL)})
		if err != nil {
			return unavailableResult()
		}
		return DeferredResult{Kind: "resume_review", Value: map[string]any{"job": review.Job, "custom_id": customID}, Outcome: "ok"}
	default:
		return unavailableResult()
	}
}

func (r *Router) component(ctx context.Context, interaction VerifiedInteraction) ([]byte, bool, error) {
	data := interaction.Component
	if data == nil || !opaqueCustomID.MatchString(data.CustomID) {
		return genericUnavailableCallback(), true, nil
	}
	userID := interaction.Envelope.Member.User.ID
	guildID := domain.GuildID(interaction.Envelope.GuildID)
	action, pendingOutcome := r.pending.Get(data.CustomID, userID, guildID)
	if pendingOutcome != PendingAvailable {
		r.recordPendingApprovalDenial(ctx, action.Kind, pendingOutcome)
		return genericUnavailableCallback(), true, nil
	}
	switch action.Kind {
	case PendingJobSelect:
		if len(data.Values) != 1 {
			return genericUnavailableCallback(), true, nil
		}
		selected, selectedOutcome := r.pending.Get(data.Values[0], userID, guildID)
		if selectedOutcome != PendingAvailable || selected.Kind != PendingJobSelect || selected.RunID == "selector" {
			return genericUnavailableCallback(), true, nil
		}
		callback, reservation, reserved := r.prepareDeferred(CallbackDeferredMessageUpdate)
		if !reserved {
			return callback, false, nil
		}
		selected, selectedOutcome = r.pending.Claim(data.Values[0], userID, guildID)
		if selectedOutcome != PendingAvailable {
			reservation.Release()
			return genericUnavailableCallback(), true, nil
		}
		if !r.enqueueDeferred(reservation, interaction, func(ctx context.Context) DeferredResult {
			identity, err := r.identity(interaction)
			if err != nil {
				return unavailableResult()
			}
			result, err := r.service.Job(ctx, identity, selected.RunID)
			return deferred("job", result, err)
		}) {
			r.pending.Restore(data.Values[0], selected)
			return genericBusyCallback(), false, nil
		}
		return callback, true, nil
	case PendingCancel:
		callback, reservation, reserved := r.prepareDeferred(CallbackDeferredMessageUpdate)
		if !reserved {
			return callback, false, nil
		}
		claimed, claimOutcome := r.pending.Claim(data.CustomID, userID, guildID)
		if claimOutcome != PendingAvailable {
			reservation.Release()
			r.recordPendingApprovalDenial(ctx, claimed.Kind, claimOutcome)
			return genericUnavailableCallback(), true, nil
		}
		if !r.enqueueDeferred(reservation, interaction, func(ctx context.Context) DeferredResult {
			identity, err := r.identity(interaction)
			if err != nil {
				return unavailableResult()
			}
			result, err := r.service.CancelJob(ctx, identity, claimed.RunID)
			return deferred("job_cancelled", result, err)
		}) {
			r.pending.Restore(data.CustomID, claimed)
			return genericBusyCallback(), false, nil
		}
		return callback, true, nil
	case PendingResumeReview:
		claimed, claimOutcome := r.pending.Claim(data.CustomID, userID, guildID)
		if claimOutcome != PendingAvailable {
			r.recordPendingApprovalDenial(ctx, claimed.Kind, claimOutcome)
			return genericUnavailableCallback(), true, nil
		}
		modalID, err := r.pending.Put(PendingAction{Kind: PendingResumeModal, ActorUserID: claimed.ActorUserID, ActorID: claimed.ActorID, GuildID: claimed.GuildID, RunID: claimed.RunID, PlanHash: claimed.PlanHash, Recoverable: claimed.Recoverable, ExpiresAt: claimed.ExpiresAt})
		if err != nil {
			r.pending.Restore(data.CustomID, claimed)
			return genericUnavailableCallback(), true, nil
		}
		callback, err := marshalCallback(Callback{Type: CallbackModal, Data: &CallbackData{CustomID: modalID, Title: "Confirm job resume", Components: []Component{{Type: 1, Components: []Component{{Type: 4, Style: 1, Label: "Plan hash prefix", CustomID: "confirmation", Required: true, MinLength: 12, MaxLength: 12}}}}}})
		return callback, true, err
	default:
		return genericUnavailableCallback(), true, nil
	}
}

func (r *Router) modal(ctx context.Context, interaction VerifiedInteraction) ([]byte, bool, error) {
	if interaction.Modal == nil || !opaqueCustomID.MatchString(interaction.Modal.CustomID) {
		return genericUnavailableCallback(), true, nil
	}
	userID := interaction.Envelope.Member.User.ID
	guildID := domain.GuildID(interaction.Envelope.GuildID)
	action, pendingOutcome := r.pending.Get(interaction.Modal.CustomID, userID, guildID)
	if pendingOutcome != PendingAvailable {
		r.recordPendingApprovalDenial(ctx, action.Kind, pendingOutcome)
		return genericUnavailableCallback(), true, nil
	}
	confirmation, valid := modalConfirmation(interaction.Modal.Components)
	prefix := application.PlanHashPrefix(action.PlanHash)
	if action.Kind != PendingResumeModal || len(prefix) != 12 || !valid || subtle.ConstantTimeCompare([]byte(prefix), []byte(confirmation)) != 1 {
		return genericUnavailableCallback(), true, nil
	}
	callback, reservation, reserved := r.prepareDeferred(CallbackDeferredChannelMessage)
	if !reserved {
		return callback, false, nil
	}
	action, pendingOutcome = r.pending.Claim(interaction.Modal.CustomID, userID, guildID)
	if pendingOutcome != PendingAvailable {
		reservation.Release()
		r.recordPendingApprovalDenial(ctx, action.Kind, pendingOutcome)
		return genericUnavailableCallback(), true, nil
	}
	if !r.enqueueDeferred(reservation, interaction, func(ctx context.Context) DeferredResult { return r.resume(ctx, interaction, action) }) {
		r.pending.Restore(interaction.Modal.CustomID, action)
		return genericBusyCallback(), false, nil
	}
	return callback, true, nil
}

func (r *Router) resume(ctx context.Context, interaction VerifiedInteraction, action PendingAction) DeferredResult {
	identity, err := r.identity(interaction)
	if err != nil {
		return unavailableResult()
	}
	review, err := r.service.ReviewResume(ctx, identity, action.RunID)
	if err != nil {
		r.telemetry.RecordApproval(ctx, "denied")
		return unavailableResult()
	}
	if !review.Job.Recoverable || review.ActorID != action.ActorID || subtle.ConstantTimeCompare([]byte(review.PlanHash), []byte(action.PlanHash)) != 1 {
		r.telemetry.RecordApproval(ctx, "changed_plan")
		return unavailableResult()
	}
	grant, err := r.approvals.IssueReconciliationPlan(ctx, action.ActorID, action.GuildID, action.PlanHash, application.ConfirmationInteraction, action.ExpiresAt)
	if err != nil {
		r.telemetry.RecordApproval(ctx, "denied")
		return unavailableResult()
	}
	r.telemetry.RecordApproval(ctx, "issued")
	if _, err := r.service.ResumeJob(ctx, identity, action.RunID, grant.ID); err != nil {
		r.telemetry.RecordApproval(ctx, "denied")
		return unavailableResult()
	}
	r.telemetry.RecordApproval(ctx, "accepted")
	return DeferredResult{Kind: "job_resumed", Outcome: "ok"}
}

func (r *Router) recordPendingApprovalDenial(ctx context.Context, kind PendingKind, outcome PendingOutcome) {
	if kind != PendingCancel && kind != PendingResumeReview && kind != PendingResumeModal {
		return
	}
	switch outcome {
	case PendingExpired, PendingReplayed, PendingWrongActor, PendingWrongGuild:
		r.telemetry.RecordApproval(ctx, string(outcome))
	}
}

func modalConfirmation(components []ModalComponent) (string, bool) {
	found := ""
	var walk func([]ModalComponent) bool
	walk = func(values []ModalComponent) bool {
		for _, component := range values {
			if component.CustomID == "confirmation" {
				if found != "" || len(component.Value) != 12 {
					return false
				}
				found = component.Value
			}
			if len(component.Components) > 0 && !walk(component.Components) {
				return false
			}
		}
		return true
	}
	return found, walk(components) && found != ""
}

func (r *Router) autocomplete(interaction VerifiedInteraction) ([]byte, error) {
	choices := []Choice{{Name: "health", Value: "health"}, {Name: "policy", Value: "policy"}, {Name: "jobs list", Value: "jobs.list"}, {Name: "jobs status", Value: "jobs.status"}, {Name: "jobs cancel", Value: "jobs.cancel"}, {Name: "jobs resume", Value: "jobs.resume"}}
	return marshalCallback(Callback{Type: CallbackAutocomplete, Data: &CallbackData{Choices: choices}})
}
func (r *Router) identity(interaction VerifiedInteraction) (application.InteractionIdentity, error) {
	if interaction.CorrelationID == "" {
		return application.InteractionIdentity{}, errors.New("interaction correlation ID is required")
	}
	return application.InteractionIdentity{UserID: interaction.Envelope.Member.User.ID, GuildID: domain.GuildID(interaction.Envelope.GuildID), CorrelationID: interaction.CorrelationID}, nil
}
func deferred(kind string, value any, err error) DeferredResult {
	if err != nil {
		return unavailableResult()
	}
	return DeferredResult{Kind: kind, Value: value, Outcome: "ok"}
}
func unavailableResult() DeferredResult {
	return DeferredResult{Kind: "unavailable", Outcome: "not_available"}
}
func marshalCallback(callback Callback) ([]byte, error) { return json.Marshal(callback) }

func marshalDeferredCallback(callbackType CallbackType) ([]byte, error) {
	callback := Callback{Type: callbackType}
	if callbackType != CallbackDeferredMessageUpdate {
		callback.Data = &CallbackData{Flags: EphemeralFlag}
	}
	return marshalCallback(callback)
}

var opaqueCustomID = regexp.MustCompile(`^(job_select|cancel|resume_review|resume_modal)\.[A-Za-z0-9_-]{24}$`)

func parseCommand(data *CommandData) (string, string, bool) {
	if data == nil || data.Name != "discord-control" || len(data.Options) != 1 {
		return "", "", false
	}
	first := data.Options[0]
	if first.Name == "health" {
		return "health", "", true
	}
	if first.Name == "policy" && len(first.Options) == 1 {
		value, ok := stringOption(first.Options[0])
		return "policy", value, ok
	}
	if first.Name != "jobs" || len(first.Options) != 1 {
		return "", "", false
	}
	second := first.Options[0]
	if second.Name == "list" && len(second.Options) == 0 {
		return "jobs.list", "", true
	}
	if len(second.Options) != 1 {
		return "", "", false
	}
	value, ok := stringOption(second.Options[0])
	return "jobs." + second.Name, value, ok
}
func stringOption(option CommandOption) (string, bool) {
	if option.Type != 3 || len(option.Value) == 0 {
		return "", false
	}
	var value string
	if json.Unmarshal(option.Value, &value) != nil || len(value) < 1 || len(value) > 100 {
		return "", false
	}
	return value, true
}
