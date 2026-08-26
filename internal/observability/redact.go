package observability

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/trace"
)

type Redactor struct {
	mu            sync.RWMutex
	values        []string
	hook          func()
	interventions atomic.Int64
}

func NewRedactor(values ...string) *Redactor {
	r := &Redactor{}
	for _, value := range values {
		r.RegisterSecret(value)
	}
	return r
}

func (r *Redactor) RegisterSecret(value string) {
	if r == nil || value == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.values {
		if existing == value {
			return
		}
	}
	r.values = append(r.values, value)
}

func (r *Redactor) SetInterventionHook(hook func()) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hook = hook
}

func (r *Redactor) Sanitize(value string) (string, bool) {
	if r == nil {
		return value, false
	}
	r.mu.RLock()
	secrets := append([]string(nil), r.values...)
	hook := r.hook
	r.mu.RUnlock()
	changed := false
	for _, secret := range secrets {
		if strings.Contains(value, secret) {
			changed = true
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	if changed {
		r.interventions.Add(1)
		if hook != nil {
			hook()
		}
	}
	return value, changed
}

func (r *Redactor) String(value string) string { sanitized, _ := r.Sanitize(value); return sanitized }

func (r *Redactor) Interventions() int64 {
	if r == nil {
		return 0
	}
	return r.interventions.Load()
}

type redactingHandler struct {
	next     slog.Handler
	redactor *Redactor
}

func (h redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}
func (h redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	copy := slog.NewRecord(record.Time, record.Level, h.redactor.String(record.Message), record.PC)
	record.Attrs(func(attr slog.Attr) bool { copy.AddAttrs(h.redactAttr(attr)); return true })
	if correlation := correlationFromContext(ctx); correlation != "" {
		copy.AddAttrs(slog.String(FieldCorrelationID, correlation))
	}
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		copy.AddAttrs(slog.String(FieldTraceID, span.TraceID().String()), slog.String(FieldSpanID, span.SpanID().String()))
	}
	return h.next.Handle(ctx, copy)
}
func (h redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		redacted[i] = h.redactAttr(attr)
	}
	return redactingHandler{next: h.next.WithAttrs(redacted), redactor: h.redactor}
}
func (h redactingHandler) WithGroup(name string) slog.Handler {
	return redactingHandler{next: h.next.WithGroup(name), redactor: h.redactor}
}
func (h redactingHandler) redactAttr(attr slog.Attr) slog.Attr {
	attr.Value = attr.Value.Resolve()
	if attr.Value.Kind() == slog.KindString {
		attr.Value = slog.StringValue(h.redactor.String(attr.Value.String()))
	}
	if attr.Value.Kind() == slog.KindGroup {
		group := attr.Value.Group()
		for i := range group {
			group[i] = h.redactAttr(group[i])
		}
		attr.Value = slog.GroupValue(group...)
	}
	return attr
}
