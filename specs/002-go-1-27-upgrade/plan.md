# Implementation Plan: Upgrade to Go 1.27

**Branch**: `develop` | **Date**: 2026-09-01 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/002-go-1-27-upgrade/spec.md`

## Summary

Raise the module's minimum toolchain to Go 1.27 for a v0.7.0 release, validate the
already-present `go-ctx` v0.12.1 pin, and adopt three stable Go 1.27 capabilities that directly
serve existing contracts: generic stream methods, the HTTP request header-value-count limit, and
the generally available `goroutineleak` runtime profile. Preserve public service identity,
configuration precedence, route shapes, lifecycle ownership, stream semantics, package direction,
and all historical feature evidence.

## Technical Context

**Language/Version**: Go 1.27; planning environment verified with Go 1.27.0 on Windows/amd64

**Primary Dependencies**: Existing `github.com/sedmess/go-ctx` v0.12.1; `net/http` and
`runtime/pprof` from Go 1.27; no new external dependency

**Storage**: N/A; no database or persistence contract changes

**Testing**: Go `testing`; compile-oriented generic-method tests; raw HTTP listener tests;
profiler route tests; `go build ./...`, `go test ./...`, `go vet ./...`, and
`go test -race ./...`

**Target Platform**: In-process Go library on Go 1.27-supported platforms; Windows/amd64 local
validation

**Project Type**: Reusable infrastructure-adapter library

**Performance Goals**: Add no stream goroutine, buffer, traversal, or profiler admission work;
reject excess request headers before handlers; retain current sequential transform order

**Constraints**: Preserve one-active-context behavior, service names, route paths, configuration
precedence, exact control-plane token behavior, request cancellation, cleanup, metric names,
bounded profile admission, module path, and dependency graph. Do not edit historical feature 001,
the Go 1.26 performance record, or the previous architecture-remediation migration guide.

**Scale/Scope**: Two generic stream methods and one alias path; one additive HTTP setting and
server field; one existing named-profile capability; colocated tests; module/policy/current docs;
one migration guide; Spec Kit artifacts

**Public API / Compatibility Impact**: The minimum Go version rises from 1.26 to 1.27. Direct
`StreamingChan.Map` and `StreamingChan.FlatMap` calls preserve result types. Exact method values,
method expressions, and interfaces containing the old non-generic signatures may need migration.
Package `FlatMap` is additive and `FlapMap` remains deprecated but functional.

**Configuration Impact**: Add `HTTP_MAX_HEADER_VALUE_COUNT`, resolved with the existing prefix
then global fallback. The default is 500. Explicit malformed, zero, or negative values fail during
`Init`; positive values populate `http.Server.MaxHeaderValueCount`.

**Lifecycle / Concurrency Impact**: No owner or phase changes. Generic methods delegate to existing
transform producers. HTTP parsing rejects over-limit input before adapter middleware. The existing
process-wide profile gate and request/server cancellation protect `goroutineleak` retrieval.

**Documentation Impact**: Amend `.specify/memory/constitution.md`; synchronize `AGENTS.md`,
`.specify/templates/plan-template.md`, `.specify/templates/tasks-template.md`, `readme.md`,
`utils.md`, and `docs/architecture.md`; add `docs/migration-v0.7.0.md`. Historical records stay
unchanged.

## Constitution Check

*GATE: Passed before Phase 0 research and re-checked after Phase 1 design.*

- [x] **Stable contracts**: The explicit upgrade request authorizes the baseline amendment.
  Minimum-version and generic-method compatibility have a v0.7.0 decision and migration path;
  the HTTP setting is additive; profiler routes and responses are unchanged.
- [x] **go-ctx alignment**: A file-hash comparison proves every `go-ctx` v0.12.1 `ctx/*.go` file
  is identical to v0.12.0. Only upstream `utils/channels` Go files differ, and this module does
  not import them. Existing DI, configuration, and lifecycle semantics remain authoritative.
- [x] **Package direction**: Work stays inside `utils/channels`, `httpserver`, and `profiler` plus
  metadata/docs. Utilities still import no adapter or `go-ctx` package; no dependency is added.
- [x] **Deterministic wiring/configuration**: The one new setting uses the existing prefix/global
  lookup. Missing uses 500; invalid or non-positive values fail during initialization.
- [x] **Lifecycle/concurrency ownership**: No new goroutine, listener, timer, channel, or profile
  owner is introduced. Existing transform producers, HTTP generations, and profiler admission
  remain responsible for cleanup and cancellation.
- [x] **Secure operations**: Header parsing gains a finite count limit. `goroutineleak` remains on
  the exact protected named-profile route with existing loopback/token policy and capacity-one gate.
- [x] **Verification/documentation**: Compile/runtime regressions, over-limit raw HTTP behavior,
  profiler access, build/test/vet/race, modernizer review, migration, and current docs are named.

**Pre-research gate result**: PASS. The baseline amendment is explicitly authorized and no
temporary Complexity Tracking exception is required.

**Post-design gate result**: PASS. Research, conceptual state, public/configuration/migration
contracts, and quickstart cover all compatibility, security, ownership, and validation outcomes.

## Project Structure

### Documentation (this feature)

```text
specs/002-go-1-27-upgrade/
|-- spec.md
|-- plan.md
|-- research.md
|-- data-model.md
|-- quickstart.md
|-- checklists/requirements.md
|-- contracts/public-api.md
|-- contracts/configuration.md
|-- contracts/migration-v0.7.0.md
`-- tasks.md
```

### Source Code and Current Project Documents

```text
go.mod                                      # existing Go 1.27 and go-ctx v0.12.1 edits
.specify/memory/constitution.md             # baseline amendment
.specify/templates/plan-template.md         # future Go 1.27 plans
.specify/templates/tasks-template.md        # future Go 1.27 gates
AGENTS.md                                    # contributor baseline
utils/channels/streaming.go                  # typed methods and FlatMap alias
utils/channels/streaming_test.go             # compile/runtime stream regressions
httpserver/rest_server.go                    # header-value-count configuration
httpserver/rest_server_test.go               # configuration and raw-request behavior
profiler/profiler_controller_test.go         # Go 1.27 profile availability and route contract
readme.md                                    # consumer configuration and diagnostic guidance
utils.md                                     # stream API guidance
docs/architecture.md                         # current baseline and adapter contracts
docs/migration-v0.7.0.md                     # published migration guide
```

**Structure Decision**: Retain all existing package boundaries. `httpserver` owns the new request
limit, `profiler` documents/tests the profile already exposed through its named-profile adapter,
and generic helpers remain in the independent `utils/channels` package.

## Phase 0 Research Outcome

[`research.md`](research.md) records the decisions and alternatives. Key outcomes:

- Go 1.27.0 is stable; generic methods, `http.Server.MaxHeaderValueCount`, and the generally
  available `runtime/pprof` `goroutineleak` profile directly fit current contracts.
- JSON v2 and experimental SIMD are excluded because their migration or stability cost exceeds
  their value to this feature; UUID and cryptographic additions are domain-irrelevant.
- `go-ctx` v0.12.1 changes no framework-kernel Go source from v0.12.0, so the user-owned pin can be
  accepted after full repository validation and current-document synchronization.
- A positive explicit HTTP limit is required; absence selects 500; invalid/non-positive input
  fails visibly instead of silently disabling the protection.
- Go 1.27 modernizers will be run with `-diff` and reviewed rather than automatically applied.

## Phase 1 Design Outcome

- [`data-model.md`](data-model.md) defines baseline, transform, header-limit, and diagnostic states.
- [`contracts/public-api.md`](contracts/public-api.md) fixes exact stream signatures and unchanged
  profiler surface behavior.
- [`contracts/configuration.md`](contracts/configuration.md) fixes precedence, default, validation,
  and over-limit behavior.
- [`contracts/migration-v0.7.0.md`](contracts/migration-v0.7.0.md) defines consumer migration.
- [`quickstart.md`](quickstart.md) defines focused and repository-wide validation.

## Complexity Tracking

No constitutional violation or temporary exception is required.
