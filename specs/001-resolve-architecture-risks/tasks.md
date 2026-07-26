# Tasks: Architecture Risk Remediation

**Input**: Design documents from `/specs/001-resolve-architecture-risks/`

**Prerequisites**: `plan.md`, `spec.md`, `research.md`, `data-model.md`, `contracts/`, and
`quickstart.md`

**Testing policy**: Tests are REQUIRED by FR-013, FR-014, the project constitution, and the
implementation plan. Regression tests are scheduled before the corresponding behavior changes.
Tests that mutate the default Prometheus registry, process-global logging, runtime profiling, or
the single active go-ctx application run serially and restore their state.

**Organization**: Tasks are grouped by user story. Setup and Foundational repair the shared test
harness only; implementation behavior remains traceable to a story.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: May run in parallel after its phase prerequisites because it touches a different file
  and has no dependency on another unfinished task in the same parallel group.
- **[Story]**: Required for user-story tasks; omitted in Setup, Foundational, and Polish.
- Every implementation task names its exact repository-relative file path and the contract its
  colocated test must prove.

## Phase 1: Setup (Shared Baseline)

**Purpose**: Preserve the declared compatibility baseline before implementation changes begin.

- [X] T001 Verify and preserve the Go 1.26 baseline and `github.com/sedmess/go-ctx v0.12.0` pin in `go.mod` before changing any dependency declaration

---

## Phase 2: Foundational (Blocking Test Isolation)

**Purpose**: Make the existing consumer test harness deterministic so every story can run its
focused and repository-level checks.

**CRITICAL**: Complete this phase before writing user-story regression tests.

- [X] T002 Replace the shared `HTTP_LISTEN` root-test setting with independently cleaned-up `BASE_HTTP_LISTEN`, `ACTUATOR_HTTP_LISTEN`, and `PROFILER_HTTP_LISTEN` values of `127.0.0.1:0` in `app_example_test.go`
- [X] T003 Add reusable root-test start/ready/`Stop().Join()` helpers that never run active go-ctx applications concurrently and can repeat cached package generations in `app_example_test.go`

**Checkpoint**: The existing root composition no longer inherits one socket setting across three
servers, and user-story tests can control complete application generations.

---

## Phase 3: User Story 1 - Dependable Startup and Shutdown (Priority: P1) MVP

**Goal**: Reserve listeners before readiness and give every listener, database pool, metric
registration, scheduled job, and lock lease an idempotent lifecycle owner that survives
`Stop().Join()` and restart.

**Independent Test**: Start the full composition with three distinct ephemeral listeners, acquire
database and scheduler resources, stop and join it, and repeat. A real duplicate bind must fail
before readiness, roll back initialized siblings, and leave the address reusable.

### Tests for User Story 1

- [X] T004 [P] [US1] Add prefix/global/present-empty/port-zero listener tests plus self-subprocess duplicate-bind and sibling-rollback startup-failure tests in `httpserver/rest_server_test.go`
- [X] T005 [US1] Add request-context cancellation, repeated framework-ordered cleanup, persistent-versus-generation registration, invalid/duplicate route, and 100-generation restart tests in `httpserver/rest_server_test.go`
- [X] T006 [P] [US1] Add provisional-init rollback, normal stop, later-service failure disposal, manual/repeated `CloseConnection`, secret-safe failure, and cached-object restart tests in `db/db_connection_test.go`
- [X] T007 [P] [US1] Extend pool-acquisition, active-query, caller-deadline, and connection-generation cancellation coverage in `db/session_context_test.go`
- [X] T008 [P] [US1] Add exact nine-family `gorm_dbstats_*` descriptor/value tests plus unregister and same-name restart tests using an isolated serial registry in `db/metrics_test.go`
- [X] T009 [P] [US1] Add deterministic provider/configuration, advisory-key SQL, local and transaction lease tests for stable contention text, buffered acquisition failure, explicit commit, cancellation/error rollback, shutdown release, repeated/concurrent unlock, and reacquisition in `scheduler/execution_lockers_test.go`
- [X] T010 [P] [US1] Add scheduler generation tests for start/stop/restart, consumer-before-locker shutdown, legacy callback compatibility, and context-aware job cancellation in `scheduler/task_scheduler_test.go`
- [X] T011 [P] [US1] Add an `integration`-tagged PostgreSQL advisory-lock test for cross-locker contention, explicit unlock, cancellation, reacquisition, and pool cleanup in `scheduler/execution_lockers_integration_test.go`
- [X] T012 [P] [US1] Add full-package distinct-listener readiness, complete stop/join, duplicate-endpoint failure, and repeated-generation composition scenarios in `app_example_test.go`

