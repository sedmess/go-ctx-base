package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/ctx/health"
	"github.com/sedmess/go-ctx/ctx/logger"
	"github.com/sedmess/go-ctx/u"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	glogger "gorm.io/gorm/logger"
)

const dbSqlitePathKey = "DB_SQLITE_PATH"
const dbDsnKey = "DB_DSN"
const dbHostKey = "DB_HOST"
const dbPortKey = "DB_PORT"
const dbUserKey = "DB_USERNAME"
const dbPasswordKey = "DB_PASSWORD"
const dbNameKey = "DB_NAME"
const dbTimeZoneKey = "DB_TIMEZONE"
const globalTimeZoneKey = "TZ"
const dbSSLModeKey = "DB_SSLMODE"
const dbMaxIdleConnsKey = "DB_MAX_IDLE_CONNS"
const dbMaxOpenConnsKey = "DB_MAX_OPEN_CONNS"
const dbConnMaxLifetimeKey = "DB_CONN_MAX_LIFETIME"

const dbDsnPattern = "host=%s port=%d user=%s password=%s dbname=%s sslmode=%s"
const dbDsnTimeZonePatternAddition = " TimeZone=%s"

const dbCheckQuery = "select null"

func NewConnection(name string, configPrefix string, isDefault bool, isCritical bool) Connection {
	var getEnvFn func(string) *ctx.EnvValue
	if isDefault {
		getEnvFn = func(key string) *ctx.EnvValue {
			return ctx.GetEnvCustomOrDefault(strings.ToUpper(configPrefix), key)
		}
	} else {
		getEnvFn = func(key string) *ctx.EnvValue {
			return ctx.GetEnvCustom(strings.ToUpper(configPrefix), key)
		}
	}
	return &connection{name: name, getEnvFn: getEnvFn, isCritical: isCritical}
}

type Connection interface {
	Init() error
	AutoMigrate(models ...any)
	Session(session func(session *Session) error) error
	SessionContext(context context.Context, session func(session *Session) error) error
	Stats() (sql.DBStats, error)
	Check() error
	Health() health.ServiceHealth
}

type connection struct {
	name       string
	getEnvFn   func(name string) *ctx.EnvValue
	isCritical bool

	rootContext context.Context `ctx:"context"`
	logger      logger.Logger

	generation atomic.Pointer[connectionGeneration]
	registerer prometheus.Registerer
}

type connectionGeneration struct {
	db         *gorm.DB
	sqlDB      *sql.DB
	context    context.Context
	cancel     context.CancelFunc
	collector  prometheus.Collector
	registerer prometheus.Registerer
	closeOnce  sync.Once
	closeErr   error
	closed     chan struct{}
}

