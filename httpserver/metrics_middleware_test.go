package httpserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/sedmess/go-ctx/ctx/logger"
)

type metricTestWriter struct {
	header http.Header
}

func (w *metricTestWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *metricTestWriter) EncodeJson(value interface{}) ([]byte, error) {
	return json.Marshal(value)
}

func (w *metricTestWriter) WriteJson(value interface{}) error {
	_, err := w.EncodeJson(value)
	return err
}

func (*metricTestWriter) WriteHeader(int) {}

func installIsolatedHTTPMetrics(t *testing.T) *prometheus.Registry {
	t.Helper()
	previous := metrics
	metrics = prometheusMetrics{
		reqCounter:  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "httpserver_requests_total"}, []string{"server", "code", "method", "path"}),
		reqDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "httpserver_request_duration"}, []string{"server", "code", "method", "path"}),
		reqBytes:    prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "httpserver_request_bytes"}, []string{"server", "code", "method", "path"}),
	}
	registry := prometheus.NewRegistry()
	registry.MustRegister(metrics.reqCounter, metrics.reqDuration, metrics.reqBytes)
	t.Cleanup(func() { metrics = previous })
	return registry
}

func metricLabels(metric *dto.Metric) map[string]string {
	labels := make(map[string]string, len(metric.Label))
	for _, pair := range metric.Label {
		labels[pair.GetName()] = pair.GetValue()
	}
	return labels
}

func findGatheredMetric(t *testing.T, registry *prometheus.Registry, familyName string, expectedLabels map[string]string) *dto.Metric {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	available := make([]map[string]string, 0)
	for _, family := range families {
		if family.GetName() != familyName {
			continue
		}
		for _, metric := range family.Metric {
			labels := metricLabels(metric)
			available = append(available, labels)
			matches := true
			for key, expected := range expectedLabels {
				if labels[key] != expected {
					matches = false
					break
				}
			}
			if matches {
				return metric
			}
		}
	}
	t.Fatalf("metric %s with labels %v not found; available: %v", familyName, expectedLabels, available)
	return nil
}

func newMetricLifecycleServer(t *testing.T, name string) *restServer {
	t.Helper()
	prefix := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	t.Setenv(prefix+"_HTTP_LISTEN", "127.0.0.1:0")
	server := NewRestServer(name, prefix, 0).(*restServer)
	server.l = logger.New(name)
	if err := server.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Dispose() })
	return server
}

func metricRequest(t *testing.T, server *restServer, method string, path string) (int, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, testServerURL(t, server, path), nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, body
}

func TestHTTPMetricsUseFiniteRouteMethodAndExistingUnits(t *testing.T) {
	registry := installIsolatedHTTPMetrics(t)
	server := newMetricLifecycleServer(t, "metric-contract")
	RegisterRoute(server, http.MethodGet, "/items/:id").Handler(func(*RequestData) (response Response) {
		time.Sleep(12 * time.Millisecond)
		response.Status(http.StatusCreated).Content(map[string]string{"result": "ok"})
		return
	})
	RegisterRoute(server, http.MethodGet, "/static/*path").Handler(func(*RequestData) (response Response) {
		response.Status(http.StatusNoContent)
		return
	})
	server.AfterStart()

	status, body := metricRequest(t, server, http.MethodGet, "/items/variable-123?expand=synthetic-secret")
	if status != http.StatusCreated || len(body) == 0 {
		t.Fatalf("parameter route response = %d/%d", status, len(body))
	}
	labels := map[string]string{"server": "metric-contract", "code": "201", "method": "GET", "path": "/items/:id"}
	counter := findGatheredMetric(t, registry, "httpserver_requests_total", labels)
	if counter.GetCounter().GetValue() != 1 {
		t.Fatalf("request counter = %f", counter.GetCounter().GetValue())
	}
	duration := findGatheredMetric(t, registry, "httpserver_request_duration", labels)
	if duration.GetHistogram().GetSampleCount() != 1 || duration.GetHistogram().GetSampleSum() < 5 {
		t.Fatalf("duration count/sum = %d/%f; expected preserved millisecond observations", duration.GetHistogram().GetSampleCount(), duration.GetHistogram().GetSampleSum())
	}
	bytesWritten := findGatheredMetric(t, registry, "httpserver_request_bytes", labels)
	if bytesWritten.GetHistogram().GetSampleCount() != 1 || bytesWritten.GetHistogram().GetSampleSum() <= 0 {
		t.Fatalf("byte count/sum = %d/%f", bytesWritten.GetHistogram().GetSampleCount(), bytesWritten.GetHistogram().GetSampleSum())
	}
	for key, value := range metricLabels(counter) {
		if strings.Contains(value, "variable-123") || strings.Contains(value, "synthetic-secret") {
			t.Fatalf("request-controlled value entered %s label: %q", key, value)
		}
	}

	status, _ = metricRequest(t, server, http.MethodGet, "/static/a/b/c.txt")
	if status != http.StatusNoContent {
		t.Fatalf("splat route status = %d", status)
	}
	findGatheredMetric(t, registry, "httpserver_requests_total", map[string]string{
		"server": "metric-contract", "code": "204", "method": "GET", "path": "/static/*path",
	})

	status, _ = metricRequest(t, server, http.MethodGet, "/unknown/value")
	if status != http.StatusNotFound {
		t.Fatalf("unmatched status = %d", status)
	}
	findGatheredMetric(t, registry, "httpserver_requests_total", map[string]string{
		"server": "metric-contract", "code": "404", "method": "GET", "path": unmatchedRouteLabel,
	})

	status, _ = metricRequest(t, server, "BREW", "/unknown/other")
	if status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
		t.Fatalf("unsupported-method status = %d", status)
	}
	findGatheredMetric(t, registry, "httpserver_requests_total", map[string]string{
		"server": "metric-contract", "code": strconv.Itoa(status), "method": otherMethodLabel, "path": unmatchedRouteLabel,
	})
}

