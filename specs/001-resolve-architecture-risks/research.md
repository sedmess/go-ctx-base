# Phase 0 Research: Architecture Risk Remediation

**Feature**: `001-resolve-architecture-risks`
**Date**: 2026-07-21
**Baseline**: Go 1.26, `github.com/sedmess/go-ctx` v0.12.0

All material research questions are resolved. The decisions below preserve the pinned framework and existing exported contracts while making the affected adapter behavior deterministic, cancelable, and testable.

## Decision 1: Use the Existing go-ctx Lifecycle Without a Fork or Upgrade

**Decision**: Keep `github.com/sedmess/go-ctx` pinned at v0.12.0 and express remediation through its existing initialization, start, stop, disposal, dependency-injection, configuration, health, statistics, and testing extension points.

Fallible validation and resource acquisition belong in one error-returning `Init` variant. `AfterStart` only activates resources that were validated and acquired successfully. `BeforeStop` quiesces consumers in dependency-safe order, and idempotent `Dispose` cleanup covers normal shutdown and cleanup after a later service fails to initialize.

**Rationale**: In v0.12.0, initialization errors are propagated before global publication, while `AfterStart` callbacks run concurrently and cannot return errors. A failed startup skips `BeforeStop`, cancels the root context, and disposes only services whose initialization completed. The initializer that returns an error must therefore roll back its own provisional resources. Normal `BeforeStop` is consumer-before-dependency; disposal is concurrent. `Stop` is immediate and idempotent, and `Join` is the cleanup barrier.

`Default` packages cache service objects with `sync.OnceValue`, so the same server, database, scheduler, and controller instances must support a fresh lifecycle generation after `Stop().Join()`.

**Alternatives considered**:

- Upgrade or fork `go-ctx`: rejected because the user requires v0.12.0 compatibility.
- Add a second container or local lifecycle convention: rejected because it duplicates the framework kernel.
- Report fallible startup from `AfterStart`: rejected because the callback has no error result and concurrent failures occur after publication.

**Primary evidence**: `go-ctx@v0.12.0/ctx/types.go`, `ctx/reflective.go`, `ctx/application_context.go`, `ctx/application_context_singleton.go`, and `docs/architecture.md`.

## Decision 2: Reserve Real HTTP Listeners During Initialization

**Decision**: Resolve the existing prefixed configuration and reserve each TCP listener during `restServer.Init`. Store the listener and call `http.Server.Serve` from `AfterStart`. Close it through idempotent stop/disposal paths and wait for the serving goroutine. Preserve current defaults, prefix-before-global fallback, present-empty behavior, and support for port `0`.

Per-run routes, middleware, request contexts, listeners, and completion channels are reset between application generations. Registrations deliberately made before the first initialization remain persistent; registrations made by initialized controllers are generation-scoped so a restart does not duplicate them.

**Rationale**: The operating system is the only accurate authority for collisions involving wildcard addresses, host aliases, IPv4/IPv6, occupied external ports, and repeated `:0` values. Prebinding produces a descriptive initialization failure before any listener serves traffic. If another service later fails, v0.12.0 disposal closes the already initialized listener.

The root integration test should use `BASE_HTTP_LISTEN`, `ACTUATOR_HTTP_LISTEN`, and `PROFILER_HTTP_LISTEN`, each set to `127.0.0.1:0`. This removes accidental fallback coupling without removing the fallback contract.

**Alternatives considered**:

- Compare configured address strings: rejected because equivalent binds need not have identical strings and `:0` is intentionally reusable.
- Keep `ListenAndServe` inside the start goroutine: rejected because a collision occurs after application startup and currently reaches a fatal logging path.
- Remove `HTTP_*` fallback: rejected as a configuration compatibility break.

## Decision 3: Enforce an Exact, Built-In Control-Plane Token Policy

**Decision**: Add exact-only `ACTUATOR_HTTP_AUTH_TOKENS` and `PROFILER_HTTP_AUTH_TOKENS` settings. They do not inherit a global token key. Values are comma-separated, trimmed, non-empty bearer tokens and are never logged or returned.

- A listener bound only to loopback remains open when its component token list is absent.
- A configured token list is enforced on loopback as well as non-loopback listeners.
- A directly non-loopback listener without a valid component token list fails controller initialization.
- Missing or malformed bearer credentials return 401; known-but-rejected credentials return 403; authorized requests retain existing route responses.

`httpserver` provides a small additive binding-classification helper using the actual reserved listener. A custom server whose exposure cannot be inspected is treated as unsafe unless the component token policy is configured. Documentation states that a reverse proxy can make a loopback listener externally reachable and therefore also requires tokens.

**Rationale**: This preserves zero-configuration local diagnostics, provides enforceable authentication and authorization for exposed diagnostics, and works for independent or mounted controllers without changing existing controller factory signatures.

**Alternatives considered**:

