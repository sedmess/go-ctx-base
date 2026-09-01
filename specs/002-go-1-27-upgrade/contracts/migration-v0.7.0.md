# Migration Contract: v0.6.x to v0.7.0

Upgrade development, CI, release, and deployment builders to Go 1.27 or later. Older toolchains
cannot parse the new generic method declarations.

The module pins `github.com/sedmess/go-ctx` v0.12.1. Its `ctx` kernel sources are unchanged from
v0.12.0 and its module baseline is Go 1.27. Remove forced older pins or validate them separately.

Direct stream calls now preserve result types. Method values or expressions without assignment
context may need explicit result type arguments. Interfaces declaring the former non-generic
signatures must use package helpers, concrete adapters, or an operation-specific interface.
Use package `FlatMap` for new code; `FlapMap` remains as a deprecated alias.

The additive `HTTP_MAX_HEADER_VALUE_COUNT` defaults to 500 and follows prefix then global fallback.
Explicit zero, negative, or malformed values fail initialization. Consumers exceeding the limit
must reduce or consolidate separately transmitted values.

The existing protected named-profile endpoint accepts `goroutineleak`. Do not expose it without
the same loopback or token safeguards already required for profiler endpoints.

Service names, routes, lifecycle, authentication, metrics, stream ordering/cancellation/errors,
and module path remain unchanged. No external dependency is added.
