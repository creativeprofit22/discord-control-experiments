package observability

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var cliFailureClasses = map[string]bool{"parse": true, "policy": true, "discord": true, "reconciliation": true, "timeout": true, "internal": true, "interrupted": true}

func (p *Provider) RecordCLIFailure(ctx context.Context, class string) {
	if p == nil || !cliFailureClasses[class] {
		class = "internal"
	}
	p.cliFailures.Add(ctx, 1, metric.WithAttributes(attribute.String("cli.failure.class", class)))
}
