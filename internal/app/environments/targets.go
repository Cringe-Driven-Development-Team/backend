package environments

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

const targetsQuery = `
	SELECT DISTINCT ON (server_id) server_id, state
	FROM (
		SELECT server_id, CASE state
			WHEN 'READY' THEN 'active'
			WHEN 'DELETING' THEN 'deleting'
			WHEN 'CLEANUP_PENDING' THEN 'cleanup'
			WHEN 'FAILED' THEN 'failed'
			ELSE 'creating' END AS state, 0 AS priority
		FROM environments.runtime_environments
		WHERE NULLIF(server_id, '') IS NOT NULL AND state <> 'DELETED'
		UNION ALL
		SELECT server_id, CASE state
			WHEN 'READY' THEN 'warm'
			WHEN 'DELETING' THEN 'deleting'
			ELSE 'creating' END AS state, 1 AS priority
		FROM environments.runtime_pool
		WHERE NULLIF(server_id, '') IS NOT NULL AND state <> 'CLAIMED'
	) targets
	ORDER BY server_id, priority
`

type targetInventory struct {
	pool              *pgxpool.Pool
	target, available *prometheus.Desc
}

// Describe объявляет состав метрик inventory.
func (m *targetInventory) Describe(ch chan<- *prometheus.Desc) {
	ch <- m.target
	ch <- m.available
}

// Collect читает один согласованный снимок inventory с ограниченным временем запроса.
func (m *targetInventory) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	rows, err := m.pool.Query(ctx, targetsQuery)
	ok := float64(0)
	if err == nil {
		defer rows.Close()
		var targets []prometheus.Metric
		for rows.Next() {
			var id, state string
			if err = rows.Scan(&id, &state); err != nil {
				break
			}
			targets = append(targets, prometheus.MustNewConstMetric(m.target, prometheus.GaugeValue, 1, id, state))
		}
		if err == nil && rows.Err() == nil {
			ok = 1
			for _, sample := range targets {
				ch <- sample
			}
		}
	}
	ch <- prometheus.MustNewConstMetric(m.available, prometheus.GaugeValue, ok)
}

// registerTargetMetrics подключает inventory Environments по OpenStack server ID.
func registerTargetMetrics(reg *prometheus.Registry, pool *pgxpool.Pool) {
	reg.MustRegister(&targetInventory{
		pool:      pool,
		target:    prometheus.NewDesc("cellestial_runtime_target_info", "Runtime VPS expected by Environments.", []string{"server_id", "state"}, nil),
		available: prometheus.NewDesc("cellestial_runtime_inventory_available", "Whether the runtime inventory query succeeded.", nil, nil),
	})
}
