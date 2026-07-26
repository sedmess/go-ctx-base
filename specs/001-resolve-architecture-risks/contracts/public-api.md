# Public Library Contract

**Feature**: `001-resolve-architecture-risks`
**Compatibility target**: `github.com/sedmess/go-ctx` v0.12.0

## Compatibility Rule

Existing exported identifiers, function signatures, interfaces, service names, route paths, success response shapes, environment precedence, and metric names/label names remain available. New APIs are additive. Corrective behavior changes are limited to unsafe configurations, invalid profiling requests, Basic credential storage, bounded metric label values, and cancellation/lifecycle defects described below.

No method is added to the existing `httpserver.RestServer` or `db.Connection` interfaces.

## HTTP Server

The following constructors and interface remain source-compatible:

```go
func NewRestServer(name string, configPrefix string, defPort int) RestServer
func NewRestServerSilent(name string, configPrefix string, defPort int) RestServer
func Default() ctx.ServicePackage

type RestServer interface {
	AddMiddleware(middleware Middleware) RestServer
	// Existing internal route and logger capabilities remain unchanged.
}
```

The concrete built-in server changes lifecycle behavior only:

- listener reservation occurs during initialization;
- initialization failure identifies the server and effective listen address through the existing go-ctx fatal startup boundary;
- `AfterStart` serves an already reserved listener;
- `BeforeStop` remains bounded by the existing five-second graceful shutdown window;
- disposal and repeated cleanup across go-ctx's ordered `BeforeStop`/`Dispose` path are idempotent;
- a completed generation can be initialized again on the same cached service object.

Additive binding classification:

```go
func IsLoopbackOnly(server RestServer) (bool, error)
```

`IsLoopbackOnly` reports the actual bound scope for an initialized built-in server. It returns an error when the server is nil, not initialized, custom, or cannot provide a trustworthy bound address. Control-plane callers treat an error as not proven local.

## Authentication

Existing signatures remain:

```go
func BearerTokenAuthenticator(
	authFn func(path string, token string) AuthenticationResultCode,
) Middleware

func BasicAuthenticator(
	authFn func(path string, username string, password string) AuthenticationResultCode,
) Middleware

func (d *RequestData) Credential() int64
```

Credential behavior:

- successful Bearer authentication retains its existing Murmur3-derived numeric value;
- successful Basic authentication stores the same numeric representation derived from the accepted username, never the password;
- the same accepted text identity produces the same value across authentication methods and process runs;
- missing, malformed, forbidden, or otherwise unsuccessful authentication stores no credential and `Credential()` returns `0`;
- a derived credential records identity established by the current middleware; it is not proof of authorization in another request or policy context.

Bearer scheme handling becomes strict: a missing header or a scheme other than `Bearer` follows the authentication-required path and is not passed to `authFn` as a token.

## Database

The existing interface and constructor remain unchanged:

```go
func NewConnection(
	name string,
	configPrefix string,
	isDefault bool,
	isCritical bool,
) Connection

type Connection interface {
	Init() error
	AutoMigrate(models ...any)
	Session(session func(session *Session) error) error
	SessionContext(context context.Context, session func(session *Session) error) error
	Stats() (sql.DBStats, error)
	Check() error
	Health() health.ServiceHealth
}
```

Additive manual-ownership helper:

```go
func CloseConnection(connection Connection) error
```

- A connection returned by `NewConnection` closes its current pool and metric registration.
- Repeated and concurrent calls are safe and return the stored close outcome.
- Manual `Init` and close phases are serialized; a new `Init` begins only after the preceding
  close call has returned. Operational calls may overlap the restart boundary and observe either
  inactive or fresh state. go-ctx provides phase ordering for container-managed connections.
- A nil connection or a custom implementation without the close capability returns a descriptive error.
- Container-managed built-in connections invoke the same operation automatically through their lifecycle callbacks.

`SessionContext` now applies its context while acquiring a pooled connection as well as while executing callback operations. `Session` remains the compatibility entry point for callers without a context.

## Scheduler

Existing APIs and the `gocron.Locker` contract remain unchanged, including:

