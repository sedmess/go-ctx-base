<!--
Sync Impact Report
- Version change: template (unratified) -> 1.0.0
- Modified principles:
  - Placeholder Principle 1 -> I. Stable Infrastructure Contracts
  - Placeholder Principle 2 -> II. go-ctx-Aligned Composition
  - Placeholder Principle 3 -> III. Deterministic Wiring and Configuration
  - Placeholder Principle 4 -> IV. Lifecycle and Concurrency Ownership
  - Placeholder Principle 5 -> V. Secure, Observable, Verified Operations
- Added sections:
  - Architecture and Technical Constraints
  - Development Workflow and Quality Gates
- Removed sections: placeholder section slots only
- Templates requiring updates:
  - ✅ updated: .specify/templates/plan-template.md
  - ✅ updated: .specify/templates/spec-template.md
  - ✅ updated: .specify/templates/tasks-template.md
- Runtime guidance:
  - ✅ created: docs/architecture.md
  - ✅ updated: readme.md
  - ✅ reviewed: utils.md; no architecture-policy change required
- Framework guidance:
  - ✅ reviewed: github.com/sedmess/go-ctx v0.12.0 readme, architecture guide,
    migration guide, and public runtime contract
- Command guidance:
  - ✅ reviewed: all .agents/skills/speckit-*/SKILL.md files; no stale
    agent-specific or architecture-version references require changes
- Follow-up TODOs: none
-->
# go-ctx-base Constitution

## Core Principles

### I. Stable Infrastructure Contracts

`go-ctx-base` MUST remain a reusable Go library of infrastructure adapters for applications
built on `github.com/sedmess/go-ctx`. Exported identifiers, service names, constructor and
`Default` package behavior, environment keys and prefixes, HTTP routes and response shapes,
database/session semantics, scheduler locking, metric names, and observable failures MUST be
treated as consumer-facing contracts. Changes MUST be additive by default. A breaking change
requires explicit scope, migration guidance, updated examples, and an intentional semantic-
version decision. Dependency upgrades, especially pre-v1 `go-ctx` upgrades, MUST be reviewed
for source, lifecycle, configuration, and behavioral compatibility before adoption.

Rationale: the module is compiled into consumer processes, so seemingly local adapter changes
can break application startup, configuration, operations, or shutdown.

### II. go-ctx-Aligned Composition

`go-ctx` MUST remain the sole dependency-injection, configuration, health, statistics, and
application-lifecycle kernel. This repository MUST provide focused adapters and utilities,
not a competing container or hidden application composition root. Applications MUST assemble
services explicitly with `ctx.ServicePackage`; reusable packages MUST expose focused
constructors or package factories. Package dependencies MUST remain acyclic: `actuator` and
`profiler` MAY depend on `httpserver`, `scheduler` MAY depend on `db`, `db` MAY depend on
`utils/channels`, and `profiler` MAY depend on `utils/values`. Generic `utils/*` packages MUST
NOT depend on `go-ctx` or infrastructure adapters. New dependencies or dependency-direction
exceptions require a plan-level rationale.

Rationale: keeping the framework kernel upstream and adapters narrowly layered preserves
replaceability, testability, and a comprehensible composition model.

### III. Deterministic Wiring and Configuration

Service registration MUST use unique, stable names and MUST respect `go-ctx` rules for the
reserved `CTX` name, pointer-to-struct services, reflected lookup, and `ctx`/`env` tags.
Missing dependencies, duplicate names, invalid configuration, unsupported methods, and
invalid provider choices MUST fail visibly. Configuration MUST preserve `go-ctx` source
precedence and lazy-loading semantics; defaults MUST be registered before the first lookup.
Multi-instance adapters MUST use explicit configuration prefixes so a global fallback cannot
silently make multiple listeners, databases, or control-plane components share a resource.
Routes and middleware MUST be registered during initialization, before HTTP servers enter
`AfterStart`. Process-global side effects, including logging and metric registration, MUST be
documented and idempotent within the supported one-active-application-per-process model.

Rationale: reflection and environment fallback trade compile-time visibility for convenience,
so strict naming, phase, and namespace rules are the runtime safety boundary.

### IV. Lifecycle and Concurrency Ownership

Every listener, database pool, scheduler, transaction-backed lock, goroutine, channel, timer,
profile, trace, and blocking operation MUST have an identifiable owner, cancellation path,
and bounded shutdown behavior. Resource-creating services MUST release owned resources through
the `go-ctx` lifecycle, and repeated stop or cleanup calls MUST be safe. Caller-provided
`context.Context` values MUST propagate through request, database, stream, and lock operations;
using `context.Background()` where caller cancellation matters requires explicit justification.
Code MUST NOT assume ordering among concurrent `AfterStart` or disposal callbacks, and it MUST
respect consumer-before-dependency `BeforeStop` ordering. Streaming APIs MUST document
backpressure, error delivery, producer termination, and consumer-abandonment behavior.
Concurrency-sensitive changes MUST pass the race detector.

Rationale: these adapters own process and external resources; leaks, blocked goroutines, and
implicit callback ordering can prevent an application from stopping or restarting safely.

### V. Secure, Observable, Verified Operations

