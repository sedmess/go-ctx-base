package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sedmess/go-ctx-base/db"
	"github.com/sedmess/go-ctx/ctx/health"
	"github.com/sedmess/go-ctx/ctx/logger"
)

type fakeLockerConnection struct {
	initializations atomic.Int32
}

func (connection *fakeLockerConnection) Init() error {
	connection.initializations.Add(1)
	return nil
}

func (*fakeLockerConnection) AutoMigrate(...any) {}

func (*fakeLockerConnection) Session(func(*db.Session) error) error {
	return errors.New("unexpected fake locker Session call")
}

func (*fakeLockerConnection) SessionContext(context.Context, func(*db.Session) error) error {
	return errors.New("unexpected fake locker SessionContext call")
}

func (*fakeLockerConnection) Stats() (sql.DBStats, error) {
	return sql.DBStats{}, nil
}

func (*fakeLockerConnection) Check() error {
	return nil
}

func (*fakeLockerConnection) Health() health.ServiceHealth {
	return health.Status(health.Up)
}

func installPostgresLockerSeams(
	t *testing.T,
	runner postgresLeaseTransactionRunner,
	closeConnection func(db.Connection) error,
) *fakeLockerConnection {
	t.Helper()
	previousCreate := createLockerConnection
	previousClose := closeLockerConnection
	previousRunner := executePostgresLeaseTransaction
	fakeConnection := &fakeLockerConnection{}
	createLockerConnection = func(string, string, bool, bool) db.Connection {
		return fakeConnection
	}
	closeLockerConnection = closeConnection
	executePostgresLeaseTransaction = runner
	t.Cleanup(func() {
		createLockerConnection = previousCreate
		closeLockerConnection = previousClose
		executePostgresLeaseTransaction = previousRunner
	})
	return fakeConnection
}

func newPostgresTestLocker(t *testing.T) *Locker {
	t.Helper()
	t.Setenv(schedulerLockProviderKey, providerPostgres)
	locker := &Locker{l: logger.New("postgres-test-locker")}
	if err := locker.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locker.Dispose() })
	return locker
}

func newLocalTestLocker(t *testing.T) *Locker {
	t.Helper()
	t.Setenv(schedulerLockProviderKey, providerLocal)
	locker := &Locker{l: logger.New("test-locker")}
	if err := locker.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locker.Dispose() })
	return locker
}

func TestLocalLockerContentionUnlockAndReacquire(t *testing.T) {
	locker := newLocalTestLocker(t)
	first, err := locker.Lock(context.Background(), "same-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locker.Lock(context.Background(), "same-key"); !errors.Is(err, alreadyLockedErr) {
		t.Fatalf("contention error = %v", err)
	}
	different, err := locker.Lock(context.Background(), "different-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := different.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}

	var waitGroup sync.WaitGroup
	for index := 0; index < 8; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if err := first.Unlock(context.Background()); err != nil {
				t.Errorf("repeated unlock: %v", err)
			}
		}()
	}
	waitGroup.Wait()

	reacquired, err := locker.Lock(context.Background(), "same-key")
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	if err := reacquired.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLockerCancellationAndShutdownRelease(t *testing.T) {
	locker := newLocalTestLocker(t)
	lockContext, cancelLock := context.WithCancel(context.Background())
	lock, err := locker.Lock(lockContext, "cancel-key")
	if err != nil {
		t.Fatal(err)
	}
	cancelLock()
	if err := lock.Unlock(lockContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled unlock error = %v", err)
	}

	reacquired, err := locker.Lock(context.Background(), "cancel-key")
	if err != nil {
		t.Fatalf("reacquire after cancellation: %v", err)
	}
	if err := reacquired.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}

	shutdownLock, err := locker.Lock(context.Background(), "shutdown-key")
	if err != nil {
		t.Fatal(err)
	}
	locker.BeforeStop()
	if err := shutdownLock.Unlock(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unlock after shutdown: %v", err)
	}
	if _, err := locker.Lock(context.Background(), "new-key"); err == nil {
		t.Fatal("stopped locker accepted a new lock")
	}

	if err := locker.Init(); err != nil {
		t.Fatalf("locker restart: %v", err)
	}
	restarted, err := locker.Lock(context.Background(), "shutdown-key")
	if err != nil {
		t.Fatalf("restart reacquire: %v", err)
	}
	if err := restarted.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLockerProviderValidationAndStableSQL(t *testing.T) {
	t.Setenv(schedulerLockProviderKey, "unsupported")
	locker := &Locker{l: logger.New("invalid-locker")}
	err := locker.Init()
	if err == nil || err.Error() != "unknown SCHEDULER_LOCK_PROVIDER: UNSUPPORTED" {
		t.Fatalf("provider error = %v", err)
	}
	const expected = "select pg_try_advisory_xact_lock(('x'||md5(?))::bit(64)::bigint) as success;"
	if postgresAdvisoryLockQuery != expected {
		t.Fatalf("advisory SQL changed: %s", postgresAdvisoryLockQuery)
	}
}

func TestLockerCancellationCompletesWithinBound(t *testing.T) {
	locker := newLocalTestLocker(t)
	lockContext, cancel := context.WithCancel(context.Background())
	lock, err := locker.Lock(lockContext, "bounded")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	done := make(chan error, 1)
	go func() { done <- lock.Unlock(lockContext) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled lock did not complete")
	}
}

