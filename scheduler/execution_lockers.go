package scheduler

import (
	"context"
	"errors"
	"github.com/go-co-op/gocron"
	"github.com/sedmess/go-ctx-base/db"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/ctx/health"
	"github.com/sedmess/go-ctx/logger"
	"strings"
	"sync"
)

const schedulerLockProviderKey = "SCHEDULER_LOCK_PROVIDER"

const (
	providerPostgres = "POSTGRES"
	providerLocal    = "LOCAL"
)

var alreadyLockedErr = errors.New("resource has already locked")

type Locker struct {
	l logger.Logger `logger:""`

	db db.Connection
	mu sync.Mutex
	lm map[string]any
}

func (instance *Locker) Init() {
	provider := strings.ToUpper(ctx.GetEnv(schedulerLockProviderKey).AsStringDefault(providerLocal))
	switch provider {
	case providerLocal:
		instance.mu = sync.Mutex{}
		instance.lm = make(map[string]any)
	case providerPostgres:
		instance.db = db.NewConnection("scheduler", "SCHEDULER", false, true)
		instance.db.Init()
	default:
		instance.l.Fatal("unknown", schedulerLockProviderKey, ":", provider)
	}
	instance.l.Info("scheduler works on", provider, "locker")
}

func (instance *Locker) Health() health.ServiceHealth {
	if instance.db == nil {
		return health.Status(health.Up)
	} else {
		return instance.db.Health()
	}
}

func (instance *Locker) Lock(context context.Context, key string) (gocron.Lock, error) {
	if instance.db != nil {
		acquireErrCh := make(chan error)
		releaseCh := make(chan bool)

		go func() {
			err := instance.db.SessionContext(context, func(session *db.Session) error {
				tx := session.Begin()
				defer tx.Commit()
				var acquired bool
				result := tx.Raw("select pg_try_advisory_xact_lock(('x'||md5(?))::bit(64)::bigint) as success;", key).Find(&acquired)
				if result.Error != nil {
					acquireErrCh <- result.Error
					return result.Error
				}
				if acquired {
					acquireErrCh <- nil
					<-releaseCh
					return nil
				} else {
					acquireErrCh <- alreadyLockedErr
					return nil
				}
			})
			if err != nil {
				instance.l.Error("on lock tx:", err)
			}
		}()

		acquireErr := <-acquireErrCh
		if acquireErr == nil {
			return &txLock{ch: releaseCh}, nil
		} else {
			return nil, acquireErr
		}
	} else {
		instance.mu.Lock()
		defer instance.mu.Unlock()

		_, found := instance.lm[key]
		if found {
			return nil, alreadyLockedErr
		}
		instance.lm[key] = true
		return &localLock{locker: instance, key: key}, nil
	}
}

type localLock struct {
	locker *Locker
	key    string
}

func (l *localLock) Unlock(context.Context) error {
	l.locker.mu.Lock()
	defer l.locker.mu.Unlock()

	delete(l.locker.lm, l.key)

	return nil
}

type txLock struct {
	ch chan bool
}

func (l *txLock) Unlock(context.Context) error {
	l.ch <- true
	return nil
}