Actuator, metrics, service-topology, and profiler endpoints MUST be treated as control-plane
interfaces. They MUST remain loopback-only or be protected by explicit authentication and
authorization before external exposure. Authentication identity types MUST be consistent
across middleware and handlers; tokens, passwords, DSNs, and other secrets MUST NOT be logged
or emitted by diagnostics. Request size and timeout limits MUST have safe defaults. Metrics
MUST use bounded-cardinality labels, health checks MUST be time-bounded, and failures MUST be
observable through structured logs, health, metrics, or returned errors without leaking
sensitive values. Public behavior changes MUST include meaningful colocated tests and updated
consumer documentation. Tests MUST assert observable contracts rather than private structure.

Rationale: infrastructure endpoints and telemetry are production attack and failure surfaces;
they are useful only when exposure is controlled and behavior is continuously verifiable.

## Architecture and Technical Constraints

- The module path MUST remain `github.com/sedmess/go-ctx-base` unless an explicitly approved
  migration changes every consumer import path. The current Go 1.26 baseline and
  `github.com/sedmess/go-ctx` v0.12.0 integration MUST remain supported until a planned,
  documented upgrade changes them.
- The supported runtime model is the `go-ctx` v0.12.0 in-process container with one active
  application context per process. Application assembly belongs in consumer composition roots;
  `app_example.go` is documentation and validation, not framework-owned domain behavior.
- `httpserver` owns REST transport, route registration, middleware, authentication helpers,
  request limits, graceful shutdown, and HTTP metrics. It MUST NOT own domain logic.
- `db` owns GORM-backed SQLite/PostgreSQL connections, sessions, transactions, pagination,
  health, and database metrics. Consumers own models and migrations. All connection pools MUST
  have lifecycle-bound cleanup.
- `scheduler` owns cron execution and local or PostgreSQL-backed distributed locks. PostgreSQL
  locking MAY depend on `db`; callers MUST release every acquired lock.
- `actuator` exposes framework health, service descriptors, and Prometheus metrics;
  `profiler` exposes Go runtime diagnostics. Both are control-plane adapters and inherit the
  security rule in Principle V.
- `logconfig` configures process-global `slog` output. Reconfiguration and import-time behavior
  MUST be documented because they affect all packages in the process.
- `utils/*` are framework-independent public helpers. Their contracts MUST NOT be silently
  replaced with similarly named `go-ctx/utils/*` APIs; consolidation requires a compatibility
  and migration plan.
- Configuration names and precedence MUST follow `docs/architecture.md` and the pinned
  `go-ctx` architecture contract. Real credentials and secret-bearing DSNs belong in process
  environment or secret stores, never examples, source, logs, metrics labels, or health details;
  test fixtures MUST use obviously non-secret values.

## Development Workflow and Quality Gates

1. A feature specification MUST identify consumer-visible contract, configuration, lifecycle,
   compatibility, security, operability, observability, and documentation outcomes without
   prescribing implementation structure.
2. An implementation plan MUST translate those outcomes into concrete service wiring,
   configuration namespaces, resource and concurrency ownership, packages, and files; show that
   dependency direction remains valid; justify every new dependency; and pass the Constitution
   Check before research and again after design. Exceptions belong in Complexity Tracking.
3. Tasks for behavioral changes MUST include tests. Lifecycle, channel, HTTP, database,
   scheduler, authentication, and configuration changes require failure, cancellation, and
   cleanup scenarios appropriate to the affected contract.
4. Edited Go files MUST be formatted with `gofmt`. The repository MUST pass `go build ./...`,
   `go test ./...`, and `go vet ./...` using the declared Go baseline before completion.
5. Changes affecting goroutines, channels, locks, servers, database sessions, scheduling,
   profiling, or lifecycle callbacks MUST also pass `go test -race ./...`.
6. Reviews MUST verify public compatibility, package direction, configuration isolation,
   resource ownership, security exposure, bounded telemetry, and documentation impact.
   Unjustified complexity or a silently skipped quality gate is a compliance failure.
7. `readme.md`, `docs/architecture.md`, package comments, examples, and migration guidance
   MUST be updated whenever their described contracts change.

## Governance

This constitution is the highest-authority engineering document in the repository. When a
specification, plan, task list, runtime guide, or local convention conflicts with it, the
lower-authority artifact MUST be corrected before implementation proceeds.

Amendments require an explicit documentation change explaining motivation, compatibility and
migration impact, affected principles, and synchronized templates or runtime guides.
Constitution versions use semantic versioning: MAJOR for incompatible principle removals or
redefinitions, MINOR for new principles or materially expanded obligations, and PATCH for
non-semantic clarification.

Every Spec Kit plan MUST perform the Constitution Check before research and again after design.
Every implementation review MUST cite validation commands and account for all applicable MUST
rules. A temporary exception requires written justification in the plan's Complexity Tracking
section and cannot silently waive a principle. `docs/architecture.md` is the implementation-
oriented architecture reference and MUST remain consistent with this constitution and the
pinned `go-ctx` runtime contract.

**Version**: 1.0.0 | **Ratified**: 2026-07-21 | **Last Amended**: 2026-07-21
