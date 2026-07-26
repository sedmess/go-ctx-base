package httpserver

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sedmess/go-ctx-base/db"
	"github.com/sedmess/go-ctx/ctx"
	"gorm.io/gorm"
)

const sqliteBenchmarkRecordID int64 = 1

type sqliteBenchmarkWorkload uint8

const (
	sqliteBenchmarkPointRead sqliteBenchmarkWorkload = iota
	sqliteBenchmarkTransactionalUpdate
)

func (workload sqliteBenchmarkWorkload) String() string {
	switch workload {
	case sqliteBenchmarkPointRead:
		return "point-read"
	case sqliteBenchmarkTransactionalUpdate:
		return "serialized-transactional-update"
	default:
		return "unknown"
	}
}

type sqliteBenchmarkRecord struct {
	ID    int64 `gorm:"primaryKey;autoIncrement:false"`
	Value int64 `gorm:"not null"`
}

func (sqliteBenchmarkRecord) TableName() string {
	return "httpserver_sqlite_benchmark_records"
}

type sqliteBenchmarkMetadata struct {
	version     string
	journalMode string
	lockingMode string
	synchronous int
	foreignKeys int
	busyTimeout int
	tempStore   int
	pageSize    int
	cacheSize   int
}

type sqliteBenchmarkExecutor interface {
	Execute(context.Context) error
}

type sqliteBenchmarkService struct {
	name           string
	connectionName string
	workload       sqliteBenchmarkWorkload

	connection db.Connection
}

func (service *sqliteBenchmarkService) Name() string {
	return service.name
}

func (service *sqliteBenchmarkService) Init(provider ctx.ServiceProvider) error {
	connection, ok := provider.ByName(service.connectionName).(db.Connection)
	if !ok {
		return fmt.Errorf("benchmark database %q is unavailable", service.connectionName)
	}
	service.connection = connection

	return connection.Session(func(session *db.Session) error {
		if err := session.AutoMigrate(&sqliteBenchmarkRecord{}); err != nil {
			return fmt.Errorf("migrate SQLite benchmark schema: %w", err)
		}
		record := sqliteBenchmarkRecord{ID: sqliteBenchmarkRecordID}
		if err := session.Create(&record).Error; err != nil {
			return fmt.Errorf("seed SQLite benchmark row: %w", err)
		}
		return nil
	})
}

func (service *sqliteBenchmarkService) Execute(requestContext context.Context) error {
	switch service.workload {
	case sqliteBenchmarkPointRead:
		return service.pointRead(requestContext)
	case sqliteBenchmarkTransactionalUpdate:
		return service.transactionalUpdate(requestContext)
	default:
		return fmt.Errorf("unsupported SQLite benchmark workload %d", service.workload)
	}
}

func (service *sqliteBenchmarkService) pointRead(requestContext context.Context) error {
	return service.connection.SessionContext(requestContext, func(session *db.Session) error {
		return readSQLiteBenchmarkRecord(session)
	})
}

func readSQLiteBenchmarkRecord(session *db.Session) error {
	var record sqliteBenchmarkRecord
	result := session.
		Select("id", "value").
		Take(&record, "id = ?", sqliteBenchmarkRecordID)
	if result.Error != nil {
		return result.Error
	}
	if record.ID != sqliteBenchmarkRecordID || record.Value != 0 {
		return fmt.Errorf("unexpected SQLite benchmark row: %+v", record)
	}
	return nil
}

func (service *sqliteBenchmarkService) transactionalUpdate(requestContext context.Context) error {
	return service.connection.SessionContext(requestContext, func(session *db.Session) error {
		return session.Tx(func(transaction *db.Session) error {
			result := transaction.
				Model(&sqliteBenchmarkRecord{}).
				Where("id = ?", sqliteBenchmarkRecordID).
				UpdateColumn("value", gorm.Expr("value + 1"))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("SQLite benchmark update affected %d rows", result.RowsAffected)
			}
			return nil
		})
	})
}

