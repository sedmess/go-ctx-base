package db

import (
	"context"
	"errors"
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
