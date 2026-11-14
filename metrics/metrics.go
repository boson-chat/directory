// Package metrics defines the directory's OpenTelemetry instruments and the
// meter provider that exports them (Prometheus on the internal listener's
// /metrics, optional OTLP push).
package metrics

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// ScopeName is the instrumentation scope of every instrument.
const ScopeName = "github.com/boson-chat/directory"

// Metrics holds the instruments. Use New or Noop.
type Metrics struct {
	registrations metric.Int64Counter
	verifications metric.Int64Counter
	statusChanges metric.Int64Counter
	healthProbes  metric.Int64Counter
	workerRuns    metric.Int64Counter
	workerSeconds metric.Float64Histogram
	rateLimited   metric.Int64Counter
	// Extra is for other packages (federation) to create instruments on.
	Meter metric.Meter
}

// New creates the instruments on mp.
func New(mp metric.MeterProvider) (*Metrics, error) {
	m := mp.Meter(ScopeName)
	var err error
	c := func(name, desc string) metric.Int64Counter {
		if err != nil {
			return nil
		}
		var ctr metric.Int64Counter
		ctr, err = m.Int64Counter(name, metric.WithDescription(desc), metric.WithUnit("{event}"))
		return ctr
	}
	out := &Metrics{
		Meter:         m,
		registrations: c("directory.registrations", "Listings registered, by owner kind (account | network_key)."),
		verifications: c("directory.verifications", "DNS checks by outcome (match | miss | error)."),
		statusChanges: c("directory.status_changes", "Listing status transitions, by from/to."),
		healthProbes:  c("directory.health.probes", "Health probes by result (up | down | unknown)."),
		workerRuns:    c("directory.worker.runs", "Background worker runs by worker and outcome (ok | error | skipped)."),
		rateLimited:   c("directory.rate_limit.hits", "Requests rejected by a rate limiter, by bucket."),
	}
	if err == nil {
		out.workerSeconds, err = m.Float64Histogram("directory.worker.duration", metric.WithDescription("Worker run time."), metric.WithUnit("s"))
	}
	if err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}
	return out, nil
}

// Noop returns instruments that record nothing.
func Noop() *Metrics {
	m, _ := New(noop.NewMeterProvider())
	return m
}

func (m *Metrics) Registration(ctx context.Context, kind string) {
	m.registrations.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", kind)))
}

func (m *Metrics) Verification(ctx context.Context, outcome string) {
	m.verifications.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

func (m *Metrics) StatusChange(ctx context.Context, from, to string) {
	m.statusChanges.Add(ctx, 1, metric.WithAttributes(attribute.String("from", from), attribute.String("to", to)))
}

func (m *Metrics) HealthProbe(ctx context.Context, result string) {
	m.healthProbes.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}

func (m *Metrics) WorkerRun(ctx context.Context, worker, outcome string, seconds float64) {
	attrs := metric.WithAttributes(attribute.String("worker", worker), attribute.String("outcome", outcome))
	m.workerRuns.Add(ctx, 1, attrs)
	if outcome != "skipped" {
		m.workerSeconds.Record(ctx, seconds, metric.WithAttributes(attribute.String("worker", worker)))
	}
}

func (m *Metrics) RateLimited(ctx context.Context, bucket string) {
	m.rateLimited.Add(ctx, 1, metric.WithAttributes(attribute.String("bucket", bucket)))
}
