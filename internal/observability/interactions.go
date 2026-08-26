package observability

import (
	"context"
	"strconv"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

func (p *Provider) StartInteractionSpan(ctx context.Context, stage string, correlationID domain.CorrelationID) (context.Context, func(string)) {
	if p == nil {
		return ctx, func(string) {}
	}
	if !oneOf(stage, "inbound", "deferred", "callback") {
		stage = "unknown"
	}
	ctx = WithCorrelationID(ctx, correlationID)
	ctx, span := p.tracer.Start(ctx, "interaction."+stage, trace.WithAttributes(
		attribute.String("interaction.stage", stage),
		attribute.String(FieldCorrelationID, boundedCorrelation(correlationID)),
	))
	return ctx, func(outcome string) {
		if !oneOf(outcome, "ok", "error", "not_available") {
			outcome = "error"
		}
		span.SetAttributes(attribute.String("interaction.outcome", outcome))
		if outcome != "ok" {
			span.SetStatus(codes.Error, outcome)
		}
		span.End()
	}
}

func (p *Provider) RecordVerification(ctx context.Context, outcome string, keySlot int) {
	if p == nil || !oneOf(outcome, "valid", "missing", "malformed", "invalid", "stale") {
		return
	}
	slot := "none"
	if outcome == "valid" && (keySlot == 0 || keySlot == 1) {
		slot = strconv.Itoa(keySlot)
	}
	p.interactionVerification.Add(ctx, 1, metric.WithAttributes(attribute.String("interaction.outcome", outcome), attribute.String("interaction.key_slot", slot)))
	if outcome != "valid" {
		p.RecordSecurityEvent(ctx, domain.SecurityEvent{Kind: "interaction_signature_failure", OccurredAt: time.Now()})
	}
}
func (p *Provider) ObserveAcknowledgement(ctx context.Context, callbackType string, duration time.Duration, missed bool) {
	if p == nil || duration < 0 || !oneOf(callbackType, "ping", "command", "component", "autocomplete", "modal", "unsupported") {
		return
	}
	attrs := metric.WithAttributes(attribute.String("interaction.type", callbackType))
	p.interactionAckDuration.Record(ctx, float64(duration.Microseconds())/1000, attrs)
	if missed {
		p.interactionDeadlineMisses.Add(ctx, 1, attrs)
		p.RecordSecurityEvent(ctx, domain.SecurityEvent{Kind: "interaction_ack_deadline_miss", OccurredAt: time.Now()})
	}
}
func (p *Provider) RecordDuplicate(ctx context.Context, outcome string) {
	if p != nil && oneOf(outcome, "cached", "in_flight", "digest_mismatch") {
		p.interactionDuplicates.Add(ctx, 1, metric.WithAttributes(attribute.String("interaction.outcome", outcome)))
	}
}
func (p *Provider) ObserveDeferred(ctx context.Context, duration time.Duration, outcome string) {
	if p != nil && duration >= 0 {
		if !oneOf(outcome, "ok", "not_available", "error") {
			outcome = "error"
		}
		p.interactionDeferred.Record(ctx, float64(duration.Microseconds())/1000, metric.WithAttributes(attribute.String("interaction.outcome", outcome)))
	}
}
func (p *Provider) RecordCallback(ctx context.Context, outcome string) {
	if p != nil && oneOf(outcome, "success", "failure") {
		p.interactionCallbacks.Add(ctx, 1, metric.WithAttributes(attribute.String("interaction.outcome", outcome)))
		if outcome == "failure" {
			p.RecordSecurityEvent(ctx, domain.SecurityEvent{Kind: "interaction_callback_failure", OccurredAt: time.Now()})
		}
	}
}
func (p *Provider) ObserveCallbackAttempt(ctx context.Context, operation, statusClass string, duration time.Duration) {
	if p == nil || duration < 0 || !oneOf(operation, "edit_original", "followup") || !oneOf(statusClass, "1xx", "2xx", "3xx", "4xx", "5xx", "transport_error") {
		return
	}
	p.interactionCallbackDuration.Record(ctx, float64(duration.Microseconds())/1000, metric.WithAttributes(
		attribute.String("interaction.operation", operation),
		attribute.String("http.response.status_class", statusClass),
	))
}
func (p *Provider) RecordApproval(ctx context.Context, outcome string) {
	if p != nil && oneOf(outcome, "issued", "accepted", "denied", "expired", "replayed", "wrong_actor", "wrong_guild", "changed_plan") {
		p.interactionApprovals.Add(ctx, 1, metric.WithAttributes(attribute.String("interaction.outcome", outcome)))
	}
}
func (p *Provider) ObserveQueueDepth(_ context.Context, depth int) {
	if p != nil && depth >= 0 {
		p.interactionQueueDepthValue.Store(int64(depth))
	}
}
func (p *Provider) ObserveHandlerActive(_ context.Context, active int) {
	if p != nil && active >= 0 {
		p.interactionHandlerActiveValue.Store(int64(active))
	}
}
func (p *Provider) RecordRegistration(ctx context.Context, outcome string) {
	if p != nil && oneOf(outcome, "noop", "updated", "failed") {
		p.interactionRegistrations.Add(ctx, 1, metric.WithAttributes(attribute.String("interaction.outcome", outcome)))
	}
}
func (p *Provider) RecordSaturation(ctx context.Context) {
	if p != nil {
		p.interactionSaturation.Add(ctx, 1)
		p.RecordSecurityEvent(ctx, domain.SecurityEvent{Kind: "interaction_handler_saturation", OccurredAt: time.Now()})
	}
}
func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
