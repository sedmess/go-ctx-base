package httpserver

import (
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"net/http"
	"strconv"
	"time"
)

const unmatchedRouteLabel = "unmatched"
const otherMethodLabel = "OTHER"

type prometheusMetrics struct {
	reqCounter  *prometheus.CounterVec
	reqDuration *prometheus.HistogramVec
	reqBytes    *prometheus.HistogramVec
}

var metrics = prometheusMetrics{}

func init() {
	labelNames := []string{"server", "code", "method", "path"}
	metrics.reqCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "httpserver_requests_total",
	}, labelNames)
	metrics.reqDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "httpserver_request_duration",
	}, labelNames)
	metrics.reqBytes = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "httpserver_request_bytes",
	}, labelNames)
}

type prometheusMiddleware struct {
	name string
}

func createPrometheusMiddleware(serverName string) rest.Middleware {
	return &prometheusMiddleware{
		name: serverName,
	}
}

func (p *prometheusMiddleware) MiddlewareFunc(handler rest.HandlerFunc) rest.HandlerFunc {
	return func(writer rest.ResponseWriter, request *rest.Request) {
		handler(writer, request)

		labels := prometheus.Labels{
			"server": p.name,
			"code":   strconv.Itoa(http.StatusOK),
			"method": boundedMetricMethod(request.Method),
			"path":   metricRoutePath(request),
		}

		if status, ok := request.Env["STATUS_CODE"].(int); ok && status > 0 {
			labels["code"] = strconv.Itoa(status)
		}

		metrics.reqCounter.With(labels).Inc()

		if request.Env["ELAPSED_TIME"] != nil {
			elapsed := request.Env["ELAPSED_TIME"].(*time.Duration)
			metrics.reqDuration.With(labels).Observe(float64(elapsed.Milliseconds()))
		}
		if request.Env["BYTES_WRITTEN"] != nil {
			bytesWritten := request.Env["BYTES_WRITTEN"].(int64)
			metrics.reqBytes.With(labels).Observe(float64(bytesWritten))
		}
	}
}

func boundedMetricMethod(method string) string {
	switch method {
	case http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions:
		return method
	default:
		return otherMethodLabel
	}
}

func metricRoutePath(request *rest.Request) string {
	if request == nil || request.Env == nil {
		return unmatchedRouteLabel
	}
	if path, ok := request.Env[routePathEnvKey].(string); ok && path != "" {
		return path
	}
	return unmatchedRouteLabel
}