func (service *sqliteBenchmarkService) prepare(poolSize int) error {
	switch service.workload {
	case sqliteBenchmarkPointRead:
		if err := service.warmReadPool(poolSize); err != nil {
			return err
		}
	case sqliteBenchmarkTransactionalUpdate:
		metadata, err := service.readMetadata()
		if err != nil {
			return err
		}
		if err := validateSQLiteBenchmarkMetadata(metadata); err != nil {
			return err
		}
		if err := service.transactionalUpdate(context.Background()); err != nil {
			return fmt.Errorf("warm SQLite benchmark update: %w", err)
		}
		if err := service.resetValue(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported SQLite benchmark workload %d", service.workload)
	}
	return service.validatePoolReady(poolSize)
}

func (service *sqliteBenchmarkService) warmReadPool(poolSize int) error {
	warmContext, cancelWarm := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelWarm()

	ready := make(chan struct{}, poolSize)
	release := make(chan struct{})
	results := make(chan error, poolSize)
	for range poolSize {
		go func() {
			results <- service.connection.SessionContext(warmContext, func(session *db.Session) error {
				metadata, err := readSQLiteBenchmarkMetadata(session)
				if err != nil {
					return err
				}
				if err := validateSQLiteBenchmarkMetadata(metadata); err != nil {
					return err
				}
				if err := readSQLiteBenchmarkRecord(session); err != nil {
					return err
				}
				ready <- struct{}{}
				select {
				case <-release:
					return nil
				case <-warmContext.Done():
					return warmContext.Err()
				}
			})
		}()
	}

	acquired := 0
	completed := 0
	var firstError error
	for acquired < poolSize && firstError == nil {
		select {
		case <-ready:
			acquired++
		case err := <-results:
			completed++
			if err == nil {
				firstError = fmt.Errorf("SQLite warm-up session exited before the barrier")
			} else {
				firstError = err
			}
		case <-warmContext.Done():
			firstError = warmContext.Err()
		}
	}
	close(release)
	for completed < poolSize {
		if err := <-results; firstError == nil && err != nil {
			firstError = err
		}
		completed++
	}
	if firstError != nil {
		return fmt.Errorf("warm SQLite read pool: %w", firstError)
	}
	return nil
}

func (service *sqliteBenchmarkService) validatePoolReady(poolSize int) error {
	stats, err := service.connection.Stats()
	if err != nil {
		return fmt.Errorf("read SQLite benchmark pool stats: %w", err)
	}
	if stats.MaxOpenConnections != poolSize ||
		stats.OpenConnections != poolSize ||
		stats.Idle != poolSize ||
		stats.InUse != 0 {
		return fmt.Errorf(
			"SQLite benchmark pool is not ready: max-open=%d open=%d idle=%d in-use=%d, want %d/%d/%d/0",
			stats.MaxOpenConnections,
			stats.OpenConnections,
			stats.Idle,
			stats.InUse,
			poolSize,
			poolSize,
			poolSize,
		)
	}
	return nil
}

func (service *sqliteBenchmarkService) resetValue() error {
	return service.connection.Session(func(session *db.Session) error {
		result := session.
			Model(&sqliteBenchmarkRecord{}).
			Where("id = ?", sqliteBenchmarkRecordID).
			UpdateColumn("value", 0)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("SQLite benchmark reset affected %d rows", result.RowsAffected)
		}
		return nil
	})
}

func (service *sqliteBenchmarkService) finalState() (int64, int64, error) {
	var value int64
	var count int64
	err := service.connection.Session(func(session *db.Session) error {
		var record sqliteBenchmarkRecord
		if err := session.
			Select("id", "value").
			Take(&record, "id = ?", sqliteBenchmarkRecordID).
			Error; err != nil {
			return err
		}
		value = record.Value
		return session.Model(&sqliteBenchmarkRecord{}).Count(&count).Error
	})
	return value, count, err
}

func (service *sqliteBenchmarkService) readMetadata() (sqliteBenchmarkMetadata, error) {
	metadata := sqliteBenchmarkMetadata{}
	err := service.connection.Session(func(session *db.Session) error {
		var err error
		metadata, err = readSQLiteBenchmarkMetadata(session)
		return err
	})
	return metadata, err
}

