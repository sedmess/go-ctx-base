# Migrating from v0.6.x to v0.7.0

v0.7.0 keeps the module path and existing adapter package boundaries. It intentionally raises the
minimum supported toolchain from Go 1.26 to Go 1.27, pins `github.com/sedmess/go-ctx` v0.12.1,
makes typed stream methods preserve their result type, adds an HTTP header-value-count setting, and
documents the Go 1.27 leaked-goroutine profile on the existing profiler endpoint.

## Required toolchain and framework

Upgrade development, CI, release, and deployment builders to Go 1.27 or later. Older Go versions
cannot parse generic method declarations. The v0.12.1 `go-ctx` kernel source is unchanged from
v0.12.0; its module baseline is Go 1.27. Remove forced older dependency pins or validate them
separately.

## Typed stream methods

Direct `StreamingChan.Map` and `StreamingChan.FlatMap` calls preserve their result element type.
Method values or expressions without assignment context may need explicit result type arguments.
Generic methods do not satisfy interfaces containing the former non-generic signatures; use the
package helpers, a concrete adapter, or an operation-specific interface in that case.

Use package `FlatMap` for new code. The historical `FlapMap` spelling remains as a deprecated
forwarding alias. Ordering, backpressure, cancellation, errors, and output ownership are unchanged.

```go
strings := channels.SliceToChannel([]int{1, 2}).Map(strconv.Itoa)
mapValue := channels.SliceToChannel([]int{3}).Map[string]
mapExpression := channels.StreamingChan[int].Map[string]
```

## HTTP request header values

`HTTP_MAX_HEADER_VALUE_COUNT` is additive, defaults to 500, and follows existing component-prefix
then global fallback behavior. Explicit malformed, zero, or negative values fail initialization.
Repeated header lines count separately; comma-separated items on one line count once. Consumers
exceeding the configured count must reduce or consolidate their transmitted values.

## Leaked-goroutine profile

Go 1.27's generally available profile is retrieved through the existing protected endpoint:

```text
GET /profiler/named_profile?name=goroutineleak&debug=0
```

The response remains a binary attachment named `goroutineleak.pprof`. Keep the profiler loopback-
only or configure its exact component bearer tokens before external or reverse-proxy exposure.

## Unchanged behavior

Service names, route paths, configuration precedence, application lifecycle, authentication,
metrics, logging, database and scheduler semantics, and module path remain unchanged. No external
dependency is added by this upgrade.

## Dependency refresh

The v0.7.0 module graph also refreshes existing dependencies: Gomega v1.43.0, Prometheus client
v1.24.1 and client model v0.6.3, PostgreSQL driver v1.6.2, and the 2026-08-24 `x/exp` revision.
Transitive SQLite/modernc, Prometheus, YAML, networking, text, and protobuf modules advance with
them; the retracted `modernc.org/libc` v1.74.3 is replaced by v1.75.6. `go-ctx` remains pinned at
v0.12.1, and no new module is introduced.
