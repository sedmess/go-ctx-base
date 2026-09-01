package profiler

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sedmess/go-ctx-base/httpserver"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/u"
)

type opaqueProfilerServer struct {
	httpserver.RestServer
}

type profilerFactoryContextProbe struct {
	appContext ctx.AppContext `ctx:"CTX"`
}

func profilerDescriptorDependsOn(dependencies []string, expected string) bool {
	for _, dependency := range dependencies {
		if dependency == expected {
			return true
		}
	}
	return false
}

func assertProfilerFactoryWiring(t *testing.T, expectedServerName string, packages ...ctx.ServicePackage) {
	t.Helper()
	packages = append(packages, ctx.PackageOf(&profilerFactoryContextProbe{}))
	application := ctx.CreateContextualizedApplication(packages...)
	defer application.Stop().Join()

	probe, ok := ctx.GetTypedService[*profilerFactoryContextProbe]()
	if !ok || probe == nil || probe.appContext == nil {
		t.Fatal("profiler factory context probe is unavailable")
	}
	services := probe.appContext.Stats().Services()
	if _, ok := services[expectedServerName]; !ok {
		t.Fatalf("profiler factory server service %q is absent: %v", expectedServerName, services)
	}
	const controllerServiceName = "*profiler.Controller"
	descriptor, ok := services[controllerServiceName]
	if !ok {
		t.Fatalf("profiler controller service %q is absent: %v", controllerServiceName, services)
	}
	if !profilerDescriptorDependsOn(descriptor.Dependencies, expectedServerName) {
		t.Fatalf("profiler controller dependencies = %v, want %q", descriptor.Dependencies, expectedServerName)
	}
}

func replaceRuntimeProfilers(
	t *testing.T,
	traceStart func(io.Writer) error,
	traceStop func(),
	cpuStart func(io.Writer) error,
	cpuStop func(),
) {
	t.Helper()
	previousTraceStart := runtimeTraceStart
	previousTraceStop := runtimeTraceStop
	previousCPUStart := runtimeCPUProfileStart
	previousCPUStop := runtimeCPUProfileStop
	if traceStart != nil {
		runtimeTraceStart = traceStart
	}
	if traceStop != nil {
		runtimeTraceStop = traceStop
	}
	if cpuStart != nil {
		runtimeCPUProfileStart = cpuStart
	}
	if cpuStop != nil {
		runtimeCPUProfileStop = cpuStop
	}
	t.Cleanup(func() {
		runtimeTraceStart = previousTraceStart
		runtimeTraceStop = previousTraceStop
		runtimeCPUProfileStart = previousCPUStart
		runtimeCPUProfileStop = previousCPUStop
	})
}

func unsetProfilerEnvironment(t *testing.T, key string) {
	t.Helper()
	value, present := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(key, value)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func reserveProfilerAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return address
}

