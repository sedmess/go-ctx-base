# Repository Guidelines

## Constitutional Authority

The project constitution at `.specify/memory/constitution.md` is the highest-authority
engineering document in this repository and is non-negotiable during ordinary feature work.
Read it together with `docs/architecture.md` before changing exported APIs, package boundaries,
service wiring, configuration, lifecycle, concurrency, control-plane endpoints, or telemetry.

If a specification, plan, task list, implementation, runtime guide, or this file conflicts with
the constitution, correct the lower-authority artifact before proceeding. Constitution changes
require a separate explicit amendment. A temporary implementation exception MUST be justified
in the feature plan's Complexity Tracking section and MUST NOT silently waive a principle.

## Required Context Before Editing

Before implementation:

1. Read `.specify/memory/constitution.md` and the relevant sections of
   `docs/architecture.md`.
2. Inspect the affected public contracts, tests, configuration keys, package factories, and
   lifecycle callbacks.
3. Check the working tree and preserve unrelated or user-owned changes. In particular, do not
   rewrite dependency files or work-in-progress patches unless they are in scope.
4. When Spec Kit artifacts exist, read the active `spec.md`, `plan.md`, and `tasks.md`. The plan
   MUST pass its Constitution Check before implementation.
5. Treat the architecture risks recorded in `docs/architecture.md` as known unresolved work,
   not as permission to reproduce or silently work around them.

## Project Role and Compatibility Baseline

This repository is the Go module `github.com/sedmess/go-ctx-base`. It is a reusable set of
infrastructure adapters built on `github.com/sedmess/go-ctx`, not a domain application and not
an alternative dependency-injection container.

The current compatibility baseline is Go 1.26 and `go-ctx` v0.12.0. Preserve that baseline
unless an explicitly planned change includes compatibility analysis, migration guidance,
updated examples, and an intentional version decision. Because `go-ctx` is pre-v1, even a
minor-version upgrade MUST be reviewed for source, lifecycle, configuration, and behavioral
changes.

Treat the following as public contracts:

- exported identifiers and interfaces;
- service names and `Default` or constructor behavior;
- `ctx` and `env` tag usage;
- environment keys, prefixes, defaults, and fallback behavior;
- HTTP routes, request/response behavior, middleware, and authentication identity;
- database session, transaction, stream, and health semantics;
- scheduler and lock behavior;
- metric names and labels, logs, health details, and observable failures.

Changes are additive by default. Breaking changes require explicit migration and release scope.

## Package Boundaries and Dependency Direction

Keep package dependencies acyclic and aligned with `docs/architecture.md`:

- `go-ctx` owns dependency injection, configuration, health, statistics, and application
  lifecycle.
- `httpserver` owns REST transport, routing, middleware, authentication helpers, request
  limits, HTTP metrics, and graceful server shutdown. It MUST NOT own domain logic.
- `db` owns GORM-backed SQLite/PostgreSQL connections, sessions, transactions, pagination,
  streams, health, and DB metrics. Consumer applications own domain models and migration policy.
- `scheduler` may depend on `db` for PostgreSQL advisory locks.
- `actuator` and `profiler` may depend on `httpserver`.
- `db` may depend on `utils/channels`; `profiler` may depend on `utils/values`.
- Generic `utils/*` packages MUST NOT import `go-ctx` or infrastructure adapters.
- `logconfig` owns process-global `slog` setup; changes to import-time behavior require explicit
  review and documentation.
- `app_example.go` is a consumer composition example and integration surface. Do not move
  reusable framework or domain behavior into the root `main` package.

Every new package or external dependency needs one focused responsibility and a documented
plan-level reason. Do not duplicate behavior that belongs in the upstream `go-ctx` kernel.

## go-ctx Runtime Contract

Base adapters MUST preserve the pinned framework behavior:

- One active application context per process is the supported isolation model.
- Services are registered explicitly through `ctx.ServicePackage`; service names are unique,
  `CTX` is reserved, and reflected services are pointers to structs.
- Route and middleware registration occurs during initialization, before the HTTP server's
  `AfterStart` callback freezes the active router.
- `AfterStart` callbacks for different services are concurrent. Never depend on their order.
- `BeforeStop` is consumer-before-dependency. Do not invert that dependency-safe order.
- Disposal callbacks for different services may be concurrent. The framework invokes each
  lifecycle callback once per service per run and does not overlap lifecycle phases for that
  service. Do not add a mutex solely to serialize its own lifecycle callbacks.
- Cleanup reached through both `BeforeStop` and `Dispose` must remain idempotent. Synchronize
  state that is also accessed by request handlers, jobs, workers, public concurrent methods, or
  other services.