### Implementation for User Story 1

- [X] T013 [P] [US1] Refactor `restServer.Init` in `httpserver/rest_server.go` to resolve the existing prefix/global configuration, create provisional per-run state, reserve the real TCP listener, and roll back locally on every initialization error
- [X] T014 [US1] Complete generation-scoped route/middleware validation, `Serve` activation, request-context cancellation, five-second graceful stop, serve-worker join, idempotent `Dispose`, and restart cleanup in `httpserver/rest_server.go`
- [X] T015 [P] [US1] Document and implement mutex-free pool generations with serialized atomic publication, caller-plus-generation context acquisition, provisional rollback, generation-owned `BeforeStop`/`Dispose` cleanup, and additive concurrent-safe `CloseConnection` without extending `Connection` in `db/db_connection.go`
- [X] T016 [P] [US1] Implement a lifecycle-neutral pull-time `sql.DB.Stats()` Prometheus collector preserving all nine names, gauge types, help meanings, and the `db_name` label in `db/metrics.go`
- [X] T017 [US1] Register, publish, unregister, complete, and restart the pull-time collector with its retained owning pool generation in `db/db_connection.go`
- [X] T018 [US1] Remove the direct `gorm.io/plugin/prometheus` dependency only after the replacement collector is wired, then reconcile `go.mod` and `go.sum` without changing other dependency versions
- [X] T019 [US1] Rework local and PostgreSQL locks into generation-owned close-once lease state machines, preserve provider keys/advisory SQL/error text, use buffered acquisition results and `Session.Tx` commit/rollback semantics, join leases before closing the exact-only scheduler connection, and add idempotent locker stop/disposal in `scheduler/execution_lockers.go`
- [X] T020 [P] [US1] Document and add a fresh scheduler context per initialization, cancel-before-stop behavior, restart-safe state, and `ScheduleTaskCronContext(cron, key string, task func(context.Context))` while preserving existing methods in `scheduler/task_scheduler.go`
- [X] T021 [US1] Exercise the additive context-aware scheduling contract for message cleanup while preserving the example's public composition in `app_example.go`
- [X] T022 [US1] Run the focused listener, root composition, database, database-metric, scheduler, and deterministic lock commands documented in `specs/001-resolve-architecture-risks/quickstart.md`

**Checkpoint**: US1 is independently complete when all owned resources close on normal stop,
cancellation, initialization failure, and later-service failure, and the same cached package
objects complete 100 start/ready/stop generations.

---

## Phase 4: User Story 2 - Safe and Consistent Operational Access (Priority: P2)

**Goal**: Keep safe local diagnostics working while requiring exact component bearer policies
for external or uninspectable exposure, producing one numeric credential contract, and bounding
all runtime profiling work.

**Independent Test**: Exercise actuator and profiler on loopback, non-loopback, and custom
servers with absent, valid, invalid, and rejected component token policies; verify unchanged
successful routes, deterministic numeric credentials, 400/401/403/429 failures, cancellation,
and secret-free output.

### Tests for User Story 2

- [X] T023 [P] [US2] Add strict Bearer-scheme, 401/403/success, preserved Bearer hash, same-identity cross-method numeric Basic hash, zero-value failure, cross-process determinism, and secret-absence tests in `httpserver/auth_middlewares_test.go`
- [X] T024 [P] [US2] Add actual-bound IPv4/IPv6 loopback, wildcard/non-loopback, port-zero, uninitialized, nil, and custom-server classification tests in `httpserver/control_plane_test.go`
- [X] T025 [P] [US2] Add stable actuator factory/service identity, all four success-route shape, and loopback-open/configured-token/non-loopback-refused/custom-server/token-isolation/secret-absence policy tests in `actuator/actuator_controller_test.go`
- [X] T026 [P] [US2] Add stable profiler factory/service identity, route/header/body compatibility, exact token policy, 15-second default parsing, 30-second bound, invalid duration/name/debug, empty-400-body, and secret-safe failure tests in `profiler/profiler_controller_test.go`
- [X] T027 [US2] Add serial nonblocking cross-route admission, empty 429 with `Retry-After: 1`, client/server cancellation, gate reuse, and exactly-once trace/CPU cleanup tests in `profiler/profiler_controller_test.go`

