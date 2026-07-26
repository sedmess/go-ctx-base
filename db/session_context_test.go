package db

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestSessionContextCancelsActiveQuery(t *testing.T) {
	connection := newTestConnection(t, "session-active-query")
	if err := connection.Init(); err != nil {
		t.Fatal(err)
	}
	defer connection.Dispose()

	timeout, cancelTimeout := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancelTimeout()
	err := connection.SessionContext(timeout, func(session *Session) error {
		return session.Exec("WITH RECURSIVE r(i) AS (VALUES(0) UNION ALL SELECT i FROM r LIMIT 1000000000) SELECT i FROM r WHERE i = 1;").Error
	})
	if err == nil {
		t.Fatal("long query ignored cancellation")
	}
}

func TestSessionContextCancelsPoolAcquisition(t *testing.T) {
	connection := newTestConnection(t, "session-pool-wait")
	t.Setenv("SESSION_POOL_WAIT_DB_MAX_OPEN_CONNS", "1")
	if err := connection.Init(); err != nil {
		t.Fatal(err)
	}
	defer connection.Dispose()

	generation, err := connection.activeGeneration()
	if err != nil {
		t.Fatal(err)
	}
	heldConnection, err := generation.sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer heldConnection.Close()

	timeout, cancelTimeout := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelTimeout()
	err = connection.SessionContext(timeout, func(*Session) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pool acquisition error = %v", err)
	}
}

func TestSessionContextObservesConnectionShutdown(t *testing.T) {
	connection := newTestConnection(t, "session-shutdown")
	if err := connection.Init(); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- connection.SessionContext(context.Background(), func(session *Session) error {
			close(started)
			<-session.Context.Done()
			return session.Context.Err()
		})
	}()
	<-started
	if err := CloseConnection(connection); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("shutdown session error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session did not stop with connection")
	}
}

func TestConnectionRejectsConcurrentStatsDuringClose(t *testing.T) {
	connection := newTestConnection(t, "stats-shutdown")
	if err := connection.Init(); err != nil {
		t.Fatal(err)
	}

	const workers = 32
	ready := make(chan struct{}, workers)
	var waitGroup sync.WaitGroup
	for range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			first := true
			for {
				if _, err := connection.Stats(); err != nil {
					return
				}
				if first {
					ready <- struct{}{}
					first = false
				}
				runtime.Gosched()
			}
		}()
	}
	for range workers {
		<-ready
	}

	if err := CloseConnection(connection); err != nil {
		t.Fatal(err)
	}

	stopped := make(chan struct{})
	go func() {
		waitGroup.Wait()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("statistics callers did not observe connection shutdown")
	}
	if _, err := connection.Stats(); err == nil {
		t.Fatal("closed connection accepted statistics request")
	}
}

func TestConnectionOperationsCanOverlapSerializedRestart(t *testing.T) {
	connection := newTestConnection(t, "stats-restart")
	if err := connection.Init(); err != nil {
		t.Fatal(err)
	}

	const workers = 32
	ready := make(chan struct{}, workers)
	stop := make(chan struct{})
	var waitGroup sync.WaitGroup
	for range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_, _ = connection.Stats()
			ready <- struct{}{}
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = connection.Stats()
					runtime.Gosched()
				}
			}
		}()
	}
	for range workers {
		<-ready
	}

	var stopOnce sync.Once
	stopWorkers := func() {
		stopOnce.Do(func() { close(stop) })
		waitGroup.Wait()
	}
	defer stopWorkers()

	for range 25 {
		if err := CloseConnection(connection); err != nil {
			t.Fatal(err)
		}
		runtime.Gosched()
		if err := connection.Init(); err != nil {
			t.Fatal(err)
		}
		runtime.Gosched()
	}

	stopWorkers()
	if err := connection.Dispose(); err != nil {
		t.Fatal(err)
	}
}
