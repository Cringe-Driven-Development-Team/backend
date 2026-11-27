package workspace

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// queueMetrics обновляет агрегаты независимо от запросов Prometheus.
func queueMetrics(reg *prometheus.Registry, pool *pgxpool.Pool) func(context.Context) error {
	gauge := func(name, help string) prometheus.Gauge {
		value := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: serviceName, Name: name, Help: help})
		reg.MustRegister(value)
		return value
	}
	queued := gauge("execution_queue_length", "Accepted runtime commands awaiting dispatch.")
	age := gauge("execution_queue_oldest_seconds", "Age of the oldest command awaiting dispatch.")
	projections := gauge("projections_pending", "Results awaiting projection to notebook cells.")
	ok := gauge("domain_metrics_available", "Whether runtime database metrics could be collected.")
	return func(ctx context.Context) error {
		timer := time.NewTicker(5 * time.Second)
		defer timer.Stop()
		for ctx.Err() == nil {
			query, cancel := context.WithTimeout(ctx, 2*time.Second)
			var waiting, pending int64
			var seconds float64
			err := pool.QueryRow(query, `SELECT count(*), COALESCE(EXTRACT(EPOCH FROM now()-min(enqueued_at)),0)::float8,
				(SELECT count(*) FROM workspace.runtime_projections)
				FROM workspace.runtime_executions WHERE data->'Public'->>'state' IN ('PENDING','QUEUED')`).
				Scan(&waiting, &seconds, &pending)
			cancel()
			if err == nil {
				queued.Set(float64(waiting))
				age.Set(max(0, seconds))
				projections.Set(float64(pending))
				ok.Set(1)
			} else {
				ok.Set(0)
			}
			select {
			case <-ctx.Done():
			case <-timer.C:
			}
		}
		return nil
	}
}