### Implementation for User Story 2

- [X] T028 [P] [US2] Enforce a strict Bearer authorization scheme, preserve accepted Bearer Murmur3 values, and store the same numeric username-derived identity for Basic without retaining passwords in `httpserver/auth_middlewares.go`
- [X] T029 [P] [US2] Preserve `Credential() int64` while returning zero for absent or invalid request-local credential state and document its request-scoped identity meaning in `httpserver/request.go`
- [X] T030 [P] [US2] Document and add `IsLoopbackOnly(RestServer) (bool, error)` using the built-in server's actual reserved listener and fail explicitly for nil, uninitialized, or uninspectable servers in `httpserver/control_plane.go`
- [X] T031 [P] [US2] Parse exact-only `ACTUATOR_HTTP_AUTH_TOKENS`, reject empty entries, compare without secret exposure, enforce tokens on every owned route when configured, fail closed for unproven nonlocal exposure, and preserve service/route contracts in `actuator/actuator_controller.go`
- [X] T032 [P] [US2] Parse exact-only `PROFILER_HTTP_AUTH_TOKENS`, compare without secret exposure, enforce the same exposure matrix, register protected existing routes, and preserve the exported `Controller.Init` signature through an internal error validator in `profiler/profiler_controller.go`
- [X] T033 [US2] Implement pre-admission input validation, the process-wide capacity-one profile gate, request/server-context timers, safe named-profile filenames/debug values, and deferred trace/CPU cleanup while preserving exported profiler methods in `profiler/profiler_controller.go`
- [X] T034 [US2] Remove the example's actuator authentication bypass and demonstrate component-owned control-plane protection without weakening message-route authentication in `app_example.go`
- [X] T035 [US2] Run the focused authentication, listener-scope, actuator, and serial profiler commands documented in `specs/001-resolve-architecture-risks/quickstart.md`

**Checkpoint**: US2 is independently complete when every unsafe exposure is rejected before
route readiness, every authorized route retains its success contract, profiling is bounded and
cancelable, and captured output contains none of the synthetic secrets.

---

## Phase 5: User Story 3 - Cancellation-Safe and Bounded Operation (Priority: P3)

**Goal**: Provide additive context-owned streams, make database streaming cancel as one
operation, derive HTTP telemetry only from finite route/method values, and add direct tests for
previously uncovered public utilities.

**Independent Test**: Cancel stream chains before the first send, after partial consumption,
under backpressure, and after an error; then gather metrics after 10,000 variable requests and
confirm bounded route/method series while all direct package tests pass repeatedly and under
the race detector.

### Tests for User Story 3

- [X] T036 [P] [US3] Add legacy ordering, buffer-capacity, backpressure, `FlapMap` spelling, close ownership, and at-most-once terminal-error compatibility tests in `utils/channels/streaming_test.go`
- [X] T037 [US3] Add context-chain cancellation tests before first send, after partial receive, during blocked send/receive, and after generator error plus race-stress completion assertions in `utils/channels/streaming_test.go`
- [X] T038 [P] [US3] Add paginator compatibility and `SessionContextStream` query/page-send/terminal-error cancellation tests with two-second completion bounds in `db/utils_test.go`
- [X] T039 [P] [US3] Add exact HTTP metric name/label/status/unit tests for parameter routes, splats, pre-routing failures, `unmatched`, supported methods, and `OTHER` in `httpserver/metrics_middleware_test.go`
- [X] T040 [US3] Add a serial 10,000-variable-path/query gather test proving series count is bounded by registered route, status, method, and server combinations in `httpserver/metrics_middleware_test.go`
- [X] T041 [P] [US3] Add direct process-global logging configuration, reconfiguration, level/output, restoration, and synthetic-secret absence tests in `logconfig/configurator_test.go`
- [X] T042 [P] [US3] Add direct empty/nil/order/cardinality mapping contract tests in `utils/slices/mappers_test.go`
- [X] T043 [P] [US3] Add direct present/absent/default/error optional-value contract tests in `utils/values/utils_test.go`

### Implementation for User Story 3

