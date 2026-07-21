package httpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/ctx/logger"
)

func newLifecycleTestServer(t *testing.T, name string) *restServer {
	t.Helper()
	prefix := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	t.Setenv(prefix+"_HTTP_LISTEN", "127.0.0.1:0")
	server := NewRestServerSilent(name, prefix, 0).(*restServer)
	server.l = logger.New(name)
	return server
}

func testServerURL(t *testing.T, server *restServer, path string) string {
	t.Helper()
	address, ok := server.boundAddress()
	if !ok {
		t.Fatal("server has no bound address")
	}
	return "http://" + address.String() + path
}

func getStatus(t *testing.T, url string) int {
	t.Helper()
	status, _ := getStatusAndHeaders(t, url)
	return status
}

func getStatusAndHeaders(t *testing.T, url string) (int, http.Header) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode, response.Header.Clone()
}

func responseHeaderMiddleware(name string) Middleware {
	return func(chain rest.HandlerFunc, writer rest.ResponseWriter, request *rest.Request) error {
		writer.Header().Add(name, "applied")
		chain(writer, request)
		return nil
	}
}

func TestListenerConfigurationAndDuplicateBind(t *testing.T) {
	t.Run("prefix wins over global fallback", func(t *testing.T) {
		t.Setenv("HTTP_LISTEN", "127.0.0.1:1")
		server := newLifecycleTestServer(t, "prefix-wins")
		if err := server.Init(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = server.Dispose() })
		if server.generation.server.Addr != "127.0.0.1:0" {
			t.Fatalf("configured address = %q", server.generation.server.Addr)
		}
	})

	t.Run("global fallback remains", func(t *testing.T) {
		t.Setenv("HTTP_LISTEN", "127.0.0.1:0")
		server := NewRestServerSilent("fallback", "NO_SUCH_PREFIX", 0).(*restServer)
		server.l = logger.New("fallback")
		if err := server.Init(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = server.Dispose() })
		if server.generation.server.Addr != "127.0.0.1:0" {
			t.Fatalf("configured address = %q", server.generation.server.Addr)
		}
	})

	t.Run("empty address preserves net http behavior", func(t *testing.T) {
		t.Setenv("HTTP_LISTEN", "127.0.0.1:1")
		t.Setenv("PRESENT_EMPTY_HTTP_LISTEN", "")
		server := NewRestServerSilent("present-empty", "PRESENT_EMPTY", 0).(*restServer)
		configured := server.getEnv(serverListenKey).AsStringDefault("127.0.0.1:2")
		if configured != "" {
			t.Fatalf("present-empty prefix fell back to global value %q", configured)
		}
		if got := effectiveBindAddress(configured); got != ":http" {
			t.Fatalf("effective empty bind = %q", got)
		}
	})

	t.Run("port zero listeners are distinct", func(t *testing.T) {
		first := newLifecycleTestServer(t, "port-zero-first")
		second := newLifecycleTestServer(t, "port-zero-second")
		if err := first.Init(); err != nil {
			t.Fatal(err)
		}
		defer first.Dispose()
		if err := second.Init(); err != nil {
			t.Fatal(err)
		}
		defer second.Dispose()
		firstAddress, _ := first.boundAddress()
		secondAddress, _ := second.boundAddress()
		if firstAddress.String() == secondAddress.String() {
			t.Fatalf("port-zero listeners shared %s", firstAddress)
		}
	})

	t.Run("real duplicate bind fails before serving and rolls back", func(t *testing.T) {
		first := newLifecycleTestServer(t, "duplicate-first")
		if err := first.Init(); err != nil {
			t.Fatal(err)
		}
		address, _ := first.boundAddress()

		t.Setenv("DUPLICATE_SECOND_HTTP_LISTEN", address.String())
		second := NewRestServerSilent("duplicate-second", "DUPLICATE_SECOND", 0).(*restServer)
		second.l = logger.New("duplicate-second")
		err := second.Init()
		if err == nil || !strings.Contains(err.Error(), "duplicate-second") || !strings.Contains(err.Error(), address.String()) {
			t.Fatalf("unexpected duplicate error: %v", err)
		}

		if err := first.Dispose(); err != nil {
			t.Fatal(err)
		}
		reused, err := net.Listen("tcp", address.String())
		if err != nil {
			t.Fatalf("released sibling address was not reusable: %v", err)
		}
		_ = reused.Close()
	})
}

