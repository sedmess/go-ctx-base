package scheduler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/sedmess/go-ctx/ctx/logger"
	"github.com/sedmess/go-ctx/u"
)

type Scheduler struct {
	l           logger.Logger   `ctx:""`
	rootContext context.Context `ctx:"context"`
	location    string          `env:"TZ=UTC"`
	locker      *Locker         `ctx:""`

	mu         sync.Mutex
	generation *schedulerGeneration
}

type schedulerGeneration struct {
	scheduler *gocron.Scheduler
	context   context.Context
	cancel    context.CancelFunc
	stopOnce  sync.Once
}

func (instance *Scheduler) Init() {
	if instance.l == nil {
		instance.l = logger.New("scheduler.Scheduler")
	}
	parentContext := instance.rootContext
	if parentContext == nil {
		parentContext = context.Background()
	}
	generationContext, cancelGeneration := context.WithCancel(parentContext)
	scheduler := gocron.NewScheduler(u.Must2(time.LoadLocation(instance.location)))
	scheduler.WithDistributedLocker(instance.locker)

	instance.mu.Lock()
	defer instance.mu.Unlock()
	if instance.generation != nil {
		cancelGeneration()
		panic("scheduler is already initialized")
	}
	instance.generation = &schedulerGeneration{
		scheduler: scheduler,
		context:   generationContext,
		cancel:    cancelGeneration,
	}
}

func (instance *Scheduler) AfterStart() {
	generation := instance.activeGeneration()
	generation.scheduler.StartAsync()
}

func (instance *Scheduler) BeforeStop() {
	instance.stopGeneration()
}

func (instance *Scheduler) Dispose() {
	instance.stopGeneration()
}

func (instance *Scheduler) stopGeneration() {
	instance.mu.Lock()
	generation := instance.generation
	instance.mu.Unlock()
	if generation == nil {
		return
	}

	generation.stopOnce.Do(func() {
		generation.cancel()
		generation.scheduler.Stop()
	})

	instance.mu.Lock()
	if instance.generation == generation {
		instance.generation = nil
	}
	instance.mu.Unlock()
}

func (instance *Scheduler) ScheduleTaskCron(cron string, key string, task func()) (*gocron.Job, error) {
	instance.l.Debug("schedule task", key, "by cron", cron)
	generation, err := instance.generationForOperation()
	if err != nil {
		return nil, err
	}
	return generation.scheduler.CronWithSeconds(cron).Tag(key).Name(key).Do(task)
}

// ScheduleTaskCronContext schedules a task with the current scheduler-generation context.
// Shutdown cancels that context before waiting for running jobs to return.
func (instance *Scheduler) ScheduleTaskCronContext(cron string, key string, task func(context.Context)) (*gocron.Job, error) {
	if task == nil {
		return nil, errors.New("scheduled context task is nil")
	}
	instance.l.Debug("schedule context task", key, "by cron", cron)
	generation, err := instance.generationForOperation()
	if err != nil {
		return nil, err
	}
	return generation.scheduler.CronWithSeconds(cron).Tag(key).Name(key).Do(func() {
		task(generation.context)
	})
}

func (instance *Scheduler) RunScheduledTaskImmediate(key string) error {
	instance.l.Debug("run task", key)
	generation, err := instance.generationForOperation()
	if err != nil {
		return err
	}
	return generation.scheduler.RunByTag(key)
}

func (instance *Scheduler) generationForOperation() (*schedulerGeneration, error) {
	instance.mu.Lock()
	defer instance.mu.Unlock()
	if instance.generation == nil {
		return nil, errors.New("scheduler is not active")
	}
	return instance.generation, nil
}

func (instance *Scheduler) activeGeneration() *schedulerGeneration {
	generation, err := instance.generationForOperation()
	if err != nil {
		panic(err)
	}
	return generation
}
