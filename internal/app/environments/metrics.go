package environments

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// runtimeMetrics читает только агрегаты по конечным состояниям provisioning, cleanup и warm pool.
func runtimeMetrics(reg *prometheus.Registry, pool *pgxpool.Pool) func(context.Context) error {
	gauge := func(name, help string) prometheus.Gauge {
		value := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: serviceName, Name: name, Help: help})
		reg.MustRegister(value)
		return value
	}
	provision := gauge("provisioning_pending", "Provisioning jobs queued or leased.")
	cleanup := gauge("cleanup_pending", "Cleanup jobs queued or leased.")
	ready := gauge("warm_pool_ready", "Ready unassigned environments.")
	stale := gauge("domain_metrics_available", "Whether environment database metrics could be collected.")
	return func(ctx context.Context) error {
		timer := time.NewTicker(5 * time.Second)
		defer timer.Stop()
		for ctx.Err() == nil {
			query, cancel := context.WithTimeout(ctx, 2*time.Second)
			var creating, deleting, prepared int64
			err := pool.QueryRow(query, `SELECT count(*) FILTER (WHERE kind='create'),count(*) FILTER (WHERE kind='delete'),
				(SELECT count(*) FROM environments.runtime_pool WHERE state='READY')
				FROM environments.runtime_jobs WHERE status IN ('queued','running')`).Scan(&creating, &deleting, &prepared)
			cancel()
			if err == nil {
				provision.Set(float64(creating))
				cleanup.Set(float64(deleting))
				ready.Set(float64(prepared))
				stale.Set(1)
			} else {
				stale.Set(0)
			}
			select {
			case <-ctx.Done():
			case <-timer.C:
			}
		}
		return nil
	}
}