func readSQLiteBenchmarkMetadata(session *db.Session) (sqliteBenchmarkMetadata, error) {
	metadata := sqliteBenchmarkMetadata{}
	queries := []struct {
		sql         string
		destination any
	}{
		{sql: "SELECT sqlite_version()", destination: &metadata.version},
		{sql: "PRAGMA journal_mode", destination: &metadata.journalMode},
		{sql: "PRAGMA locking_mode", destination: &metadata.lockingMode},
		{sql: "PRAGMA synchronous", destination: &metadata.synchronous},
		{sql: "PRAGMA foreign_keys", destination: &metadata.foreignKeys},
		{sql: "PRAGMA busy_timeout", destination: &metadata.busyTimeout},
		{sql: "PRAGMA temp_store", destination: &metadata.tempStore},
		{sql: "PRAGMA page_size", destination: &metadata.pageSize},
		{sql: "PRAGMA cache_size", destination: &metadata.cacheSize},
	}
	for _, query := range queries {
		if err := session.Raw(query.sql).Scan(query.destination).Error; err != nil {
			return sqliteBenchmarkMetadata{}, fmt.Errorf("%s: %w", query.sql, err)
		}
	}
	return metadata, nil
}

func validateSQLiteBenchmarkMetadata(metadata sqliteBenchmarkMetadata) error {
	if metadata.version == "" {
		return fmt.Errorf("SQLite benchmark version is empty")
	}
	if metadata.journalMode != "memory" ||
		metadata.lockingMode != "normal" ||
		metadata.synchronous != 2 ||
		metadata.foreignKeys != 1 ||
		metadata.busyTimeout != 5000 ||
		metadata.tempStore != 2 ||
		metadata.pageSize != 4096 ||
		metadata.cacheSize != -2000 {
		return fmt.Errorf("unexpected SQLite benchmark configuration: %+v", metadata)
	}
	return nil
}

type sqliteBenchmarkController struct {
	name        string
	serverName  string
	serviceName string
	method      string

	service sqliteBenchmarkExecutor
}

func (controller *sqliteBenchmarkController) Name() string {
	return controller.name
}

func (controller *sqliteBenchmarkController) Init(provider ctx.ServiceProvider) error {
	server, ok := provider.ByName(controller.serverName).(RestServer)
	if !ok {
		return fmt.Errorf("benchmark server %q is unavailable", controller.serverName)
	}
	service, ok := provider.ByName(controller.serviceName).(sqliteBenchmarkExecutor)
	if !ok {
		return fmt.Errorf("benchmark service %q is unavailable", controller.serviceName)
	}
	controller.service = service

	RegisterRoute(server, controller.method, loopbackBenchmarkPath).
		Handler(controller.handle)
	return nil
}

func (controller *sqliteBenchmarkController) handle(request *RequestData) (response Response) {
	if err := controller.service.Execute(request.Request.Context()); err != nil {
		response.Error(fmt.Errorf("SQLite benchmark operation: %w", err))
		return
	}
	response.Ok().Content(loopbackBenchmarkResponse{Status: "ok"})
	return
}

type sqliteHTTPBenchmarkFixture struct {
	http       *loopbackBenchmarkFixture
	connection db.Connection
	service    *sqliteBenchmarkService
	workload   sqliteBenchmarkWorkload
}

func BenchmarkRestServerSQLite(b *testing.B) {
	workloads := []sqliteBenchmarkWorkload{
		sqliteBenchmarkPointRead,
		sqliteBenchmarkTransactionalUpdate,
	}
	for _, workload := range workloads {
		b.Run(workload.String(), func(b *testing.B) {
			b.Run("serial", func(b *testing.B) {
				fixture := newSQLiteHTTPBenchmarkFixture(b, workload, "serial")
				fixture.runSerial(b)
			})
			b.Run("parallel", func(b *testing.B) {
				fixture := newSQLiteHTTPBenchmarkFixture(b, workload, "parallel")
				fixture.runParallel(b)
			})
		})
	}
}

