# Implementation Plan: Architecture Risk Remediation

**Branch**: `001-resolve-architecture-risks` | **Date**: 2026-07-21 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/001-resolve-architecture-risks/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes
the execution workflow.

## Summary

Resolve all seven risks in `docs/architecture.md` without upgrading or forking `go-ctx` v0.12.0. The implementation reserves HTTP listeners during fallible initialization, makes every pool/lock/goroutine a restartable lifecycle generation, protects externally reachable control-plane routes with exact component bearer-token policies, bounds and cancels profiling, restores a consistent numeric credential, replaces raw metric paths with registered route expressions, and adds context-owned streaming APIs plus direct regression/race coverage.

Existing public interfaces, service names, route paths, configuration precedence/fallbacks, successful response shapes, and metric names/label names remain stable. Intentional corrective behavior changes are documented for unsafe listener/control-plane configurations, invalid or overlapping profiling requests, Basic credential storage, and dynamic metric label values. No dependency is added; the lifecycle-unsafe GORM Prometheus plugin is replaced with a pull-time collector using the existing Prometheus client.

## Technical Context

**Language/Version**: Go 1.26 baseline; planning environment verified with Go 1.26.5 on Windows/amd64

**Primary Dependencies**: `github.com/sedmess/go-ctx` v0.12.0; `net/http`; `github.com/ant0ine/go-json-rest` v3.3.2; GORM v1.31.2 with SQLite/PostgreSQL drivers; `github.com/go-co-op/gocron` v1.37.0; Prometheus client v1.24.0; Murmur3 v1.1.0. No new dependency; remove direct `gorm.io/plugin/prometheus` use after metric compatibility is implemented.

**Storage**: Existing SQLite/PostgreSQL pools through `db` and GORM; no schema, migration, or domain-data change

**Testing**: Go `testing`, existing Gomega usage where retained, `ctx/ctx_testing`, Prometheus gather/test helpers, deterministic fakes, self-subprocess startup-failure tests, optional environment-backed PostgreSQL integration, `go test`, `go vet`, and the race detector

**Target Platform**: Cross-platform in-process Go library embedded in one active go-ctx application; network server deployments with loopback or explicitly protected control-plane endpoints

**Project Type**: Reusable infrastructure-adapter library

**Performance Goals**: Detect bind conflicts before `AfterStart`; complete 100 sequential listener generations without retained resources; terminate context-owned stream/lock/profile work within two seconds of cancellation in controlled tests; retain the five-second HTTP graceful-shutdown and database-health bounds; cap HTTP profiling at 30 seconds with no queue and headroom below the inherited 60-second write timeout; keep metric series bounded across at least 10,000 variable requests.

**Constraints**: Pin `go-ctx` v0.12.0 and Go 1.26; preserve one-active-context, configuration precedence, service identity, public interface signatures, routes, successful response shapes, and metric names; no dependency-direction reversal; no secret output; no unbounded owned goroutine, lock, timer, profile, or metric label; process-global logging, metrics, and profiling tests run serially.

**Scale/Scope**: Seven recorded risks across `httpserver`, `actuator`, `profiler`, `db`, `scheduler`, `utils/channels`, root composition, and verification-only coverage for `logconfig`, `utils/slices`, and `utils/values`; three listener namespaces, two database providers, two lock providers, seven control-plane routes, three HTTP metric families, nine database metric families, and all existing public deployment modes.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- [x] **Stable contracts**: [Public API](contracts/public-api.md), [configuration](contracts/configuration.md), and [HTTP/control-plane](contracts/http-control-plane.md) inventory every affected constructor, interface, service identity, key/prefix, route, response, credential, session, lock, metric, and failure mode. Existing signatures remain; additive APIs and intentional corrective behavior have explicit migration entries.
- [x] **go-ctx alignment**: The design pins v0.12.0, reserves fallible resources in initialization, performs dependency-ordered quiescence in `BeforeStop`, uses idempotent disposal for final/failed-start cleanup, preserves concurrent `AfterStart` and disposal semantics, and designs cached service instances for restart only after `Stop().Join()`. No framework fork, upgrade, or competing container is introduced.
- [x] **Package direction**: `actuator` and `profiler` continue using `httpserver`; `scheduler` continues using `db`; `db` continues using `utils/channels`; generic utilities import only standard-library packages. The new database collector stays in `db` and uses the already approved Prometheus dependency. No cycle or new package direction is introduced.
- [x] **Deterministic wiring/configuration**: All service names and `Default` factories remain stable. Listener binding and route registration occur during initialization. Existing `BASE_`, `ACTUATOR_`, and `PROFILER_` precedence/fallback is retained, tests use distinct ephemeral values, and component token keys are exact-only. Real socket reservation replaces ambiguous string comparison.
- [x] **Lifecycle/concurrency ownership**: The [runtime state model](data-model.md) and [ownership contract](contracts/lifecycle-observability.md) name generation owners, contexts, signals, idempotent cleanup, failure rollback, stop order, and restart behavior for listeners, pools, metric registrations, locks, jobs, channels, timers, profiles, traces, and workers.
- [x] **Secure operations**: Actuator/profiler are loopback-open only when no component policy is configured; non-loopback/unknown exposure requires exact component bearer tokens and fails closed otherwise. Profile duration/admission is bounded, credentials are numeric and request-local, health remains time-bounded, secrets are excluded, and route/method labels are finite.
- [x] **Verification/documentation**: The [quickstart](quickstart.md) defines unit, composition, failure, cancellation, restart, 10,000-request cardinality, optional PostgreSQL integration, three-run suite, build, vet, and race gates. Planned updates cover `readme.md`, `utils.md`, `docs/architecture.md`, package comments, the root example, and a migration guide.

