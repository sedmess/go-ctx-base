//go:build integration

package scheduler

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/sedmess/go-ctx/ctx/logger"
)

func TestPostgresLockerIntegration(t *testing.T) {
	dsn := os.Getenv("GO_CTX_BASE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GO_CTX_BASE_TEST_POSTGRES_DSN is not configured")
	}
	t.Setenv(schedulerLockProviderKey, providerPostgres)
	t.Setenv("SCHEDULER_DB_DSN", dsn)

	first := &Locker{l: logger.New("postgres-locker-first"), connectionName: "scheduler-integration-first"}
	second := &Locker{l: logger.New("postgres-locker-second"), connectionName: "scheduler-integration-second"}
	if err := first.Init(); err != nil {
		t.Fatal(err)
	}
	defer first.Dispose()
	if err := second.Init(); err != nil {
		t.Fatal(err)
	}
	defer second.Dispose()

	firstLock, err := first.Lock(context.Background(), "integration-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Lock(context.Background(), "integration-key"); !errors.Is(err, alreadyLockedErr) {
		t.Fatalf("cross-locker contention error = %v", err)
	}
	if err := firstLock.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}
	reacquired, err := second.Lock(context.Background(), "integration-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := reacquired.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}

	cancelContext, cancel := context.WithCancel(context.Background())
	canceledLock, err := first.Lock(cancelContext, "integration-cancel-key")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	cancellationDone := make(chan error, 1)
	go func() { cancellationDone <- canceledLock.Unlock(cancelContext) }()
	select {
	case err := <-cancellationDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled lock result = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled PostgreSQL lease did not complete")
	}
	reacquiredAfterCancellation, err := second.Lock(context.Background(), "integration-cancel-key")
	if err != nil {
		t.Fatalf("reacquire after cancellation: %v", err)
	}
	if err := reacquiredAfterCancellation.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}

	firstConnection := first.generation.db
	secondConnection := second.generation.db
	if err := first.Dispose(); err != nil {
		t.Fatal(err)
	}
	if err := second.Dispose(); err != nil {
		t.Fatal(err)
	}
	if _, err := firstConnection.Stats(); err == nil {
		t.Fatal("first locker pool remained active after disposal")
	}
	if _, err := secondConnection.Stats(); err == nil {
		t.Fatal("second locker pool remained active after disposal")
	}
}
