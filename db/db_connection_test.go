package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sedmess/go-ctx/ctx"
)

type capturingRegisterer struct {
	delegate  prometheus.Registerer
	collector prometheus.Collector
}

func (registerer *capturingRegisterer) Register(collector prometheus.Collector) error {
	registerer.collector = collector
	return registerer.delegate.Register(collector)
}

func (registerer *capturingRegisterer) MustRegister(collectors ...prometheus.Collector) {
	for _, collector := range collectors {
		if err := registerer.Register(collector); err != nil {
			panic(err)
		}
	}
}

func (registerer *capturingRegisterer) Unregister(collector prometheus.Collector) bool {
	return registerer.delegate.Unregister(collector)
}

type markerRegisterer struct {
	delegate   *prometheus.Registry
	markerPath string
}

func (registerer *markerRegisterer) Register(collector prometheus.Collector) error {
	return registerer.delegate.Register(collector)
}

func (registerer *markerRegisterer) MustRegister(collectors ...prometheus.Collector) {
	registerer.delegate.MustRegister(collectors...)
}

func (registerer *markerRegisterer) Unregister(collector prometheus.Collector) bool {
	unregistered := registerer.delegate.Unregister(collector)
	if unregistered {
		_ = os.WriteFile(registerer.markerPath, []byte("collector-unregistered"), 0o600)
	}
	return unregistered
}

type laterDatabaseInitializationFailure struct{}

func (*laterDatabaseInitializationFailure) Name() string {
	return "z-db-later-initialization-failure"
}

func (*laterDatabaseInitializationFailure) Init() error {
	return errors.New("synthetic later database initialization failure")
}

func newTestConnection(t *testing.T, name string) *connection {
	t.Helper()
	prefix := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	t.Setenv(prefix+"_DB_SQLITE_PATH", fmt.Sprintf("file:%s?mode=memory&cache=shared", name))
	connection := NewConnection(name, prefix, false, false).(*connection)
	connection.registerer = prometheus.NewRegistry()
	return connection
}

func TestConnectionLifecycleAndRestart(t *testing.T) {
	connection := newTestConnection(t, "connection-lifecycle")
	if err := connection.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Stats(); err != nil {
		t.Fatal(err)
	}

	var waitGroup sync.WaitGroup
	errorsFound := make(chan error, 16)
	for index := 0; index < 16; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			errorsFound <- CloseConnection(connection)
		}()
	}
	waitGroup.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatalf("concurrent close: %v", err)
		}
	}
	if _, err := connection.Stats(); err == nil {
		t.Fatal("closed connection still reported stats")
	}
	if err := CloseConnection(connection); err != nil {
		t.Fatalf("repeated close: %v", err)
	}

	if err := connection.Init(); err != nil {
		t.Fatalf("restart init: %v", err)
	}
	if err := connection.Session(func(session *Session) error {
		return session.Exec("select 1").Error
	}); err != nil {
		t.Fatalf("restart session: %v", err)
	}
	if err := connection.Dispose(); err != nil {
		t.Fatal(err)
	}
}

