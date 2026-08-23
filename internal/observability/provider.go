package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/buildinfo"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type Options struct {
	Writer       io.Writer
	Exporter     string
	Endpoint     string
	Build        buildinfo.Info
	Redactor     *Redactor
	SpanExporter sdktrace.SpanExporter
	MetricReader sdkmetric.Reader
	Security     SecurityOptions
}

type Provider struct {
	Logger                        *slog.Logger
	TracerProvider                *sdktrace.TracerProvider
	MeterProvider                 *sdkmetric.MeterProvider
	tracer                        trace.Tracer
	counter                       metric.Int64Counter
	duration                      metric.Float64Histogram
	discordRequests               metric.Int64Counter
	discordDuration               metric.Float64Histogram
	discordRateLimitWait          metric.Float64Histogram
	discordRetries                metric.Int64Counter
	discordCancellations          metric.Int64Counter
	discordPartial                metric.Int64Counter
	cliFailures                   metric.Int64Counter
	mcpInvocations                metric.Int64Counter
	mcpDuration                   metric.Float64Histogram
	securityEvents                metric.Int64Counter
	approvalLatency               metric.Float64Histogram
	reconciliationPlanSize        metric.Int64Histogram
	reconciliationRisk            metric.Int64Counter
	reconciliationDrift           metric.Int64Counter
	reconciliationConflicts       metric.Int64Counter
	reconciliationStepLatency     metric.Float64Histogram
	reconciliationRetries         metric.Int64Counter
	reconciliationPartial         metric.Int64Counter
	reconciliationResumes         metric.Int64Counter
	reconciliationOutcomes        metric.Int64Counter
	moderationActions             metric.Int64Counter
	moderationDuration            metric.Float64Histogram
	moderationHierarchyFailures   metric.Int64Counter
	autoModerationDrift           metric.Int64Counter
	escalationOutcomes            metric.Int64Counter
	lockdownEvents                metric.Int64Counter
	lockdownOperationCount        metric.Int64Histogram
	interactionVerification       metric.Int64Counter
	interactionAckDuration        metric.Float64Histogram
	interactionDeadlineMisses     metric.Int64Counter
	interactionDuplicates         metric.Int64Counter
	interactionDeferred           metric.Float64Histogram
	interactionCallbacks          metric.Int64Counter
	interactionCallbackDuration   metric.Float64Histogram
	interactionApprovals          metric.Int64Counter
	interactionSaturation         metric.Int64Counter
	interactionQueueDepth         metric.Int64ObservableGauge
	interactionQueueDepthValue    *atomic.Int64
	interactionHandlerActive      metric.Int64ObservableGauge
	interactionHandlerActiveValue *atomic.Int64
	interactionRegistrations      metric.Int64Counter
	securityMonitor               *securityMonitor
	build                         buildinfo.Info
	redactor                      *Redactor
}

