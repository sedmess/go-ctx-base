# Feature Specification: Upgrade to Go 1.27

**Feature Branch**: `develop` (feature directory `002-go-1-27-upgrade`)

**Created**: 2026-09-01

**Status**: Draft

**Input**: User description: "Upgrade the project to Go 1.27 and use its new features through Spec Kit."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Adopt the Go 1.27 Baseline (Priority: P1)

As a maintainer or consumer, I can build and validate the module against one accurately
documented Go 1.27 minimum and its compatible framework version.

**Why this priority**: The declared toolchain and framework baseline governs every later source
change and is a consumer compatibility contract.

**Independent Test**: With an actual Go 1.27 toolchain, build and validate every package, then
inspect current policy, architecture, module metadata, and migration guidance for one consistent
baseline while historical feature records remain unchanged.

**Acceptance Scenarios**:

1. **Given** a consumer using Go 1.27, **When** they build and test the module, **Then** every
   package and the runnable composition example compile and the required gates pass.
2. **Given** a consumer using an older Go version, **When** they review the migration guide,
   **Then** the new minimum and pre-v1 release impact are explicit before adoption.
3. **Given** the already-present module edits for Go 1.27 and `go-ctx` v0.12.1, **When** the
   upgrade is validated, **Then** those edits are preserved, the framework delta is reviewed,
   and current documentation matches the validated result.

---

### User Story 2 - Preserve Stream Transformation Types (Priority: P2)

As a library consumer, I can transform a typed stream with method syntax and receive a stream of
the mapper's result type without losing that type to `any`.

**Why this priority**: Go 1.27 removes the language limitation behind the existing type-erasing
method workaround, improving compile-time safety without changing stream ownership or ordering.

**Independent Test**: Map and flat-map integer streams to string streams with method syntax,
assign the results directly to typed stream variables, and verify types, values, order, empty
streams, cancellation variants, and error propagation.

**Acceptance Scenarios**:

1. **Given** a typed stream and a mapper returning another type, **When** a consumer calls its map
   method, **Then** the returned stream retains the mapper result type and source order.
2. **Given** a typed stream and a mapper returning typed substreams, **When** a consumer calls its
   flat-map method, **Then** the result retains the nested element type and deterministic order.
3. **Given** a consumer using the historical `FlapMap` package helper, **When** they upgrade,
   **Then** it remains functional and a correctly spelled `FlatMap` alternative is documented.
4. **Given** cancellation or a source or nested-stream error, **When** a transformation runs,
   **Then** its existing cancellation, completion, and terminal-error behavior remains observable.

---

### User Story 3 - Apply Safer Go 1.27 Runtime Capabilities (Priority: P3)

As an operator, I can bound request-header fan-out and retrieve the new leaked-goroutine profile
through the existing protected profiler surface.

**Why this priority**: These stable Go 1.27 capabilities directly strengthen existing HTTP and
diagnostic responsibilities without adding a package, dependency, or externally exposed route.

**Independent Test**: Configure a low header-value limit and prove an over-limit request is
rejected before its handler runs; then retrieve the leaked-goroutine named profile through a
loopback profiler instance and verify the existing authentication and binary-download contract.

**Acceptance Scenarios**:

1. **Given** the default HTTP server configuration, **When** requests are accepted, **Then** no
   more than 500 separately transmitted header values are parsed per request.
2. **Given** a positive namespaced or global header-value-count setting, **When** the server
   initializes, **Then** the namespaced value wins, global fallback remains compatible, and an
   invalid or non-positive value fails visibly before readiness.
3. **Given** a request exceeding the configured value count, **When** it reaches the server,
   **Then** it is rejected before application middleware or handlers process it.
4. **Given** an authorized profiler endpoint, **When** an operator requests the
   `goroutineleak` named profile, **Then** the existing bounded, capacity-one, binary profile
   response is returned without weakening control-plane protection.

### Edge Cases

- A mapper explicitly returns `any`; the method still returns a stream of `any`.
- A mapped source or nested stream is empty, fails, or is abandoned under cancellation.
- A consumer stores a generic stream method as a method value or expression, or declared an
  interface containing the previous non-generic method signature.
- A header contains comma-separated values on one line; it counts as one transmitted value,
  while repeated header lines count separately.
- A header-value setting is absent, invalid text, zero, or negative.
- The leaked-goroutine profile contains no detected leaks; the profile download remains valid.
- Go 1.27 modernizers or new packages are irrelevant to current contracts; they remain out of
  scope instead of causing cosmetic or experimental churn.
- Historical Go 1.26 and feature-001 artifacts remain accurate records and are not relabeled.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The module MUST declare Go 1.27 as its minimum supported language and toolchain.
- **FR-002**: Current project policy, architecture, module metadata, templates, consumer guidance,
  and migration guidance MUST consistently identify the validated Go 1.27 baseline.