Any failed gate MUST be resolved before implementation or recorded with a concrete rationale
in Complexity Tracking below. Re-evaluate every item after design because contracts and resource
ownership often become concrete only in Phase 1.

**Pre-design gate result**: PASS. Phase 0 began with no requested constitution exception.

**Post-design re-check**: PASS. `research.md`, `data-model.md`, all contracts, and `quickstart.md` preserve package direction and the v0.12.0 kernel while assigning every affected resource and behavioral migration explicitly. No gate regressed during design.

## Project Structure

### Documentation (this feature)

```text
specs/001-resolve-architecture-risks/
|-- plan.md              # This file (/speckit-plan output)
|-- research.md          # Phase 0 output
|-- data-model.md        # Phase 1 runtime state and ownership model
|-- quickstart.md        # Phase 1 validation guide
|-- contracts/
|   |-- public-api.md
|   |-- configuration.md
|   |-- http-control-plane.md
|   `-- lifecycle-observability.md
`-- tasks.md             # /speckit-tasks output; not created by /speckit-plan
```

### Source Code (repository root)

```text
httpserver/
|-- rest_server.go                    # prebind/serve lifecycle, request context, restart-safe registrations
|-- request_handlers.go               # route-template request metadata
|-- auth_middlewares.go               # strict Bearer parsing and numeric Basic credential
|-- request.go                        # unchanged Credential() signature and clarified contract
|-- metrics_middleware.go             # bounded route/method label values
|-- control_plane.go                  # new built-in listener scope classifier
|-- rest_server_test.go               # new bind, rollback, stop, and 100-generation tests
|-- auth_middlewares_test.go           # new status/credential/secret tests
|-- metrics_middleware_test.go         # new names, templates, fallback, and 10k cardinality tests
`-- control_plane_test.go              # new actual-bind scope tests

actuator/
|-- init.go                            # stable factories/service names
|-- actuator_controller.go             # token policy validation and protected route registration
`-- actuator_controller_test.go        # new route shape and exposure-policy tests

profiler/
|-- init.go                            # stable factories/service names
|-- profiler_controller.go             # parsing, process-wide gate, context cancellation, safe names
`-- profiler_controller_test.go        # new route, bound, busy, and cleanup tests

db/
|-- db_connection.go                   # restartable pool generation, CloseConnection, context acquisition
|-- metrics.go                         # new pull-time compatible gorm_dbstats collector
|-- utils.go                           # context-owned SessionContextStream
|-- db_connection_test.go              # new pool lifecycle/restart/secret tests
|-- session_context_test.go             # expand acquisition and query cancellation coverage
|-- utils_test.go                       # new stream/pagination/error tests
`-- metrics_test.go                     # new exact-name/register/restart tests

scheduler/
|-- task_scheduler.go                  # scheduler generation and additive context-aware jobs
|-- execution_lockers.go               # cancellation/idempotent local and PostgreSQL lock state
|-- task_scheduler_test.go              # new stop/restart/context-job tests
|-- execution_lockers_test.go           # new failure/cancel/unlock/race tests
`-- execution_lockers_integration_test.go # new optional PostgreSQL integration test

