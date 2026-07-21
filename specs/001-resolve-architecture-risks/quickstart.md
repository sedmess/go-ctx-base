# Validation Quickstart: Architecture Risk Remediation

**Feature**: `001-resolve-architecture-risks`
**Purpose**: Runnable post-implementation validation for the seven architecture risks

This guide is intended for the completed implementation. It does not replace the required assertions in colocated tests. See [public API](contracts/public-api.md), [configuration](contracts/configuration.md), [HTTP/control-plane](contracts/http-control-plane.md), [lifecycle and observability](contracts/lifecycle-observability.md), and the [runtime state model](data-model.md) for the contracts under test.

## 1. Prerequisites

- Go 1.26 or later.
- No other active go-ctx application in the test process.
- Loopback networking available for ephemeral TCP listeners.
- PostgreSQL only for the optional provider integration scenario.

Confirm the toolchain and pinned framework:

```powershell
go version
go list -m github.com/sedmess/go-ctx
```

Expected:

- `go version` reports Go 1.26 or later.
- the module query reports `github.com/sedmess/go-ctx v0.12.0`.

## 2. Listener Configuration and Lifecycle

Run the focused HTTP lifecycle suite:

```powershell
go test ./httpserver -run 'TestRestServer|TestDuplicateListener|TestListenerConfiguration' -count=1
```

Expected:

- prefix-specific values win and the single-server global fallback still works;
- a present-empty prefixed value suppresses the global fallback and retains `net/http` behavior;
- repeated port-zero listeners reserve distinct endpoints;
- a real duplicate bind fails during initialization before serving;
- request cancellation, repeated stop, and 100 completed lifecycle generations leave no listener or worker behind;
- persistent pre-initialization routes/middleware survive restart without duplicate generation registrations.

Run the consumer composition lifecycle and startup-rollback tests:

```powershell
go test . -run 'TestRootComposition' -count=1
```

Expected: `TestRootCompositionRepeatedGenerations` reuses three concrete prefixed endpoints for
100 full start/ready/`Stop().Join()` generations and probes the base, actuator, and profiler
routes every time. `TestRootCompositionDuplicateEndpointRollsBackBeforeReadiness` proves the
framework never reaches `AfterStart`, releases the already initialized sibling listener before
fatal exit, and leaves the address reusable. No fixed port or shared `HTTP_LISTEN` is required.

## 3. Authentication, Control Plane, and Profiling

```powershell
go test ./httpserver -run 'TestBearer|TestBasic|TestCredential|TestAuthentication|TestControlPlane' -count=1
go test ./actuator -count=1
go test ./profiler -count=1
```

Expected:

- Bearer and Basic success paths produce deterministic numeric credentials;
- missing/malformed credentials return 401, rejected credentials return 403, and no password/token appears in output;
- all existing actuator and profiler success routes retain their response shapes;
- loopback without component tokens remains available;
- configured tokens are enforced even on loopback;
- non-loopback or uninspectable control-plane servers without component tokens fail closed;
- omitted profile duration retains 15 seconds at the parsing boundary, invalid bounds return 400, and concurrent profiling returns 429 without starting work;
- cancellation releases the process-wide profiling gate, and deterministic runtime seams prove
  a started trace or CPU capture stops exactly once while a failed start is not stopped.

These tests must remain serial because runtime profiling and some registries are process-global.

## 4. Bounded HTTP Metrics

```powershell
go test ./httpserver -run 'TestHTTPMetric|TestBoundedMetric' -count=1
```

Expected:

- metric names and label keys are unchanged;
- parameterized and splat requests use their registered route expressions;
- unknown routes and pre-routing failures use `path="unmatched"`;
- unsupported method tokens use `method="OTHER"`;
- at least 10,000 requests with varying identifiers and queries create only the documented bounded series combinations.

## 5. Database Lifecycle and Metrics

```powershell
go test ./db -run 'TestConnection|TestSessionContext|TestSessionContextStream|TestDatabaseMetrics' -count=1
```

Expected:

- a pool closes on normal stop, later-service startup failure, and local initialization rollback;
- repeated/concurrent cleanup is safe and the cached connection object starts a fresh generation;
- caller cancellation interrupts pool acquisition, active queries, and streams;
- canceled stream producers close within two seconds and never block attempting a terminal error send;
- the nine existing `gorm_dbstats_*` names and `db_name` label remain available;
- metric registration follows the active pool across restart without a refresh ticker;
- captured errors and health output contain no configured DSN or password.