- [X] T044 [P] [US3] Document and add `SingleElemChannelContext`, `SingleElemChannelErrContext`, `SliceToChannelContext`, `CreateChannelContext`, and `CreateChannelBufferedContext` with non-nil operation contexts and cancellation-aware value/error sends in `utils/channels/streaming.go`
- [X] T045 [US3] Document and add `MapContext`, `FlatMapContext`, `ForEachChanElemContext`, and `CollectToSliceContext` with shared cancellation, ordered delivery, one terminal error, and producer-owned close while retaining every legacy API in `utils/channels/streaming.go`
- [X] T046 [US3] Migrate `SessionContextStream` to the context-owned buffered producer so pool work, pagination, value sends, and terminal errors share cancellation while `SessionStream` remains compatible in `db/utils.go`
- [X] T047 [P] [US3] Wrap registered handlers with their finite `rest.Route.PathExp` request metadata before route middleware and preserve all typed/raw handler contracts in `httpserver/request_handlers.go`
- [X] T048 [US3] Read route metadata after handling, use `unmatched` when no route ran, collapse unsupported methods to `OTHER`, and preserve existing Prometheus names, labels, status, duration unit, and histogram settings in `httpserver/metrics_middleware.go`
- [X] T049 [US3] Run the focused streaming, database-stream, HTTP-cardinality, logging, slice, and value commands documented in `specs/001-resolve-architecture-risks/quickstart.md`

**Checkpoint**: US3 is independently complete when context-owned work terminates within two
seconds of cancellation, legacy streams retain their documented drain-required behavior, and
variable request targets cannot create unbounded metric labels.

---

## Phase 6: Polish and Cross-Cutting Concerns

**Purpose**: Synchronize consumer guidance, record migration impact, and run every constitutional
repository gate before claiming a risk is resolved.

- [X] T050 [P] Document listener precedence, exact control-plane token keys, safe proxy exposure, profile limits, lifecycle ownership, additive APIs, and bounded metric labels in `readme.md`
- [X] T051 [P] Document legacy drain-required behavior and the context-owned cancellation/backpressure/terminal-error contract in `utils.md`
- [X] T052 [P] Create the pre-v1 minor-release migration guide covering early bind refusal, exact control-plane tokens, 400/429 profiling statuses, numeric Basic credentials, route-template labels, stop-aware descriptors, and additive context APIs in `docs/migration-architecture-remediation.md`
- [X] T053 [P] Synchronize actual focused test names, commands, bounds, and optional PostgreSQL instructions with `specs/001-resolve-architecture-risks/quickstart.md`
- [X] T054 Run `gofmt` on every edited Go file under `httpserver/`, `actuator/`, `profiler/`, `db/`, `scheduler/`, `utils/`, `logconfig/`, `app_example.go`, and `app_example_test.go`
- [X] T055 Verify `github.com/sedmess/go-ctx v0.12.0` remains pinned, `gorm.io/plugin/prometheus` is no longer direct, and `go build ./...` succeeds from `go.mod`
- [X] T056 Run `go test -count=3 ./...` and record any pre-existing versus feature-caused failure against `specs/001-resolve-architecture-risks/quickstart.md`
- [X] T057 Run `go vet ./...` and record the result against `specs/001-resolve-architecture-risks/quickstart.md`
- [X] T058 Run `go test -race ./...` serially enough to respect process-global fixtures and record the result against `specs/001-resolve-architecture-risks/quickstart.md`
- [X] T059 Follow the optional PostgreSQL command in `specs/001-resolve-architecture-risks/quickstart.md` when a test DSN is available and carry the pass result or explicit optional skip into T060's architecture evidence
- [X] T060 Update the seven-risk table, implemented ownership model, corrective behavior, and exact passing test evidence without claiming skipped proof in `docs/architecture.md`
- [X] T061 Re-audit constitution compliance, public signatures, configuration precedence, route/response shapes, metric names/labels, secret safety, and unchanged factory/service identities in `httpserver/init.go`, `db/init.go`, `scheduler/init.go`, `actuator/init.go`, and `profiler/init.go` against `specs/001-resolve-architecture-risks/plan.md` and `specs/001-resolve-architecture-risks/contracts/`

---

## Dependencies and Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies.
- **Foundational (Phase 2)**: Depends on Setup and blocks all story work because the current root
  test configuration collides before feature behavior can be validated.
- **User Story 1 (Phase 3)**: Depends on Foundational and establishes listener, request-context,
  database-generation, scheduler, and lock ownership used by later stories.
- **User Story 2 (Phase 4)**: Depends on US1's reserved-listener and request-context contracts;
  after that point it can run in parallel with US3.
- **User Story 3 (Phase 5)**: Its stream branch depends on US1 database generations and its HTTP
  metric branch depends on US1 route/server generations; it can run in parallel with US2.
