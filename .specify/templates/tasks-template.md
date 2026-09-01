---
description: "Task list template for go-ctx-base feature implementation"
---

# Tasks: [FEATURE NAME]

**Input**: Design documents from `/specs/[###-feature-name]/`

**Prerequisites**: `plan.md` and `spec.md` are required; use `research.md`, `data-model.md`,
`contracts/`, and `quickstart.md` when present.

**Testing policy**: Tests are REQUIRED for public behavior, wiring, configuration, lifecycle,
concurrency, HTTP, database, scheduler, authentication, or observability changes. Tests may be
omitted only for documentation-only work or when the plan records a concrete rationale.

**Organization**: Group tasks by user story so each story is independently implementable and
testable. Cross-cutting architecture gates belong in Setup, Foundational, or Polish phases.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: May run in parallel because it touches different files and has no unfinished
  dependency.
- **[Story]**: Required for user-story tasks, for example `[US1]`; omit in shared phases.
- Every task MUST start with `- [ ] T###` and include an exact repository-relative file path.

## Path Conventions

- Go packages live directly under repository-root directories such as `httpserver/`, `db/`,
  `scheduler/`, `actuator/`, `profiler/`, `logconfig/`, and `utils/`.
- Tests are colocated with implementation as `*_test.go`.
- Consumer-level composition lives in `app_example.go` and `app_example_test.go`.
- Architecture documentation lives in `docs/architecture.md`; feature artifacts live under
  `specs/[###-feature-name]/`.
- Replace every `path/to/...` example below with a real path from `plan.md`.

<!--
  SAMPLE TASKS FOLLOW. /speckit-tasks MUST replace them with feature-specific work derived from
  the user stories, requirements, contracts, data model, research, constitution, and plan.
  Do not retain unused samples in a generated tasks.md.
-->

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Prepare files, dependencies, fixtures, and validation commands shared by all stories.

