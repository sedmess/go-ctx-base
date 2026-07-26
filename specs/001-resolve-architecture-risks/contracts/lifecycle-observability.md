# Lifecycle, Streaming, and Observability Contract

**Feature**: `001-resolve-architecture-risks`

## Ownership Matrix

| Resource | Owner | Created | Normal quiescence | Final/startup-failure cleanup | Restart rule |
|---|---|---|---|---|---|
| TCP listener | Built-in `restServer` generation | Error-returning initialization | Cancel request context, graceful shutdown within existing five-second bound, join serve worker | Idempotent disposal closes listener/server and joins any worker | Fresh listener and signals; persistent pre-init registrations retained |
| HTTP request context | Built-in `restServer` generation | Initialization | Canceled when server shutdown begins | Cancel repeated safely | Fresh parent context |
| Default SQL pool | Concrete `db.connection` generation | Initialization | Cancel generation and close after consumers' stop callbacks | Same idempotent close from disposal or local rollback | Fresh pool and metric registration |
| Scheduler SQL pool | `scheduler.Locker` generation | Locker initialization in `POSTGRES` mode | Stop scheduler, cancel/join leases, then close | Locker disposal/local rollback | Fresh pool |
| Local lock entry | Locker generation plus lock lease | Successful lock acquisition | Explicit unlock, caller cancellation, or locker stop | Locker disposal clears only after leases join | Fresh synchronized map |
| PostgreSQL advisory transaction | Lock lease | Successful transaction-scoped acquisition | Explicit unlock commits; cancellation/error/shutdown rolls back | Completion joined before pool close | Fresh transaction per lease |
| Scheduled context-aware job | Scheduler generation | Job execution | Scheduler cancels generation before `Stop` waits | Scheduler disposal repeats stop safely | Fresh scheduler/context |
| Stream producer goroutine | Context-aware stream operation | Constructor/transform call | Generator completion or operation cancellation | Producer closes output exactly once | New operation per call |
| Runtime profile/trace | Process-wide profiler gate plus request | Valid admitted request | Completion or request/server cancellation | Deferred runtime stop and gate release | Gate remains reusable |
| DB metric series | Active connection generation | Successful pool publication | Unregister during close | Generation-owned unregister during rollback/disposal | Same names may register after completed cleanup |

Lifecycle concurrency in this table follows go-ctx v0.12.0: callbacks for distinct services may
run concurrently, but the framework invokes one callback per service in each phase and completes
one phase before entering the next. Synchronization is therefore required only where lifecycle
state overlaps operational callers or cross-service resources; repeated cleanup across
`BeforeStop` and `Dispose` remains idempotent.

## Database Behavior

### Initialization and Publication

1. Resolve provider configuration without logging credentials or a DSN.
2. Open GORM and obtain the underlying SQL pool into local provisional variables.
3. Apply pool settings and create the pull-time metric registration.
4. If any step fails, unregister metrics and close the provisional pool before returning.
5. Publish the complete generation through an atomic pointer from serialized `Init`, replacing
   only a prior generation whose cleanup-completion signal is already closed.

An operation attempted before initialization, after close, or while a generation is rejecting new work returns a descriptive error rather than dereferencing absent state.

The connection retains its completed generation until the next serialized initialization.
Concurrent `CloseConnection` callers therefore converge on that generation's close-once guard and
completion signal without mutating the service's generation pointer during shutdown. Operational
readers may overlap the atomic restart publication and observe either inactive completed state or
fresh active state.

### Context Propagation

`SessionContext` combines the caller context with connection-generation shutdown and applies it before GORM acquires a dedicated connection. Caller deadlines remain authoritative. `Session` remains available for compatibility and is still terminated by generation shutdown when the connection is container-managed.

### Health and Secrets

Database health retains the existing five-second internal timeout and severity rules. Details may contain a sanitized operational reason and pool statistics, but never a DSN, password, token, or secret-bearing configuration value.

## Database Metric Compatibility

Each active connection exports one `db_name` series for every existing metric:

```text
gorm_dbstats_max_open_connections
gorm_dbstats_open_connections
gorm_dbstats_in_use
gorm_dbstats_idle
gorm_dbstats_wait_count
gorm_dbstats_wait_duration
gorm_dbstats_max_idle_closed
gorm_dbstats_max_lifetime_closed
gorm_dbstats_max_idletime_closed
```

Values are read from `sql.DB.Stats()` during Prometheus collection. There is no refresh ticker or connection-owned metrics goroutine. A closed generation disappears from collection, and the same connection name can register again only after generation-owned removal and completed cleanup of the prior registration.

## Scheduler and Lock Behavior

### Local Provider

- Acquiring a free key returns a lease.
- Acquiring an already held key returns the existing `resource has already locked` error.
- Different keys are independent.
- Explicit unlock, caller cancellation, and locker shutdown converge on one release operation.
- A released key can be acquired again.

### PostgreSQL Provider

- The existing transaction-scoped `pg_try_advisory_xact_lock` key derivation remains unchanged.
- A query failure returns promptly to the caller.
- A false acquisition result returns the existing contention error.
- Explicit unlock ends the transaction successfully.
- Caller cancellation, acquisition failure, job cancellation, or locker shutdown returns from the transaction with an error so it rolls back.
- Completion is acknowledged before the owned database generation closes.

`Unlock` always signals release first. If its supplied context is already canceled, it may return that context error after release has been initiated; repeated calls never block.

### Shutdown Ordering

```text
Scheduler.BeforeStop
  -> cancel context-aware jobs
  -> gocron Stop waits for jobs
Locker.BeforeStop
  -> cancel remaining lock leases
  -> join lease workers
  -> close private database
Dispose
  -> repeat idempotent cleanup for final/failed-start safety
```

## Stream Behavior

### Context-Aware Producers

- The producer owns and closes its output channel.
- Values retain generator order.
- Unbuffered constructors remain unbuffered; buffered constructors retain the requested finite capacity.
- Each send observes the operation context and the per-send context.
- Cancellation prevents subsequent value and error sends.
- A generator error produces at most one terminal error when the consumer can still receive it.
- Output closes promptly after cancellation, including while a producer is blocked by backpressure.

### Transforms and Consumers

- Context-aware maps, flat maps, iteration, and collection select on the shared operation context while receiving and sending.
- A transform chain uses the same cancellation context for every owned stage.
- A callback error remains the terminal consumer result.
- Context cancellation returns the context error; collection may return values received before cancellation together with that error.

### Compatibility Boundary

Legacy contextless functions and `SessionStream` retain their signatures and established backpressure. Consumers must drain them to closure. Silent abandonment cannot be detected from a receive-only channel; code that may stop early uses the context-aware API and cancels before abandoning the output.

## Secret-Safe Failure Contract

The following outputs never include raw control-plane tokens, bearer headers, Basic passwords, database passwords, or full secret-bearing DSNs:

- initialization and bind errors;
- authentication errors;
- structured logs;
- health details;
- Prometheus labels;
- actuator topology and metric output;
- profiler validation and busy responses.

Tests use obviously synthetic credentials and assert their absence from captured output.
