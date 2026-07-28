package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer = otel.Tracer("ilter-proxy")
	meter  = otel.Meter("ilter-proxy")

	requestCounter metric.Int64Counter
	requestLatency metric.Float64Histogram

	promptRequests metric.Int64Counter
	promptErrors   metric.Int64Counter

	// Handler exposes the Prometheus metrics HTTP handler.
	Handler http.Handler

	guardrailsMeter         = otel.Meter("ilter-guardrails")
	guardrailsCheckedTotal  metric.Int64Counter
	guardrailsBlockedTotal  metric.Int64Counter
	guardrailsWarnedTotal   metric.Int64Counter
	guardrailsCheckDuration metric.Float64Histogram
)

var (
	initOnce           sync.Once
	initGuardrailsOnce sync.Once
)

// InitGuardrailsMetrics initializes guardrails OTel instruments.
func InitGuardrailsMetrics() error {
	var initErr error
	initGuardrailsOnce.Do(func() {
		var err error

		guardrailsCheckedTotal, err = guardrailsMeter.Int64Counter(
			"ilter_guardrails_checked_total",
			metric.WithDescription("Total number of requests checked by guardrails"),
		)
		if err != nil {
			initErr = fmt.Errorf("create guardrails checked counter: %w", err)
			return
		}

		guardrailsBlockedTotal, err = guardrailsMeter.Int64Counter(
			"ilter_guardrails_blocked_total",
			metric.WithDescription("Total number of requests blocked by guardrails"),
		)
		if err != nil {
			initErr = fmt.Errorf("create guardrails blocked counter: %w", err)
			return
		}

		guardrailsWarnedTotal, err = guardrailsMeter.Int64Counter(
			"ilter_guardrails_warned_total",
			metric.WithDescription("Total number of requests warned by guardrails"),
		)
		if err != nil {
			initErr = fmt.Errorf("create guardrails warned counter: %w", err)
			return
		}

		guardrailsCheckDuration, err = guardrailsMeter.Float64Histogram(
			"ilter_guardrails_check_duration_ms",
			metric.WithDescription("Guardrails check duration in milliseconds"),
		)
		if err != nil {
			initErr = fmt.Errorf("create guardrails check duration histogram: %w", err)
			return
		}
	})
	return initErr
}

// InitMetrics initializes OTel meter instruments and the Prometheus exporter.
func InitMetrics() error {
	var initErr error
	initOnce.Do(func() {
		reg := prometheus.NewRegistry()

		exporter, err := otelprom.New(otelprom.WithRegisterer(reg))
		if err != nil {
			initErr = fmt.Errorf("failed to create prometheus exporter: %w", err)
			return
		}

		provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
		otel.SetMeterProvider(provider)

		Handler = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})

		requestCounter, err = meter.Int64Counter(
			"ilter_http_requests_total",
			metric.WithDescription("Total number of HTTP requests processed"),
		)
		if err != nil {
			initErr = fmt.Errorf("failed to create request counter: %w", err)
			return
		}

		requestLatency, err = meter.Float64Histogram(
			"ilter_http_request_duration_seconds",
			metric.WithDescription("Histogram of response latency (seconds) of HTTP requests"),
		)
		if err != nil {
			initErr = fmt.Errorf("failed to create request latency histogram: %w", err)
			return
		}

		promptRequests, err = meter.Int64Counter(
			"ilter_prompt_requests_total",
			metric.WithDescription("Total number of prompt template injection requests"),
		)
		if err != nil {
			initErr = fmt.Errorf("failed to create prompt request counter: %w", err)
			return
		}

		promptErrors, err = meter.Int64Counter(
			"ilter_prompt_render_errors_total",
			metric.WithDescription("Total number of prompt template rendering errors"),
		)
		if err != nil {
			initErr = fmt.Errorf("failed to create prompt error counter: %w", err)
			return
		}
	})
	return initErr
}

type metricsRecorder struct {
	http.ResponseWriter
	statusCode int
	wrote      bool
}

func (rw *metricsRecorder) Write(b []byte) (int, error) {
	if !rw.wrote {
		rw.wrote = true
		rw.statusCode = http.StatusOK
	}
	return rw.ResponseWriter.Write(b)
}

func (rw *metricsRecorder) WriteHeader(code int) {
	if rw.wrote {
		return
	}
	rw.wrote = true
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *metricsRecorder) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// ObservabilityHandler wraps the standard http.Handler to collect OTel metrics and traces.
func ObservabilityHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		ctx, span := tracer.Start(r.Context(), "HTTP "+r.Method+" "+r.URL.Path, trace.WithAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.url", r.URL.String()),
		))
		defer span.End()

		rec := &metricsRecorder{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		r = r.WithContext(ctx)

		next.ServeHTTP(rec, r)

		duration := time.Since(start).Seconds()

		span.SetAttributes(
			attribute.Int("http.status_code", rec.statusCode),
		)

		attrs := metric.WithAttributes(
			attribute.String("method", r.Method),
			attribute.String("status", strconv.Itoa(rec.statusCode)),
		)
		requestCounter.Add(ctx, 1, attrs)
		requestLatency.Record(ctx, duration, attrs)
	})
}

// InitTracer initializes a global OpenTelemetry trace provider sending spans to the OTLP endpoint.
func InitTracer(ctx context.Context, endpoint string, samplingRatio float64) (*sdktrace.TracerProvider, error) {
	if endpoint == "" {
		return nil, nil
	}

	exporter, err := otlptracehttp.New(
		ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(), // Usually OTLP collector is insecure locally
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(samplingRatio))

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(tp)
	return tp, nil
}
