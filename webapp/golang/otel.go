package main

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

func initTracerProvider(ctx context.Context) (func(context.Context) error, error) {
	client := otlptracehttp.NewClient(
		otlptracehttp.WithEndpoint("otlp-vaxila.mackerelio.com"),
		otlptracehttp.WithHeaders(map[string]string{
			"Accept":           "*/*",
			"Mackerel-Api-Key": os.Getenv("MACKEREL_API_KEY"),
		}),
		otlptracehttp.WithCompression(otlptracehttp.GzipCompression),
	)
	exporter, err := otlptrace.New(ctx, client)
	if err != nil {
		return nil, err
	}

	resources, err := resource.New(
		ctx,
		resource.WithProcessPID(),
		resource.WithHost(),
		resource.WithAttributes(
			semconv.ServiceName("private-isu"),
			semconv.ServiceVersion("v0.0.0"),
			semconv.DeploymentEnvironment("production"),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(resources),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tp.Shutdown, nil
}

func initMetricProvider(ctx context.Context) (func(context.Context) error, error) {
	exporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint("otlp.mackerelio.com:4317"),
		otlpmetricgrpc.WithHeaders(map[string]string{
			"Mackerel-Api-Key": os.Getenv("MACKEREL_API_KEY"),
		}),
		otlpmetricgrpc.WithCompressor("gzip"),
	)
	if err != nil {
		return nil, err
	}

	resources, err := resource.New(
		ctx,
		resource.WithProcessPID(),
		resource.WithHost(),
		resource.WithAttributes(
			semconv.ServiceName("private-isu"),
			semconv.ServiceVersion("v0.0.0"),
			semconv.DeploymentEnvironment("production"),
		),
	)
	if err != nil {
		return nil, err
	}

	mp := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(exporter)),
		metric.WithResource(resources),
	)
	otel.SetMeterProvider(mp)

	return mp.Shutdown, nil
}
