package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sedmess/go-ctx-base/db"
	"github.com/sedmess/go-ctx-base/httpserver"
	"github.com/sedmess/go-ctx-base/scheduler"
	"github.com/sedmess/go-ctx-base/utils/channels"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/ctx/ctx_testing"
	"github.com/sedmess/go-ctx/ctx/logger"
)

const (
	rootHelperModeKey        = "GO_CTX_BASE_ROOT_HELPER"
	rootGenerationHelperMode = "generations"
	rootDuplicateHelperMode  = "duplicate"
	rootAuthenticationToken  = "synthetic-root-test-token"
	rootGenerationCount      = 100
	rootRollbackObserverName = "a-root-rollback-observer"
	rootRollbackMarkerKey    = "GO_CTX_BASE_ROOT_ROLLBACK_MARKER"
	rootAfterStartMarkerKey  = "GO_CTX_BASE_ROOT_AFTER_START_MARKER"
	rootDuplicateAddressKey  = "GO_CTX_BASE_ROOT_DUPLICATE_ADDRESS"
	rootProfilerAddressKey   = "GO_CTX_BASE_ROOT_PROFILER_ADDRESS"
)

type rootListenerAddresses struct {
	base     string
	actuator string
	profiler string
}

type rootStartupRollbackObserver struct {
	address          string
	rollbackMarker   string
	afterStartMarker string
}

func (observer *rootStartupRollbackObserver) AfterStart() {
	_ = os.WriteFile(observer.afterStartMarker, []byte("after-start-ran"), 0o600)
}

func (observer *rootStartupRollbackObserver) Dispose() error {
	deadline := time.Now().Add(2 * time.Second)
	for {
		listener, err := net.Listen("tcp", observer.address)
		if err == nil {
			_ = listener.Close()
			return os.WriteFile(observer.rollbackMarker, []byte("sibling-listener-released"), 0o600)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("sibling listener %s was not released: %w", observer.address, err)
		}
		time.Sleep(time.Millisecond)
	}
}

type messageServiceStub struct {
	l logger.Logger `ctx:""`
}

func (s *messageServiceStub) SaveMessage(string, string, string) error {
	s.l.Info("SaveMessage stub")
	return nil
}

func (s *messageServiceStub) GetMessages(string, int64) channels.StreamingChan[Message] {
	s.l.Info("GetMessage stub")
	return channels.CreateChannel(func(sink func(data Message, context context.Context) bool) error {
		return nil
	})
}

func newRootTestingApplication() ctx_testing.TestingApplication {
	return newRootTestingApplicationWithListeners(rootListenerAddresses{
		base:     "127.0.0.1:0",
		actuator: "127.0.0.1:0",
		profiler: "127.0.0.1:0",
	})
}

func newRootTestingApplicationWithListeners(addresses rootListenerAddresses) ctx_testing.TestingApplication {
	return ctx_testing.CreateTestingApplication(Packages...).
		WithParameter("DB_SQLITE_PATH", "file::memory:?cache=shared").
		WithParameter("BASE_HTTP_LISTEN", addresses.base).
		WithParameter("ACTUATOR_HTTP_LISTEN", addresses.actuator).
		WithParameter("PROFILER_HTTP_LISTEN", addresses.profiler).
		WithParameter("HTTP_AUTH_TOKENS", rootAuthenticationToken).
		WithParameter("ACTUATOR_HTTP_AUTH_TOKENS", rootAuthenticationToken).
		WithParameter("PROFILER_HTTP_AUTH_TOKENS", rootAuthenticationToken).
		WithTestingService(ctx_testing.Instead[MessageService](&messageServiceStub{}))
}

func runRootTestingApplication(runFn func() int) int {
	return newRootTestingApplication().Run(runFn)
}

func reserveRootListenerAddresses() (rootListenerAddresses, error) {
	listeners := make([]net.Listener, 0, 3)
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	for range 3 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return rootListenerAddresses{}, err
		}
		listeners = append(listeners, listener)
	}
	return rootListenerAddresses{
		base:     listeners[0].Addr().String(),
		actuator: listeners[1].Addr().String(),
		profiler: listeners[2].Addr().String(),
	}, nil
}

func waitForRootEndpoint(client *http.Client, address string, path string) error {
	deadline := time.Now().Add(2 * time.Second)
	var lastError error
	for time.Now().Before(deadline) {
		request, err := http.NewRequest(http.MethodGet, "http://"+address+path, nil)
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+rootAuthenticationToken)
		response, err := client.Do(request)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
			lastError = fmt.Errorf("status %d", response.StatusCode)
		} else {
			lastError = err
		}
		time.Sleep(time.Millisecond)
	}
	return fmt.Errorf("endpoint %s%s did not become ready: %w", address, path, lastError)
}