utils/channels/
|-- streaming.go                       # additive context-owned producers/transforms/consumers
`-- streaming_test.go                  # new legacy and cancellation contract tests

utils/slices/mappers_test.go            # new direct mapping contract tests
utils/values/utils_test.go              # new direct optional/value contract tests
logconfig/configurator_test.go          # new serial process-global configuration tests

app_example.go                          # remove unsafe actuator bypass; use context-aware work where applicable
app_example_test.go                     # explicit BASE_/ACTUATOR_/PROFILER_ ephemeral listeners
readme.md                               # operational configuration and compatibility guidance
utils.md                                # streaming cancellation/backpressure contract
docs/architecture.md                    # implemented ownership and risk evidence
docs/migration-architecture-remediation.md # new corrective-behavior migration guide
go.mod / go.sum                         # remove lifecycle-unsafe gorm Prometheus plugin dependency only
```

**Structure Decision**: Retain the current single-module, package-colocated layout. Lifecycle and transport changes stay in `httpserver`; control-plane policy remains in `actuator`/`profiler` while using a small `httpserver` binding capability; database ownership and metric collection stay in `db`; scheduler alone owns its lock coordination and private database; generic stream changes stay independent in `utils/channels`. Tests remain beside the behavior they prove. No domain code, new container, cross-layer helper package, or dependency-direction exception is introduced.

## Design Overview

### 1. HTTP Initialization, Routing, and Shutdown

The unexported built-in server adopts one context-aware, error-returning initialization callback. It resolves the same configuration values, creates a per-run request context, constructs provisional server state, and reserves the actual TCP listener. Empty `http.Server.Addr` retains the standard `:http` bind behavior. A bind error closes provisional state locally and returns a message containing the server identity and non-secret effective address.

Controller route registration still happens during initialization through normal go-ctx dependencies. `registerRoute` wraps each handler with its finite route expression and validates the accumulated route set immediately; invalid or duplicate routes panic inside the owning controller's initialization and are converted by go-ctx into the existing startup failure. `AfterStart` only freezes middleware, installs the already validated router, and starts `Serve` on the reserved listener.

The server distinguishes persistent pre-initialization registrations from controller registrations made after the server dependency has initialized. Disposal clears only generation registrations, ensuring cached `Default` objects restart without route duplication while preserving deliberate one-time consumer setup.

`BeforeStop` cancels the server request context, invokes the existing five-second graceful shutdown, and joins the serve worker. `Dispose` closes and joins again through generation-identity and close-once guards, covering later-service initialization failure and concurrent disposal without touching a new generation.

### 2. Control-Plane Access, Credentials, and Profiling

`httpserver.IsLoopbackOnly` inspects the actual reserved address of a built-in server. Actuator and profiler load only their exact component token settings. A valid token list installs the existing bearer middleware on every owned route; without tokens, the controller proceeds only when loopback confinement is proven. Invalid/nonlocal state fails before route readiness. The exported `profiler.Controller.Init` signature remains unchanged: it delegates to an internal error-returning validator and panics on validation failure so go-ctx captures it through the established initialization boundary.

Bearer parsing requires the correct scheme. Basic authentication hashes the accepted username through the same existing credential helper used by Bearer, leaving `Credential() int64` and all established Bearer values intact. Control-plane comparisons do not log tokens, and the example no longer teaches an actuator authentication bypass.

Profiler HTTP handlers parse and validate inputs before admission. A package-global capacity-one gate is appropriate because runtime profiling is process-global under the supported one-application model. Internal context-aware trace/profile functions use stopped timers and deferred runtime cleanup; existing exported methods delegate without signature changes. Invalid input returns 400, gate contention returns 429 with `Retry-After: 1`, and successful binary responses retain their paths, filenames, and shapes.

### 3. Bounded HTTP Metrics

The route wrapper writes the registered `rest.Route.PathExp` into request-local state before route-level middleware or handlers execute. The outer Prometheus middleware reads that value after handling, falling back to `unmatched` when routing never reached a handler. It maps only supported methods directly and collapses all others to `OTHER`. Metric family names, label names, status codes, server values, duration unit, and histogram configuration remain unchanged.

### 4. Database Pool and Metric Generations

The concrete connection retains `Init() error` to satisfy the existing public interface. An injected application context, or a background context for manual use, seeds a per-run generation. Initialization acquires GORM/SQL resources locally, applies settings, registers a per-generation Prometheus collector, and only then publishes state under a mutex. Any error unregisters/closes provisional state before return.