- **FR-003**: The already-present `go-ctx` v0.12.1 pin MUST be compatibility-reviewed against
  v0.12.0 and validated without silently replacing the framework lifecycle, configuration, or
  dependency-injection contracts.
- **FR-004**: The minimum-toolchain and public-method compatibility impact MUST be documented for
  an intentional v0.7.0 release while preserving the module path.
- **FR-005**: `StreamingChan.Map` MUST retain the mapper's result element type.
- **FR-006**: `StreamingChan.FlatMap` MUST retain the mapped stream's element type.
- **FR-007**: The channels package MUST provide a correctly spelled package-level `FlatMap` and
  retain `FlapMap` as a deprecated compatibility alias.
- **FR-008**: Package-level transforms and method transforms MUST preserve current ordering,
  backpressure, error, cancellation, and producer-owned close behavior.
- **FR-009**: Typed stream behavior MUST have compile-time and runtime regression coverage for
  concrete and `any` results, method references, empty streams, cancellation, and errors.
- **FR-010**: HTTP servers MUST support the additive `HTTP_MAX_HEADER_VALUE_COUNT` setting with
  existing namespaced-first/global-fallback precedence and a default of 500.
- **FR-011**: Missing settings MUST use the default; malformed, zero, and negative configured
  values MUST fail visibly during initialization without exposing sensitive configuration.
- **FR-012**: Requests exceeding the configured header-value count MUST be rejected before
  application middleware and handlers execute.
- **FR-013**: The existing named-profile endpoint MUST accept Go 1.27's `goroutineleak` profile
  under its existing admission, authentication, loopback, response, and error contracts.
- **FR-014**: New Go 1.27 modernizers MUST be reviewed in non-mutating mode, and experimental or
  unrelated features MUST remain out of scope unless they serve an existing public contract.
- **FR-015**: The upgrade MUST add no external dependency and MUST preserve package direction,
  service names, configuration keys other than the one additive key, route paths, metric names,
  lifecycle ownership, and observable failure safety.
- **FR-016**: Historical performance measurements, migration documents, and feature-001 artifacts
  MUST remain unchanged where their Go 1.26 or v0.12.0 statements describe historical evidence.

### Contract and Operational Impact *(mandatory)*

- **Consumer Contracts**: The minimum Go version becomes 1.27. Stream map methods preserve their
  output type; direct calls improve, while exact method values, method expressions, and interfaces
  using the old signatures may require migration. `FlatMap` is additive and `FlapMap` remains.
- **Configuration Behavior**: `HTTP_MAX_HEADER_VALUE_COUNT` is additive, defaults to 500, and uses
  the established component-prefix then global fallback model. Invalid configured values fail
  during initialization.
- **Lifecycle Outcomes**: No resource owner, goroutine, channel, server phase, shutdown order, or
  restart contract changes. Stream and profiler work retain existing cancellation and admission.
- **Security and Observability**: Header fan-out gains a finite limit. The leaked-goroutine profile
  uses the existing protected profiler route; no new route or weaker exposure policy is introduced.
- **Compatibility and Migration**: The baseline and generic method signatures require a documented
  v0.7.0 migration. Existing successful HTTP routes, stream helpers, and framework behavior remain.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Build, test, vet, and race gates pass with Go 1.27.0 or later.
- **SC-002**: All current baseline statements agree on Go 1.27 and the validated `go-ctx` version,
  while 100% of historical Go 1.26 evidence remains unchanged.
- **SC-003**: Map and flat-map method results assign directly to differently typed stream variables
  in tests with zero type assertions or intermediate `any` conversions.
- **SC-004**: All typed, empty, error, cancellation, canonical-alias, and compatibility-alias stream
  scenarios pass while preserving deterministic order.
- **SC-005**: Tests prove the default header-value count is 500, namespaced precedence and global
  fallback work, invalid values fail before readiness, and an over-limit request never runs its
  handler.
- **SC-006**: An authenticated or loopback-only request for `goroutineleak` returns a successful
  binary profile through the existing endpoint and admission contract.
- **SC-007**: Every Go 1.27 modernizer proposal is either adopted with validation or recorded as
  inapplicable, with zero new external dependencies.
- **SC-008**: Migration guidance accounts for the minimum toolchain, framework pin, generic method
  signatures, flat-map naming, header-limit setting, and leaked-goroutine diagnostic.

## Assumptions

- The explicit upgrade request authorizes the synchronized constitutional baseline amendment
  required by repository governance.
- v0.7.0 is the intended pre-v1 release scope because the current repository tag is v0.6.0 and the
  minimum toolchain plus generic method signatures affect source compatibility.
- The user-owned `go.mod` and `go.sum` edits are intentional inputs: this feature validates and
  documents them but does not claim to have authored or independently chosen them.
- Go 1.27's JSON v2, UUID, cryptographic, and experimental SIMD additions are out of scope because
  they do not improve an existing contract enough to justify migration risk.