func createProfilerApplication(t *testing.T, tokens *string) (string, ctx.Application) {
	t.Helper()
	address := reserveProfilerAddress(t)
	t.Setenv("PROFILER_TEST_HTTP_LISTEN", address)
	if tokens == nil {
		unsetProfilerEnvironment(t, profilerTokenConfigKey)
	} else {
		t.Setenv(profilerTokenConfigKey, *tokens)
	}
	const serverName = "profiler-test-server"
	server := httpserver.NewRestServerSilent(serverName, "PROFILER_TEST", 0)
	application := ctx.CreateContextualizedApplication(ctx.PackageOf(
		server,
		&Controller{serverServiceName: serverName},
	))
	baseURL := "http://" + address
	deadline := time.Now().Add(2 * time.Second)
	for {
		response, err := (&http.Client{Timeout: 100 * time.Millisecond}).Get(baseURL + "/profiler-test-readiness")
		if err == nil {
			_ = response.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			application.Stop().Join()
			t.Fatalf("profiler server did not become ready: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	return baseURL, application
}

func startProfilerApplication(t *testing.T, tokens *string) string {
	t.Helper()
	baseURL, application := createProfilerApplication(t, tokens)
	t.Cleanup(func() { application.Stop().Join() })
	return baseURL
}

func requestProfile(t *testing.T, baseURL string, path string, token string) (int, http.Header, []byte) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, response.Header.Clone(), body
}

func TestProfilerParsingContracts(t *testing.T) {
	duration, err := parseProfileDuration(url.Values{})
	if err != nil || duration != 15*time.Second {
		t.Fatalf("default duration = %s, %v", duration, err)
	}
	for _, value := range []string{"", "bad", "0s", "-1s", "31s"} {
		if _, err := parseProfileDuration(url.Values{"duration": {value}}); err == nil {
			t.Fatalf("duration %q was accepted", value)
		}
	}
	if duration, err := parseProfileDuration(url.Values{"duration": {"30s"}}); err != nil || duration != 30*time.Second {
		t.Fatalf("maximum duration = %s, %v", duration, err)
	}

	name, debug, err := parseNamedProfile(url.Values{"name": {"goroutine"}, "debug": {"2"}})
	if err != nil || name != "goroutine" || debug != 2 {
		t.Fatalf("named profile = %q/%d, %v", name, debug, err)
	}
	for _, query := range []url.Values{
		{},
		{"name": {"missing-profile"}},
		{"name": {"goroutine\n"}},
		{"name": {"goroutine"}, "debug": {"3"}},
		{"name": {"goroutine"}, "debug": {"bad"}},
	} {
		if _, _, err := parseNamedProfile(query); err == nil {
			t.Fatalf("named profile query %#v was accepted", query)
		}
	}
}

func TestProfilerRoutesAndTokenPolicy(t *testing.T) {
	if defaultServerName != "base.profiler-http-server" {
		t.Fatal("stable profiler service identity changed")
	}
	var _ interface{ Init(ctx.ServiceProvider) } = &Controller{}

	baseURL := startProfilerApplication(t, nil)
	status, header, body := requestProfile(t, baseURL, "/profiler/named_profile?name=goroutine&debug=0", "")
	if status != http.StatusOK || len(body) == 0 {
		t.Fatalf("named profile status/body = %d/%d", status, len(body))
	}
	if disposition := header.Get("Content-Disposition"); disposition != "attachment; filename=\"goroutine.pprof\"" {
		t.Fatalf("content disposition = %q", disposition)
	}
	if contentType := header.Get("Content-Type"); contentType != "application/octet-stream" {
		t.Fatalf("content type = %q", contentType)
	}
	status, header, body = requestProfile(t, baseURL, "/profiler/trace?duration=1ms", "")
	if status != http.StatusOK || len(body) == 0 || header.Get("Content-Disposition") != "attachment; filename=\"trace.pprof\"" || header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("trace response = %d/%q/%q/%d", status, header.Get("Content-Type"), header.Get("Content-Disposition"), len(body))
	}
	status, header, body = requestProfile(t, baseURL, "/profiler/cpu_profile?duration=10ms", "")
	if status != http.StatusOK || len(body) == 0 || header.Get("Content-Disposition") != "attachment; filename=\"profile.pprof\"" || header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("CPU response = %d/%q/%q/%d", status, header.Get("Content-Type"), header.Get("Content-Disposition"), len(body))
	}

	for _, path := range []string{
		"/profiler/trace?duration=0s",
		"/profiler/cpu_profile?duration=31s",
		"/profiler/named_profile?name=missing-profile",
		"/profiler/named_profile?name=goroutine&debug=3",
	} {
		status, _, body := requestProfile(t, baseURL, path, "")
		if status != http.StatusBadRequest || len(body) != 0 {
			t.Fatalf("%s status/body = %d/%d", path, status, len(body))
		}
	}
}

