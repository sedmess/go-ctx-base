package httpserver

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/sedmess/go-ctx/ctx"
)

const loopbackBenchmarkPath = "/benchmark"

var loopbackBenchmarkBody = []byte(`{"status":"ok"}`)

type loopbackBenchmarkResponse struct {
	Status string `json:"status"`
}

type loopbackBenchmarkController struct {
	name       string
	serverName string
	rawHandler bool
}

func (controller *loopbackBenchmarkController) Name() string {
	return controller.name
}

func (controller *loopbackBenchmarkController) Init(provider ctx.ServiceProvider) error {
	server, ok := provider.ByName(controller.serverName).(RestServer)
	if !ok {
		return fmt.Errorf("benchmark server %q is unavailable", controller.serverName)
	}

	route := RegisterRoute(server, http.MethodGet, loopbackBenchmarkPath)
	if controller.rawHandler {
		route.HandlerRaw(func(_ *RequestData, responseWriter rest.ResponseWriter) error {
			writer, ok := responseWriter.(http.ResponseWriter)
			if !ok {
				return fmt.Errorf("benchmark response writer does not implement http.ResponseWriter")
			}
			writer.WriteHeader(http.StatusOK)
			_, err := writer.Write(loopbackBenchmarkBody)
			return err
		})
		return nil
	}

	route.Handler(func(*RequestData) (response Response) {
		response.Ok().Content(loopbackBenchmarkResponse{Status: "ok"})
		return
	})
	return nil
}

type loopbackBenchmarkFixture struct {
	client     *http.Client
	method     string
	requestURL string
}

func BenchmarkRestServerLoopback(b *testing.B) {
	stacks := []struct {
		name   string
		silent bool
	}{
		{name: "instrumented"},
		{name: "silent", silent: true},
	}
	handlers := []struct {
		name string
		raw  bool
	}{
		{name: "json"},
		{name: "raw", raw: true},
	}

	for _, stack := range stacks {
		for _, handler := range handlers {
			b.Run(stack.name+"/"+handler.name, func(b *testing.B) {
				b.Run("serial", func(b *testing.B) {
					fixture := newLoopbackBenchmarkFixture(
						b,
						stack.name,
						stack.silent,
						handler.raw,
						"serial",
					)
					fixture.runSerial(b)
				})
				b.Run("parallel", func(b *testing.B) {
					fixture := newLoopbackBenchmarkFixture(
						b,
						stack.name,
						stack.silent,
						handler.raw,
						"parallel",
					)
					fixture.runParallel(b)
				})
			})
		}
	}
}

func newLoopbackBenchmarkFixture(
	b *testing.B,
	stackName string,
	silent bool,
	rawHandler bool,
	runMode string,
) *loopbackBenchmarkFixture {
	b.Helper()

	configureLoopbackBenchmarkEnvironment(b)

	handlerName := "json"
	if rawHandler {
		handlerName = "raw"
	}
	serverName := "httpserver-benchmark-" + stackName + "-" + handlerName + "-" + runMode
	configPrefix := strings.ToUpper(strings.ReplaceAll(serverName, "-", "_"))
	b.Setenv(configPrefix+"_HTTP_LISTEN", "127.0.0.1:0")

	var server RestServer
	if silent {
		server = NewRestServerSilent(serverName, configPrefix, 0)
	} else {
		server = NewRestServer(serverName, configPrefix, 0)
	}
	concreteServer := server.(*restServer)
	controller := &loopbackBenchmarkController{
		name:       serverName + "-controller",
		serverName: serverName,
		rawHandler: rawHandler,
	}
	return startLoopbackBenchmarkFixture(
		b,
		concreteServer,
		http.MethodGet,
		nil,
		server,
		controller,
	)
}

func configureLoopbackBenchmarkEnvironment(b *testing.B) {
	b.Helper()

	b.Setenv("SLOG_LEVEL", "warn")
	b.Setenv("SLOG_HANDLER", "text")
	b.Setenv("SLOG_ADD_SOURCE", "false")
	b.Setenv("SLOG_ADD_COMMON_TAGS", "false")
}