- `Application.Stop` is immediate and idempotent; `Join` waits for framework-owned cleanup.
- Configuration is lazy. Register defaults before the first lookup.
- Typed service lookup returns `(zero, false)` for ordinary absence; always check the boolean.

If an adapter needs different kernel behavior, change or upgrade `go-ctx` through an explicit
compatibility plan rather than implementing a competing local convention.

## Wiring and Configuration Rules

Service names, provider choices, configuration values, and supported methods must be validated.
Duplicate names, missing dependencies, invalid configuration, and unsupported choices MUST fail
visibly rather than being ignored.

Use explicit prefixes for multi-instance components. `GetEnvCustomOrDefault(prefix, key)` falls
back to the global key, so a shared `HTTP_LISTEN` can make the default, actuator, and profiler
servers compete for one socket. Tests and examples that compose multiple listeners MUST assign
distinct `BASE_`, `ACTUATOR_`, and `PROFILER_` listen values or isolate their service packages.

Preserve `go-ctx` configuration precedence and exact-key behavior. Do not log credentials,
tokens, passwords, or secret-bearing DSNs. Test fixtures must use obviously non-secret values.

## Lifecycle, Context, and Concurrency

For every listener, database pool, scheduler, transaction-backed lock, goroutine, channel,
timer, trace, profile, or blocking operation, document and implement:

- its owner and creation phase;
- its cancellation or termination signal;
- bounded shutdown and cleanup behavior;
- initialization- and callback-failure behavior;
- repeated-stop and restart behavior;
- blocking, backpressure, and consumer-abandonment behavior where applicable.

Resource-creating services MUST release owned resources through the `go-ctx` lifecycle.
Propagate caller-provided `context.Context` values through HTTP, database, stream, and lock
operations. Do not replace a meaningful caller context with `context.Background()` without an
explicit rationale and tests.

Synchronize shared mutable state. Do not rely on map iteration or unspecified callback order.
Every acquired scheduler lock must have a release path, including error and cancellation paths.

## Security and Observability

Actuator health, service-topology, Prometheus metrics, and profiler endpoints are control-plane
interfaces. They MUST remain loopback-only or be protected by explicit authentication and
authorization before external exposure. Profiling duration and other expensive diagnostic
inputs require safe limits.

Authentication middleware and handlers MUST agree on one credential type and meaning. A hash or
derived identifier is not proof of authorization outside the middleware that established it.

Use structured logs and returned errors without leaking secrets. Health checks must be bounded.
Metrics must use bounded-cardinality labels; never use raw IDs, credentials, or unnormalized
resource paths as labels.

## Testing and Required Validation

Tests use Go's standard `testing` package and live beside the package they cover as `*_test.go`.
Use `go-ctx/ctx/ctx_testing` for consumer-level application composition, environment overrides,
and named service substitution.

Behavior changes MUST include meaningful regression tests. Exercise the affected startup,
steady-state, cancellation, shutdown, cleanup, failure, repeated-stop, and restart paths.
Tests must assert observable contracts rather than private implementation details.
Documentation-only changes may omit tests with an explicit rationale.

Required completion gates are:

```text
gofmt -w path/to/edited_file.go
go build ./...
go test ./...
go vet ./...
go test -race ./...  # lifecycle, server, DB, lock, stream, or concurrency changes
```

Do not silently skip a gate. If a command is blocked by the environment or by a pre-existing
failure, report the exact command and evidence. The current architecture guide records a known
root integration-test listener collision; feature work touching composition or HTTP
configuration must address or explicitly account for it rather than weakening the test gate.

## Coding and Documentation Style

Run `gofmt` on edited Go files and use standard Go import grouping. Follow established lowercase
package names and filenames. Use PascalCase for exported identifiers and camelCase for internal
identifiers. Add comments where exported behavior or non-obvious lifecycle rules need
explanation.

Keep changes focused and preserve public compatibility. Avoid hidden global state, unbounded
goroutines, silent fallbacks, and unnecessary abstractions. Reflection, process-global state,
or concurrency changes require focused tests.

Update `readme.md`, `docs/architecture.md`, package comments, examples, and migration guidance
whenever their described APIs, routes, configuration, lifecycle, package boundaries, security,
or operational behavior changes. Architecture documents must describe implemented behavior and
must not claim an identified risk is fixed until code and tests prove it.

## Review and Handoff

Every implementation handoff or pull request must state:

- affected packages and public contracts;
- constitution compliance or a documented Complexity Tracking exception;
- lifecycle and resource ownership decisions;
- security and observability impact;
- validation commands run and their results;
- documentation and migration updates;
- any remaining pre-existing blocker kept outside the requested scope.