func runRootGenerationHelper() int {
	addresses, err := reserveRootListenerAddresses()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	transport := &http.Transport{DisableKeepAlives: true}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	defer transport.CloseIdleConnections()
	for generation := 0; generation < rootGenerationCount; generation++ {
		code := newRootTestingApplicationWithListeners(addresses).Run(func() int {
			probes := []struct {
				address string
				path    string
			}{
				{address: addresses.base, path: "/messages?to=readiness&since=0"},
				{address: addresses.actuator, path: "/actuator/health"},
				{address: addresses.profiler, path: "/profiler/named_profile?name=goroutine&debug=0"},
			}
			for _, probe := range probes {
				if err := waitForRootEndpoint(client, probe.address, probe.path); err != nil {
					fmt.Fprintf(os.Stderr, "root generation %d: %v\n", generation, err)
					return 1
				}
			}
			return 0
		})
		if code != 0 {
			return code
		}
	}
	return 0
}

func runRootDuplicateHelper() int {
	duplicateAddress := os.Getenv(rootDuplicateAddressKey)
	profilerAddress := os.Getenv(rootProfilerAddressKey)
	application := newRootTestingApplicationWithListeners(rootListenerAddresses{
		base:     duplicateAddress,
		actuator: duplicateAddress,
		profiler: profilerAddress,
	}).WithTestingServices(ctx.PackageOf(ctx.WithName(rootRollbackObserverName, &rootStartupRollbackObserver{
		address:          duplicateAddress,
		rollbackMarker:   os.Getenv(rootRollbackMarkerKey),
		afterStartMarker: os.Getenv(rootAfterStartMarkerKey),
	})))
	return application.Run(func() int {
		_ = os.WriteFile(os.Getenv(rootAfterStartMarkerKey), []byte("application-reported-ready"), 0o600)
		return 1
	})
}

func TestMain(m *testing.M) {
	switch os.Getenv(rootHelperModeKey) {
	case rootGenerationHelperMode:
		os.Exit(runRootGenerationHelper())
	case rootDuplicateHelperMode:
		os.Exit(runRootDuplicateHelper())
	}
	os.Exit(runRootTestingApplication(m.Run))
}

func Test_MessageController(t *testing.T) {
	messageController, ok := ctx.GetTypedService[*messageController]()
	if !ok || messageController == nil {
		t.FailNow()
	}
	messages := messageController.messageService.GetMessages("", 0)
	slice, err := messages.CollectToSlice()
	if err != nil {
		t.FailNow()
	}
	if len(slice) > 0 {
		t.FailNow()
	}
}

func TestRootCompositionServicesReady(t *testing.T) {
	if server, ok := ctx.GetTypedService[httpserver.RestServer](); !ok || server == nil {
		t.Fatal("default HTTP server is not ready")
	}
	if connection, ok := ctx.GetTypedService[db.Connection](); !ok || connection == nil {
		t.Fatal("default database is not ready")
	}
	if taskScheduler, ok := ctx.GetTypedService[*scheduler.Scheduler](); !ok || taskScheduler == nil {
		t.Fatal("scheduler is not ready")
	}
}

func TestRootCompositionRepeatedGenerations(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^$")
	command.Env = append(os.Environ(), rootHelperModeKey+"="+rootGenerationHelperMode)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%d repeated root generations failed: %v\n%s", rootGenerationCount, err, output)
	}
}

func TestRootCompositionDuplicateEndpointRollsBackBeforeReadiness(t *testing.T) {
	addresses, err := reserveRootListenerAddresses()
	if err != nil {
		t.Fatal(err)
	}
	duplicateAddress := addresses.base
	profilerAddress := addresses.profiler
	markerDirectory := t.TempDir()
	rollbackMarker := filepath.Join(markerDirectory, "rollback-complete")
	afterStartMarker := filepath.Join(markerDirectory, "after-start")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^$")
	command.Env = append(os.Environ(),
		rootHelperModeKey+"="+rootDuplicateHelperMode,
		rootDuplicateAddressKey+"="+duplicateAddress,
		rootProfilerAddressKey+"="+profilerAddress,
		rootRollbackMarkerKey+"="+rollbackMarker,
		rootAfterStartMarkerKey+"="+afterStartMarker,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("duplicate root endpoint unexpectedly reached readiness:\n%s", output)
	}
	if !strings.Contains(string(output), duplicateAddress) ||
		!strings.Contains(string(output), "base.actuator-http-server") ||
		!strings.Contains(string(output), "cannot listen") {
		t.Fatalf("duplicate root failure was not actionable:\n%s", output)
	}
	marker, markerErr := os.ReadFile(rollbackMarker)
	if markerErr != nil || string(marker) != "sibling-listener-released" {
		t.Fatalf("root sibling rollback marker = %q, %v; output:\n%s", marker, markerErr, output)
	}
	if marker, markerErr := os.ReadFile(afterStartMarker); markerErr == nil {
		t.Fatalf("failed root composition ran AfterStart/readiness: %q", marker)
	} else if !os.IsNotExist(markerErr) {
		t.Fatal(markerErr)
	}
	reused, err := net.Listen("tcp", duplicateAddress)
	if err != nil {
		t.Fatalf("rolled-back root address was not reusable: %v", err)
	}
	_ = reused.Close()
}