- Always require authentication: rejected because it breaks current local-only diagnostics.
- Trust arbitrary server middleware or an operator assertion: rejected because the controller cannot prove that every diagnostic route is protected.
- Fall back to a global token list: rejected because accidental security-policy inheritance is unsafe.

## Decision 4: Bound, Serialize, and Cancel Runtime Profiling

**Decision**: Preserve the three profiler routes, the 15-second omitted-duration default, binary success bodies, download headers, and existing exported `Controller` methods. The HTTP layer applies these rules:

- malformed, zero, negative, or greater-than-30-second durations return 400 before work starts;
- a single process-wide, nonblocking admission gate covers trace, CPU, and named-profile endpoint work;
- a conflicting request returns 429 instead of waiting;
- request and server shutdown contexts cancel timers and always stop a started trace or CPU profile;
- named profile names must exist and be safe for response headers, and debug accepts only the documented values 0, 1, or 2.

The server supplies request contexts derived from a per-run server context and cancels that context when shutdown begins.

**Rationale**: Runtime trace and CPU profiling are process-global. The current `time.After` waits ignore cancellation, malformed durations silently fall back to 15 seconds, and overlapping requests become opaque server errors. A 30-second cap preserves the existing 15-second default while remaining safely below the inherited 60-second HTTP write timeout, leaving time to encode and write the response.

**Alternatives considered**:

- Rely on runtime start errors: rejected because it does not bound waiting or provide a useful status.
- Queue profiling requests: rejected because queued expensive work is not bounded.
- Change public methods to require a context: rejected as source-breaking; handlers can use internal context-aware operations.

## Decision 5: Preserve the Numeric Authentication Identity

**Decision**: Retain `RequestData.Credential() int64` and the existing bearer value derivation. Basic authentication stores `int64(murmur3.Sum64(username))` instead of the raw username; passwords are never used as identity input. Failed or absent authentication leaves the request-local credential absent, so the accessor returns the existing zero value. Malformed or non-Bearer authorization schemes are rejected before invoking bearer authorization.

**Rationale**: Basic authentication currently stores a string that always panics when the numeric accessor asserts its type. Hashing the accepted username repairs the defect, matches the existing deterministic bearer representation, preserves bearer values, and avoids storing raw secret material.

**Alternatives considered**:

- Change the accessor to string or a new identity type: rejected as source-breaking.
- Store raw usernames or tokens: rejected because it is inconsistent and exposes identity or secret material.
- Change hash algorithms: rejected because it changes established bearer credential values.

## Decision 6: Derive Bounded Metrics From Registered Routes

**Decision**: Preserve the three `httpserver_*` metric names and the `server`, `code`, `method`, and `path` label names. Route registration wraps handlers to place the finite registered route expression in request-local state. Metrics use that expression as `path`; requests that never reach a registered route use the constant `unmatched`. Supported HTTP methods retain their names, and all other method tokens collapse to `OTHER`.

**Rationale**: Registered route expressions are already the correct bounded dimension. Redacting raw paths heuristically misses attacker-controlled values, while removing the dimension would discard useful route-level observability.

**Alternatives considered**:

- Regex replacement of identifiers: rejected as incomplete.
- Remove the path label: rejected because a bounded template is available.
- Reimplement the dependency's router matcher in metrics: rejected as fragile duplication.

**Migration impact**: Metric and label names remain stable, but dashboards grouping raw dynamic paths migrate to route templates and unknown traffic collapses to `unmatched`.

## Decision 7: Make Database Pools Lifecycle-Owned and Pull Metrics at Scrape Time

**Decision**: Leave the exported `db.Connection` interface unchanged. Implement a restartable, mutex-protected lifecycle generation on the concrete connection, plus an additive `CloseConnection(Connection) error` helper for manually owned connections. `BeforeStop` cancels and closes the current generation in dependency order; `Dispose` repeats the same idempotent cleanup for failed-start and final cleanup. A failing initializer closes all provisional resources before returning.

`SessionContext` calls `db.WithContext(callerContext).Connection(...)` so cancellation covers pool acquisition as well as the later query. The effective operation context also observes connection-generation shutdown without replacing the caller's deadline.

Replace `gorm.io/plugin/prometheus` with a project-owned pull-time collector that registers and unregisters with each connection generation. Preserve the following metric names, help meanings, gauge types, and `db_name` label:

- `gorm_dbstats_max_open_connections`
- `gorm_dbstats_open_connections`
- `gorm_dbstats_in_use`
- `gorm_dbstats_idle`
- `gorm_dbstats_wait_count`
- `gorm_dbstats_wait_duration`
- `gorm_dbstats_max_idle_closed`
- `gorm_dbstats_max_lifetime_closed`
- `gorm_dbstats_max_idletime_closed`

**Rationale**: Adding `Close` to `Connection` would break consumer implementations. The concrete service can satisfy go-ctx disposal without changing that interface, while the additive helper gives owners of `NewConnection` an explicit close path.

