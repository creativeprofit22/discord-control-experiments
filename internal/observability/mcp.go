package observability

import (
	"context"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var mcpResultClasses = map[string]bool{"ok": true, "plan": true, "schema_error": true, "invalid_arguments": true, "policy_denied": true, "ownership_denied": true, "approval_required": true, "approval_rejected": true, "approval_invalid": true, "cancelled": true, "queue_rejected": true, "limit_exceeded": true, "operation_failed": true, "internal_error": true}

var mcpPolicyReasons = map[domain.PolicyReason]bool{
	domain.PolicyAllowed: true, domain.PolicyUnknownPrincipal: true, domain.PolicyAmbiguousPrincipal: true,
	domain.PolicyMalformedPrincipal: true, domain.PolicyGuildDenied: true, domain.PolicyCapabilityDenied: true,
	domain.PolicyModeDenied: true, domain.PolicyDestructiveDenied: true, domain.PolicyResourceDenied: true,
	domain.PolicyUnknownCommand: true, domain.PolicyError: true, domain.PolicyApprovalRequired: true,
	domain.PolicyApprovalInvalid: true, domain.PolicyAuditUnavailable: true,
}

func (p *Provider) StartMCPInvocation(ctx context.Context, tool string, correlation domain.CorrelationID) (context.Context, func(string, domain.PolicyReason, time.Duration)) {
	if p == nil {
		return ctx, nil
	}
	tool = boundedMCPTool(tool)
	ctx = WithCorrelationID(ctx, correlation)
	ctx, span := p.tracer.Start(ctx, "mcp.tool", trace.WithAttributes(
		attribute.String("mcp.tool", tool),
		attribute.String(FieldCorrelationID, boundedCorrelation(correlation)),
	))
	return ctx, func(result string, policyReason domain.PolicyReason, duration time.Duration) {
		result = boundedMCPResult(result)
		attributes := []attribute.KeyValue{attribute.String("mcp.tool", tool), attribute.String("mcp.result", result)}
		spanAttributes := []attribute.KeyValue{attribute.String("mcp.result", result)}
		if mcpPolicyReasons[policyReason] {
			policyAttribute := attribute.String("mcp.policy_reason", string(policyReason))
			attributes = append(attributes, policyAttribute)
			spanAttributes = append(spanAttributes, policyAttribute)
		}
		span.SetAttributes(spanAttributes...)
		if result != "ok" && result != "plan" {
			span.SetStatus(codes.Error, result)
		}
		span.End()
		p.mcpInvocations.Add(ctx, 1, metric.WithAttributes(attributes...))
		p.mcpDuration.Record(ctx, float64(duration)/float64(time.Millisecond), metric.WithAttributes(attributes...))
	}
}

func boundedMCPTool(tool string) string {
	if len(tool) > 96 {
		return "unknown"
	}
	return tool
}

func boundedMCPResult(result string) string {
	if !mcpResultClasses[result] {
		return "internal_error"
	}
	return result
}

func boundedCorrelation(value domain.CorrelationID) string {
	if len(value) > 128 {
		return string(value[:128])
	}
	return string(value)
}