func TestDuplicateListenerStartupFailureSubprocess(t *testing.T) {
	if os.Getenv("GO_CTX_BASE_DUPLICATE_LISTENER_HELPER") == "1" {
		address := os.Getenv("GO_CTX_BASE_DUPLICATE_LISTENER_ADDRESS")
		_ = os.Setenv("FIRST_HTTP_LISTEN", address)
		_ = os.Setenv("SECOND_HTTP_LISTEN", address)
		ctx.CreateContextualizedApplication(ctx.PackageOf(
			NewRestServerSilent("first-listener", "FIRST", 0),
			NewRestServerSilent("second-listener", "SECOND", 0),
		))
		return
	}

	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reservation.Addr().String()
	_ = reservation.Close()

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestDuplicateListenerStartupFailureSubprocess$")
	command.Env = append(
		os.Environ(),
		"GO_CTX_BASE_DUPLICATE_LISTENER_HELPER=1",
		"GO_CTX_BASE_DUPLICATE_LISTENER_ADDRESS="+address,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("duplicate listener startup unexpectedly succeeded:\n%s", output)
	}
	if !strings.Contains(string(output), address) || !strings.Contains(string(output), "cannot listen") {
		t.Fatalf("startup failure was not actionable:\n%s", output)
	}
}

func TestRestServerGenerationLifecycle(t *testing.T) {
	server := newLifecycleTestServer(t, "generation")
	server.AddMiddleware(responseHeaderMiddleware("X-Persistent-Middleware"))
	RegisterRoute(server, http.MethodGet, "/persistent").Handler(func(*RequestData) (response Response) {
		response.Ok().Content("persistent")
		return
	})

	for generation := 0; generation < 100; generation++ {
		if err := server.Init(); err != nil {
			t.Fatalf("generation %d init: %v", generation, err)
		}
		server.AddMiddleware(responseHeaderMiddleware("X-Generation-Middleware"))
		RegisterRoute(server, http.MethodGet, "/generation").Handler(func(*RequestData) (response Response) {
			response.Ok().Content("generation")
			return
		})
		server.AfterStart()
		status, headers := getStatusAndHeaders(t, testServerURL(t, server, "/persistent"))
		if status != http.StatusOK {
			t.Fatalf("generation %d persistent status = %d", generation, status)
		}
		if values := headers.Values("X-Persistent-Middleware"); len(values) != 1 {
			t.Fatalf("generation %d persistent middleware applications = %d", generation, len(values))
		}
		if values := headers.Values("X-Generation-Middleware"); len(values) != 1 {
			t.Fatalf("generation %d generation middleware applications = %d", generation, len(values))
		}
		status, headers = getStatusAndHeaders(t, testServerURL(t, server, "/generation"))
		if status != http.StatusOK {
			t.Fatalf("generation %d route status = %d", generation, status)
		}
		if values := headers.Values("X-Persistent-Middleware"); len(values) != 1 {
			t.Fatalf("generation %d persistent route middleware applications = %d", generation, len(values))
		}
		if values := headers.Values("X-Generation-Middleware"); len(values) != 1 {
			t.Fatalf("generation %d generation route middleware applications = %d", generation, len(values))
		}
		server.BeforeStop()
		if err := server.Dispose(); err != nil {
			t.Fatalf("generation %d dispose: %v", generation, err)
		}
	}
}

func TestRestServerRejectsInvalidRoute(t *testing.T) {
	server := newLifecycleTestServer(t, "invalid-route")
	defer func() {
		panicValue := recover()
		if panicValue == nil {
			t.Fatal("invalid route did not panic during registration")
		}
		if !strings.Contains(fmt.Sprint(panicValue), "invalid route registration") {
			t.Fatalf("invalid route panic = %v", panicValue)
		}
	}()
	RegisterRoute(server, http.MethodGet, "invalid").Handler(func(*RequestData) (response Response) {
		response.Ok()
		return
	})
}

func TestRestServerRejectsDuplicateGenerationRoute(t *testing.T) {
	server := newLifecycleTestServer(t, "duplicate-route")
	if err := server.Init(); err != nil {
		t.Fatal(err)
	}
	defer server.Dispose()

	RegisterRoute(server, http.MethodGet, "/same").Handler(func(*RequestData) (response Response) {
		response.Ok()
		return
	})
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate route did not panic during registration")
		}
	}()
	RegisterRoute(server, http.MethodGet, "/same").Handler(func(*RequestData) (response Response) {
		response.Ok()
		return
	})
}

func TestRestServerCancelsRequestsAndCleanupIsIdempotent(t *testing.T) {
	server := newLifecycleTestServer(t, "request-cancel")
	if err := server.Init(); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	finished := make(chan error, 1)
	RegisterRoute(server, http.MethodGet, "/block").Handler(func(request *RequestData) (response Response) {
		close(started)
		<-request.Request.Context().Done()
		finished <- request.Request.Context().Err()
		response.Ok()
		return
	})
	server.AfterStart()
	blockURL := testServerURL(t, server, "/block")

	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		_, _ = http.Get(blockURL)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}

	var cleanup sync.WaitGroup
	for index := 0; index < 8; index++ {
		cleanup.Add(1)
		go func() {
			defer cleanup.Done()
			server.BeforeStop()
			_ = server.Dispose()
		}()
	}
	cleanup.Wait()

	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("request context error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request context was not canceled")
	}
	select {
	case <-requestDone:
	case <-time.After(2 * time.Second):
		t.Fatal("request client did not return")
	}
}