func startLoopbackBenchmarkFixture(
	b *testing.B,
	server *restServer,
	method string,
	beforeReadiness func() error,
	services ...any,
) *loopbackBenchmarkFixture {
	b.Helper()

	application := ctx.CreateContextualizedApplication(ctx.PackageOf(services...))
	b.Cleanup(func() {
		application.Stop().Join()
	})

	address, ok := server.boundAddress()
	if !ok {
		b.Fatal("benchmark server has no bound address")
	}

	parallelism := runtime.GOMAXPROCS(0)
	connectionCapacity := max(32, parallelism*4)
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		DisableCompression:    true,
		MaxIdleConns:          connectionCapacity,
		MaxIdleConnsPerHost:   connectionCapacity,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
	}
	b.Cleanup(transport.CloseIdleConnections)

	fixture := &loopbackBenchmarkFixture{
		client:     client,
		method:     method,
		requestURL: "http://" + address.String() + loopbackBenchmarkPath,
	}
	if beforeReadiness != nil {
		if err := beforeReadiness(); err != nil {
			b.Fatalf("prepare benchmark fixture: %v", err)
		}
	}
	readinessClient := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}
	fixture.verifyReady(b, readinessClient)
	return fixture
}

func (fixture *loopbackBenchmarkFixture) verifyReady(b *testing.B, client *http.Client) {
	b.Helper()

	request, err := http.NewRequest(fixture.method, fixture.requestURL, nil)
	if err != nil {
		b.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		b.Fatalf("benchmark readiness request: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if response.StatusCode != http.StatusOK {
		b.Fatalf("benchmark readiness status = %d", response.StatusCode)
	}
	if readErr != nil {
		b.Fatalf("read benchmark readiness response: %v", readErr)
	}
	if closeErr != nil {
		b.Fatalf("close benchmark readiness response: %v", closeErr)
	}
	if !bytes.Equal(body, loopbackBenchmarkBody) {
		b.Fatalf("benchmark readiness body = %q", body)
	}
}

func (fixture *loopbackBenchmarkFixture) runSerial(b *testing.B) {
	request, err := http.NewRequest(fixture.method, fixture.requestURL, nil)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := fixture.doRequest(request); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	reportRequestsPerSecond(b)
}

func (fixture *loopbackBenchmarkFixture) runParallel(b *testing.B) {
	workerRequests := make([]*http.Request, runtime.GOMAXPROCS(0))
	for index := range workerRequests {
		request, err := http.NewRequest(fixture.method, fixture.requestURL, nil)
		if err != nil {
			b.Fatal(err)
		}
		workerRequests[index] = request
	}

	var failed atomic.Bool
	var firstError error
	var firstErrorOnce sync.Once
	var nextWorker atomic.Uint64

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(parallel *testing.PB) {
		workerIndex := int(nextWorker.Add(1) - 1)
		if workerIndex >= len(workerRequests) {
			firstErrorOnce.Do(func() {
				firstError = fmt.Errorf(
					"benchmark started more than %d parallel workers",
					len(workerRequests),
				)
				failed.Store(true)
			})
			return
		}
		request := workerRequests[workerIndex]

		for parallel.Next() {
			if failed.Load() {
				return
			}
			if err := fixture.doRequest(request); err != nil {
				firstErrorOnce.Do(func() {
					firstError = err
					failed.Store(true)
				})
				return
			}
		}
	})
	b.StopTimer()

	if firstError != nil {
		b.Fatal(firstError)
	}
	reportRequestsPerSecond(b)
}

func (fixture *loopbackBenchmarkFixture) doRequest(request *http.Request) error {
	response, err := fixture.client.Do(request)
	if err != nil {
		return fmt.Errorf("benchmark request: %w", err)
	}
	bytesRead, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("benchmark response status = %d", response.StatusCode)
	}
	if readErr != nil {
		return fmt.Errorf("read benchmark response: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close benchmark response: %w", closeErr)
	}
	if bytesRead != int64(len(loopbackBenchmarkBody)) {
		return fmt.Errorf("benchmark response size = %d", bytesRead)
	}
	return nil
}

func reportRequestsPerSecond(b *testing.B) {
	elapsed := b.Elapsed()
	if elapsed > 0 {
		b.ReportMetric(float64(b.N)/elapsed.Seconds(), "req/s")
	}
}