- [ ] T001 Confirm affected package paths and public contracts in specs/[###-feature-name]/plan.md
- [ ] T002 [P] Add shared test fixtures in path/to/fixture_test.go
- [ ] T003 [P] Verify the Go 1.27 and pinned framework baseline in go.mod when explicitly planned

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Complete architecture and safety work that blocks every user story.

**CRITICAL**: No user-story implementation starts until this phase is complete.

- [ ] T004 Define stable service names and package assembly in path/to/init.go
- [ ] T005 [P] Add isolated environment-prefix handling in path/to/config.go
- [ ] T006 [P] Add lifecycle cleanup and cancellation ownership in path/to/service.go
- [ ] T007 Add control-plane protection and bounded telemetry in path/to/handler.go
- [ ] T008 Add colocated failure-path tests in path/to/service_test.go
- [ ] T009 Update architecture contracts in docs/architecture.md

**Checkpoint**: Constitution gates pass and shared contracts are ready for story work.

---

## Phase 3: User Story 1 - [Title] (Priority: P1) MVP

**Goal**: [What this story delivers]

**Independent Test**: [Observable scenario that proves this story works alone]

### Tests for User Story 1

> Schedule regression and contract tests before their implementation. Demonstrate the pre-fix
> failure when practical.

- [ ] T010 [P] [US1] Add public contract test in path/to/contract_test.go
- [ ] T011 [P] [US1] Add lifecycle or integration test in path/to/integration_test.go

### Implementation for User Story 1

- [ ] T012 [P] [US1] Add or update public types in path/to/types.go
- [ ] T013 [P] [US1] Add or update configuration and wiring in path/to/init.go
- [ ] T014 [US1] Implement service behavior in path/to/service.go
- [ ] T015 [US1] Implement the adapter or endpoint in path/to/handler.go
- [ ] T016 [US1] Add visible validation and failure handling in path/to/handler.go
- [ ] T017 [US1] Add bounded logs, health, or metrics in path/to/observability.go

**Checkpoint**: User Story 1 is independently functional and testable.

---

## Phase 4: User Story 2 - [Title] (Priority: P2)

**Goal**: [What this story delivers]

**Independent Test**: [Observable scenario that proves this story works alone]

### Tests for User Story 2

- [ ] T018 [P] [US2] Add public contract test in path/to/contract_test.go
- [ ] T019 [P] [US2] Add lifecycle or integration test in path/to/integration_test.go

### Implementation for User Story 2

- [ ] T020 [P] [US2] Add or update public types in path/to/types.go
- [ ] T021 [US2] Implement service behavior in path/to/service.go
- [ ] T022 [US2] Implement the adapter or endpoint in path/to/handler.go
- [ ] T023 [US2] Integrate with User Story 1 through public contracts in path/to/integration.go

**Checkpoint**: User Stories 1 and 2 both work independently.

---

## Phase 5: User Story 3 - [Title] (Priority: P3)

**Goal**: [What this story delivers]

**Independent Test**: [Observable scenario that proves this story works alone]

### Tests for User Story 3

- [ ] T024 [P] [US3] Add public contract test in path/to/contract_test.go
- [ ] T025 [P] [US3] Add lifecycle or integration test in path/to/integration_test.go

### Implementation for User Story 3

- [ ] T026 [P] [US3] Add or update public types in path/to/types.go
- [ ] T027 [US3] Implement service behavior in path/to/service.go
- [ ] T028 [US3] Implement the adapter or endpoint in path/to/handler.go

**Checkpoint**: All requested stories are independently functional.

---

[Add more user-story phases as needed, preserving priority order and traceability.]

---

## Phase N: Polish and Cross-Cutting Concerns

**Purpose**: Complete contract, security, observability, documentation, and repository gates.

- [ ] TXXX [P] Update consumer guidance in readme.md
- [ ] TXXX [P] Update package and runtime architecture in docs/architecture.md
- [ ] TXXX Add compatibility or migration guidance in path/to/migration.md
- [ ] TXXX Add missing failure, cancellation, cleanup, or restart tests in path/to/service_test.go
- [ ] TXXX Run gofmt on every edited Go file
- [ ] TXXX Run go build ./..., go test ./..., and go vet ./...
- [ ] TXXX Run go test -race ./... for lifecycle, server, DB, lock, stream, or concurrency work
- [ ] TXXX Run the scenarios in specs/[###-feature-name]/quickstart.md

---

## Dependencies and Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies.
- **Foundational (Phase 2)**: Depends on Setup and blocks every user story.
- **User Stories (Phase 3+)**: Depend on Foundational; independent stories may run in parallel.
- **Polish (Final Phase)**: Depends on every story selected for the delivery.

### User Story Dependencies

- **User Story 1 (P1)**: [Dependencies or "Foundational only"]
- **User Story 2 (P2)**: [Dependencies or "Foundational only"]
- **User Story 3 (P3)**: [Dependencies or "Foundational only"]

Dependencies between stories require explicit justification; integration must use public
contracts and preserve each story's independent test.

### Within Each User Story

1. Regression and contract tests before corresponding implementation tasks.
2. Public types and configuration before service behavior.
3. Service behavior before adapters or endpoints.
4. Core behavior before cross-package integration.
5. Story checkpoint before dependent story work.

### Parallel Opportunities

- Tasks marked `[P]` touch different files and have no unfinished dependency.
- Test fixtures and documentation may run in parallel with independent implementation files.
- Different stories may run in parallel only after Foundational is complete and when their
  files and public contracts do not conflict.

## Parallel Example: User Story 1

```text
Task: "Add public contract test in httpserver/request_handlers_test.go"
Task: "Add lifecycle integration test in httpserver/rest_server_test.go"
```

## Implementation Strategy

### MVP First

1. Complete Setup.
2. Complete Foundational and re-check constitution gates.
3. Complete User Story 1 tests and implementation.
4. Stop and validate User Story 1 independently.
5. Run applicable repository gates before demo or release.

### Incremental Delivery

1. Deliver the P1 story as a complete, tested slice.
2. Add later stories in priority order without breaking earlier contracts.
3. Re-run affected integration and race gates after each slice.
4. Finish cross-cutting documentation and full repository validation.

## Notes

- `[P]` means different files and no dependency, not merely "could be worked on at once."
- `[US#]` provides traceability to a user story.
- Every behavior task must identify its colocated test or an approved documentation-only reason.
- Service names, environment prefixes, route paths, metrics, and lifecycle behavior are public
  contracts and must appear explicitly in task descriptions when affected.
- Do not generate vague tasks, omit file paths, introduce package cycles, or hide constitution
  exceptions.
