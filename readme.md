# Go Contextualized Application Framework

A modular framework for building contextualized Go applications with dependency injection, service management, and common infrastructure components.

## Key Components

### Database Layer (`db/`)
- Connection management for SQLite and PostgreSQL
- Context-aware sessions with transactions
- Streaming query support with pagination
- Connection pooling and health checks
- GORM integration with Prometheus metrics
- **Exceptional**: Hybrid SQLite/PostgreSQL support with automatic configuration

### HTTP Server (`httpserver/`)
- REST API framework with middleware support
- Typed request handlers with automatic JSON marshaling
- Authentication middleware (Bearer Token & Basic Auth)
- Integrated Prometheus metrics collection
- Request size limiting and timeout handling
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

Environment variable based configuration with service prefixes:
```shell
# HTTP Server
HTTP_LISTEN=:8080
HTTP_MAX_REQUEST_SIZE=1048576

# PostgreSQL
DB_HOST=localhost
DB_USERNAME=postgres
DB_PASSWORD=secret

# SQLite
DB_SQLITE_PATH=file::memory:

# Scheduling
SCHEDULER_LOCK_PROVIDER=POSTGRES  # or LOCAL
```

## License
Apache 2.0 - See [LICENCE](LICENCE) for details
