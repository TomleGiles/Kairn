// Package otel initialise OpenTelemetry (traces et métriques) pour tous les
// services Go. Sans point de collecte configuré, les fournisseurs restent
// inactifs (no-op) : aucune donnée ne quitte le processus.
package otel

import (
	"context"
	"errors"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Setup configure les exportateurs OTLP/HTTP si endpoint est renseigné.
// La fonction renvoyée arrête proprement les fournisseurs.
func Setup(ctx context.Context, service, endpoint string) (func(context.Context), error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	noop := func(context.Context) {}
	if endpoint == "" {
		return noop, nil
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(service)))
	if err != nil {
		return noop, err
	}
	te, err := otlptracehttp.New(ctx)
	if err != nil {
		return noop, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(te), sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.2))))
	otel.SetTracerProvider(tp)
	me, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return func(c context.Context) { _ = tp.Shutdown(c) }, err
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(sdkmetric.NewPeriodicReader(me, sdkmetric.WithInterval(30*time.Second))))
	otel.SetMeterProvider(mp)
	return func(c context.Context) {
		c, cancel := context.WithTimeout(c, 5*time.Second)
		defer cancel()
		_ = errors.Join(tp.Shutdown(c), mp.Shutdown(c))
	}, nil
}

// HTTPHandler instrumente un handler HTTP (traces et métriques de requêtes).
func HTTPHandler(h http.Handler, name string) http.Handler {
	return otelhttp.NewHandler(h, name, otelhttp.WithFilter(func(r *http.Request) bool {
		return r.URL.Path != "/healthz" && r.URL.Path != "/readyz"
	}))
}

// HTTPClient renvoie un client HTTP instrumenté.
func HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: otelhttp.NewTransport(http.DefaultTransport)}
}