func New(ctx context.Context, options Options) (*Provider, error) {
	if options.Writer == nil || options.Redactor == nil {
		return nil, errors.New("observability writer and redactor are required")
	}
	spanExporter, metricReader, err := exporters(ctx, options)
	if err != nil {
		return nil, err
	}
	traceOptions := []sdktrace.TracerProviderOption{}
	if spanExporter != nil {
		traceOptions = append(traceOptions, sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(spanExporter)))
	}
	meterOptions := []sdkmetric.Option{}
	if metricReader != nil {
		meterOptions = append(meterOptions, sdkmetric.WithReader(metricReader))
	}
	tp := sdktrace.NewTracerProvider(traceOptions...)
	mp := sdkmetric.NewMeterProvider(meterOptions...)
	meter := mp.Meter("discord-control")
	counter, err := meter.Int64Counter("discord_control.operations")
	if err != nil {
		return nil, err
	}
	duration, err := meter.Float64Histogram("discord_control.operation.duration", metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}
	discordRequests, err := meter.Int64Counter("discord.rest.request.count")
	if err != nil {
		return nil, err
	}
	discordDuration, err := meter.Float64Histogram("discord.rest.request.duration", metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}
	discordRateLimitWait, err := meter.Float64Histogram("discord.rest.ratelimit.wait", metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}
	discordRetries, err := meter.Int64Counter("discord.rest.retry.count")
	if err != nil {
		return nil, err
	}
	discordCancellations, err := meter.Int64Counter("discord.rest.cancellation.count")
	if err != nil {
		return nil, err
	}
	discordPartial, err := meter.Int64Counter("discord.command.partial.count")
	if err != nil {
		return nil, err
	}
	cliFailures, err := meter.Int64Counter("discord.cli.failure.count")
	if err != nil {
		return nil, err
	}
	mcpInvocations, err := meter.Int64Counter("discord.mcp.invocation.count")
	if err != nil {
		return nil, err
	}
	mcpDuration, err := meter.Float64Histogram("discord.mcp.invocation.duration", metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}
	logger := slog.New(redactingHandler{next: slog.NewJSONHandler(options.Writer, nil), redactor: options.Redactor})
	securityEvents, err := meter.Int64Counter("discord.security.event.count")
	if err != nil {
		return nil, err
	}
	approvalLatency, err := meter.Float64Histogram("discord.approval.latency", metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}
	reconciliationPlanSize, err := meter.Int64Histogram("discord.reconciliation.plan.operation_count")
	if err != nil {
		return nil, err
	}
	reconciliationRisk, err := meter.Int64Counter("discord.reconciliation.plan.risk_count")
	if err != nil {
		return nil, err
	}
	reconciliationDrift, err := meter.Int64Counter("discord.reconciliation.drift.count")
	if err != nil {
		return nil, err
	}
	reconciliationConflicts, err := meter.Int64Counter("discord.reconciliation.unmanaged_conflict.count")
	if err != nil {
		return nil, err
	}
	reconciliationStepLatency, err := meter.Float64Histogram("discord.reconciliation.step.duration", metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}
	reconciliationRetries, err := meter.Int64Counter("discord.reconciliation.retry.count")
	if err != nil {
		return nil, err
	}
	reconciliationPartial, err := meter.Int64Counter("discord.reconciliation.partial.count")
	if err != nil {
		return nil, err
	}
	reconciliationResumes, err := meter.Int64Counter("discord.reconciliation.resume.count")
	if err != nil {
		return nil, err
	}
	reconciliationOutcomes, err := meter.Int64Counter("discord.reconciliation.run.outcome_count")
	if err != nil {
		return nil, err
	}
	moderationActions, err := meter.Int64Counter("discord.moderation.action.count")
	if err != nil {
		return nil, err
	}
	moderationDuration, err := meter.Float64Histogram("discord.moderation.action.duration", metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}
	moderationHierarchyFailures, err := meter.Int64Counter("discord.moderation.hierarchy_failure.count")
	if err != nil {
		return nil, err
	}
	autoModerationDrift, err := meter.Int64Counter("discord.automod.drift.count")
	if err != nil {
		return nil, err
	}
	escalationOutcomes, err := meter.Int64Counter("discord.moderation.escalation.count")
	if err != nil {
		return nil, err
	}
	lockdownEvents, err := meter.Int64Counter("discord.moderation.lockdown.count")
	if err != nil {
		return nil, err
	}
	lockdownOperationCount, err := meter.Int64Histogram("discord.moderation.lockdown.operation_count")
	if err != nil {
		return nil, err
	}
	interactionVerification, err := meter.Int64Counter("discord.interaction.signature.count")
	if err != nil {
		return nil, err
	}
	interactionAckDuration, err := meter.Float64Histogram("discord.interaction.ack.duration", metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}
	interactionDeadlineMisses, err := meter.Int64Counter("discord.interaction.ack.deadline_miss_count")
	if err != nil {
		return nil, err
	}
	interactionDuplicates, err := meter.Int64Counter("discord.interaction.duplicate.count")
	if err != nil {
		return nil, err
	}
	interactionDeferred, err := meter.Float64Histogram("discord.interaction.deferred.duration", metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}
	interactionCallbacks, err := meter.Int64Counter("discord.interaction.callback.count")
	if err != nil {
		return nil, err
	}
	interactionCallbackDuration, err := meter.Float64Histogram("discord.interaction.callback.duration", metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}
	interactionApprovals, err := meter.Int64Counter("discord.interaction.approval.count")
	if err != nil {
		return nil, err
	}
	interactionSaturation, err := meter.Int64Counter("discord.interaction.saturation.count")
	if err != nil {
		return nil, err
	}
	interactionQueueDepthValue := new(atomic.Int64)
	interactionQueueDepth, err := meter.Int64ObservableGauge("discord.interaction.queue.depth", metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
		observer.Observe(interactionQueueDepthValue.Load())
		return nil
	}))
	if err != nil {
		return nil, err
	}
	interactionHandlerActiveValue := new(atomic.Int64)
	interactionHandlerActive, err := meter.Int64ObservableGauge("discord.interaction.handler.active", metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
		observer.Observe(interactionHandlerActiveValue.Load())
		return nil
	}))
	if err != nil {
		return nil, err
	}
	interactionRegistrations, err := meter.Int64Counter("discord.interaction.registration.count")
	if err != nil {
		return nil, err
	}
	monitor := newSecurityMonitor(options.Security, logger)
	provider := &Provider{Logger: logger, TracerProvider: tp, MeterProvider: mp, tracer: tp.Tracer("discord-control"), counter: counter, duration: duration, discordRequests: discordRequests, discordDuration: discordDuration, discordRateLimitWait: discordRateLimitWait, discordRetries: discordRetries, discordCancellations: discordCancellations, discordPartial: discordPartial, cliFailures: cliFailures, mcpInvocations: mcpInvocations, mcpDuration: mcpDuration, securityEvents: securityEvents, approvalLatency: approvalLatency, reconciliationPlanSize: reconciliationPlanSize, reconciliationRisk: reconciliationRisk, reconciliationDrift: reconciliationDrift, reconciliationConflicts: reconciliationConflicts, reconciliationStepLatency: reconciliationStepLatency, reconciliationRetries: reconciliationRetries, reconciliationPartial: reconciliationPartial, reconciliationResumes: reconciliationResumes, reconciliationOutcomes: reconciliationOutcomes, moderationActions: moderationActions, moderationDuration: moderationDuration, moderationHierarchyFailures: moderationHierarchyFailures, autoModerationDrift: autoModerationDrift, escalationOutcomes: escalationOutcomes, lockdownEvents: lockdownEvents, lockdownOperationCount: lockdownOperationCount, interactionVerification: interactionVerification, interactionAckDuration: interactionAckDuration, interactionDeadlineMisses: interactionDeadlineMisses, interactionDuplicates: interactionDuplicates, interactionDeferred: interactionDeferred, interactionCallbacks: interactionCallbacks, interactionCallbackDuration: interactionCallbackDuration, interactionApprovals: interactionApprovals, interactionSaturation: interactionSaturation, interactionQueueDepth: interactionQueueDepth, interactionQueueDepthValue: interactionQueueDepthValue, interactionHandlerActive: interactionHandlerActive, interactionHandlerActiveValue: interactionHandlerActiveValue, interactionRegistrations: interactionRegistrations, securityMonitor: monitor, build: options.Build, redactor: options.Redactor}
	options.Redactor.SetInterventionHook(func() {
		provider.RecordSecurityEvent(context.Background(), domain.SecurityEvent{Kind: "redaction_intervention", OccurredAt: time.Now()})
	})
	return provider, nil
}