func TestProfilerGoroutineLeakProfile(t *testing.T) {
	if profile := pprof.Lookup("goroutineleak"); profile == nil {
		t.Fatal("Go 1.27 goroutineleak profile is unavailable")
	}
	name, debug, err := parseNamedProfile(url.Values{"name": {"goroutineleak"}, "debug": {"0"}})
	if err != nil || name != "goroutineleak" || debug != 0 {
		t.Fatalf("goroutineleak parsing = %q/%d, %v", name, debug, err)
	}

	tokens := "goroutine-leak-token"
	baseURL := startProfilerApplication(t, &tokens)
	path := "/profiler/named_profile?name=goroutineleak&debug=0"
	if status, _, _ := requestProfile(t, baseURL, path, ""); status != http.StatusUnauthorized {
		t.Fatalf("missing-token status = %d", status)
	}
	status, header, body := requestProfile(t, baseURL, path, tokens)
	if status != http.StatusOK || len(body) == 0 {
		t.Fatalf("goroutineleak status/body = %d/%d", status, len(body))
	}
	if disposition := header.Get("Content-Disposition"); disposition != "attachment; filename=\"goroutineleak.pprof\"" {
		t.Fatalf("goroutineleak content disposition = %q", disposition)
	}
	if contentType := header.Get("Content-Type"); contentType != "application/octet-stream" {
		t.Fatalf("goroutineleak content type = %q", contentType)
	}
}

func TestProfilerFactoriesPreserveObservableServiceWiring(t *testing.T) {
	t.Setenv(profilerTokenConfigKey, "synthetic-factory-token")

	t.Run("default HTTP server", func(t *testing.T) {
		const prefix = "PROFILER_FACTORY_DEFAULT"
		t.Setenv(prefix+"_HTTP_LISTEN", "127.0.0.1:0")
		serverName := u.GetInterfaceName[httpserver.RestServer]()
		assertProfilerFactoryWiring(t, serverName, ctx.PackageOf(
			httpserver.NewRestServerSilent(serverName, prefix, 0),
			AddToDefaultHttpServer(),
		))
	})

	t.Run("named HTTP server", func(t *testing.T) {
		const (
			prefix     = "PROFILER_FACTORY_NAMED"
			serverName = "profiler-factory-http-server"
		)
		t.Setenv(prefix+"_HTTP_LISTEN", "127.0.0.1:0")
		assertProfilerFactoryWiring(t, serverName, ctx.PackageOf(
			httpserver.NewRestServerSilent(serverName, prefix, 0),
			AddToHttpServer(serverName),
		))
	})

	t.Run("independent server", func(t *testing.T) {
		t.Setenv("PROFILER_HTTP_LISTEN", "127.0.0.1:0")
		assertProfilerFactoryWiring(t, defaultServerName, RunAsIndependentServer())
	})
}

func TestProfilerPolicyUsesOnlyExactComponentTokensAndHidesSecrets(t *testing.T) {
	server := &opaqueProfilerServer{}
	unsetProfilerEnvironment(t, profilerTokenConfigKey)
	t.Setenv("HTTP_AUTH_TOKENS", "global-token")
	t.Setenv("ACTUATOR_HTTP_AUTH_TOKENS", "actuator-token")
	if _, err := profilerAccessMiddleware(server); err == nil {
		t.Fatal("global or actuator token was used as a profiler fallback")
	}

	const secret = "synthetic-profiler-secret"
	t.Setenv(profilerTokenConfigKey, secret+",")
	if _, err := profilerAccessMiddleware(server); err == nil {
		t.Fatal("invalid profiler token list was accepted")
	} else if strings.Contains(err.Error(), secret) {
		t.Fatalf("profiler policy error exposed token: %v", err)
	}

	t.Setenv(profilerTokenConfigKey, "valid-token, valid-token")
	if middleware, err := profilerAccessMiddleware(server); err != nil || middleware == nil {
		t.Fatalf("configured custom-server policy = %v/%v", middleware, err)
	}
}

