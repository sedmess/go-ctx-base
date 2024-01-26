package scheduler

import (
	"github.com/go-co-op/gocron"
	"github.com/sedmess/go-ctx/logger"
	"github.com/sedmess/go-ctx/u"
	"time"
)

type Scheduler struct {
	l        logger.Logger `logger:""`
	location string        `env:"TZ" envDef:"UTC"`
	locker   *Locker       `inject:""`

	s *gocron.Scheduler
}

func (instance *Scheduler) Init() {
	instance.s = gocron.NewScheduler(u.Must2(time.LoadLocation(instance.location)))
	instance.s.WithDistributedLocker(instance.locker)
}

func (instance *Scheduler) AfterStart() {
	instance.s.StartAsync()
}

func (instance *Scheduler) BeforeStop() {
	instance.s.Stop()
}

func (instance *Scheduler) ScheduleTaskCron(cron string, key string, task func()) (*gocron.Job, error) {
	instance.l.Debug("schedule task", key, "by cron", cron)
	return instance.s.CronWithSeconds(cron).Tag(key).Name(key).Do(task)
}

func (instance *Scheduler) RunScheduledTaskImmediate(key string) error {
	instance.l.Debug("run task", key)
	return instance.s.RunByTag(key)
}
