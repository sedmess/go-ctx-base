# Runtime State Model: Architecture Risk Remediation

**Feature**: `001-resolve-architecture-risks`
**Date**: 2026-07-21

This feature introduces no domain entity, schema migration, or persistent business data. Its data model consists of lifecycle generations and request-local contract values owned by infrastructure adapters.

## Listener Generation

Represents one initialized run of a built-in HTTP server.

### Fields

| Field | Meaning | Validation |
|---|---|---|
| Service name | Stable go-ctx service identity | Non-empty, unique, and unchanged across restarts |
| Configuration prefix | Namespace used before global `HTTP_*` fallback | Existing uppercase prefix is preserved |
| Configured address | Value produced by existing configuration precedence | Present-empty retains current `net/http` meaning |
| Effective bind address | Address supplied to the TCP listener | Empty server address maps to the current `:http` behavior |
| Bound address | Address returned by the operating system | May differ when port `0` is requested |
| Persistent registrations | Routes and middleware added before initialization | Survive completed application generations |
| Generation registrations | Routes and middleware added by services during the current initialization | Cleared during disposal |
| Request context and cancel function | Parent for accepted request contexts | Created once per generation; canceled once during shutdown |
| Listener and HTTP server | Network resources for this generation | Published only after a successful bind |
| Serve completion signal | Proves the serving goroutine has exited | Closed exactly once by the serving goroutine |

### State Transitions

```text
dormant
  -> initializing
      -> bound
          -> serving
              -> quiescing
                  -> closed
                      -> dormant (next Init)
      -> dormant (bind or setup failure with local rollback)

bound/serving
  -> closed (Dispose after later-service startup failure)
```

### Invariants

- No serving goroutine starts before a listener is bound successfully.
- A generation owns at most one listener and one serving goroutine.
- Framework-ordered stop and disposal cannot close a later generation's listener; a later
  generation starts only after the preceding application's `Stop().Join()` completes.
- Port `0` values are never rejected merely because their configured strings match.
- Routes and middleware are frozen for the serving generation before traffic is accepted.

## Database Connection Generation

Represents one GORM and `database/sql` pool generation owned by a connection service or by the scheduler locker.

### Fields

| Field | Meaning | Validation |
|---|---|---|
| Connection name | Service identity and `db_name` metric label | Non-empty and unique among active metric registrations |
| Configuration resolver | Existing prefix-only or prefix-with-global-fallback behavior | Selection remains fixed by `NewConnection` arguments |
| Provider | SQLite or PostgreSQL | Exactly one valid configuration path resolves |
| GORM handle | Query/session facade | Published only after complete setup |
| SQL pool | Owned external resource | Closed once per generation |
| Generation context | Shutdown signal combined with caller contexts | Never replaces a caller deadline or cancellation |
| Metric registration | Generation-owned registration of pull-time pool statistics | Removed before that generation publishes cleanup completion |
| Lifecycle state | Current generation phase | Atomic generation publication, generation cancellation, close-once guard, and one-way completion signal; no service mutex |

### State Transitions

```text
uninitialized
  -> opening
      -> active
          -> rejecting-new-work
              -> closing
                  -> closed
                      -> opening (next serialized Init replaces the completed generation)
      -> uninitialized (first setup failure after provisional rollback)
      -> closed (restart setup failure retains the completed generation)
```

### Invariants

- Provisional pools are closed if the connection's own initializer fails.
- `BeforeStop`, `Dispose`, and `CloseConnection` converge on one idempotent close operation.
- A completed generation remains attached until a serialized later `Init` replaces it after the
  cleanup-completion signal.
- Concurrent initialization is outside the go-ctx contract; restart begins only after
  `Stop().Join()`.
- Operational readers may overlap atomic restart publication and observe either the completed
  generation as inactive or the fresh active generation.
- Context-aware pool acquisition and queries observe both caller cancellation and generation shutdown.
- No polling goroutine is required to expose database statistics.
- Metric names and the `db_name` label remain stable across generations.

## Locker Generation

Represents the state shared by all local or PostgreSQL locks created during one application run.

### Fields

| Field | Meaning | Validation |
|---|---|---|
| Provider | `LOCAL` or `POSTGRES` | Any other value fails initialization |
| Generation context | Locker shutdown signal | Fresh for every successful `Init` |
| Local key set | Currently held local keys | Synchronized; empty after shutdown |
| Active leases | Lock operations not yet completed | Joined before owned database closure |
| Owned connection | Private scheduler database for PostgreSQL mode | Absent in local mode; closed by the locker |

### State Transitions

```text
dormant -> active -> stopping -> joined -> disposed -> dormant
```

`Scheduler.BeforeStop` precedes `Locker.BeforeStop` because the scheduler depends on the locker. The scheduler first stops new job execution; the locker then cancels leases, joins their work, and closes its private connection.

## Lock Lease

Represents one local key reservation or one transaction-scoped PostgreSQL advisory lock.

### Fields

