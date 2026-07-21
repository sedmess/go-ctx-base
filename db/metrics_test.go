package db

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestDatabaseMetricsLifecycle(t *testing.T) {
	registry := prometheus.NewRegistry()
	connection := newTestConnection(t, "database-metrics")
	connection.registerer = registry

	assertMetrics := func(expected int) {
		t.Helper()
		families, err := registry.Gather()
		if err != nil {
			t.Fatal(err)
		}
		if len(families) != expected {
			t.Fatalf("metric family count = %d, want %d", len(families), expected)
		}
		for _, family := range families {
			if len(family.Metric) != 1 || len(family.Metric[0].Label) != 1 {
				t.Fatalf("unexpected metric shape for %s", family.GetName())
			}
			label := family.Metric[0].Label[0]
			if label.GetName() != "db_name" || label.GetValue() != "database-metrics" {
				t.Fatalf("unexpected label for %s: %v", family.GetName(), label)
			}
			if family.GetType().String() != "GAUGE" {
				t.Fatalf("%s type = %s", family.GetName(), family.GetType())
			}
		}
	}

	if err := connection.Init(); err != nil {
		t.Fatal(err)
	}
	assertMetrics(len(dbStatMetrics))
	if err := CloseConnection(connection); err != nil {
		t.Fatal(err)
	}
	assertMetrics(0)

	if err := connection.Init(); err != nil {
		t.Fatal(err)
	}
	assertMetrics(len(dbStatMetrics))
	if err := connection.Dispose(); err != nil {
		t.Fatal(err)
	}
}