The pinned GORM Prometheus plugin starts an uncancelable `time.Tick` goroutine per connection and ignores duplicate registration failures. Closing only the SQL pool leaks the ticker and can leave same-name restart metrics attached to a closed pool. Scrape-time `sql.DB.Stats()` requires no background worker.

**Alternatives considered**:

- Extend `Connection`: rejected as source-breaking.
- Close only during normal stop: rejected because failed startup skips stop callbacks.
- Keep a cancellable polling worker: viable but unnecessary when pull-time collection is available.
- Remove DB metrics: rejected as an observability break.

## Decision 8: Make Scheduler Locks Generation- and Context-Owned

**Decision**: Preserve the `gocron.Locker` signatures, advisory-lock key SQL, error text, provider setting, service names, and existing scheduler methods. Rework each lock as an idempotent state machine with a buffered acquisition result, close-once release signal, completion signal, terminal error, and locker-generation context.

- PostgreSQL acquisition runs inside `Session.Tx`; explicit unlock ends successfully, while caller cancellation, failure, or locker shutdown rolls back and releases the transaction-scoped advisory lock.
- Local locks use the same explicit-unlock, caller-cancellation, and shutdown release contract.
- `Unlock` signals release before inspecting its own context because gocron can call it with an already canceled job context.
- `Scheduler.BeforeStop` stops jobs before `Locker.BeforeStop` cancels and joins active locks; only then is the locker's privately owned database closed.
- Each `Init` creates fresh maps, contexts, channels, wait groups, and cleanup guards.

Add `ScheduleTaskCronContext(cron, key string, task func(context.Context))` while preserving `ScheduleTaskCron`. The scheduler cancels its per-run context before waiting for context-aware jobs during shutdown. Legacy callbacks remain supported and must return for scheduler stop to complete.

**Rationale**: The current unbuffered handshake can block forever when session creation fails, waits only for explicit unlock after acquisition, commits without checking the transaction result, and blocks on repeated unlock. PostgreSQL transaction-scoped locks are released reliably only when their transaction ends.

**Alternatives considered**:

- Session-level advisory locks: rejected because pooled-connection ownership is unreliable.
- Clear only the local map during shutdown: rejected because it does not handle cancellation during operation.
- Change existing scheduled callback signatures: rejected as source-breaking.

## Decision 9: Add Context-Owned Streams Without Changing Channel Semantics

**Decision**: Keep `StreamingChan[T]` as a receive-only channel and retain all existing exported functions, including `FlapMap`. Add context-owned constructors, finite-source helpers, transforms, iteration, and collection. Every value and terminal-error send in the new APIs observes the operation context, producers close output exactly once, order and declared buffer size remain stable, and at most one terminal error is delivered.

`db.SessionContextStream` uses the context-owned buffered producer so cancellation covers queries, data sends, and terminal error handling. `SessionStream` remains the background-context compatibility form. Repository consumers that can abandon work migrate to the context-aware surface.

The public abandonment rule is explicit: a consumer that stops receiving before stream closure must cancel the context supplied to the context-aware producer. Legacy contextless streams must be fully drained.

**Rationale**: A receive-only channel cannot detect that its last consumer discarded it. Automatic unsignaled-abandonment detection would require replacing the public channel type, adding unbounded buffering, dropping values, or imposing a timeout that changes backpressure. The additive context contract preserves source compatibility and makes cancellation deterministic.

**Alternatives considered**:

- Replace `StreamingChan` with a struct/cancel handle: rejected because it breaks ranging, assignment, and receive operations.
- Unlimited buffering: rejected as an unbounded-memory trade.
- Silent nonblocking sends: rejected because they lose ordered data and errors.

## Decision 10: Verify Each Risk Directly and Keep Process-Global Tests Serial

**Decision**: Add colocated unit, failure, cancellation, restart, and race tests for HTTP, authentication, metrics, actuator, profiler, database, scheduler, channels, logging, slices, and values. Keep tests that mutate environment, the default Prometheus registry, logging, runtime profiling, or the single active go-ctx application serial.

Use `ctx_testing` with explicit namespaced listener parameters for consumer composition. Expected fatal startup failures run through a self-subprocess because v0.12.0 reports application creation failures through its existing fatal boundary. PostgreSQL advisory-lock integration is environment-backed and supplements deterministic unit tests around the lock state machine.

**Rationale**: The current tree has direct tests only for database query cancellation and the execution pool, while the root suite fails from listener collision. Process-global concurrency would make otherwise correct tests intermittent.

**Alternatives considered**:

- Assert only private fields: rejected because tests must prove observable contracts.
- Use fixed test ports: rejected because they collide with unrelated processes.
- Skip PostgreSQL behavior entirely: rejected because the distributed-lock risk is provider-specific.

## Dependency Outcome

- Keep `github.com/sedmess/go-ctx` at v0.12.0.
- Add no dependency.
- Remove the direct `gorm.io/plugin/prometheus` dependency after replacing its lifecycle-unsafe polling behavior with the existing Prometheus client library.