func TestProfilerConfiguredTokenAndBusyResponse(t *testing.T) {
	tokens := " profiler-one , profiler-two "
	baseURL := startProfilerApplication(t, &tokens)
	path := "/profiler/named_profile?name=goroutine&debug=0"
	if status, _, _ := requestProfile(t, baseURL, path, ""); status != http.StatusUnauthorized {
		t.Fatalf("missing-token status = %d", status)
	}
	if status, _, _ := requestProfile(t, baseURL, path, "rejected"); status != http.StatusForbidden {
		t.Fatalf("rejected-token status = %d", status)
	}

	release, err := tryAcquireProfile()
	if err != nil {
		t.Fatal(err)
	}
	status, header, body := requestProfile(t, baseURL, path, "profiler-two")
	release()
	if status != http.StatusTooManyRequests || header.Get("Retry-After") != "1" || len(body) != 0 {
		t.Fatalf("busy response = %d/%q/%d", status, header.Get("Retry-After"), len(body))
	}
	if status, _, body := requestProfile(t, baseURL, path, "profiler-two"); status != http.StatusOK || len(body) == 0 {
		t.Fatalf("accepted-token status/body = %d/%d", status, len(body))
	}
}

func TestProfilerCancellationAlwaysReleasesRuntimeAndGate(t *testing.T) {
	controller := &Controller{}
	operationContext, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := controller.traceContext(operationContext, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("trace cancellation = %v", err)
	}
	if release, err := tryAcquireProfile(); err != nil {
		t.Fatalf("trace left admission occupied: %v", err)
	} else {
		release()
	}

	operationContext, cancel = context.WithCancel(context.Background())
	cancel()
	if _, err := controller.profileContext(operationContext, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cpu cancellation = %v", err)
	}
	if release, err := tryAcquireProfile(); err != nil {
		t.Fatalf("cpu profile left admission occupied: %v", err)
	} else {
		release()
	}
}

func TestProfilerRuntimeCleanupExactlyOnce(t *testing.T) {
	assertAdmissionAvailable := func(t *testing.T) {
		t.Helper()
		release, err := tryAcquireProfile()
		if err != nil {
			t.Fatalf("profiling admission remained occupied: %v", err)
		}
		release()
	}

	t.Run("trace success", func(t *testing.T) {
		var starts atomic.Int32
		var stops atomic.Int32
		replaceRuntimeProfilers(t, func(io.Writer) error {
			starts.Add(1)
			return nil
		}, func() { stops.Add(1) }, nil, nil)
		if _, err := (&Controller{}).Trace(0); err != nil {
			t.Fatal(err)
		}
		if starts.Load() != 1 || stops.Load() != 1 {
			t.Fatalf("trace starts/stops = %d/%d", starts.Load(), stops.Load())
		}
		assertAdmissionAvailable(t)
	})

	t.Run("trace start failure", func(t *testing.T) {
		startError := errors.New("synthetic trace start failure")
		var stops atomic.Int32
		replaceRuntimeProfilers(t, func(io.Writer) error { return startError }, func() { stops.Add(1) }, nil, nil)
		if _, err := (&Controller{}).Trace(time.Second); !errors.Is(err, startError) {
			t.Fatalf("trace start error = %v", err)
		}
		if stops.Load() != 0 {
			t.Fatalf("trace stop called %d times after failed start", stops.Load())
		}
		assertAdmissionAvailable(t)
	})

	t.Run("trace client cancellation", func(t *testing.T) {
		started := make(chan struct{})
		var stops atomic.Int32
		replaceRuntimeProfilers(t, func(io.Writer) error {
			close(started)
			return nil
		}, func() { stops.Add(1) }, nil, nil)
		operationContext, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := (&Controller{}).traceContext(operationContext, 30*time.Second)
			done <- err
		}()
		<-started
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("trace cancellation = %v", err)
		}
		if stops.Load() != 1 {
			t.Fatalf("trace stop calls = %d", stops.Load())
		}
		assertAdmissionAvailable(t)
	})

	t.Run("cpu success", func(t *testing.T) {
		var starts atomic.Int32
		var stops atomic.Int32
		replaceRuntimeProfilers(t, nil, nil, func(io.Writer) error {
			starts.Add(1)
			return nil
		}, func() { stops.Add(1) })
		if _, err := (&Controller{}).Profile(0); err != nil {
			t.Fatal(err)
		}
		if starts.Load() != 1 || stops.Load() != 1 {
			t.Fatalf("CPU starts/stops = %d/%d", starts.Load(), stops.Load())
		}
		assertAdmissionAvailable(t)
	})

	t.Run("cpu start failure", func(t *testing.T) {
		startError := errors.New("synthetic CPU start failure")
		var stops atomic.Int32
		replaceRuntimeProfilers(t, nil, nil, func(io.Writer) error { return startError }, func() { stops.Add(1) })
		if _, err := (&Controller{}).Profile(time.Second); !errors.Is(err, startError) {
			t.Fatalf("CPU start error = %v", err)
		}
		if stops.Load() != 0 {
			t.Fatalf("CPU stop called %d times after failed start", stops.Load())
		}
		assertAdmissionAvailable(t)
	})

	t.Run("cpu server cancellation", func(t *testing.T) {
		started := make(chan struct{})
		var stops atomic.Int32
		replaceRuntimeProfilers(t, nil, nil, func(io.Writer) error {
			close(started)
			return nil
		}, func() { stops.Add(1) })
		operationContext, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := (&Controller{}).profileContext(operationContext, 30*time.Second)
			done <- err
		}()
		<-started
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("CPU cancellation = %v", err)
		}
		if stops.Load() != 1 {
			t.Fatalf("CPU stop calls = %d", stops.Load())
		}
		assertAdmissionAvailable(t)
	})
}