func exporters(ctx context.Context, options Options) (sdktrace.SpanExporter, sdkmetric.Reader, error) {
	if options.SpanExporter != nil || options.MetricReader != nil {
		return options.SpanExporter, options.MetricReader, nil
	}
	switch options.Exporter {
	case "", "none":
		return nil, nil, nil
	case "stderr":
		spans, err := stdouttrace.New(stdouttrace.WithWriter(options.Writer))
		if err != nil {
			return nil, nil, err
		}
		metrics, err := stdoutmetric.New(stdoutmetric.WithWriter(options.Writer))
		if err != nil {
			return nil, nil, err
		}
		return spans, sdkmetric.NewPeriodicReader(metrics), nil
	case "otlp-http":
		traceEndpoint, err := otlpSignalEndpoint(options.Endpoint, "/v1/traces")
		if err != nil {
			return nil, nil, err
		}
		metricEndpoint, err := otlpSignalEndpoint(options.Endpoint, "/v1/metrics")
		if err != nil {
			return nil, nil, err
		}
		spans, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(traceEndpoint))
		if err != nil {
			return nil, nil, err
		}
		metrics, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(metricEndpoint))
		if err != nil {
			return nil, nil, err
		}
		return spans, sdkmetric.NewPeriodicReader(metrics), nil
	default:
		return nil, nil, errors.New("unsupported telemetry exporter")
	}
}

func otlpSignalEndpoint(endpoint, defaultPath string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse OTLP endpoint: %w", err)
	}
	if parsed.Path != "" {
		return endpoint, nil
	}
	parsed.Path = defaultPath
	return parsed.String(), nil
}

