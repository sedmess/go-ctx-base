# Tasks: Upgrade to Go 1.27

**Input**: Design documents from `/specs/002-go-1-27-upgrade/`

**Prerequisites**: `plan.md`, `spec.md`, `research.md`, `data-model.md`, `contracts/`, and
`quickstart.md`

**Testing policy**: Regression tests are required for typed stream methods, HTTP configuration and
protocol rejection, and profiler behavior. Full build, test, vet, and race gates are required.

## Phase 1: Setup (Shared Scope Guards)

**Purpose**: Lock the active feature and preserve user-owned and historical work.

- [X] T001 Verify `.specify/feature.json` targets `specs/002-go-1-27-upgrade/`, snapshot the existing `go.mod` and `go.sum` diff, and confirm `specs/001-resolve-architecture-risks/`, `docs/performance.md`, and `docs/migration-architecture-remediation.md` are excluded

---

## Phase 2: Foundational (Constitution Amendment)

**Purpose**: Authorize and synchronize the new minimum before source uses Go 1.27 syntax.

**CRITICAL**: No user-story implementation starts until this phase is complete.

- [X] T002 Amend the Go and framework compatibility baseline with semantic-version impact accounting in `.specify/memory/constitution.md`
- [X] T003 [P] Update future Go 1.27 planning, validation, and contributor guidance in `.specify/templates/plan-template.md`, `.specify/templates/tasks-template.md`, and `AGENTS.md`

**Checkpoint**: Governing and contributor baselines are Go 1.27 and `go-ctx` v0.12.1 with no temporary exception.

---

## Phase 3: User Story 1 - Adopt the Go 1.27 Baseline (Priority: P1) MVP

**Goal**: Maintainers and consumers have one declared, validated, documented Go 1.27 baseline.

**Independent Test**: Use Go 1.27 to inspect module metadata and build every package while current
guidance agrees and historical records remain unchanged.

### Implementation for User Story 1

- [X] T004 [US1] Verify and preserve the user-owned Go 1.27 and `go-ctx` v0.12.1 declarations in `go.mod` and `go.sum`, and record the v0.12.0-to-v0.12.1 compatibility result in `specs/002-go-1-27-upgrade/research.md`
- [X] T005 [P] [US1] Update the current Go 1.27, `go-ctx` v0.12.1, and v0.7.0 baseline in `readme.md` and `docs/architecture.md` without relabeling historical evidence
- [X] T006 [P] [US1] Publish the approved migration contract from `specs/002-go-1-27-upgrade/contracts/migration-v0.7.0.md` to `docs/migration-v0.7.0.md`

**Checkpoint**: Go 1.27 consumers can identify and compile against the intended baseline before adopting new APIs.

---

## Phase 4: User Story 2 - Preserve Stream Transformation Types (Priority: P2)

**Goal**: Method-style map and flat-map retain concrete result types while compatibility helpers remain.

**Independent Test**: Compile direct inferred calls, contextual and explicit method references,
and typed assignments; validate ordering, empty streams, `any`, cancellation, source/nested errors,
and both flat-map spellings.

### Tests for User Story 2

- [X] T007 [US2] Add failing compile/runtime regressions for typed map/flat-map, method references, `any`, ordering, empty streams, cancellation, source/nested errors, and both flat-map spellings in `utils/channels/streaming_test.go`

### Implementation for User Story 2

- [X] T008 [US2] Add canonical package `FlatMap` and retain `FlapMap` as a deprecated forwarding alias in `utils/channels/streaming.go`
- [X] T009 [US2] Replace the type-erasing `StreamingChan.Map` and `StreamingChan.FlatMap` methods with Go 1.27 generic methods in `utils/channels/streaming.go`
- [X] T010 [US2] Run the focused compile, ordering, cancellation, error, and alias tests for `utils/channels/streaming.go` and `utils/channels/streaming_test.go`
- [X] T011 [P] [US2] Document typed method signatures, invariants, compatibility alias, and method-reference/interface migration in `utils.md`, `readme.md`, and `docs/migration-v0.7.0.md`

**Checkpoint**: Typed method calls work without `any`, and all existing stream ownership semantics pass.

---

## Phase 5: User Story 3 - Apply Safer Go 1.27 Runtime Capabilities (Priority: P3)

**Goal**: Bound HTTP header-value fan-out and expose leaked-goroutine diagnostics through the existing protected route.

**Independent Test**: Prove default/precedence/invalid limit behavior, reject a raw over-limit
request before its handler runs, and retrieve `goroutineleak.pprof` through the existing policy.

### Tests for User Story 3

- [X] T012 [P] [US3] Add default, namespaced precedence, global fallback, malformed/non-positive, and raw over-limit handler-isolation tests in `httpserver/rest_server_test.go`
- [X] T013 [P] [US3] Add Go 1.27 `goroutineleak` lookup, parsing, loopback route, binary filename/content, and token-policy coverage in `profiler/profiler_controller_test.go`

### Implementation for User Story 3

