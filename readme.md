# Go Contextualized Application Framework

A modular framework for building contextualized Go applications with dependency injection, service management, and common infrastructure components.

`go-ctx-base` is the infrastructure-adapter layer for
[`github.com/sedmess/go-ctx` v0.12.0](https://github.com/sedmess/go-ctx/tree/v0.12.0).
The base framework owns dependency injection, configuration, diagnostics, and application
lifecycle; this module supplies HTTP, database, scheduling, actuator, profiler, logging, and
utility packages.

See [Architecture](docs/architecture.md) for package boundaries, runtime flow, configuration
namespaces, security constraints, and the current architecture-risk assessment. Mandatory
engineering rules are defined by the [project constitution](.specify/memory/constitution.md).

## DeepWiki Documentation
[![DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/sedmess/go-ctx-base)

## Key Components

### Database Layer (`db/`)
- Connection management for SQLite and PostgreSQL
- Context-aware sessions with transactions
- Context-owned streaming query support with pagination
- Connection pooling and health checks
- Pull-time SQL pool statistics through Prometheus
- **Exceptional**: Hybrid SQLite/PostgreSQL support with automatic configuration

### HTTP Server (`httpserver/`)
- REST API framework with middleware support
- Typed request handlers with automatic JSON marshaling
- Authentication middleware (Bearer Token & Basic Auth)
- Integrated Prometheus metrics collection
- Request size limiting and timeout handling
- Early listener reservation and generation-safe graceful shutdown
- **Exceptional**: Dual-format health checks (JSON/plaintext)

### Task Scheduler (`scheduler/`)
- Cron-style job scheduling
- Distributed locking using PostgreSQL advisory locks or in-memory locks
- Cluster-safe execution coordination
- **Exceptional**: Transaction-based locking for PostgreSQL backend

### Monitoring (`actuator/`)
- Health check aggregator
- Service discovery endpoint
- Metrics endpoint for Prometheus
- Dependency tracking visualization
- Component-specific bearer protection for nonlocal exposure

### Runtime Profiling (`profiler/`)
- Runtime trace, CPU, and named profile endpoints
- A process-wide capacity-one admission gate
- Request/server cancellation and a 30-second HTTP capture limit

### Utilities (`utils/`)
- Concurrent execution pools with semaphores
- Streaming channel patterns
- Slice manipulation helpers
- Optional type wrappers
- **Exceptional**: Buffered channel generators with error propagation

## Getting Started

```go
package main

import (
    "github.com/sedmess/go-ctx-base/db"
    "github.com/sedmess/go-ctx-base/httpserver"
    "github.com/sedmess/go-ctx-base/scheduler"
    "github.com/sedmess/go-ctx/ctx"
)

func main() {
    ctx.CreateContextualizedApplication(
        httpserver.Default(),
        db.Default(),
        scheduler.Default(),
        ctx.PackageOf(
            &MyController{},
            &MyService{},
        ),
    ).Join()
}
```

## Configuration

Configuration follows `go-ctx` precedence and supports service prefixes. Default components
check their namespaced key first and then the unprefixed fallback. When multiple HTTP servers
are enabled, use distinct prefixed listen addresses so they do not inherit the same socket.

```shell
# Default HTTP server
BASE_HTTP_LISTEN=127.0.0.1:8080
HTTP_MAX_REQUEST_SIZE=1048576

# Independent control-plane servers (keep loopback-only unless protected)
ACTUATOR_HTTP_LISTEN=127.0.0.1:8089
PROFILER_HTTP_LISTEN=127.0.0.1:8099
ACTUATOR_HTTP_AUTH_TOKENS=replace-with-actuator-token
PROFILER_HTTP_AUTH_TOKENS=replace-with-profiler-token

# Default PostgreSQL connection
BASE_DB_HOST=localhost
BASE_DB_USERNAME=postgres
# Supply BASE_DB_PASSWORD through the deployment's secret-injection mechanism.

# Default SQLite connection (alternative to PostgreSQL)
BASE_DB_SQLITE_PATH=file::memory:

# Scheduling
SCHEDULER_LOCK_PROVIDER=POSTGRES  # or LOCAL
SCHEDULER_DB_HOST=localhost       # required for POSTGRES locking
```

Unprefixed `HTTP_*` and `DB_*` keys remain supported as fallbacks for the default instances.
The exact `ACTUATOR_HTTP_AUTH_TOKENS` and `PROFILER_HTTP_AUTH_TOKENS` keys have no global
fallback. Each is a comma-separated list with trimmed, non-empty entries. Without its token
setting, a control-plane component starts only when its actual listener is proven loopback-only.
A reverse proxy makes that endpoint operationally external even if the process binds loopback,
so configure the component token before proxying it.

Listener configuration is resolved with the component prefix first and the existing global
fallback second. The resolved socket is reserved during initialization: duplicate endpoints fail
before readiness, while `127.0.0.1:0` gives each server a distinct ephemeral port. Listener,
request context, SQL pool, database metric registration, scheduler, and lock state belong to one
application generation and are released by stop/disposal before a cached service is restarted.

Context-aware channel constructors and transforms, `db.SessionContextStream`,
`scheduler.ScheduleTaskCronContext`, `db.CloseConnection`, and
`httpserver.IsLoopbackOnly` are additive APIs. Existing stream APIs remain compatible and must be
drained to closure; cancel the shared context before abandoning a context-aware stream. HTTP
metric names and label keys are unchanged, but the `path` value is now a registered route
expression (or `unmatched`) and unsupported methods are labeled `OTHER`.

See the [configuration model](docs/architecture.md#configuration-model) for exact namespaces
and inherited `go-ctx` source precedence. Upgrade behavior is summarized in the
[architecture-remediation migration guide](docs/migration-architecture-remediation.md).

## License
Apache 2.0 - See [LICENCE](LICENCE) for details