- **Polish (Phase 6)**: Depends on every selected story and on their focused test checkpoints.

### User Story Dependency Graph

```text
Setup -> Foundational -> US1 (P1 MVP)
                              |-> US2 (P2) -|
                              |-> US3 (P3) -|-> Polish and repository gates
```

### User Story Dependencies

- **User Story 1 (P1)**: Foundational only.
- **User Story 2 (P2)**: US1 T013-T014 for trustworthy bound-scope and request-cancellation state;
  US2 otherwise uses additive public contracts.
- **User Story 3 (P3)**: US1 T014 for generation-safe route metadata and T015 for connection
  cancellation; stream and metric sub-branches remain independent of US2.

### Within Each User Story

1. Write regression and contract tests before their corresponding implementation tasks.
2. Implement owner state and additive public APIs before cross-package consumers.
3. Complete package-local behavior before root composition or focused validation.
4. Run the story checkpoint without weakening existing compatibility assertions.
5. Do not start documentation claims of resolution until the applicable tests pass.

## Parallel Opportunities

- All test tasks marked `[P]` can be authored concurrently after their phase prerequisite because
  they use distinct files; process-global tests still execute serially.
- In US1, HTTP generation work (T013), database generation work (T015), the standalone database
  collector (T016), and scheduler context work (T020) can proceed concurrently after tests exist.
- After US1, US2 and US3 can proceed concurrently; within US2 the authentication (T028-T029),
  listener-scope (T030), actuator (T031), and profiler-policy (T032) files form separate branches.
- Within US3, channel implementation (T044) and route metadata (T047) can proceed concurrently;
  T046 follows T044 and T048 follows T047.
- Consumer and migration documents T050-T053 can be updated concurrently after all stories pass
  their focused checks.

## Parallel Example: User Story 1

```text
Task T004: "Add listener bind/configuration failure tests in httpserver/rest_server_test.go"
Task T006: "Add pool generation lifecycle tests in db/db_connection_test.go"
Task T009: "Add lock lease state-machine tests in scheduler/execution_lockers_test.go"
Task T010: "Add scheduler generation tests in scheduler/task_scheduler_test.go"
```

After those tests are in place:

```text
Task T013: "Implement listener generations in httpserver/rest_server.go"
Task T015: "Implement pool generations in db/db_connection.go"
Task T016: "Implement the pull-time collector in db/metrics.go"
Task T020: "Implement scheduler contexts in scheduler/task_scheduler.go"
```

## Parallel Example: User Story 2

```text
Task T023: "Add authentication contract tests in httpserver/auth_middlewares_test.go"
Task T024: "Add listener-scope tests in httpserver/control_plane_test.go"
Task T025: "Add actuator policy tests in actuator/actuator_controller_test.go"
Task T026: "Add profiler contract tests in profiler/profiler_controller_test.go"
```

After T028-T030:

```text
Task T031: "Implement actuator token policy in actuator/actuator_controller.go"
Task T032: "Implement profiler token policy in profiler/profiler_controller.go"
```

## Parallel Example: User Story 3

```text
Task T036: "Add channel compatibility tests in utils/channels/streaming_test.go"
Task T038: "Add database stream tests in db/utils_test.go"
Task T039: "Add bounded HTTP metric tests in httpserver/metrics_middleware_test.go"
Task T041: "Add logging tests in logconfig/configurator_test.go"
Task T042: "Add slice tests in utils/slices/mappers_test.go"
Task T043: "Add value tests in utils/values/utils_test.go"
```

After tests exist:

```text
Task T044: "Implement context-owned channel producers in utils/channels/streaming.go"
Task T047: "Attach route expressions in httpserver/request_handlers.go"
```

## Implementation Strategy

### MVP First

1. Complete Setup and Foundational test isolation.
2. Complete US1 tests before changing lifecycle behavior.
3. Implement listener, pool, scheduler, and lock generations.
4. Run T022 and stop for an MVP review: startup failures must be early, cleanup idempotent, and
   100 generations reusable while go-ctx remains v0.12.0.

### Incremental Delivery

1. Deliver US1 as the resource-safety foundation.
2. Add US2 and demonstrate safe local diagnostics plus protected external diagnostics without
   changing successful route contracts.
3. Add US3 and demonstrate cancellation bounds and metric-series bounds without replacing
   legacy stream APIs.
4. Run focused checks after each story, then complete documentation and the full build/test/vet/
   race gates.

### Requirement Traceability