func (instance *connection) Init() error {
	if generation := instance.generation.Load(); generation != nil && !generation.isClosed() {
		return fmt.Errorf("database %q is already initialized", instance.name)
	}

	instance.logger = logger.New(instance.name)

	var dbProvider gorm.Dialector

	sqlitePath := instance.getEnvFn(dbSqlitePathKey)
	dbDsn := instance.getEnvFn(dbDsnKey)
	dbHost := instance.getEnvFn(dbHostKey)
	dbPort := instance.getEnvFn(dbPortKey)
	dbUser := instance.getEnvFn(dbUserKey)
	dbName := instance.getEnvFn(dbNameKey)
	dbPassword := instance.getEnvFn(dbPasswordKey)
	dbSSLMode := instance.getEnvFn(dbSSLModeKey)
	dbTimeZone := instance.getEnvFn(dbTimeZoneKey)
	globalTimeZone := ctx.GetEnv(globalTimeZoneKey)
	if dbDsn.IsPresent() {
		instance.logger.Info("use Postgres DB")
		dbProvider = postgres.Open(dbDsn.AsString())
	} else if instance.presentAll(dbHost, dbPort, dbUser, dbPassword, dbName) {
		dsn := fmt.Sprintf(dbDsnPattern, dbHost.AsString(), dbPort.AsInt(), dbUser.AsString(), dbPassword.AsString(), dbName.AsString(), dbSSLMode.AsStringDefault("disable"))
		if dbTimeZone.IsPresent() {
			dsn += fmt.Sprintf(dbDsnTimeZonePatternAddition, dbTimeZone.AsString())
		} else if globalTimeZone.IsPresent() {
			dsn += fmt.Sprintf(dbDsnTimeZonePatternAddition, globalTimeZone.AsString())
		}
		instance.logger.Info("use Postgres DB")
		dbProvider = postgres.Open(dsn)
	} else if sqlitePath.IsPresent() {
		instance.logger.Info("use SQLite DB")
		dbProvider = sqlite.Open(sqlitePath.AsString())
	} else {
		return errors.New("undefined DB connection")
	}

	gormDB, err := gorm.Open(
		dbProvider,
		&gorm.Config{
			PrepareStmt:    true,
			TranslateError: true,
			Logger: glogger.New(&logAdapter{loggingFn: func(msg string) {
				instance.logger.Debug(msg)
			}}, glogger.Config{
				SlowThreshold:             0,
				Colorful:                  false,
				IgnoreRecordNotFoundError: true,
				ParameterizedQueries:      true,
				LogLevel:                  glogger.Error,
			}),
		},
	)
	if err != nil {
		return fmt.Errorf("database %q initialization failed", instance.name)
	}
	sqlDb, err := gormDB.DB()
	if err != nil {
		return fmt.Errorf("database %q pool initialization failed", instance.name)
	}
	dbMaxIdleConns := instance.getEnvFn(dbMaxIdleConnsKey)
	if dbMaxIdleConns.IsPresent() {
		sqlDb.SetMaxIdleConns(dbMaxIdleConns.AsInt())
	}
	dbMaxOpenConns := instance.getEnvFn(dbMaxOpenConnsKey)
	if dbMaxOpenConns.IsPresent() {
		sqlDb.SetMaxOpenConns(dbMaxOpenConns.AsInt())
	}
	dbConnMaxLifetime := instance.getEnvFn(dbConnMaxLifetimeKey)
	if dbConnMaxLifetime.IsPresent() {
		sqlDb.SetConnMaxLifetime(dbConnMaxLifetime.AsDuration())
	}

	parentContext := instance.rootContext
	if parentContext == nil {
		parentContext = context.Background()
	}
	generationContext, cancelGeneration := context.WithCancel(parentContext)
	registerer := instance.registerer
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	collector := newDBStatsCollector(sqlDb, instance.name)
	if err := registerer.Register(collector); err != nil {
		cancelGeneration()
		_ = sqlDb.Close()
		return fmt.Errorf("database %q metrics registration failed", instance.name)
	}

	generation := &connectionGeneration{
		db:         gormDB,
		sqlDB:      sqlDb,
		context:    generationContext,
		cancel:     cancelGeneration,
		collector:  collector,
		registerer: registerer,
		closed:     make(chan struct{}),
	}

	// go-ctx serializes Init and starts a new application only after the previous
	// Stop().Join(). The completed generation remains attached until this point,
	// and atomic publication keeps operational readers safe at the restart boundary.
	instance.generation.Store(generation)

	return nil
}

func (instance *connection) AutoMigrate(models ...any) {
	u.Must(instance.Session(func(session *Session) error {
		return session.Tx(func(session *Session) error {
			return session.AutoMigrate(models...)
		})
	}))
}

func (instance *connection) Session(dbFunc func(session *Session) error) error {
	generation, err := instance.activeGeneration()
	if err != nil {
		return err
	}
	return instance.sessionContext(generation.context, generation, dbFunc)
}

func (instance *connection) SessionContext(baseContext context.Context, dbFunc func(session *Session) error) error {
	if baseContext == nil {
		return errors.New("database session context is nil")
	}
	generation, err := instance.activeGeneration()
	if err != nil {
		return err
	}
	return instance.sessionContext(baseContext, generation, dbFunc)
}