```go
func (instance *Scheduler) ScheduleTaskCron(
	cron string,
	key string,
	task func(),
) (*gocron.Job, error)

func (instance *Scheduler) RunScheduledTaskImmediate(key string) error

func (instance *Locker) Lock(
	context context.Context,
	key string,
) (gocron.Lock, error)
```

Additive context-aware scheduling:

```go
func (instance *Scheduler) ScheduleTaskCronContext(
	cron string,
	key string,
	task func(context.Context),
) (*gocron.Job, error)
```

The supplied task receives the scheduler generation context. Shutdown cancels that context before waiting for scheduled work. Existing `func()` jobs retain their established contract and must return before scheduler stop can finish.

All lock implementations now release on explicit unlock, acquisition-context cancellation, or locker shutdown. Repeated unlock is nonblocking and idempotent. The existing same-key contention error remains stable.

## Streaming Channels

`StreamingChan[T]` remains a named receive-only channel. Every existing function and method remains source-compatible, including the historical `FlapMap` spelling.

Additive context-owned APIs:

```go
func SingleElemChannelContext[T any](
	ctx context.Context,
	data T,
) StreamingChan[T]

func SingleElemChannelErrContext[T any](
	ctx context.Context,
	data T,
	err error,
) StreamingChan[T]

func SliceToChannelContext[T any](
	ctx context.Context,
	data []T,
) StreamingChan[T]

func CreateChannelContext[T any](
	ctx context.Context,
	generator func(sink func(data T, sendContext context.Context) bool) error,
) StreamingChan[T]

func CreateChannelBufferedContext[T any](
	ctx context.Context,
	bufSize int,
	generator func(sink func(data []T, sendContext context.Context) bool) error,
) StreamingChan[T]

func MapContext[P any, Q any](
	ctx context.Context,
	ch StreamingChan[P],
	mapper func(data P) Q,
) StreamingChan[Q]

func FlatMapContext[P any, Q any](
	ctx context.Context,
	ch StreamingChan[P],
	mapper func(data P) StreamingChan[Q],
) StreamingChan[Q]

func (ch StreamingChan[T]) ForEachChanElemContext(
	ctx context.Context,
	onEach func(data T) error,
) error

func (ch StreamingChan[T]) CollectToSliceContext(
	ctx context.Context,
) ([]T, error)
```

Context rules:

- contexts must be non-nil;
- a sink succeeds only while both the operation context and its per-send context remain active;
- cancellation closes producer-owned output without sending values after cancellation;
- values retain source order and configured buffer capacity/backpressure;
- a producer emits at most one non-nil terminal error and closes output exactly once;
- a consumer that stops before closure cancels the operation context before it stops receiving;
- legacy contextless streams remain drain-required.

`db.SessionContextStream` retains its signature and uses the new context-owned behavior internally. `db.SessionStream` remains the compatibility background-context form.

## Profiler

Existing exported method signatures remain unchanged:

```go
func (c *Controller) Trace(duration time.Duration) (*bytes.Buffer, error)
func (c *Controller) Profile(duration time.Duration) (*bytes.Buffer, error)
func (c *Controller) NamedProfile(name string, debug int) (*bytes.Buffer, error)
```

HTTP handlers use internal context-aware variants and process-wide admission control. Direct callers retain the existing signatures and must wait for their requested operation; concurrent process-global captures may now return a descriptive busy error instead of an opaque runtime failure.

## Stable Service Identities

| Component | Service identity |
|---|---|
| Default HTTP server | Reflected `httpserver.RestServer` interface name |
| Default database | Reflected `db.Connection` interface name |
| Scheduler | Existing reflected `*scheduler.Scheduler` name |
| Scheduler locker | Existing reflected `*scheduler.Locker` name |
| Independent actuator HTTP server | `base.actuator-http-server` |
| Actuator controller | `base.actuator-controller` |
| Independent profiler HTTP server | `base.profiler-http-server` |
| Profiler controller | Existing reflected `*profiler.Controller` name |

No service is renamed, silently replaced, or registered outside normal `ctx.ServicePackage` composition.
