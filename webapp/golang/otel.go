package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

func initTracerProvider(ctx context.Context) (func(context.Context) error, error) {
	var exporters []trace.SpanExporter

	// OTLP エクスポーター（環境変数で制御、デフォルトで有効）
	if os.Getenv("OTEL_DISABLE_OTLP") != "true" {
		client := otlptracehttp.NewClient(
			otlptracehttp.WithEndpoint("otlp-vaxila.mackerelio.com"),
			otlptracehttp.WithHeaders(map[string]string{
				"Accept":           "*/*",
				"Mackerel-Api-Key": os.Getenv("MACKEREL_API_KEY"),
			}),
			otlptracehttp.WithCompression(otlptracehttp.GzipCompression),
			otlptracehttp.WithTimeout(10*time.Second),
			otlptracehttp.WithRetry(otlptracehttp.RetryConfig{
				Enabled:         true, // リトライを有効化
				InitialInterval: 1 * time.Second,
				MaxInterval:     5 * time.Second,
				MaxElapsedTime:  30 * time.Second, // 制限エラーの場合は早めに諦める
			}),
		)
		otlpExporter, err := otlptrace.New(ctx, client)
		if err != nil {
			log.Printf("Warning: Failed to create OTLP trace exporter: %v", err)
		} else {
			exporters = append(exporters, otlpExporter)
		}
	}

	// コンソール エクスポーター（環境変数で制御）
	if os.Getenv("OTEL_ENABLE_CONSOLE_OUTPUT") == "true" {
		consoleExporter, err := stdouttrace.New(
			stdouttrace.WithPrettyPrint(),
		)
		if err != nil {
			return nil, err
		}
		exporters = append(exporters, consoleExporter)
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

	// サンプリング率を環境変数から取得（デフォルト10%）
	samplingRate := 0.1
	if rateStr := os.Getenv("OTEL_TRACE_SAMPLING_RATE"); rateStr != "" {
		if rate, err := strconv.ParseFloat(rateStr, 64); err == nil && rate >= 0.0 && rate <= 1.0 {
			samplingRate = rate
		} else {
			log.Printf("Warning: Invalid OTEL_TRACE_SAMPLING_RATE value '%s', using default 0.1", rateStr)
		}
	}

	// ペアレントベースサンプリング設定
	// ペアレントがある場合は常にトレース、ない場合はサンプリングしない
	sampler := trace.ParentBased(
		trace.TraceIDRatioBased(samplingRate), // 環境変数で設定可能なサンプリング率
		trace.WithRemoteParentSampled(trace.AlwaysSample()),    // リモートペアレントがサンプルされている場合は常にサンプル
		trace.WithRemoteParentNotSampled(trace.NeverSample()),  // リモートペアレントがサンプルされていない場合はサンプルしない
		trace.WithLocalParentSampled(trace.AlwaysSample()),     // ローカルペアレントがサンプルされている場合は常にサンプル
		trace.WithLocalParentNotSampled(trace.NeverSample()),   // ローカルペアレントがサンプルされていない場合はサンプルしない
	)

	// エクスポーターが一つもない場合はノープエクスポーターを使用
	if len(exporters) == 0 {
		log.Println("Warning: No trace exporters configured, using no-op tracer")
		tp := trace.NewTracerProvider(
			trace.WithResource(resources),
			trace.WithSampler(trace.NeverSample()),
		)
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.TraceContext{})
		return tp.Shutdown, nil
	}

	// マルチエクスポーター設定
	var tpOptions []trace.TracerProviderOption
	for _, exp := range exporters {
		tpOptions = append(tpOptions, trace.WithBatcher(exp))
	}
	tpOptions = append(tpOptions, trace.WithResource(resources))
	tpOptions = append(tpOptions, trace.WithSampler(sampler))

	tp := trace.NewTracerProvider(tpOptions...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tp.Shutdown, nil
}

func initMetricProvider(ctx context.Context) (func(context.Context) error, error) {
	var readers []metric.Reader

	// OTLP メトリクス エクスポーター（環境変数で制御、デフォルトで有効）
	if os.Getenv("OTEL_DISABLE_OTLP") != "true" {
		otlpExporter, err := otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithEndpoint("otlp.mackerelio.com:4317"),
			otlpmetricgrpc.WithHeaders(map[string]string{
				"Mackerel-Api-Key": os.Getenv("MACKEREL_API_KEY"),
			}),
			otlpmetricgrpc.WithCompressor("gzip"),
			otlpmetricgrpc.WithTimeout(10*time.Second),
			otlpmetricgrpc.WithRetry(otlpmetricgrpc.RetryConfig{
				Enabled:         true, // リトライを有効化
				InitialInterval: 1 * time.Second,
				MaxInterval:     5 * time.Second,
				MaxElapsedTime:  30 * time.Second, // 制限エラーの場合は早めに諦める
			}),
		)
		if err != nil {
			log.Printf("Warning: Failed to create OTLP metric exporter: %v", err)
		} else {
			readers = append(readers, metric.NewPeriodicReader(otlpExporter))
		}
	}

	// コンソール メトリクス エクスポーター（環境変数で制御）
	if os.Getenv("OTEL_ENABLE_CONSOLE_OUTPUT") == "true" {
		consoleExporter, err := stdoutmetric.New(
			stdoutmetric.WithPrettyPrint(),
		)
		if err != nil {
			return nil, err
		}
		readers = append(readers, metric.NewPeriodicReader(consoleExporter))
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

	// リーダーが一つもない場合はノープメーターを使用
	if len(readers) == 0 {
		log.Println("Warning: No metric readers configured, using no-op meter")
		mp := metric.NewMeterProvider(
			metric.WithResource(resources),
		)
		otel.SetMeterProvider(mp)
		return mp.Shutdown, nil
	}

	// マルチリーダー設定
	var mpOptions []metric.Option
	for _, reader := range readers {
		mpOptions = append(mpOptions, metric.WithReader(reader))
	}
	mpOptions = append(mpOptions, metric.WithResource(resources))

	mp := metric.NewMeterProvider(mpOptions...)
	otel.SetMeterProvider(mp)

	return mp.Shutdown, nil
}