func (instance *connection) sessionContext(baseContext context.Context, generation *connectionGeneration, dbFunc func(session *Session) error) error {
	operationContext, cancelOperation := context.WithCancel(baseContext)
	stopGenerationCancellation := context.AfterFunc(generation.context, cancelOperation)
	defer func() {
		stopGenerationCancellation()
		cancelOperation()
	}()

	return generation.db.WithContext(operationContext).Connection(func(db *gorm.DB) error {
		return dbFunc(newSession(operationContext, db))
	})
}

func (instance *connection) Name() string {
	return instance.name
}

func (instance *connection) Stats() (sql.DBStats, error) {
	generation, err := instance.activeGeneration()
	if err != nil {
		return sql.DBStats{}, err
	}
	return generation.sqlDB.Stats(), nil
}

func (instance *connection) Check() error {
	timeoutContext, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFn()
	err := instance.SessionContext(timeoutContext, func(session *Session) error {
		return session.Exec(dbCheckQuery).Error
	})
	if err != nil {
		return errors.New("database health check failed")
	}
	return nil
}

func (instance *connection) Health() health.ServiceHealth {
	status := health.ServiceHealth{}
	status.Details = make(map[string]any)
	err := instance.Check()
	if err == nil {
		stats, err := instance.Stats()
		if err == nil {
			status.Details["stats"] = stats
		}
		status.Status = health.Up
		return status
	}
	status.Details["error"] = "database health check failed"
	if instance.isCritical {
		status.Status = health.DownCritical
	} else {
		status.Status = health.Down
	}
	return status
}

func (instance *connection) BeforeStop() {
	if err := instance.closeConnection(); err != nil && instance.logger != nil {
		instance.logger.Error("database close failed")
	}
}

func (instance *connection) Dispose() error {
	return instance.closeConnection()
}

func (instance *connection) closeConnection() error {
	generation := instance.generation.Load()
	if generation == nil {
		return nil
	}
	return generation.close()
}

func (generation *connectionGeneration) close() error {
	generation.closeOnce.Do(func() {
		defer close(generation.closed)
		generation.cancel()
		if generation.collector != nil {
			generation.registerer.Unregister(generation.collector)
		}
		generation.closeErr = generation.sqlDB.Close()
	})
	return generation.closeErr
}

func (generation *connectionGeneration) isClosed() bool {
	select {
	case <-generation.closed:
		return true
	default:
		return false
	}
}

func (instance *connection) activeGeneration() (*connectionGeneration, error) {
	generation := instance.generation.Load()
	if generation == nil || generation.isClosed() {
		return nil, fmt.Errorf("database %q is not active", instance.name)
	}
	if err := generation.context.Err(); err != nil {
		return nil, fmt.Errorf("database %q is not active: %w", instance.name, err)
	}
	return generation, nil
}

// CloseConnection closes a manually owned built-in connection. Container-managed connections
// invoke the same idempotent operation automatically during shutdown and disposal. Close calls
// may be repeated or concurrent. Manual Init and close phases remain serialized; operational
// calls may overlap a completed-generation restart and observe either inactive or fresh state.
func CloseConnection(connection Connection) error {
	if connection == nil {
		return errors.New("database connection is nil")
	}
	value := reflect.ValueOf(connection)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return errors.New("database connection is nil")
	}
	closeable, ok := connection.(interface{ closeConnection() error })
	if !ok {
		return errors.New("database connection does not support close")
	}
	return closeable.closeConnection()
}

func (instance *connection) presentAll(values ...*ctx.EnvValue) bool {
	for _, value := range values {
		if !value.IsPresent() {
			return false
		}
	}
	return true
}

type logAdapter struct {
	loggingFn func(msg string)
}

func (a *logAdapter) Printf(format string, v ...interface{}) {
	a.loggingFn(fmt.Sprintf(format, v...))
}
