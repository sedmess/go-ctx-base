# Architecture Remediation Migration Guide

This guide covers the pre-v1 minor release that resolves the listener, lifecycle, control-plane,
profiling, streaming, and telemetry risks recorded in `docs/architecture.md`. The compatibility
baseline remains Go 1.26 and `github.com/sedmess/go-ctx v0.12.0`; existing exported identifiers,
service names, successful route shapes, metric family/label names, and configuration precedence
remain available.

## Required deployment review

HTTP listeners are now bound during service initialization instead of after application readiness.
A duplicate or invalid effective address therefore refuses startup early and go-ctx performs its
normal initialization rollback. Applications that compose the base, actuator, and profiler
servers must give them distinct prefixed addresses. Ephemeral test compositions should use:

```text
BASE_HTTP_LISTEN=127.0.0.1:0
ACTUATOR_HTTP_LISTEN=127.0.0.1:0
PROFILER_HTTP_LISTEN=127.0.0.1:0
```

Actuator and profiler remain open without tokens only when their actual reserved listener is
proven loopback-only. Nonlocal, wildcard, unknown, or custom exposure requires the exact component
setting:

```text
ACTUATOR_HTTP_AUTH_TOKENS=<comma-separated actuator tokens>
PROFILER_HTTP_AUTH_TOKENS=<comma-separated profiler tokens>
```

There is no global token fallback. Empty entries invalidate the policy. Configure tokens for a
loopback listener too when a reverse proxy exposes it outside the host. Actuator and profiler
tokens are deliberately isolated from one another.

## Corrective behavior changes

- Malformed or missing Bearer authorization returns 401 before the authorization callback;
  rejected tokens return 403.
- A successful Basic request now stores the numeric Murmur3 identity derived from the username,
  matching Bearer's numeric credential representation. Code that expected a string or a panic from
  `RequestData.Credential()` must migrate to the documented `int64`; absent/invalid state is zero.
- Profiler duration values must be greater than zero and no more than 30 seconds. Invalid duration,
  name, or debug values return an empty 400 response.
- Only one trace, CPU, or named-profile request runs process-wide. Concurrent work receives an
  empty 429 response with `Retry-After: 1`.
- HTTP metric `path` values now use the registered route expression, or `unmatched` when routing
  never reaches a handler. Supported method labels are finite; all other tokens become `OTHER`.
  Metric names and the `server`, `code`, `method`, and `path` label keys are unchanged.
- Database metric descriptors remain the nine `gorm_dbstats_*` families with `db_name`, but values
  are collected directly from `sql.DB.Stats()` and disappear when their owning pool generation
  closes. There is no refresh ticker.

## Lifecycle ownership

Listeners, request contexts, SQL pools, database metric registrations, scheduler state, and lock
leases now belong to one initialization generation. Normal stop, initialization rollback, later
service failure, repeated cleanup, and restart converge on idempotent cleanup. PostgreSQL advisory
leases commit on explicit unlock and roll back on cancellation, failure, or locker shutdown.

This may expose latent consumer bugs that retained a session, lock, handler, or scheduled callback
beyond its application generation. Treat the generation context as authoritative and let legacy
scheduled callbacks return before shutdown.

## Additive APIs

No existing interface was extended. Prefer these additive entry points for cancellable or manually
owned work:

```go
db.CloseConnection(connection)
httpserver.IsLoopbackOnly(server)
scheduler.ScheduleTaskCronContext(cron, key, task)

channels.SingleElemChannelContext(ctx, value)
channels.SingleElemChannelErrContext(ctx, value, err)
channels.SliceToChannelContext(ctx, values)
channels.CreateChannelContext(ctx, generator)
channels.CreateChannelBufferedContext(ctx, size, generator)
channels.MapContext(ctx, stream, mapper)
channels.FlatMapContext(ctx, stream, mapper)
stream.ForEachChanElemContext(ctx, callback)
stream.CollectToSliceContext(ctx)
```

`db.SessionContextStream` now uses context-owned production internally. Cancel its context before
abandoning the channel. Legacy contextless streams and `db.SessionStream` retain their established
drain-required behavior.

## Upgrade validation

Run the focused commands and full repository gates in
[`specs/001-resolve-architecture-risks/quickstart.md`](../specs/001-resolve-architecture-risks/quickstart.md).
The PostgreSQL advisory-lock scenario is optional only when no test DSN is available; deterministic
locker tests remain required.