func TestPostgresLockerDeterministicAcquisitionFailures(t *testing.T) {
	testCases := []struct {
		name       string
		want       error
		runner     postgresLeaseTransactionRunner
		stableText bool
	}{
		{
			name: "session failure before query",
			want: errors.New("synthetic session acquisition failure"),
		},
		{
			name: "query failure",
			want: errors.New("synthetic advisory query failure"),
		},
		{
			name:       "lock not acquired",
			want:       alreadyLockedErr,
			stableText: true,
		},
	}
	for index := range testCases {
		testCase := &testCases[index]
		t.Run(testCase.name, func(t *testing.T) {
			var closeCalls atomic.Int32
			switch testCase.name {
			case "session failure before query":
				testCase.runner = func(context.Context, db.Connection, string, func(error), func() error) error {
					return testCase.want
				}
			case "query failure":
				testCase.runner = func(_ context.Context, _ db.Connection, _ string, report func(error), _ func() error) error {
					report(testCase.want)
					return testCase.want
				}
			case "lock not acquired":
				testCase.runner = func(_ context.Context, _ db.Connection, _ string, report func(error), _ func() error) error {
					report(alreadyLockedErr)
					return alreadyLockedErr
				}
			}
			fakeConnection := installPostgresLockerSeams(t, testCase.runner, func(db.Connection) error {
				closeCalls.Add(1)
				return nil
			})
			locker := newPostgresTestLocker(t)
			result := make(chan error, 1)
			go func() {
				_, err := locker.Lock(context.Background(), "failure-key")
				result <- err
			}()
			select {
			case err := <-result:
				if !errors.Is(err, testCase.want) {
					t.Fatalf("acquisition error = %v, want %v", err, testCase.want)
				}
				if testCase.stableText && err.Error() != "resource has already locked" {
					t.Fatalf("contention text = %q", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("buffered acquisition failure stranded the caller")
			}
			locker.BeforeStop()
			if fakeConnection.initializations.Load() != 1 || closeCalls.Load() != 1 {
				t.Fatalf("connection init/close calls = %d/%d", fakeConnection.initializations.Load(), closeCalls.Load())
			}
		})
	}
}

func TestPostgresLockerCommitRollbackShutdownAndReacquisition(t *testing.T) {
	var commits atomic.Int32
	var rollbacks atomic.Int32
	var closeCalls atomic.Int32
	runner := func(_ context.Context, _ db.Connection, _ string, report func(error), waitForRelease func() error) error {
		report(nil)
		err := waitForRelease()
		if err == nil {
			commits.Add(1)
		} else {
			rollbacks.Add(1)
		}
		return err
	}
	installPostgresLockerSeams(t, runner, func(db.Connection) error {
		if rollbacks.Load() < 2 {
			t.Errorf("private database closed before active leases rolled back: %d", rollbacks.Load())
		}
		closeCalls.Add(1)
		return nil
	})
	locker := newPostgresTestLocker(t)

	explicit, err := locker.Lock(context.Background(), "explicit")
	if err != nil {
		t.Fatal(err)
	}
	var unlocks sync.WaitGroup
	for index := 0; index < 8; index++ {
		unlocks.Add(1)
		go func() {
			defer unlocks.Done()
			if err := explicit.Unlock(context.Background()); err != nil {
				t.Errorf("concurrent explicit unlock: %v", err)
			}
		}()
	}
	unlocks.Wait()
	if commits.Load() != 1 || rollbacks.Load() != 0 {
		t.Fatalf("explicit commit/rollback counts = %d/%d", commits.Load(), rollbacks.Load())
	}

	cancelContext, cancel := context.WithCancel(context.Background())
	canceledLease, err := locker.Lock(cancelContext, "canceled")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := canceledLease.Unlock(cancelContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lease result = %v", err)
	}
	if commits.Load() != 1 || rollbacks.Load() != 1 {
		t.Fatalf("cancellation commit/rollback counts = %d/%d", commits.Load(), rollbacks.Load())
	}

	reacquired, err := locker.Lock(context.Background(), "canceled")
	if err != nil {
		t.Fatalf("reacquire after cancellation: %v", err)
	}
	if err := reacquired.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}
	if commits.Load() != 2 || rollbacks.Load() != 1 {
		t.Fatalf("reacquisition commit/rollback counts = %d/%d", commits.Load(), rollbacks.Load())
	}

	shutdownLease, err := locker.Lock(context.Background(), "shutdown")
	if err != nil {
		t.Fatal(err)
	}
	locker.BeforeStop()
	if err := shutdownLease.Unlock(context.Background()); err != nil {
		t.Fatalf("unlock after shutdown = %v", err)
	}
	if commits.Load() != 2 || rollbacks.Load() != 2 || closeCalls.Load() != 1 {
		t.Fatalf("final commit/rollback/close counts = %d/%d/%d", commits.Load(), rollbacks.Load(), closeCalls.Load())
	}
}

func TestPostgresLockerTransactionFailureReleasesLease(t *testing.T) {
	transactionError := errors.New("synthetic transaction completion failure")
	var closeCalls atomic.Int32
	runner := func(_ context.Context, _ db.Connection, _ string, report func(error), _ func() error) error {
		report(nil)
		return transactionError
	}
	installPostgresLockerSeams(t, runner, func(db.Connection) error {
		closeCalls.Add(1)
		return nil
	})
	locker := newPostgresTestLocker(t)
	lease, err := locker.Lock(context.Background(), "transaction-error")
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Unlock(context.Background()); !errors.Is(err, transactionError) {
		t.Fatalf("transaction completion error = %v", err)
	}
	locker.BeforeStop()
	if closeCalls.Load() != 1 {
		t.Fatalf("connection close calls = %d", closeCalls.Load())
	}
}
