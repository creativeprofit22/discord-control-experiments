package observability

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type SecurityAlert struct {
	Kind             domain.SecurityEventKind
	Reason           domain.PolicyReason
	Mode             domain.ExecutionMode
	DestructiveClass domain.DestructiveClass
	CorrelationID    domain.CorrelationID
	Threshold        bool
}

type SecurityAlertSink interface {
	Alert(context.Context, SecurityAlert)
}

type SecurityOptions struct {
	DenialThreshold            int
	DenialWindow               time.Duration
	DestructiveThreshold       int
	DestructiveWindow          time.Duration
	SignatureFailureThreshold  int
	SignatureFailureWindow     time.Duration
	AckDeadlineMissThreshold   int
	AckDeadlineMissWindow      time.Duration
	CallbackFailureThreshold   int
	CallbackFailureWindow      time.Duration
	HandlerSaturationThreshold int
	HandlerSaturationWindow    time.Duration
	AlertSink                  SecurityAlertSink
	Now                        func() time.Time
}

type loggerAlertSink struct{ logger *slog.Logger }

func (s loggerAlertSink) Alert(ctx context.Context, alert SecurityAlert) {
	s.logger.WarnContext(ctx, "Discord security alert", "kind", alert.Kind, "reason", alert.Reason, "mode", alert.Mode, "destructive_class", alert.DestructiveClass, "correlation_id", alert.CorrelationID, "threshold", alert.Threshold)
}

type windowState struct {
	times         []time.Time
	suppressUntil time.Time
}
type securityMonitor struct {
	mu              sync.Mutex
	denial          windowState
	destructive     windowState
	signature       windowState
	ackDeadline     windowState
	callbackFailure windowState
	saturation      windowState
	options         SecurityOptions
}

func newSecurityMonitor(options SecurityOptions, logger *slog.Logger) *securityMonitor {
	if options.DenialThreshold <= 0 || options.DenialThreshold > 10000 {
		options.DenialThreshold = 10
	}
	if options.DestructiveThreshold <= 0 || options.DestructiveThreshold > 10000 {
		options.DestructiveThreshold = 5
	}
	if options.DenialWindow < time.Second || options.DenialWindow > 24*time.Hour {
		options.DenialWindow = 5 * time.Minute
	}
	if options.DestructiveWindow < time.Second || options.DestructiveWindow > 24*time.Hour {
		options.DestructiveWindow = 5 * time.Minute
	}
	if options.SignatureFailureThreshold <= 0 {
		options.SignatureFailureThreshold = 20
	}
	if options.AckDeadlineMissThreshold <= 0 {
		options.AckDeadlineMissThreshold = 3
	}
	if options.CallbackFailureThreshold <= 0 {
		options.CallbackFailureThreshold = 5
	}
	if options.HandlerSaturationThreshold <= 0 {
		options.HandlerSaturationThreshold = 5
	}
	if options.SignatureFailureWindow < time.Second {
		options.SignatureFailureWindow = 5 * time.Minute
	}
	if options.AckDeadlineMissWindow < time.Second {
		options.AckDeadlineMissWindow = 5 * time.Minute
	}
	if options.CallbackFailureWindow < time.Second {
		options.CallbackFailureWindow = 5 * time.Minute
	}
	if options.HandlerSaturationWindow < time.Second {
		options.HandlerSaturationWindow = 5 * time.Minute
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.AlertSink == nil {
		options.AlertSink = loggerAlertSink{logger: logger}
	}
	return &securityMonitor{options: options}
}

func (p *Provider) RecordSecurityEvent(ctx context.Context, event domain.SecurityEvent) {
	defer func() { _ = recover() }()
	if p == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("security.event.kind", string(event.Kind)), attribute.String("security.policy.reason", string(event.Reason)), attribute.String("security.execution.mode", string(event.Mode)), attribute.String("security.destructive.class", string(event.DestructiveClass)))
	p.securityEvents.Add(ctx, 1, attrs)
	immediate := event.Kind == "cross_guild" || event.Kind == "policy_error" || event.Kind == "approval_bypass" || event.Kind == "approval_invalid" || event.Kind == "redaction_intervention" || event.Kind == "audit_precommit_failure" || event.Kind == "audit_completion_failure"
	if immediate {
		p.securityMonitor.options.AlertSink.Alert(ctx, SecurityAlert{Kind: event.Kind, Reason: event.Reason, Mode: event.Mode, DestructiveClass: event.DestructiveClass, CorrelationID: event.CorrelationID})
	}
	if event.Kind == "policy_denial" {
		p.securityMonitor.record(ctx, &p.securityMonitor.denial, p.securityMonitor.options.DenialThreshold, p.securityMonitor.options.DenialWindow, event)
	}
	if event.Kind == "destructive_apply" {
		p.securityMonitor.record(ctx, &p.securityMonitor.destructive, p.securityMonitor.options.DestructiveThreshold, p.securityMonitor.options.DestructiveWindow, event)
	}
	switch event.Kind {
	case "interaction_signature_failure":
		p.securityMonitor.record(ctx, &p.securityMonitor.signature, p.securityMonitor.options.SignatureFailureThreshold, p.securityMonitor.options.SignatureFailureWindow, event)
	case "interaction_ack_deadline_miss":
		p.securityMonitor.record(ctx, &p.securityMonitor.ackDeadline, p.securityMonitor.options.AckDeadlineMissThreshold, p.securityMonitor.options.AckDeadlineMissWindow, event)
	case "interaction_callback_failure":
		p.securityMonitor.record(ctx, &p.securityMonitor.callbackFailure, p.securityMonitor.options.CallbackFailureThreshold, p.securityMonitor.options.CallbackFailureWindow, event)
	case "interaction_handler_saturation":
		p.securityMonitor.record(ctx, &p.securityMonitor.saturation, p.securityMonitor.options.HandlerSaturationThreshold, p.securityMonitor.options.HandlerSaturationWindow, event)
	}
}

func (m *securityMonitor) record(ctx context.Context, state *windowState, threshold int, window time.Duration, event domain.SecurityEvent) {
	now := m.options.Now().UTC()
	m.mu.Lock()
	first := 0
	for first < len(state.times) && now.Sub(state.times[first]) >= window {
		first++
	}
	state.times = append(state.times[:0], state.times[first:]...)
	state.times = append(state.times, now)
	alert := len(state.times) >= threshold && !now.Before(state.suppressUntil)
	if alert {
		state.suppressUntil = now.Add(window)
	}
	m.mu.Unlock()
	if alert {
		m.options.AlertSink.Alert(ctx, SecurityAlert{Kind: event.Kind, Reason: event.Reason, Mode: event.Mode, DestructiveClass: event.DestructiveClass, CorrelationID: event.CorrelationID, Threshold: true})
	}
}

func (p *Provider) ObserveApprovalLatency(ctx context.Context, duration time.Duration) {
	defer func() { _ = recover() }()
	if p == nil || duration < 0 {
		return
	}
	p.approvalLatency.Record(ctx, float64(duration.Microseconds())/1000)
}