func newSQLiteHTTPBenchmarkFixture(
	b *testing.B,
	workload sqliteBenchmarkWorkload,
	runMode string,
) *sqliteHTTPBenchmarkFixture {
	b.Helper()

	configureLoopbackBenchmarkEnvironment(b)

	baseName := "httpserver-benchmark-sqlite-" + workload.String() + "-" + runMode
	httpPrefix := strings.ToUpper(strings.ReplaceAll(baseName+"_http", "-", "_"))
	databasePrefix := strings.ToUpper(strings.ReplaceAll(baseName+"_db", "-", "_"))
	serverName := baseName + "-server"
	databaseName := baseName + "-database"
	serviceName := baseName + "-service"
	b.Setenv(httpPrefix+"_HTTP_LISTEN", "127.0.0.1:0")

	poolSize := 1
	if workload == sqliteBenchmarkPointRead && runMode == "parallel" {
		poolSize = runtime.GOMAXPROCS(0)
	}
	method := http.MethodGet
	if workload == sqliteBenchmarkTransactionalUpdate {
		method = http.MethodPost
	}
	dsn := "file:" + databaseName +
		"?mode=memory&cache=shared" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=cache_size(-2000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(MEMORY)" +
		"&_pragma=locking_mode(NORMAL)" +
		"&_pragma=page_size(4096)" +
		"&_pragma=synchronous(FULL)" +
		"&_pragma=temp_store(MEMORY)"
	b.Setenv(databasePrefix+"_DB_SQLITE_PATH", dsn)
	b.Setenv(databasePrefix+"_DB_MAX_OPEN_CONNS", strconv.Itoa(poolSize))
	b.Setenv(databasePrefix+"_DB_MAX_IDLE_CONNS", strconv.Itoa(poolSize))

	server := NewRestServer(serverName, httpPrefix, 0)
	concreteServer := server.(*restServer)
	connection := db.NewConnection(databaseName, databasePrefix, false, false)
	service := &sqliteBenchmarkService{
		name:           serviceName,
		connectionName: databaseName,
		workload:       workload,
	}
	controller := &sqliteBenchmarkController{
		name:        baseName + "-controller",
		serverName:  serverName,
		serviceName: serviceName,
		method:      method,
	}
	httpFixture := startLoopbackBenchmarkFixture(
		b,
		concreteServer,
		method,
		func() error {
			return service.prepare(poolSize)
		},
		server,
		connection,
		service,
		controller,
	)
	if workload == sqliteBenchmarkTransactionalUpdate {
		if err := service.resetValue(); err != nil {
			b.Fatalf("reset SQLite benchmark after readiness: %v", err)
		}
	}
	return &sqliteHTTPBenchmarkFixture{
		http:       httpFixture,
		connection: connection,
		service:    service,
		workload:   workload,
	}
}

func (fixture *sqliteHTTPBenchmarkFixture) runSerial(b *testing.B) {
	before := fixture.databaseStats(b)
	fixture.http.runSerial(b)
	fixture.reportAndVerify(b, before)
}

func (fixture *sqliteHTTPBenchmarkFixture) runParallel(b *testing.B) {
	before := fixture.databaseStats(b)
	fixture.http.runParallel(b)
	fixture.reportAndVerify(b, before)
}

func (fixture *sqliteHTTPBenchmarkFixture) databaseStats(b *testing.B) sql.DBStats {
	b.Helper()
	stats, err := fixture.connection.Stats()
	if err != nil {
		b.Fatalf("read SQLite benchmark pool stats: %v", err)
	}
	return stats
}

func (fixture *sqliteHTTPBenchmarkFixture) reportAndVerify(
	b *testing.B,
	before sql.DBStats,
) {
	b.Helper()

	after := fixture.databaseStats(b)
	if b.N > 0 {
		b.ReportMetric(float64(after.WaitCount-before.WaitCount)/float64(b.N), "db-waits/op")
		b.ReportMetric(
			float64((after.WaitDuration-before.WaitDuration).Nanoseconds())/float64(b.N),
			"db-wait-ns/op",
		)
	}
	b.ReportMetric(float64(after.OpenConnections), "db-open-conns")

	expectedValue := int64(0)
	if fixture.workload == sqliteBenchmarkTransactionalUpdate {
		expectedValue = int64(b.N)
	}
	value, count, err := fixture.service.finalState()
	if err != nil {
		b.Fatalf("verify SQLite benchmark final state: %v", err)
	}
	if count != 1 || value != expectedValue {
		b.Fatalf(
			"SQLite benchmark final state: rows=%d value=%d, want rows=1 value=%d",
			count,
			value,
			expectedValue,
		)
	}
}