func TestConnectionProvisionalInitializationRollback(t *testing.T) {
	registry := prometheus.NewRegistry()
	active := newTestConnection(t, "provisional-rollback")
	active.registerer = registry
	if err := active.Init(); err != nil {
		t.Fatal(err)
	}
	defer active.Dispose()

	capture := &capturingRegisterer{delegate: registry}
	provisional := newTestConnection(t, "provisional-rollback")
	provisional.registerer = capture
	if err := provisional.Init(); err == nil {
		_ = provisional.Dispose()
		t.Fatal("duplicate metric registration unexpectedly succeeded")
	}
	if provisional.generation != nil {
		t.Fatal("failed provisional generation was published")
	}
	collector, ok := capture.collector.(*dbStatsCollector)
	if !ok || collector == nil || collector.pool == nil {
		t.Fatalf("provisional collector was not captured: %T", capture.collector)
	}
	if err := collector.pool.PingContext(context.Background()); err == nil {
		t.Fatal("provisional SQL pool remained usable after registration failure")
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != len(dbStatMetrics) {
		t.Fatalf("active collector families = %d, want %d", len(families), len(dbStatMetrics))
	}
	if err := active.Dispose(); err != nil {
		t.Fatal(err)
	}
	families, err = registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != 0 {
		t.Fatalf("collector families after owner disposal = %d", len(families))
	}
}

func TestConnectionNormalContainerStopClosesGeneration(t *testing.T) {
	connection := newTestConnection(t, "normal-container-stop")
	application := ctx.CreateContextualizedApplication(ctx.PackageOf(connection))
	generation := connection.generation
	if generation == nil || generation.sqlDB == nil {
		application.Stop().Join()
		t.Fatal("container did not publish a database generation")
	}
	pool := generation.sqlDB
	application.Stop().Join()
	if connection.generation != nil {
		t.Fatal("container stop retained the database generation")
	}
	if err := pool.PingContext(context.Background()); err == nil {
		t.Fatal("container stop left the SQL pool usable")
	}
}

func TestConnectionLaterServiceFailureDisposesGeneration(t *testing.T) {
	const helperKey = "GO_CTX_BASE_DB_LATER_FAILURE_HELPER"
	if os.Getenv(helperKey) == "1" {
		markerPath := os.Getenv("GO_CTX_BASE_DB_DISPOSAL_MARKER")
		const prefix = "DB_LATER_FAILURE"
		_ = os.Setenv(prefix+"_DB_SQLITE_PATH", "file:db-later-failure?mode=memory&cache=shared")
		connection := NewConnection("a-db-startup-rollback", prefix, false, false).(*connection)
		connection.registerer = &markerRegisterer{delegate: prometheus.NewRegistry(), markerPath: markerPath}
		ctx.CreateContextualizedApplication(ctx.PackageOf(connection, &laterDatabaseInitializationFailure{}))
		return
	}

	markerPath := t.TempDir() + string(os.PathSeparator) + "database-disposed"
	command := exec.Command(os.Args[0], "-test.run=^TestConnectionLaterServiceFailureDisposesGeneration$")
	command.Env = append(os.Environ(), helperKey+"=1", "GO_CTX_BASE_DB_DISPOSAL_MARKER="+markerPath)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("later-service initialization failure unexpectedly succeeded:\n%s", output)
	}
	if !strings.Contains(string(output), "z-db-later-initialization-failure") {
		t.Fatalf("startup failure did not identify the later service:\n%s", output)
	}
	marker, readErr := os.ReadFile(markerPath)
	if readErr != nil || string(marker) != "collector-unregistered" {
		t.Fatalf("database disposal marker = %q, %v; output:\n%s", marker, readErr, output)
	}
}

func TestConnectionInitializationFailureIsSecretSafe(t *testing.T) {
	const secret = "architecture-test-password"
	prefix := "SECRET_SAFE_CONNECTION"
	t.Setenv(prefix+"_DB_DSN", "postgres://synthetic-user:"+secret+"@127.0.0.1:1/not-real?sslmode=disable")
	connection := NewConnection("secret-safe", prefix, false, false)
	err := connection.Init()
	if err == nil {
		_ = CloseConnection(connection)
		t.Fatal("invalid database unexpectedly initialized")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("initialization error exposed secret: %v", err)
	}
}

func TestCloseConnectionValidation(t *testing.T) {
	if err := CloseConnection(nil); err == nil {
		t.Fatal("nil connection close succeeded")
	}

	var typedNil *connection
	if err := CloseConnection(typedNil); err == nil {
		t.Fatal("typed nil connection close succeeded")
	}
}

func TestConnectionGenerationCancellation(t *testing.T) {
	connection := newTestConnection(t, "generation-cancel")
	rootContext, cancelRoot := context.WithCancel(context.Background())
	connection.rootContext = rootContext
	if err := connection.Init(); err != nil {
		t.Fatal(err)
	}
	cancelRoot()
	err := connection.Session(func(*Session) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("session error = %v", err)
	}
	if err := connection.Dispose(); err != nil {
		t.Fatal(err)
	}
}
