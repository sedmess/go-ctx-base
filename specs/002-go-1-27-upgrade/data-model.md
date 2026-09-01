# Conceptual Model: Go 1.27 Upgrade

This feature adds no persisted domain data. The model records public configuration and runtime
states that tests and migration guidance must preserve.

## Compatibility Baseline

- Minimum toolchain: Go 1.27.
- Framework pin: `github.com/sedmess/go-ctx` v0.12.1.
- Intended release: v0.7.0.
- Module path: unchanged.

Invariants: current metadata and guidance agree; historical evidence remains unchanged; framework
kernel lifecycle and configuration semantics remain unchanged.

## Typed Stream Transform

Fields: source type `T`, result type `Q`, mapper, and the existing optional operation context.

```text
source open -> value mapped in order -> result sent -> source continues
source open -> source/nested error -> one terminal error -> result closes
source open -> operation canceled -> owned producer terminates -> result closes
source closed -> result closes
```

Result type is retained at compile time. Ordering, backpressure, cancellation, error, and close
ownership do not change. `FlapMap` and `FlatMap` are equivalent package entry points.

## HTTP Header-Value Limit

Fields: component prefix, namespaced setting, global fallback, positive effective count, and
default 500.

```text
setting absent -> effective 500 -> listener initialized
positive setting -> effective configured count -> listener initialized
malformed/non-positive setting -> initialization failure -> no readiness
request within limit -> normal routing
request over limit -> protocol rejection -> no adapter handler execution
```

## Named Runtime Profile

The `goroutineleak` name with debug 0 flows through the existing route, authentication policy,
loopback classification, admission slot, and binary attachment response. No new route or owner is
created; an empty leak result is still a valid profile.
