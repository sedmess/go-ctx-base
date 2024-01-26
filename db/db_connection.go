package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/glebarez/sqlite"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/ctx/health"
	"github.com/sedmess/go-ctx/logger"
	"github.com/sedmess/go-ctx/u"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	glogger "gorm.io/gorm/logger"
	"strings"
	"time"
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
	Init()
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

	logger logger.Logger

	db *gorm.DB
}

func (instance *connection) Init() {
	instance.logger = logger.New(instance)

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
		instance.logger.Fatal("undefined DB connection")
	}

	instance.db = u.Must2(
		gorm.Open(
			dbProvider,
			&gorm.Config{
				PrepareStmt:    true,
				TranslateError: true,
				Logger: glogger.New(logger.GetLogger(logger.DEBUG), glogger.Config{
					SlowThreshold:             0,
					Colorful:                  false,
					IgnoreRecordNotFoundError: true,
					ParameterizedQueries:      true,
					LogLevel:                  glogger.Error,
				}),
			},
		),
	)
	sqlDb := u.Must2(instance.db.DB())
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
}

func (instance *connection) AutoMigrate(models ...any) {
	u.Must(instance.Session(func(session *Session) error {
		return session.Tx(func(session *Session) error {
			return session.AutoMigrate(models...)
		})
	}))
}

func (instance *connection) Session(dbFunc func(session *Session) error) error {
	return instance.db.Connection(func(db *gorm.DB) error {
		return dbFunc(newSession(context.Background(), db))
	})
}

func (instance *connection) SessionContext(baseContext context.Context, dbFunc func(session *Session) error) error {
	return instance.db.Connection(func(db *gorm.DB) error {
		return dbFunc(newSession(baseContext, db))
	})
}

func (instance *connection) Name() string {
	return instance.name
}

func (instance *connection) Stats() (sql.DBStats, error) {
	sqlDb, err := instance.db.DB()
	if err != nil {
		instance.logger.Error("error on gathering DBStats:", err)
		return sql.DBStats{}, errors.New("can't get sql.DB instance")
	}
	dbStats := sqlDb.Stats()
	return dbStats, nil
}

func (instance *connection) Check() error {
	timeoutContext, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFn()
	_, err := instance.db.ConnPool.QueryContext(timeoutContext, dbCheckQuery)
	return err
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
	status.Details["error"] = err.Error()
	if instance.isCritical {
		status.Status = health.DownCritical
	} else {
		status.Status = health.Down
	}
	return status
}

func (instance *connection) presentAll(values ...*ctx.EnvValue) bool {
	for _, value := range values {
		if !value.IsPresent() {
			return false
		}
	}
	return true
}