func (p *Provider) Observe(ctx context.Context, operation domain.Operation, fn func(context.Context) error) error {
	if fn == nil {
		return errors.New("observed function is required")
	}
	attrs := []attribute.KeyValue{
		attribute.String(FieldOperation, string(operation.Name)), attribute.String(FieldAdapter, string(operation.Adapter)),
		attribute.String(FieldCorrelationID, string(operation.CorrelationID)), attribute.Int(FieldRetryCount, operation.RetryCount),
		attribute.String(FieldBuildVersion, p.build.Version), attribute.String(FieldBuildCommit, p.build.Commit),
	}
	ctx, span := p.tracer.Start(ctx, string(operation.Name), trace.WithAttributes(attrs...))
	start := operation.StartedAt
	if start.IsZero() {
		start = time.Now()
	}
	err := fn(ctx)
	result := "ok"
	if err != nil {
		result = "error"
		sanitized := errors.New(p.redactor.String(err.Error()))
		span.RecordError(sanitized)
		span.SetStatus(codes.Error, sanitized.Error())
	}
	durationMS := float64(time.Since(start).Microseconds()) / 1000
	span.SetAttributes(attribute.String(FieldResult, result), attribute.Float64(FieldDurationMS, durationMS))
	span.End()
	metricAttrs := metric.WithAttributes(append(attrs, attribute.String(FieldResult, result))...)
	p.counter.Add(ctx, 1, metricAttrs)
	p.duration.Record(ctx, durationMS, metricAttrs)
	p.Logger.InfoContext(ctx, "operation completed",
		FieldOperation, operation.Name, FieldAdapter, operation.Adapter, FieldCorrelationID, operation.CorrelationID,
		FieldResult, result, FieldDurationMS, durationMS, FieldRetryCount, operation.RetryCount,
		FieldBuildVersion, p.build.Version, FieldBuildCommit, p.build.Commit,
	)
	return err
}

func (p *Provider) ObserveDiscordCommand(ctx context.Context, observation *application.DiscordObservation, fn func(context.Context) error) error {
	if observation == nil || fn == nil {
		return errors.New("discord observation and function are required")
	}
	attrs := []attribute.KeyValue{attribute.String(FieldCommandName, observation.CommandName), attribute.String(FieldRouteTemplate, observation.RouteTemplate), attribute.String(FieldHTTPMethod, observation.Method), attribute.String(FieldAPIPathVersion, "v10")}
	ctx, span := p.tracer.Start(ctx, "discord.command."+observation.CommandName, trace.WithAttributes(attrs...))
	started := time.Now()
	err := fn(ctx)
	durationMS := float64(time.Since(started).Microseconds()) / 1000
	resultClass := observation.ResultClass
	if resultClass == "" {
		if err != nil {
			resultClass = "error"
		} else {
			resultClass = "ok"
		}
	}
	attemptAttrs := append(append([]attribute.KeyValue{}, attrs...), attribute.String(FieldStatusClass, observation.StatusClass), attribute.String(FieldResult, resultClass))
	for attempt := 0; attempt < observation.Attempts; attempt++ {
		_, child := p.tracer.Start(ctx, "discord.rest.attempt", trace.WithAttributes(append(attemptAttrs, attribute.Int(FieldRetryCount, attempt))...))
		child.End()
	}
	if err != nil {
		sanitized := errors.New(p.redactor.String(err.Error()))
		span.RecordError(sanitized)
		span.SetStatus(codes.Error, sanitized.Error())
		var cancellation *domain.CancellationError
		if errors.As(err, &cancellation) {
			p.discordCancellations.Add(ctx, 1, metric.WithAttributes(attrs...))
		}
	}
	span.SetAttributes(attribute.String(FieldResult, resultClass), attribute.String(FieldStatusClass, observation.StatusClass))
	span.End()
	metricAttrs := metric.WithAttributes(attemptAttrs...)
	requests := observation.Attempts
	if requests < 1 {
		requests = 1
	}
	p.discordRequests.Add(ctx, int64(requests), metricAttrs)
	p.discordDuration.Record(ctx, durationMS, metricAttrs)
	p.discordRateLimitWait.Record(ctx, 0, metric.WithAttributes(append(attrs, attribute.String(FieldRateLimitScope, observation.RateLimitScope))...))
	if observation.Attempts > 1 {
		p.discordRetries.Add(ctx, int64(observation.Attempts-1), metric.WithAttributes(append(attrs, attribute.String(FieldRetryReason, observation.RetryReason))...))
	}
	if observation.Partial != nil {
		p.discordPartial.Add(ctx, 1, metric.WithAttributes(append(attrs, attribute.String(FieldResult, string(observation.Partial.Outcome)))...))
	}
	return err
}