`BeforeStop`, `Dispose() error`, and additive `CloseConnection` converge on one identity-safe close path. It rejects new work, cancels the generation, unregisters the exact collector, atomically detaches the pool, and closes it once. Adding `BeforeStop` intentionally changes the database service descriptor's `isStopAware` flag to true; the service name and type remain unchanged.

`SessionContext` combines caller and generation cancellation before calling GORM `WithContext(...).Connection`, covering pool wait and callback work. The compatibility `Session` uses the generation context. Health keeps its five-second query bound and sanitizes returned/logged errors.

The new collector reads `sql.DB.Stats()` at scrape time and reproduces all nine existing `gorm_dbstats_*` gauges with `db_name`. Removing `gorm.io/plugin/prometheus` eliminates its uncancelable ticker and stale same-name restart collectors without changing the exposed metric contract.

### 5. Scheduler, Jobs, and Locks

`Scheduler.Init` and `Locker.Init` keep their existing public signatures and receive application context through injected fields. Each run creates new scheduler/locker contexts and state. `ScheduleTaskCronContext` is additive; `BeforeStop` cancels its generation before calling gocron `Stop`, while legacy jobs retain their existing return-before-stop responsibility.

Local and PostgreSQL leases share close-once release and completion semantics. PostgreSQL acquisition uses a buffered result and `Session.Tx`, avoiding the current unbuffered handshake and unchecked manual commit. The callback waits for explicit release, caller cancellation, or locker shutdown. Explicit release returns normally so the transaction commits; cancellation/error returns an error so it rolls back. `Unlock` signals first and then observes its supplied context, making gocron's canceled-context deferred unlock safe and repeated unlock nonblocking.

Normal dependency ordering stops `Scheduler` before `Locker`. The locker then cancels/joins leases and closes its private built-in connection through `db.CloseConnection`. `Dispose` repeats the path for failed-start/final safety. Adding `BeforeStop` intentionally makes the locker service descriptor stop-aware.

### 6. Context-Owned Streams

Existing channel types and functions remain. New context-aware helpers share one operation context through producers, maps, flat maps, database pagination, iteration, and collection. Each send selects on both operation and per-send contexts, and terminal errors use the same cancellation-aware path. The producer alone closes output.

`SessionContextStream` migrates to `CreateChannelBufferedContext`; query, page send, and terminal-error delivery therefore stop together. Contextless wrappers retain established buffering/backpressure and are documented as drain-required. Tests define abandonment as partial consumption followed by canceling the supplied context, which is the only source-compatible deterministic signal available to a receive-only channel.

### 7. Direct Verification and Documentation

Direct tests are added for every package named in the risk assessment. Network tests use actual ephemeral listeners; startup-fatal cases use a self-subprocess; process-global Prometheus/logging/profiling tests are serial; lock/database behavior uses deterministic internal seams plus optional real PostgreSQL coverage. Tests assert observable state, statuses, output, completion channels, metric descriptors, and resource reuse rather than private field layouts.

Documentation updates occur only after implementation evidence exists. `docs/architecture.md` retains the risk table with resolution status and test evidence, `readme.md` documents safe operations and configuration, `utils.md` states cancellation/backpressure, package comments describe public behavior, and the migration guide records intentional failure/status/metric-value changes.

## Resource and Failure Semantics

| Concern | Normal path | Initialization failure | Cancellation/shutdown | Repeated stop/restart |
|---|---|---|---|---|
| Listener | Init reserves; AfterStart serves | Failing initializer rolls back; prior server disposed | Request context canceled, five-second graceful shutdown, worker joined | Close is idempotent; fresh listener and generation registrations |
| Default DB | Init publishes complete pool/collector | Provisional pool closes; prior initialized pool disposed | Consumer-before-dependency stop cancels and closes | State detaches atomically; same cached object opens anew |
| Scheduler DB/locks | Locker owns DB and leases | Locker rolls back its own DB; prior dependencies disposed | Scheduler stops, then locker cancels/joins/ closes | Fresh provider map/context/signals |
| Streams | Producer closes after completion | Generator error delivered once when receivable | Shared context unblocks sends/receives and closes output | Each call owns a new operation |
| Profiler | Valid admitted request captures | Invalid config/input starts no work | Client/server context stops timer/runtime capture and frees gate | Process gate reusable after every exit |
| Metrics | Registered routes/pools emit bounded series | Provisional collector unregistered | Pool collector removed; HTTP collectors remain process-global and bounded | Same DB name re-registers only after old identity removal |

See [data-model.md](data-model.md) for complete states and invariants.

## Compatibility and Migration Matrix

