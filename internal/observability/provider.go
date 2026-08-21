package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

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
}

type Provider struct {
	Logger         *slog.Logger
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	tracer         trace.Tracer
	counter        metric.Int64Counter
	duration       metric.Float64Histogram
	build          buildinfo.Info
	redactor       *Redactor
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
	logger := slog.New(redactingHandler{next: slog.NewJSONHandler(options.Writer, nil), redactor: options.Redactor})
	return &Provider{Logger: logger, TracerProvider: tp, MeterProvider: mp, tracer: tp.Tracer("discord-control"), counter: counter, duration: duration, build: options.Build, redactor: options.Redactor}, nil
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
		spans, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(options.Endpoint))
		if err != nil {
			return nil, nil, err
		}
		metrics, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(options.Endpoint))
		if err != nil {
			return nil, nil, err
		}
		return spans, sdkmetric.NewPeriodicReader(metrics), nil
	default:
		return nil, nil, errors.New("unsupported telemetry exporter")
	}
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
