# Research: Go 1.27 Upgrade

**Date**: 2026-09-01

## Stable release and selected capabilities

**Decision**: Require Go 1.27 and adopt generic methods, `http.Server.MaxHeaderValueCount`, and
the generally available `runtime/pprof` `goroutineleak` profile.

**Rationale**: Go 1.27.0 was released on 2026-08-19. These three stable additions directly improve
existing typed streaming, HTTP request safety, and protected runtime diagnostics. Official evidence:
[release notes](https://go.dev/doc/go1.27) and
[release announcement](https://go.dev/blog/go1.27).

**Alternatives considered**:

- Metadata-only upgrade: rejected because the user asked to use new features.
- Broad standard-library rewrite: rejected because compatibility risk must be tied to existing
  adapter responsibilities.

## Generic stream methods

**Decision**: Replace the type-erasing `StreamingChan.Map` and `StreamingChan.FlatMap` methods with
method-specific result type parameters. Add canonical package `FlatMap`; retain `FlapMap` as a
deprecated forwarding alias. Keep package-level `Map`, `MapContext`, and `FlatMapContext` behavior.

**Rationale**: Go 1.27 permits methods to declare type parameters. Direct calls can now infer the
result element type while delegating to existing package helpers, so no concurrency path changes.
The version-matched `go-ctx` v0.12.1 module uses the same migration pattern.

**Alternatives considered**:

- Keep returning `StreamingChan[any]`: rejected because it wastes the new type-system capability.
- Replace package helpers or redesign streams: rejected because it would broaden lifecycle and
  backpressure risk.

## HTTP header-value count

**Decision**: Add `HTTP_MAX_HEADER_VALUE_COUNT`, default 500, using existing component-prefix then
global fallback. Require a positive explicit value and assign it to
`http.Server.MaxHeaderValueCount` during initialization.

**Rationale**: Go 1.27 defines `http.DefaultMaxHeaderValueCount` as 500. Repeated header lines count
separately; comma-separated values in one line count once. A finite configurable count complements
the existing byte-size limit and rejects header fan-out before application code.

**Alternatives considered**:

- Rely only on `MaxHeaderBytes`: rejected because many small values are a distinct parsing cost.
- Permit zero/negative values: rejected because the Go server maps them back to the default, which
  would make the explicit configuration misleading.
- Add a second middleware limit: rejected because the standard server already owns parsing.

## Leaked-goroutine diagnostics

**Decision**: Keep the existing `/profiler/named_profile` route and explicitly support, test, and
document `name=goroutineleak&debug=0`.

**Rationale**: `goroutineleak` is a predefined `runtime/pprof` profile in Go 1.27. The current
generic named-profile path already validates `pprof.Lookup`, applies loopback/token protection,
uses one process-wide admission slot, and returns a binary attachment. No new route or owner is
needed.

**Alternatives considered**:

- Add `/profiler/goroutineleak`: rejected because it duplicates the named-profile contract.
- Expose the standard `net/http/pprof` mux: rejected because it would bypass existing security and
  bounded-admission controls.

## go-ctx v0.12.1 compatibility

**Decision**: Accept the user's existing v0.12.1 pin after validation and synchronize current docs.

**Rationale**: SHA-256 comparison of all `ctx/*.go` files in locally cached v0.12.0 and v0.12.1
shows no difference. Comparing every Go source file shows changes only in upstream
`utils/channels/streaming.go` and its test; this repository does not import that package. The
upstream `go.mod` change is Go 1.26 to 1.27. Therefore DI, configuration, health, statistics, and
lifecycle kernel code is unchanged, while full local gates still verify integration.

**Alternatives considered**:

- Roll back to v0.12.0: rejected because it would overwrite a user-owned edit and retain a
  dependency whose own module baseline is older than this feature.
- Treat the pin as automatically safe: rejected because pre-v1 updates still require source and
  behavioral review under project policy.

## Other Go 1.27 features and modernizers

**Decision**: Run `atomictypes`, `embedlit`, `slicesbackward`, and `unsafefuncs` through
`go fix -diff`; adopt only reviewed behavior-preserving suggestions. Exclude JSON v2, UUID,
post-quantum crypto, and experimental SIMD from source changes.

**Rationale**: JSON v2 has stricter defaults and may change exact error text; no current public
contract needs UUID or new cryptography; SIMD remains experimental. Modernizer review can find
relevant cleanup without mutating blindly.

**Alternatives considered**:

- Migrate all JSON code: rejected pending a dedicated response/error compatibility plan.
- Enable SIMD: rejected because experimental APIs violate the stable reusable-library baseline.

**Audit result**: The four selected analyzers produced no `atomictypes`, `slicesbackward`, or
`unsafefuncs` diagnostics. `embedlit` suggested moving existing embedded-field assignments into
composite literals at `actuator/actuator_controller.go:79` and `db/db_connection.go:265`. Those
style-only changes are unrelated to the Go 1.27 contracts in this feature and were not applied.
The initial `go fix -diff` rendering also exposed whole-file CRLF/LF noise, so the exact findings
were confirmed non-mutating with the same Go 1.27 fix tool through `go vet -vettool`.

## Release and historical records

**Decision**: Document the change for v0.7.0 and amend the current constitution baseline without
rewriting historical Go 1.26 evidence.

**Rationale**: v0.6.0 is the current repository tag. A pre-v1 minor release is appropriate for a
minimum-toolchain increase and public generic method signatures. Performance measurements and
feature 001 remain time-stamped evidence, not current-baseline documentation.

## Post-implementation dependency refresh

**Decision**: On 2026-09-01, apply compatible updates for every direct requirement with an
available version, retain `github.com/sedmess/go-ctx` v0.12.1, and let `go mod tidy` select the
transitive graph rather than forcing unrelated transitive-only upgrades.

**Result**: Direct updates are Gomega v1.43.0, Prometheus client v1.24.1, Prometheus client model
v0.6.3, PostgreSQL driver v1.6.2, and `golang.org/x/exp` at
`v0.0.0-20260824195058-e88cd73687aa`. The SQLite stack and supporting Prometheus, YAML, networking,
text, protobuf, and modernc modules advanced transitively. In particular, retracted
`modernc.org/libc` v1.74.3 was replaced by v1.75.6. No module was newly introduced.

**Validation**: `go mod verify`, `go mod tidy -diff`, build, all 11 package tests, vet, and the
official Go 1.27 Linux race suite passed. The optional live PostgreSQL lock test was skipped because
`GO_CTX_BASE_TEST_POSTGRES_DSN` was not configured; deterministic database and lock tests passed.

**Remaining update reports**: `go list -m -u all` still reports newer transitive-only modules from
parent module graphs. They were not promoted to explicit minimum-version overrides because this
repository does not directly require their packages and its direct requirements are current.