func TestHTTPMetricsUseUnmatchedBeforeRouting(t *testing.T) {
	registry := installIsolatedHTTPMetrics(t)
	server := newMetricLifecycleServer(t, "metric-preroute")
	server.AddMiddleware(func(_ rest.HandlerFunc, writer rest.ResponseWriter, _ *rest.Request) error {
		writer.WriteHeader(http.StatusTeapot)
		return nil
	})
	RegisterRoute(server, http.MethodGet, "/never-runs").HandlerRaw(func(*RequestData, rest.ResponseWriter) error {
		t.Fatal("route handler ran after global middleware rejected the request")
		return nil
	})
	server.AfterStart()
	status, _ := metricRequest(t, server, http.MethodGet, "/never-runs")
	if status != http.StatusTeapot {
		t.Fatalf("pre-routing status = %d", status)
	}
	findGatheredMetric(t, registry, "httpserver_requests_total", map[string]string{
		"server": "metric-preroute", "code": "418", "method": "GET", "path": unmatchedRouteLabel,
	})
}

func TestBoundedMetricMethodContract(t *testing.T) {
	for _, method := range []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
	} {
		if got := boundedMetricMethod(method); got != method {
			t.Fatalf("method %q became %q", method, got)
		}
	}
	for _, method := range []string{"", "get", "CONNECT", "BREW", "GET-synthetic-secret"} {
		if got := boundedMetricMethod(method); got != otherMethodLabel {
			t.Fatalf("unsupported method %q became %q", method, got)
		}
	}
}

func TestHTTPMetricCardinalityIsBoundedAcrossTenThousandTargets(t *testing.T) {
	registry := installIsolatedHTTPMetrics(t)
	middleware := createPrometheusMiddleware("metric-cardinality")
	handler := middleware.MiddlewareFunc(func(_ rest.ResponseWriter, request *rest.Request) {
		elapsed := 10 * time.Millisecond
		request.Env["ELAPSED_TIME"] = &elapsed
		request.Env["BYTES_WRITTEN"] = int64(12)
		if request.Method == http.MethodGet {
			request.Env["STATUS_CODE"] = http.StatusOK
			request.Env[routePathEnvKey] = "/items/:id"
			return
		}
		request.Env["STATUS_CODE"] = http.StatusNotFound
	})
	writer := &metricTestWriter{}
	for index := 0; index < 10_000; index++ {
		method := http.MethodGet
		if index%2 == 1 {
			method = fmt.Sprintf("CUSTOM-%d", index)
		}
		httpRequest := httptest.NewRequest(method, fmt.Sprintf("http://example.test/variable/%d?query=%d", index, index), nil)
		handler(writer, &rest.Request{Request: httpRequest, Env: make(map[string]interface{})})
	}

	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, family := range families {
		seen[family.GetName()] = len(family.Metric)
		for _, metric := range family.Metric {
			for key, value := range metricLabels(metric) {
				if strings.Contains(value, "variable") || strings.Contains(value, "query") || strings.Contains(value, "CUSTOM-") {
					t.Fatalf("unbounded %s label %q in %s", key, value, family.GetName())
				}
			}
		}
	}
	for _, familyName := range []string{"httpserver_requests_total", "httpserver_request_duration", "httpserver_request_bytes"} {
		if seen[familyName] != 2 {
			t.Fatalf("%s series count = %d, want 2", familyName, seen[familyName])
		}
	}
}