func assertProfileRequestOwnsAdmission(t *testing.T, baseURL string) {
	t.Helper()
	time.Sleep(25 * time.Millisecond)
	status, header, body := requestProfile(t, baseURL, "/profiler/named_profile?name=goroutine", "")
	if status != http.StatusTooManyRequests || header.Get("Retry-After") != "1" || len(body) != 0 {
		t.Fatalf("concurrent profiling response = %d/%q/%d", status, header.Get("Retry-After"), len(body))
	}
}

func waitForProfileAvailable(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		release, err := tryAcquireProfile()
		if err == nil {
			release()
			return
		}
		if !errors.Is(err, errProfileBusy) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("profiling admission was not released")
}

func TestProfilerClientAndServerCancellationReleaseAdmission(t *testing.T) {
	t.Run("client cancellation stops trace", func(t *testing.T) {
		baseURL, application := createProfilerApplication(t, nil)
		defer func() { application.Stop().Join() }()
		requestContext, cancelRequest := context.WithCancel(context.Background())
		request, err := http.NewRequestWithContext(requestContext, http.MethodGet, baseURL+"/profiler/trace?duration=30s", nil)
		if err != nil {
			t.Fatal(err)
		}
		requestDone := make(chan error, 1)
		go func() {
			response, requestErr := http.DefaultClient.Do(request)
			if response != nil {
				_ = response.Body.Close()
			}
			requestDone <- requestErr
		}()
		assertProfileRequestOwnsAdmission(t, baseURL)
		cancelRequest()
		select {
		case err := <-requestDone:
			if err == nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("client cancellation result = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("canceled trace request did not return")
		}
		waitForProfileAvailable(t)
	})

	t.Run("server cancellation stops CPU profile", func(t *testing.T) {
		baseURL, application := createProfilerApplication(t, nil)
		requestDone := make(chan struct{})
		go func() {
			defer close(requestDone)
			response, _ := http.Get(baseURL + "/profiler/cpu_profile?duration=30s")
			if response != nil {
				_ = response.Body.Close()
			}
		}()
		assertProfileRequestOwnsAdmission(t, baseURL)
		application.Stop().Join()
		select {
		case <-requestDone:
		case <-time.After(2 * time.Second):
			t.Fatal("server-canceled CPU request did not return")
		}
		waitForProfileAvailable(t)
	})
}
