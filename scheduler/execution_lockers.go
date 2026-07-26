package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/go-co-op/gocron"
	"github.com/sedmess/go-ctx-base/db"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/ctx/health"
	"github.com/sedmess/go-ctx/ctx/logger"
)

const schedulerLockProviderKey = "SCHEDULER_LOCK_PROVIDER"

const (
	providerPostgres = "POSTGRES"
	providerLocal    = "LOCAL"
)

const postgresAdvisoryLockQuery = "select pg_try_advisory_xact_lock(('x'||md5(?))::bit(64)::bigint) as success;"

var alreadyLockedErr = errors.New("resource has already locked")

var createLockerConnection = db.NewConnection
var closeLockerConnection = db.CloseConnection

type postgresLeaseTransactionRunner func(
	operationContext context.Context,
	connection db.Connection,
	key string,
	reportAcquisition func(error),
	waitForRelease func() error,
) error

// executePostgresLeaseTransaction is an internal boundary around the concrete GORM
// transaction. Tests replace it to exercise every lease state deterministically without
// requiring a live PostgreSQL server; the integration-tagged suite still covers the provider.
var executePostgresLeaseTransaction postgresLeaseTransactionRunner = func(
	operationContext context.Context,
	connection db.Connection,
	key string,
	reportAcquisition func(error),
	waitForRelease func() error,
) error {
	return connection.SessionContext(operationContext, func(session *db.Session) error {
		return session.Tx(func(transaction *db.Session) error {
			var locked bool
			result := transaction.Raw(postgresAdvisoryLockQuery, key).Find(&locked)
			if result.Error != nil {
				reportAcquisition(result.Error)
				return result.Error
			}
			if !locked {
				reportAcquisition(alreadyLockedErr)
				return alreadyLockedErr
			}

			reportAcquisition(nil)
			return waitForRelease()
		})
	})
}

type Locker struct {
	l              logger.Logger   `ctx:""`
	rootContext    context.Context `ctx:"context"`
	connectionName string

	mu         sync.Mutex
	generation *lockerGeneration
	lastClose  error
}

type lockerGeneration struct {
	provider string
	context  context.Context
	cancel   context.CancelFunc
	db       db.Connection
	local    map[string]*localLock
	leases   sync.WaitGroup
	stopping bool
	stopOnce sync.Once
	stopErr  error
	stopped  chan struct{}
}

type lockLease struct {
	releaseOnce sync.Once
	release     chan struct{}
	done        chan struct{}
	terminal    error
}

func newLockLease() *lockLease {
	return &lockLease{release: make(chan struct{}), done: make(chan struct{})}
}

func (lease *lockLease) signalRelease() {
	lease.releaseOnce.Do(func() {
		close(lease.release)
	})
}

func (lease *lockLease) finish(err error) {
	lease.terminal = err
	// Closing done publishes terminal to every Unlock caller waiting on the channel.
	close(lease.done)
}

func (lease *lockLease) result() error {
	return lease.terminal
}

func (instance *Locker) Init() error {
	if instance.l == nil {
		instance.l = logger.New("scheduler.Locker")
	}
	instance.mu.Lock()
	if instance.generation != nil {
		instance.mu.Unlock()
		return errors.New("scheduler locker is already initialized")
	}
	instance.mu.Unlock()

	provider := strings.ToUpper(ctx.GetEnv(schedulerLockProviderKey).AsStringDefault(providerLocal))
	parentContext := instance.rootContext
	if parentContext == nil {
		parentContext = context.Background()
	}
	generationContext, cancelGeneration := context.WithCancel(parentContext)
	generation := &lockerGeneration{
		provider: provider,
		context:  generationContext,
		cancel:   cancelGeneration,
		local:    make(map[string]*localLock),
		stopped:  make(chan struct{}),
	}

	switch provider {
	case providerLocal:
	case providerPostgres:
		connectionName := instance.connectionName
		if connectionName == "" {
			connectionName = "scheduler"
		}
		generation.db = createLockerConnection(connectionName, "SCHEDULER", false, true)
		if err := generation.db.Init(); err != nil {
			cancelGeneration()
			return err
		}
	default:
		cancelGeneration()
		return errors.New("unknown " + schedulerLockProviderKey + ": " + provider)
	}

	instance.mu.Lock()
	if instance.generation != nil {
		instance.mu.Unlock()
		cancelGeneration()
		if generation.db != nil {
			_ = closeLockerConnection(generation.db)
		}
		return errors.New("scheduler locker is already initialized")
	}
	instance.generation = generation
	instance.lastClose = nil
	instance.mu.Unlock()

	instance.l.Info("scheduler works on", provider, "locker")
	return nil
}

