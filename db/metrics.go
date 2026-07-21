package db

import (
	"database/sql"

	"github.com/prometheus/client_golang/prometheus"
)

type dbStatsCollector struct {
	pool        *sql.DB
	descriptors []*prometheus.Desc
}

type dbStatMetric struct {
	name  string
	help  string
	value func(sql.DBStats) float64
}

var dbStatMetrics = []dbStatMetric{
	{
		name:  "gorm_dbstats_max_open_connections",
		help:  "Maximum number of open connections to the database.",
		value: func(stats sql.DBStats) float64 { return float64(stats.MaxOpenConnections) },
	},
	{
		name:  "gorm_dbstats_open_connections",
		help:  "The number of established connections both in use and idle.",
		value: func(stats sql.DBStats) float64 { return float64(stats.OpenConnections) },
	},
	{
		name:  "gorm_dbstats_in_use",
		help:  "The number of connections currently in use.",
		value: func(stats sql.DBStats) float64 { return float64(stats.InUse) },
	},
	{
		name:  "gorm_dbstats_idle",
		help:  "The number of idle connections.",
		value: func(stats sql.DBStats) float64 { return float64(stats.Idle) },
	},
	{
		name:  "gorm_dbstats_wait_count",
		help:  "The total number of connections waited for.",
		value: func(stats sql.DBStats) float64 { return float64(stats.WaitCount) },
	},
	{
		name:  "gorm_dbstats_wait_duration",
		help:  "The total time blocked waiting for a new connection.",
		value: func(stats sql.DBStats) float64 { return float64(stats.WaitDuration) },
	},
	{
		name:  "gorm_dbstats_max_idle_closed",
		help:  "The total number of connections closed due to SetMaxIdleConns.",
		value: func(stats sql.DBStats) float64 { return float64(stats.MaxIdleClosed) },
	},
	{
		name:  "gorm_dbstats_max_lifetime_closed",
		help:  "The total number of connections closed due to SetConnMaxLifetime.",
		value: func(stats sql.DBStats) float64 { return float64(stats.MaxLifetimeClosed) },
	},
	{
		name:  "gorm_dbstats_max_idletime_closed",
		help:  "The total number of connections closed due to SetConnMaxIdleTime.",
		value: func(stats sql.DBStats) float64 { return float64(stats.MaxIdleTimeClosed) },
	},
}

func newDBStatsCollector(pool *sql.DB, databaseName string) *dbStatsCollector {
	labels := prometheus.Labels{"db_name": databaseName}
	descriptors := make([]*prometheus.Desc, 0, len(dbStatMetrics))
	for _, metric := range dbStatMetrics {
		descriptors = append(descriptors, prometheus.NewDesc(metric.name, metric.help, nil, labels))
	}
	return &dbStatsCollector{pool: pool, descriptors: descriptors}
}

func (collector *dbStatsCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, descriptor := range collector.descriptors {
		ch <- descriptor
	}
}

func (collector *dbStatsCollector) Collect(ch chan<- prometheus.Metric) {
	stats := collector.pool.Stats()
	for index, metric := range dbStatMetrics {
		ch <- prometheus.MustNewConstMetric(collector.descriptors[index], prometheus.GaugeValue, metric.value(stats))
	}
}
