package actuator

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sedmess/go-ctx-base/httpserver"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/u"
)

type opaqueActuatorServer struct {
	httpserver.RestServer
}

type actuatorFactoryContextProbe struct {
	appContext ctx.AppContext `ctx:"CTX"`
}

func containsServiceDependency(dependencies []string, expected string) bool {
	for _, dependency := range dependencies {
		if dependency == expected {
			return true
		}
	}
	return false
}

func assertActuatorFactoryWiring(t *testing.T, expectedServerName string, packages ...ctx.ServicePackage) {
	t.Helper()
	packages = append(packages, ctx.PackageOf(&actuatorFactoryContextProbe{}))
	application := ctx.CreateContextualizedApplication(packages...)
	defer application.Stop().Join()

	probe, ok := ctx.GetTypedService[*actuatorFactoryContextProbe]()
	if !ok || probe == nil || probe.appContext == nil {
		t.Fatal("actuator factory context probe is unavailable")
	}
	services := probe.appContext.Stats().Services()
	if _, ok := services[expectedServerName]; !ok {
		t.Fatalf("actuator factory server service %q is absent: %v", expectedServerName, services)
	}
	descriptor, ok := services[controllerName]
	if !ok {
		t.Fatalf("actuator controller service %q is absent: %v", controllerName, services)
	}
	if !containsServiceDependency(descriptor.Dependencies, expectedServerName) {
		t.Fatalf("actuator controller dependencies = %v, want %q", descriptor.Dependencies, expectedServerName)
	}
}

func unsetEnvironment(t *testing.T, key string) {
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

func reserveTestAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return address
}

func startActuatorApplication(t *testing.T, tokens *string) string {
	t.Helper()
	address := reserveTestAddress(t)
	t.Setenv("ACTUATOR_TEST_HTTP_LISTEN", address)
	if tokens == nil {
		unsetEnvironment(t, actuatorTokenConfigKey)
	} else {
		t.Setenv(actuatorTokenConfigKey, *tokens)
	}
	const serverName = "actuator-test-server"
	server := httpserver.NewRestServerSilent(serverName, "ACTUATOR_TEST", 0)
	application := ctx.CreateContextualizedApplication(ctx.PackageOf(
		server,
		&controller{serverServiceName: serverName},
	))
	t.Cleanup(func() { application.Stop().Join() })
	return "http://" + address
}

func actuatorRequest(t *testing.T, baseURL string, path string, token string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	return response
}

func actuatorResponseBody(t *testing.T, baseURL string, path string) (int, http.Header, []byte) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(baseURL + path)
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

func TestActuatorLoopbackRoutesRemainAvailable(t *testing.T) {
	if defaultServerName != "base.actuator-http-server" || controllerName != "base.actuator-controller" {
		t.Fatal("stable actuator service identity changed")
	}
	if (&controller{}).Name() != controllerName {
		t.Fatal("actuator controller Name changed")
	}
	baseURL := startActuatorApplication(t, nil)
	for _, path := range []string{
		"/actuator/health",
		"/actuator/health/plain",
		"/actuator/services",
		"/actuator/metrics",
	} {
		if status := actuatorRequest(t, baseURL, path, "").StatusCode; status != http.StatusOK {
			t.Fatalf("%s status = %d", path, status)
		}
	}

	status, header, body := actuatorResponseBody(t, baseURL, "/actuator/health")
	var health map[string]interface{}
	if status != http.StatusOK || !strings.Contains(header.Get("Content-Type"), "application/json") || json.Unmarshal(body, &health) != nil || len(health) == 0 {
		t.Fatalf("health response shape = %d/%q/%q", status, header.Get("Content-Type"), body)
	}
	status, header, body = actuatorResponseBody(t, baseURL, "/actuator/health/plain")
	if status != http.StatusOK || header.Get("Content-Type") != "text/plain; charset=utf-8" || len(body) == 0 {
		t.Fatalf("plain health response shape = %d/%q/%q", status, header.Get("Content-Type"), body)
	}
	status, header, body = actuatorResponseBody(t, baseURL, "/actuator/services")
	var services map[string]interface{}
	if status != http.StatusOK || !strings.Contains(header.Get("Content-Type"), "application/json") || json.Unmarshal(body, &services) != nil || len(services) == 0 {
		t.Fatalf("services response shape = %d/%q/%q", status, header.Get("Content-Type"), body)
	}
	status, header, body = actuatorResponseBody(t, baseURL, "/actuator/metrics")
	if status != http.StatusOK || !strings.Contains(header.Get("Content-Type"), "text/plain") || len(body) == 0 {
		t.Fatalf("metrics response shape = %d/%q/%d", status, header.Get("Content-Type"), len(body))
	}
}

