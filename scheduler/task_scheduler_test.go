package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sedmess/go-ctx-base/db"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/ctx/logger"
)

func TestSchedulerContextTaskStopsAndRestarts(t *testing.T) {
	locker := newLocalTestLocker(t)
	scheduler := &Scheduler{
		l:        logger.New("test-scheduler"),
		location: "UTC",
		locker:   locker,
	}
	scheduler.Init()
	scheduler.AfterStart()

	started := make(chan struct{})
	finished := make(chan struct{})
	if _, err := scheduler.ScheduleTaskCronContext("0 0 0 1 1 *", "context-job", func(taskContext context.Context) {
		close(started)
		<-taskContext.Done()
		close(finished)
	}); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.RunScheduledTaskImmediate("context-job"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("context job did not start")
	}
	scheduler.BeforeStop()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("context job did not observe scheduler stop")
	}

	scheduler.Init()
	var legacyRuns atomic.Int32
	if _, err := scheduler.ScheduleTaskCron("0 0 0 1 1 *", "legacy-job", func() {
		legacyRuns.Add(1)
	}); err != nil {
		t.Fatal(err)
	}
	scheduler.AfterStart()
	if err := scheduler.RunScheduledTaskImmediate("legacy-job"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for legacyRuns.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if legacyRuns.Load() != 1 {
		t.Fatalf("legacy runs = %d", legacyRuns.Load())
	}
	scheduler.BeforeStop()
	scheduler.Dispose()
}

func TestSchedulerStopsBeforeLockerClosesPrivateDatabase(t *testing.T) {
	var closeCalls atomic.Int32
	var orderingViolation atomic.Bool
	scheduler := &Scheduler{}
	fakeConnection := installPostgresLockerSeams(t,
		func(context.Context, db.Connection, string, func(error), func() error) error {
			return errors.New("unexpected lease acquisition")
		},
		func(db.Connection) error {
			scheduler.mu.Lock()
			stillActive := scheduler.generation != nil
			scheduler.mu.Unlock()
			if stillActive {
				orderingViolation.Store(true)
			}
			closeCalls.Add(1)
			return nil
		},
	)
	t.Setenv(schedulerLockProviderKey, providerPostgres)
	locker := &Locker{}
	application := ctx.CreateContextualizedApplication(ctx.PackageOf(scheduler, locker))
	application.Stop().Join()

	if orderingViolation.Load() {
		t.Fatal("locker closed its private database before scheduler shutdown completed")
	}
	if fakeConnection.initializations.Load() != 1 || closeCalls.Load() != 1 {
		t.Fatalf("private connection init/close calls = %d/%d", fakeConnection.initializations.Load(), closeCalls.Load())
	}
}
