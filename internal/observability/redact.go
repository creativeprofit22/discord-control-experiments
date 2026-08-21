package observability

import (
	"context"
	"log/slog"
	"strings"
)

type Redactor struct{ values []string }

func NewRedactor(values ...string) *Redactor {
	kept := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			kept = append(kept, value)
		}
	}
	return &Redactor{values: kept}
}

func (r *Redactor) String(value string) string {
	for _, secret := range r.values {
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	return value
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