| Field | Meaning | Validation |
|---|---|---|
| Key | Scheduler job or caller lock identity | Preserved exactly for current hashing and error behavior |
| Provider | Local map or PostgreSQL transaction | Inherited from the locker generation |
| Effective context | Caller cancellation combined with locker shutdown | Cancellation of either releases the lease |
| Acquisition result | Exactly one success or failure result | Buffered so the worker cannot strand the caller |
| Release signal | Explicit release request | Close-once and nonblocking |
| Completion signal | Transaction/map release completion | Closed exactly once |
| Terminal error | Acquisition or release outcome | Expected cancellation is distinguished from operational failure |

### State Transitions

```text
requesting
  -> not-acquired
  -> held
      -> releasing (explicit unlock, caller cancellation, or locker shutdown)
          -> released
  -> failed
```

### Invariants

- At most one local lease holds a key in one locker generation.
- A PostgreSQL lease is held only while its transaction is open.
- Repeated or concurrent `Unlock` calls never block and never release another lease.
- Explicit unlock signals release even if the context supplied to `Unlock` is already canceled.

## Control-Plane Access Policy

Represents the effective access decision for one actuator or profiler controller.

### Fields

| Field | Meaning | Validation |
|---|---|---|
| Component | Actuator or profiler | Determines the exact token key and routes |
| Server service name | Target `RestServer` service | Existing service names remain unchanged |
| Bound network scope | Loopback-only, non-loopback, or unknown | Derived from the actual built-in listener when available |
| Token policy | Component-specific accepted bearer tokens | Trimmed, no empty entries, never logged or returned |
| Effective mode | Local-open, token-protected, or refused | Non-loopback/unknown without tokens is refused |

### State Transitions

```text
unvalidated
  -> local-open
  -> token-protected
  -> refused (startup error; no routes published as ready)
```

## Authenticated Credential

Represents the request-local result of successful authentication.

| Field | Meaning | Validation |
|---|---|---|
| Authentication method | Bearer or Basic | Must have been accepted by its authorization callback |
| Accepted identity | Bearer token or Basic username | Used only as hash input; raw value is not exposed as the credential |
| Credential | Deterministic signed 64-bit value | Existing Murmur3 derivation is preserved |
| Authorized | Whether middleware called the downstream handler | Failed/absent authentication stores no credential |

The value is an identity established by the middleware for this request; it is not independent proof of authorization outside that request path.

## Profiling Operation

Represents one HTTP-triggered trace, CPU profile, or named profile capture.

### Fields

| Field | Meaning | Validation |
|---|---|---|
| Kind | Trace, CPU, or named profile | Must match an existing route |
| Request context | Client and server-generation cancellation | Required for HTTP-triggered work |
| Duration | Trace/CPU capture duration | Missing is 15 seconds; otherwise greater than zero and at most 30 seconds |
| Name | Runtime profile name | Required, existing, and safe for a response filename |
| Debug | Named-profile output mode | One of 0, 1, or 2 |
| Admission lease | Process-wide exclusive profiling gate | Nonblocking; one HTTP profiling operation at a time |
| State | Validation/capture/completion phase | Gate and runtime resource are released on every exit |

### State Transitions

```text
received -> rejected-invalid
received -> rejected-busy
received -> admitted -> running -> completed
                              -> canceled
                              -> failed
```

## Stream Operation

Represents one context-owned producer and its receive-only output.

### Fields

| Field | Meaning | Validation |
|---|---|---|
| Operation context | Consumer-owned abandonment/cancellation signal | Non-nil; shared with transforms that must terminate together |
| Output channel | Ordered values or one terminal error | Producer-owned and closed exactly once |
| Capacity | Existing unbuffered or requested bounded buffer | Negative sizes are rejected consistently with channel creation |
| Generator | Producer callback | Must stop after the sink returns false |
| Terminal state | Success, error, or cancellation | At most one non-nil terminal error is sent |

### State Transitions

```text
created -> producing -> completed -> closed
                   -> failed -> closed
                   -> canceled -> closed
```

### Abandonment Rule

A consumer that stops receiving before closure cancels the context supplied to the context-aware producer. Contextless legacy streams remain source-compatible and drain-required because a receive-only channel cannot detect silent abandonment.

## HTTP Metric Series Key

Represents one bounded Prometheus series identity.

| Dimension | Allowed value |
|---|---|
| `server` | Stable registered server name |
| `code` | Recorded HTTP response code |
| `method` | Supported standard method or `OTHER` |
| `path` | Registered route expression or the constant `unmatched` |

Raw URL paths, path parameter values, query values, credentials, and other request-controlled identifiers never enter the series key.

## Relationships

```text
go-ctx application generation
  |-- owns Listener Generation(s)
  |     |-- parent Request Context
  |     `-- hosts Control-Plane Access Policy and Profiling Operation(s)
  |-- owns default Database Connection Generation
  `-- owns Scheduler
        `-- depends on Locker Generation
              |-- owns Lock Lease(s)
              `-- owns private Database Connection Generation in POSTGRES mode

Request handler / database query
  `-- may own Stream Operation

Registered route + response
  `-- yields HTTP Metric Series Key
```