func TestActuatorFactoriesPreserveObservableServiceWiring(t *testing.T) {
	t.Setenv(actuatorTokenConfigKey, "synthetic-factory-token")

	t.Run("default HTTP server", func(t *testing.T) {
		const prefix = "ACTUATOR_FACTORY_DEFAULT"
		t.Setenv(prefix+"_HTTP_LISTEN", "127.0.0.1:0")
		serverName := u.GetInterfaceName[httpserver.RestServer]()
		assertActuatorFactoryWiring(t, serverName, ctx.PackageOf(
			httpserver.NewRestServerSilent(serverName, prefix, 0),
			AddToDefaultHttpServer(),
		))
	})

	t.Run("named HTTP server", func(t *testing.T) {
		const (
			prefix     = "ACTUATOR_FACTORY_NAMED"
			serverName = "actuator-factory-http-server"
		)
		t.Setenv(prefix+"_HTTP_LISTEN", "127.0.0.1:0")
		assertActuatorFactoryWiring(t, serverName, ctx.PackageOf(
			httpserver.NewRestServerSilent(serverName, prefix, 0),
			AddToHttpServer(serverName),
		))
	})

	t.Run("independent server", func(t *testing.T) {
		t.Setenv("ACTUATOR_HTTP_LISTEN", "127.0.0.1:0")
		assertActuatorFactoryWiring(t, defaultServerName, RunAsIndependentServer())
	})
}

func TestActuatorConfiguredTokensProtectEveryRoute(t *testing.T) {
	tokens := " actuator-one , actuator-two "
	baseURL := startActuatorApplication(t, &tokens)
	for _, path := range []string{
		"/actuator/health",
		"/actuator/health/plain",
		"/actuator/services",
		"/actuator/metrics",
	} {
		if status := actuatorRequest(t, baseURL, path, "").StatusCode; status != http.StatusUnauthorized {
			t.Fatalf("%s missing-token status = %d", path, status)
		}
		if status := actuatorRequest(t, baseURL, path, "rejected").StatusCode; status != http.StatusForbidden {
			t.Fatalf("%s rejected-token status = %d", path, status)
		}
		if status := actuatorRequest(t, baseURL, path, "actuator-two").StatusCode; status != http.StatusOK {
			t.Fatalf("%s accepted-token status = %d", path, status)
		}
	}
}

func TestActuatorPolicyFailsClosed(t *testing.T) {
	unsetEnvironment(t, actuatorTokenConfigKey)
	t.Setenv("ACTUATOR_NONLOCAL_HTTP_LISTEN", "0.0.0.0:0")
	server := httpserver.NewRestServerSilent("actuator-nonlocal", "ACTUATOR_NONLOCAL", 0)
	application := ctx.CreateContextualizedApplication(ctx.PackageOf(server))
	defer func() { application.Stop().Join() }()
	if _, err := actuatorAccessMiddleware(server); err == nil {
		t.Fatal("non-loopback actuator without tokens was accepted")
	}

	application.Stop().Join()
	invalid := "valid,"
	t.Setenv(actuatorTokenConfigKey, invalid)
	if _, err := actuatorAccessMiddleware(server); err == nil {
		t.Fatal("empty token entry was accepted")
	}
}

func TestActuatorPolicyUsesOnlyExactComponentTokensAndHidesSecrets(t *testing.T) {
	server := &opaqueActuatorServer{}
	unsetEnvironment(t, actuatorTokenConfigKey)
	t.Setenv("HTTP_AUTH_TOKENS", "global-token")
	t.Setenv("PROFILER_HTTP_AUTH_TOKENS", "profiler-token")
	if _, err := actuatorAccessMiddleware(server); err == nil {
		t.Fatal("global or profiler token was used as an actuator fallback")
	}

	const secret = "synthetic-actuator-secret"
	t.Setenv(actuatorTokenConfigKey, secret+",")
	if _, err := actuatorAccessMiddleware(server); err == nil {
		t.Fatal("invalid actuator token list was accepted")
	} else if strings.Contains(err.Error(), secret) {
		t.Fatalf("actuator policy error exposed token: %v", err)
	}

	t.Setenv(actuatorTokenConfigKey, "valid-token, valid-token")
	if middleware, err := actuatorAccessMiddleware(server); err != nil || middleware == nil {
		t.Fatalf("configured custom-server policy = %v/%v", middleware, err)
	}
}