func (p *Provider) ObserveReconciliation(ctx context.Context, observation application.ReconciliationObservation) {
	spanAttributes := []attribute.KeyValue{
		attribute.String(FieldCorrelationID, string(observation.CorrelationID)),
		attribute.String(FieldPlanHashPrefix, observation.PlanHashPrefix),
		attribute.String(FieldRunID, string(observation.RunID)),
		attribute.String(FieldStepID, string(observation.OperationID)),
		attribute.String(FieldResourceKind, string(observation.ResourceKind)),
		attribute.String(FieldRisk, string(observation.Risk)),
		attribute.String(FieldOutcome, observation.Outcome),
	}
	_, span := p.tracer.Start(ctx, "reconciliation."+observation.Outcome, trace.WithAttributes(spanAttributes...))
	span.End()
	metricAttributes := metric.WithAttributes(
		attribute.String(FieldResourceKind, string(observation.ResourceKind)),
		attribute.String(FieldRisk, string(observation.Risk)),
		attribute.String(FieldOutcome, observation.Outcome),
	)
	if observation.OperationCount > 0 {
		p.reconciliationPlanSize.Record(ctx, int64(observation.OperationCount), metricAttributes)
	}
	for risk, count := range observation.RiskCounts {
		if count > 0 {
			p.reconciliationRisk.Add(ctx, int64(count), metric.WithAttributes(attribute.String(FieldRisk, string(risk))))
		}
	}
	if observation.ManagedDrift > 0 {
		p.reconciliationDrift.Add(ctx, int64(observation.ManagedDrift), metricAttributes)
		if observation.ResourceKind == domain.ResourceAutoModerationRule {
			p.autoModerationDrift.Add(ctx, int64(observation.ManagedDrift), metric.WithAttributes(attribute.String(FieldOutcome, observation.Outcome)))
		}
	}
	if observation.UnmanagedConflicts > 0 {
		p.reconciliationConflicts.Add(ctx, int64(observation.UnmanagedConflicts), metricAttributes)
	}
	if observation.Latency > 0 {
		p.reconciliationStepLatency.Record(ctx, float64(observation.Latency.Microseconds())/1000, metricAttributes)
	}
	if observation.Retries > 0 {
		p.reconciliationRetries.Add(ctx, int64(observation.Retries), metricAttributes)
	}
	if observation.Partial {
		p.reconciliationPartial.Add(ctx, 1, metricAttributes)
	}
	if observation.Resumed {
		p.reconciliationResumes.Add(ctx, 1, metricAttributes)
	}
	if observation.Outcome != "" {
		p.reconciliationOutcomes.Add(ctx, 1, metricAttributes)
	}
	p.Logger.InfoContext(ctx, "reconciliation event", FieldCorrelationID, observation.CorrelationID, FieldPlanHashPrefix, observation.PlanHashPrefix, FieldRunID, observation.RunID, FieldStepID, observation.OperationID, FieldResourceKind, observation.ResourceKind, FieldRisk, observation.Risk, FieldOutcome, observation.Outcome, FieldRetryCount, observation.Retries)
}

func (p *Provider) ObserveModeration(ctx context.Context, observation application.ModerationObservation) {
	if p == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("moderation.action.kind", string(observation.ActionKind)), attribute.String("moderation.outcome", observation.Outcome), attribute.String("moderation.reason_class", observation.Reason), attribute.String("moderation.policy.version", observation.PolicyVersion))
	if observation.ActionKind != "" {
		p.moderationActions.Add(ctx, 1, attrs)
	}
	if observation.Latency > 0 {
		p.moderationDuration.Record(ctx, float64(observation.Latency.Microseconds())/1000, attrs)
	}
	if observation.Reason == "hierarchy" {
		p.moderationHierarchyFailures.Add(ctx, 1, attrs)
	}
	if observation.ActionKind == "" {
		p.escalationOutcomes.Add(ctx, 1, attrs)
	}
}

func (p *Provider) ObserveLockdown(ctx context.Context, observation application.LockdownObservation) {
	if p == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("lockdown.operation", observation.Operation), attribute.String("lockdown.outcome", observation.Outcome))
	p.lockdownEvents.Add(ctx, 1, attrs)
	if observation.OperationCount > 0 {
		p.lockdownOperationCount.Record(ctx, int64(observation.OperationCount), attrs)
	}
}

func (p *Provider) Shutdown(ctx context.Context) error {
	return errors.Join(p.MeterProvider.Shutdown(ctx), p.TracerProvider.Shutdown(ctx))
}

func CorrelationID() (domain.CorrelationID, error) {
	var value [16]byte
	if _, err := io.ReadFull(rand.Reader, value[:]); err != nil {
		return "", fmt.Errorf("read OS randomness: %w", err)
	}
	return domain.CorrelationID(hex.EncodeToString(value[:])), nil
}