## 6. Scheduler and Lock Ownership

```powershell
go test ./scheduler -run 'Test(LocalLocker|Locker|PostgresLocker|Scheduler)' -count=1
```

Expected:

- same-key exclusion and different-key independence are preserved;
- explicit unlock, caller cancellation, and locker shutdown release local and simulated transaction leases;
- repeated/concurrent unlock never blocks;
- acquisition failure returns promptly rather than stranding the caller;
- scheduler stop cancels context-aware jobs, joins lock workers, and closes its private database afterward;
- restart builds a fresh scheduler and locker generation.

### Optional PostgreSQL Integration

Supply the test-only DSN through the environment or a test secret store, then run:

```powershell
$env:GO_CTX_BASE_TEST_POSTGRES_DSN='postgres://test_user:test_password@127.0.0.1:5432/test_db?sslmode=disable'
go test -tags=integration ./scheduler -run TestPostgresLockerIntegration -count=1
```

Expected: two lockers contend for one key; explicit unlock and cancellation each make it reacquirable, and shutdown leaves no advisory transaction or pool open. The example credentials are synthetic test-only values.

## 7. Streaming and Previously Untested Utilities

```powershell
go test ./utils/channels -run 'TestLegacyStream|TestContextStream|TestContextTransform|TestContextTerminal' -count=1
go test ./utils/slices ./utils/values ./logconfig -count=1
```

Expected:

- legacy channel ordering, buffer bounds, error delivery, and closing behavior remain compatible;
- context-owned producers and transforms stop when cancellation is signaled before the first value, after partial consumption, or while blocked by backpressure;
- cancellation immediately after a generator error closes the stream within two seconds without later values or a blocked terminal-error send;
- at most one terminal error is delivered and cancellation never blocks trying to send it;
- slice, value, and logging contracts have direct regression coverage;
- log configuration tests restore process-global state and expose no synthetic secret value.

## 8. Required Repository Gates

Format every edited Go file during implementation, then run:

```powershell
go build ./...
go test -count=3 ./...
go vet ./...
go test -race ./...
```

Expected: every command exits successfully. The three repeated test runs contain no listener collision or intermittent lifecycle failure, and the race detector reports no race in listener, pool, lock, stream, profiler, logging, or process-global metric state.

## 9. Success-Criterion Trace

| Criterion | Primary evidence |
|---|---|
| SC-001 and SC-002 | `TestRootCompositionRepeatedGenerations` and `TestRootCompositionDuplicateEndpointRollsBackBeforeReadiness` |
| SC-003 | Bounded stream/database tests plus `TestPostgresLockerCommitRollbackShutdownAndReacquisition` |
| SC-004 | Actuator/profiler loopback, token-protected, non-loopback, and unknown-server matrices |
| SC-005 | `TestProfilerParsingContracts`, admission tests, and `TestProfilerRuntimeCleanupExactlyOnce` |
| SC-006 | `TestAuthenticationMethodsShareFailureAndSecretSafeContract` and cross-process hash tests |
| SC-007 | `TestHTTPMetricCardinalityIsBoundedAcrossTenThousandTargets` selected by the focused command above |
| SC-008 | Direct package tests plus three full-suite runs and the race gate |
| SC-009 | Module-version query and unchanged consumer example compilation |
| SC-010 | Updated architecture risk table linked to the test evidence above |

## 10. Recorded Implementation Result

Validation completed on 2026-07-22 with Go 1.26.5:

| Command | Result |
|---|---|
| `go list -m github.com/sedmess/go-ctx` | PASS: `v0.12.0` |
| `go build ./...` | PASS |
| `go test -count=3 ./...` | PASS for every package on all three runs |
| `go vet ./...` | PASS |
| `go test -race ./...` | PASS for every package |
| `go test -tags=integration ./scheduler -run TestPostgresLockerIntegration -count=1 -v` | OPTIONAL SKIP: `GO_CTX_BASE_TEST_POSTGRES_DSN` not configured |

No pre-existing or feature-caused failures remained in the required gates. The optional skip is
recorded as a skip only and is not claimed as live PostgreSQL evidence.