| Surface | Classification | Result and migration |
|---|---|---|
| `go-ctx` dependency/module/toolchain | Preserved | Remains v0.12.0, original module path, Go 1.26 baseline |
| Existing exported interfaces and methods | Preserved | No method added to `RestServer` or `Connection`; existing profiler/scheduler/stream signatures remain |
| New public helpers | Additive | `IsLoopbackOnly`, `CloseConnection`, context-aware scheduling, and context-aware streaming APIs |
| Service names and `Default` factories | Preserved | Same identities/objects; implementations become generation-safe |
| Service descriptors | Corrective observable change | Database connection and locker report `isStopAware=true` because they now own ordered cleanup |
| Listener settings and fallback | Preserved with earlier failure | Assign distinct prefixed endpoints when multiple services would resolve to one socket |
| Control-plane exposure | Intentional security change | Non-loopback/unknown deployments add exact component token settings; safe loopback remains zero-config |
| Actuator/profiler routes and success bodies | Preserved | Invalid profile inputs now 400 and concurrent profile work 429; migration guide lists statuses |
| Credential accessor/Bearer values | Preserved; Basic defect fixed | Basic now yields deterministic numeric identity rather than panicking |
| HTTP metric names/labels | Names preserved; values corrected | Dynamic paths migrate to route expressions; unmatched and unsupported methods collapse |
| DB metric names/label | Preserved | Collection becomes scrape-time and lifecycle-owned; no polling ticker |
| Legacy streams | Preserved with explicit usage rule | Drain legacy streams; use and cancel context-aware variants when early exit is possible |

Because external diagnostic configuration and telemetry label semantics intentionally change, the remediated release must be classified as the next documented pre-v1 minor release rather than an undocumented patch. The exact release tag remains a release-owner decision; implementation cannot be accepted without `docs/migration-architecture-remediation.md`.

## Verification Strategy

| Risk | Required proof |
|---|---|
| Listener collision | Prefix/fallback tests, three real `:0` binds, duplicate-bind initialization subprocess, sibling rollback, root composition, 100 application generations |
| Pool/lock lifecycle | Pool provisional/normal/failed-start/restart cleanup, acquisition cancellation, local and transaction lock state tests, stop ordering, optional PostgreSQL contention/reacquire |
| Control-plane exposure | Loopback/no-token, configured-token, non-loopback/no-token, custom/unknown, 401/403/success, proxy documentation, captured-secret absence |
| Credential mismatch | Cross-method deterministic identity, Bearer value compatibility, Basic password exclusion, absent/failure zero value, malformed scheme |
| Streaming cancellation | Cancel before first/after partial/under backpressure/after error; close/completion signals; ordering/buffer/error compatibility; race stress |
| Metric cardinality | Exact metric/label names, route/splat/unmatched/OTHER cases, 10,000 variable requests with bounded gathered series |
| Verification breadth | Direct HTTP, scheduler, actuator, profiler, logconfig, channel, slice, and value tests; build, three full test runs, vet, full race gate |

All implementation Go files are formatted with `gofmt`. Required final commands are exactly those in [quickstart.md](quickstart.md), including `go build ./...`, `go test -count=3 ./...`, `go vet ./...`, and `go test -race ./...`. A skipped optional PostgreSQL run must be reported explicitly; deterministic provider-state tests are never optional.

## Design Artifacts

- [research.md](research.md): framework/dependency evidence and selected alternatives
- [data-model.md](data-model.md): runtime generations, leases, policies, streams, and metric keys
- [public-api.md](contracts/public-api.md): preserved and additive library surface
- [configuration.md](contracts/configuration.md): precedence, listener, token, database, scheduler, and migration rules
- [http-control-plane.md](contracts/http-control-plane.md): routes, access, profile statuses, listener failure, and HTTP metrics
- [lifecycle-observability.md](contracts/lifecycle-observability.md): ownership, cleanup, DB metrics, locks, streams, and secrets
- [quickstart.md](quickstart.md): runnable end-to-end validation and success-criterion trace

## Complexity Tracking

No constitution violation or temporary exception is planned. The process-global profiler gate and Prometheus registrations match existing process-global resources, are bounded/idempotent, and remain within the supported one-active-application-per-process model.

**Post-implementation dependency review (2026-07-22)**: A user-authorized compatible update advanced the non-framework Go module graph and the runtime Alpine image after implementation. `go-ctx` remains pinned at v0.12.0, no package direction or public contract changed, and the build, three-run test, vet, and race gates all pass with the refreshed graph.
