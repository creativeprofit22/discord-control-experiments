package observability

import (
	"context"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type correlationContextKey struct{}

func WithCorrelationID(ctx context.Context, id domain.CorrelationID) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, correlationContextKey{}, boundedCorrelation(id))
}

func correlationFromContext(ctx context.Context) string {
	value, _ := ctx.Value(correlationContextKey{}).(string)
	return value
}

// Metric identifiers are intentionally absent: identifiers belong only in correlated logs,
// traces, and audits. Metric dimensions must use closed, low-cardinality values.
var forbiddenMetricAttributes = map[string]struct{}{
	"actor_id": {}, "guild_id": {}, "channel_id": {}, "workflow_id": {}, "run_id": {},
	"resource_id": {}, FieldCorrelationID: {}, FieldPlanHashPrefix: {}, "url": {}, "content": {},
}

var closedDimensions = map[string]map[string]struct{}{
	"workflow.event":   set("queue_depth", "schedule_due", "lease_contention", "retry", "dead_letter", "source_failure", "deduplicated", "published", "approval_wait", "partial_failure"),
	"workflow.outcome": set("recorded", "ok", "failed", "denied", "cancelled", "partial", "other"),
	FieldResult:        set("ok", "error", "cancelled", "timeout", "partial", "other"),
	FieldStatusClass:   set("2xx", "3xx", "4xx", "5xx", "network_error", "other"),
}

func set(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func closedValue(key, value string) string {
	allowed, constrained := closedDimensions[key]
	if !constrained {
		return value
	}
	if _, ok := allowed[value]; ok {
		return value
	}
	return "other"
}

func metricAttributeAllowed(key string) bool {
	_, forbidden := forbiddenMetricAttributes[key]
	return !forbidden
}
