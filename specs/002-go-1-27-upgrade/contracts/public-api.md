# Public API Contract: Go 1.27 Upgrade

## Typed stream methods

```go
func (ch StreamingChan[T]) Map[Q any](mapper func(T) Q) StreamingChan[Q]
func (ch StreamingChan[T]) FlatMap[Q any](mapper func(T) StreamingChan[Q]) StreamingChan[Q]
```

Direct calls infer `Q`. Contextually typed method values may infer it; otherwise method values and
expressions specify `[Q]`. Generic methods do not satisfy interfaces that declare the former
non-generic signatures.

## Package helpers

```go
func Map[P, Q any](StreamingChan[P], func(P) Q) StreamingChan[Q]
func FlatMap[P, Q any](StreamingChan[P], func(P) StreamingChan[Q]) StreamingChan[Q]
func FlapMap[P, Q any](StreamingChan[P], func(P) StreamingChan[Q]) StreamingChan[Q]
```

`FlapMap` is deprecated but forwards to `FlatMap`. Existing context-aware helpers remain unchanged.
Every path preserves source/nested order, one terminal error, backpressure, cancellation, and
producer-owned close according to its existing context contract.

## Profiler contract

No route or exported method is added. The existing request is:

```text
GET /profiler/named_profile?name=goroutineleak&debug=0
```

It retains existing loopback/token policy, capacity-one admission, error statuses, and binary
download headers. A successful attachment is named `goroutineleak.pprof`.

## Unchanged contracts

Module/package names, service/factory identities, tags, route paths, lifecycle, shutdown, restart,
metrics, logging, and authentication identity remain unchanged.