func (instance *Locker) Health() health.ServiceHealth {
	instance.mu.Lock()
	generation := instance.generation
	instance.mu.Unlock()
	if generation == nil {
		return health.Status(health.Down)
	}
	if generation.db == nil {
		return health.Status(health.Up)
	}
	return generation.db.Health()
}

func (instance *Locker) Lock(lockContext context.Context, key string) (gocron.Lock, error) {
	if lockContext == nil {
		return nil, errors.New("lock context is nil")
	}
	if err := lockContext.Err(); err != nil {
		return nil, err
	}

	instance.mu.Lock()
	generation := instance.generation
	if generation == nil || generation.stopping {
		instance.mu.Unlock()
		return nil, errors.New("scheduler locker is not active")
	}
	if generation.provider == providerLocal {
		if _, found := generation.local[key]; found {
			instance.mu.Unlock()
			return nil, alreadyLockedErr
		}
		lock := &localLock{locker: instance, generation: generation, key: key, lease: newLockLease()}
		generation.local[key] = lock
		generation.leases.Add(1)
		instance.mu.Unlock()
		go lock.watch(lockContext)
		return lock, nil
	}

	lease := newLockLease()
	generation.leases.Add(1)
	instance.mu.Unlock()
	return instance.acquirePostgresLock(lockContext, key, generation, lease)
}

func (instance *Locker) acquirePostgresLock(lockContext context.Context, key string, generation *lockerGeneration, lease *lockLease) (gocron.Lock, error) {
	acquired := make(chan error, 1)
	go func() {
		defer generation.leases.Done()
		operationContext, cancelOperation := context.WithCancel(lockContext)
		stopGenerationCancellation := context.AfterFunc(generation.context, cancelOperation)
		defer func() {
			stopGenerationCancellation()
			cancelOperation()
		}()

		var acquisitionOnce sync.Once
		reportAcquisition := func(err error) {
			acquisitionOnce.Do(func() {
				acquired <- err
			})
		}

		err := executePostgresLeaseTransaction(operationContext, generation.db, key, reportAcquisition, func() error {
			if err := operationContext.Err(); err != nil {
				return err
			}
			select {
			case <-lease.release:
				return operationContext.Err()
			case <-operationContext.Done():
				return operationContext.Err()
			}
		})
		reportAcquisition(err)
		lease.finish(err)
	}()

	if err := <-acquired; err != nil {
		return nil, err
	}
	return &txLock{lease: lease}, nil
}

func (instance *Locker) BeforeStop() {
	if err := instance.closeGeneration(); err != nil && instance.l != nil {
		instance.l.Error("scheduler locker close failed")
	}
}

func (instance *Locker) Dispose() error {
	return instance.closeGeneration()
}

func (instance *Locker) closeGeneration() error {
	instance.mu.Lock()
	generation := instance.generation
	if generation == nil {
		err := instance.lastClose
		instance.mu.Unlock()
		return err
	}
	if generation.stopping {
		stopped := generation.stopped
		instance.mu.Unlock()
		<-stopped
		return generation.stopErr
	}
	generation.stopping = true
	instance.mu.Unlock()

	generation.stopOnce.Do(func() {
		defer close(generation.stopped)
		generation.cancel()
		generation.leases.Wait()
		if generation.db != nil {
			generation.stopErr = closeLockerConnection(generation.db)
		}
	})

	instance.mu.Lock()
	if instance.generation == generation {
		instance.generation = nil
		instance.lastClose = generation.stopErr
	}
	instance.mu.Unlock()
	return generation.stopErr
}

type localLock struct {
	locker     *Locker
	generation *lockerGeneration
	key        string
	lease      *lockLease
}

func (lock *localLock) watch(lockContext context.Context) {
	defer lock.generation.leases.Done()
	var terminal error
	select {
	case <-lock.lease.release:
	case <-lockContext.Done():
		terminal = lockContext.Err()
	case <-lock.generation.context.Done():
		terminal = lock.generation.context.Err()
	}

	lock.locker.mu.Lock()
	if current := lock.generation.local[lock.key]; current == lock {
		delete(lock.generation.local, lock.key)
	}
	lock.locker.mu.Unlock()
	lock.lease.finish(terminal)
}

func (lock *localLock) Unlock(unlockContext context.Context) error {
	lock.lease.signalRelease()
	<-lock.lease.done
	if unlockContext != nil {
		if err := unlockContext.Err(); err != nil {
			return err
		}
	}
	return lock.lease.result()
}

type txLock struct {
	lease *lockLease
}

func (lock *txLock) Unlock(unlockContext context.Context) error {
	lock.lease.signalRelease()
	<-lock.lease.done
	if unlockContext != nil {
		if err := unlockContext.Err(); err != nil {
			return err
		}
	}
	if err := lock.lease.result(); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("release postgres scheduler lock: %w", err)
	}
	return nil
}