| Scope | Primary tasks |
|---|---|
| FR-001 to FR-005, SC-001 to SC-003 | T004-T022 |
| FR-006 to FR-009, SC-004 to SC-006 | T023-T035 |
| FR-010 to FR-012, SC-003 and SC-007 | T036-T049 |
| FR-013 to FR-016, SC-008 to SC-010 | T004-T012, T023-T027, T036-T043, T050-T061 |
| go-ctx v0.12.0 and public compatibility | T001, T055, T061 |

## Notes

- `[P]` means different files and no unfinished dependency, not permission to run process-global
  test cases concurrently.
- Tests that expect go-ctx's fatal startup boundary use a self-subprocess rather than replacing
  the v0.12.0 contract.
- The optional PostgreSQL integration test supplements but never replaces deterministic lock
  state tests.
- No task may extend `httpserver.RestServer` or `db.Connection`, rename a service, add a global
  token fallback, expose a raw path metric label, log secrets, or change the pinned framework.
- If implementation cannot satisfy a preserved contract additively, stop before that task,
  document the conflict in `specs/001-resolve-architecture-risks/plan.md`, and request an explicit
  compatibility decision.

## Phase 7: Convergence

- [X] T062 CRITICAL Reconcile the focused commands and resolved-risk evidence in `specs/001-resolve-architecture-risks/quickstart.md` and `docs/architecture.md` after T063-T070, including a command that actually selects `TestHTTPMetricCardinalityIsBoundedAcrossTenThousandTargets`, and cite only scenarios directly proved without treating the optional PostgreSQL skip as live evidence per Constitution V, FR-016, SC-010, T053, and T060 (contradicts)
- [X] T063 Complete `app_example_test.go` with serialized full-composition tests that probe readiness on the base, actuator, and profiler endpoints, perform 100 consecutive start/ready/`Stop().Join()` cycles, and use a duplicate-endpoint startup-failure helper to prove no `AfterStart` readiness plus sibling resource release/reuse while preserving go-ctx v0.12.0 behavior per SC-001, SC-002, US1/AC1, US1/AC2, US1/AC4, T003, and T012 (partial)
- [X] T064 Complete `httpserver/rest_server_test.go` with a real present-empty prefixed-listen precedence case, invalid-route rejection, and persistent-versus-generation route and middleware assertions across restart without extending `RestServer` per FR-013, T004, and T005 (partial)
- [X] T065 Complete `db/db_connection_test.go` with deterministic post-pool/pre-publication failure rollback, collector and SQL-pool release assertions, normal container `BeforeStop` cleanup, and disposal after a later service fails initialization, retaining idempotent restart and secret-safe output per FR-003, US1/AC3, and T006 (partial)
- [X] T066 Complete PostgreSQL lease and scheduler lifecycle coverage in `scheduler/execution_lockers_test.go`, `scheduler/task_scheduler_test.go`, and `scheduler/execution_lockers_integration_test.go`: deterministically prove false/query acquisition completion, explicit-commit versus cancellation/error rollback, shutdown release, consumer-before-locker/private-pool close ordering, and add optional live cancellation, reacquisition, and pool-cleanup scenarios per FR-004, US1/AC3, T009, T010, and T011 (partial)
- [X] T067 Complete `httpserver/auth_middlewares_test.go` with the same table-driven success/401/403 contract for Bearer and Basic, inspection that failed requests retain the zero credential state, and captured response/log/error checks proving synthetic tokens and passwords are absent while preserving accepted Bearer hashes per FR-008, FR-009, SC-006, and T023 (partial)
- [X] T068 Add observable compatibility tests for `AddToDefaultHttpServer`, `AddToHttpServer`, and `RunAsIndependentServer` in both `actuator/actuator_controller_test.go` and `profiler/profiler_controller_test.go`, proving their established controller/server service identities and dependencies without relying only on private constants per FR-015, T025, and T026 (partial)
- [X] T069 Add deterministic, serialized profiler runtime seams and assertions in `profiler/profiler_controller.go` and `profiler/profiler_controller_test.go` proving trace and CPU cleanup executes exactly once on success, start failure, client cancellation, and server cancellation while the admission gate remains reusable and exported contracts stay unchanged per FR-007, FR-013, and T027 (partial)
- [X] T070 Add a controlled cancel-immediately-after-generator-error case and race-stress assertion in `utils/channels/streaming_test.go` proving the producer closes within two seconds, emits no later values, and never blocks or emits more than one terminal error per US3/AC1 and T037 (partial)
