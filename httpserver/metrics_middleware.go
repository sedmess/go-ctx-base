package httpserver

import (
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"strconv"
	"time"
)

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
			"method": request.Method,
			"path":   request.URL.Path,
		}

		if request.Env["STATUS_CODE"] != nil {
			labels["code"] = strconv.Itoa(request.Env["STATUS_CODE"].(int))
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