- [X] T014 [US3] Add validated `HTTP_MAX_HEADER_VALUE_COUNT` resolution and assign the effective value to the server generation in `httpserver/rest_server.go`
- [X] T015 [US3] Run focused HTTP and profiler validation for `httpserver/rest_server_test.go` and `profiler/profiler_controller_test.go`
- [X] T016 [P] [US3] Document the new HTTP key, default/counting behavior, over-limit rejection, and protected `goroutineleak` usage in `readme.md`, `docs/architecture.md`, and `docs/migration-v0.7.0.md`

**Checkpoint**: Header fan-out is bounded before application code and diagnostics add no route or security regression.

---

## Phase 6: Polish and Cross-Cutting Validation

**Purpose**: Review modernization, format source, run every constitutional gate, and protect history.

- [X] T017 Run `gofmt` on `utils/channels/streaming.go`, `utils/channels/streaming_test.go`, `httpserver/rest_server.go`, `httpserver/rest_server_test.go`, and `profiler/profiler_controller_test.go`
- [X] T018 Run the Go 1.27 `atomictypes`, `embedlit`, `slicesbackward`, and `unsafefuncs` modernizers with `-diff` and record adopted or inapplicable results in `specs/002-go-1-27-upgrade/research.md` and `specs/002-go-1-27-upgrade/quickstart.md`
- [X] T019 Run `go build ./...` with Go 1.27 and record the result in `specs/002-go-1-27-upgrade/quickstart.md`
- [X] T020 Run `go test ./...` with Go 1.27 and record the result in `specs/002-go-1-27-upgrade/quickstart.md`
- [X] T021 Run `go vet ./...` with Go 1.27 and record the result in `specs/002-go-1-27-upgrade/quickstart.md`
- [X] T022 Run `go test -race ./...` with Go 1.27 and record any platform or pre-existing blocker exactly in `specs/002-go-1-27-upgrade/quickstart.md`
- [X] T023 Run `git diff --check`, prove `specs/001-resolve-architecture-risks/`, `docs/performance.md`, and `docs/migration-architecture-remediation.md` are unchanged, and preserve the original `go.mod` and `go.sum` ownership evidence
- [X] T024 Re-audit FR-001 through FR-016, SC-001 through SC-008, public contracts, package direction, lifecycle ownership, security exposure, documentation, and migration against `specs/002-go-1-27-upgrade/plan.md`

---

## Dependencies and Execution Order

### Phase Dependencies

- Setup starts immediately and establishes protected paths.
- Foundational depends on Setup and blocks Go 1.27 source syntax.
- US1 depends on the constitutional baseline amendment.
- US2 depends on US1's Go 1.27 module baseline; its tests precede implementation.
- US3 depends on US1; HTTP and profiler tests may be written in parallel.
- Polish depends on all user stories.

### User Story Dependency Graph

```text
Setup -> Constitution amendment -> US1 baseline
                                      |-> US2 typed streams -|
                                      |-> US3 runtime safety |-> Polish
```

### Parallel Opportunities

- T003 can update templates/contributor guidance while T002 updates the constitution.
- T005 and T006 touch separate current documentation after T004 verifies the module baseline.
- T012 and T013 use separate packages and can be authored together.
- T011 and T016 touch shared documents and therefore must be serialized despite distinct stories.

## Implementation Strategy

### MVP First

1. Protect existing work and amend the baseline.
2. Verify module/framework compatibility and publish current baseline/migration guidance.
3. Stop at the US1 checkpoint if a minimum-version-only review is desired.

### Incremental Delivery

1. Implement typed stream methods with tests and no concurrency redesign.
2. Add the standard-library HTTP limit and prove protocol-level rejection.
3. Prove `goroutineleak` works through the existing protected profiler surface.
4. Review modernizers, then run build/test/vet/race and documentation audits.

## Requirement Traceability

| Scope | Primary tasks |
|---|---|
| FR-001 to FR-004, SC-001 to SC-002 | T001-T006, T019-T023 |
| FR-005 to FR-009, SC-003 to SC-004 | T007-T011, T017, T020, T022 |
| FR-010 to FR-013, SC-005 to SC-006 | T012-T016, T017, T020, T022 |
| FR-014 to FR-016, SC-007 to SC-008 | T018, T023-T024 |

## Format Validation

All 29 tasks use the required checkbox, sequential ID, optional parallel marker, story label where
applicable, imperative description, and exact repository-relative file path.

---

## Phase 7: User-Requested Dependency Refresh

**Purpose**: Refresh compatible direct and transitive modules without changing the validated
`go-ctx` runtime contract or forcing unrelated transitive-only minimum versions.

- [X] T025 Inventory available direct and transitive updates and identify retracted modules in `go.mod` and `go.sum`
- [X] T026 Apply compatible direct updates, retain `github.com/sedmess/go-ctx` v0.12.1, and normalize the graph with `go mod tidy` in `go.mod` and `go.sum`
- [X] T027 Verify checksums and tidiness, then run build, all-package tests, and vet against `go.mod` and `go.sum`
- [X] T028 Run the official Go 1.27 Linux race suite and record the optional live PostgreSQL skip in `specs/002-go-1-27-upgrade/quickstart.md`
- [X] T029 Document exact direct/transitive updates, the removed retracted version, and remaining transitive-only update reports in `specs/002-go-1-27-upgrade/research.md` and `docs/migration-v0.7.0.md`
