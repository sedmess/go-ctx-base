# HTTP and SQLite Performance Baseline

This document defines a reproducible short-run throughput baseline for an HTTP service built
with `go-ctx-base` and the pinned `go-ctx` v0.12.0 runtime. It is a regression reference for
this source tree and test machine, not a production capacity guarantee or SLA.

## Result

On the machine described below, the representative default path produced a short-run median
of approximately **37,000 requests/second** at `GOMAXPROCS=8`. The exact five-sample median
was 37,225 requests/second for the instrumented server returning a 15-byte JSON response.
With the same HTTP stack, one validated GORM primary-key read from shared in-memory SQLite per
request produced a median of **14,830 requests/second**. One explicitly committed update per
request, serialized through the benchmark's deliberately one-connection database pool,
produced **8,918 requests/second** with eight HTTP workers.

The dedicated variant batch produced these independently calculated medians:

| Server and handler | Median req/s | Five-sample range | Median B/op | Median allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Instrumented, framework JSON response | 37,225 | 36,942-38,601 | 8,590 | 144 |
| Instrumented, raw static response | 38,589 | 36,014-39,808 | 8,499 | 142 |
| Silent, framework JSON response | 46,808 | 43,356-47,613 | 5,989 | 79 |
| Silent, raw static response | 43,638 | 41,072-47,764 | 5,916 | 77 |

The raw and JSON ranges overlap, so this run does not establish a meaningful difference
between those two handler styles. Descriptively, the instrumented JSON median was about 20.5%
below the silent JSON median and incurred 65 more combined client/server allocations per
operation. The variants ran in fixed order and no confidence interval or significance test was
calculated, so this delta is a local comparison rather than a statistical claim.

With one request in flight, the instrumented JSON path had a median end-to-end loopback round
trip of 294,709 ns (about 295 microseconds), or 3,393 requests/second. This is an average
serial round trip, not a latency percentile.

## Scaling

`BenchmarkRestServerLoopback` uses one `testing.RunParallel` worker per `GOMAXPROCS` by default.
The representative instrumented JSON path scaled as follows across three recorded batches,
with five samples at each point:

| GOMAXPROCS | In-process clients | Median req/s | Five-sample range |
| ---: | ---: | ---: | ---: |
| 1 | 1 | 10,251 | 10,061-10,322 |
| 2 | 2 | 17,623 | 17,472-19,008 |
| 4 | 4 | 29,567 | 28,994-30,355 |
| 8 | 8 | 37,225 | 36,942-38,601 |
| 16 | 16 | 11,419 | 11,384-11,679 |

The lower 16-thread result was consistent across its five samples, but its cause was not
investigated. It is specific to this same-process Windows loopback setup on an 8-core/16-thread
CPU and must not be generalized as a framework limit. `GOMAXPROCS=8`, the best tested point,
is the chosen local comparison point for this host.

The `ns/op` reported by a parallel Go benchmark is elapsed wall time divided by completed
operations. It is useful for throughput comparison but is not per-request latency under load.

## SQLite end-to-end result

`BenchmarkRestServerSQLite` keeps the default instrumented HTTP stack and adds this request
path:

```text
HTTP GET (read) / POST (update) -> controller -> benchmark service
    -> db.Connection.SessionContext -> GORM -> SQLite -> {"status":"ok"}
```

Every successful request performs one validated workload: either one row `SELECT`, or one row
`UPDATE` inside an explicit transaction and commit. Status and response size are validated for
every request; any HTTP, GORM, pool, transaction, row-count, or final-state error fails the
sample. All recorded samples completed without a reported error. There is no application or
database retry loop.

| Operation | HTTP workers | DB pool | Median req/s | Five-sample range | Median B/op | Median allocs/op | Median DB waits/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Hot primary-key read | 1 | 1 | 3,600 | 2,723-3,937 | 16,314 | 234 | 0 |
| Hot primary-key read | 8 | 8 | 14,830 | 14,462-15,445 | 16,375 | 233 | 0 |
| One-row transaction update | 1 | 1 | 3,443 | 3,274-3,532 | 20,308 | 249 | 0 |
| Serialized one-row transaction update | 8 | 1 | 8,918 | 8,594-9,055 | 20,873 | 252 | 0.9999 |

The medians of the five reported sample-average `ns/op` values were 277,772 ns for the
one-in-flight read and 290,484 ns for the one-in-flight transaction update. They are not
per-request latency percentiles. In the parallel update, almost every session waited for the
single pool connection; median aggregate pool wait was 534,660 ns per completed request. That
pool metric can exceed benchmark `ns/op`: it sums wait time across concurrent requests and is
not request latency. The 8,918 req/s result measures pipelined HTTP work around one serialized
SQLite writer. It is not concurrent write scaling.

The hot read scaled across the tested pool and worker counts as follows:

| GOMAXPROCS | HTTP workers | DB connections | Median req/s | Five-sample range |
| ---: | ---: | ---: | ---: | ---: |
| 1 | 1 | 1 | 5,615 | 5,331-5,749 |
| 2 | 2 | 2 | 8,024 | 6,832-8,143 |
| 4 | 4 | 4 | 12,290 | 11,593-12,557 |
| 8 | 8 | 8 | 14,830 | 14,462-15,445 |

The eight-worker SQLite read median was about 40% of the HTTP-only median measured on the same
host. This comparison describes these two synthetic workloads; it does not assign all of the
difference to SQLite itself because the DB path also adds service dispatch, session contexts,
pool acquisition, GORM mapping, and result validation.

### SQLite workload and configuration

Each benchmark leaf creates a fresh real go-ctx application and a fresh named shared-memory
database. Migration, one-row seeding, connection and hot-row warm-up, HTTP readiness, and
update reset are outside the timer. The schema is:

```text
httpserver_sqlite_benchmark_records(
    id    INTEGER PRIMARY KEY,
    value INTEGER NOT NULL
)
```

The initial and final row count is exactly one. The read selects `id` and `value` for the fixed
existing primary key `id=1` and verifies both fields; this is a hot-cache hit, not a data-size
or random-key benchmark. The update runs `value = value + 1` inside one explicit `Session.Tx`
and commits once per request. It verifies one affected row and, after timing, verifies
`value == b.N`. Inserts are not used, so calibration does not grow the table.

Every operation creates one `SessionContext` from the HTTP request context. The framework's
GORM configuration has prepared statements and translated errors enabled, but
`SessionContext` performs these callbacks on a dedicated `sql.Conn`; this benchmark is not
labeled as prepared-statement throughput. Parallel reads use
`DB_MAX_OPEN_CONNS=DB_MAX_IDLE_CONNS=GOMAXPROCS`; every connection validates its PRAGMAs and
performs the read while held at an untimed barrier. Before timing, the fixture asserts that
max-open, open, and idle counts equal the requested pool size and that no connection is in
use. Serial reads and all writes use max-open/max-idle one. Connection lifetime remains
unlimited. Writes have no retry policy and queue at the `database/sql` pool instead of
creating SQLite lock errors.

The exact DSN shape is:

```text
file:<leaf-name>?mode=memory&cache=shared
  &_pragma=busy_timeout(5000)
  &_pragma=cache_size(-2000)
  &_pragma=foreign_keys(1)
  &_pragma=journal_mode(MEMORY)
  &_pragma=locking_mode(NORMAL)
  &_pragma=page_size(4096)
  &_pragma=synchronous(FULL)
  &_pragma=temp_store(MEMORY)
```

The fixture queries and validates those settings on every physical pooled connection before
measurement. The runtime reported SQLite 3.53.3. The database and journal exist only in
memory, so this is non-durable throughput; `synchronous=FULL` does not turn it into a disk
durability test.

## What the benchmark measures

The benchmark in `httpserver/rest_server_benchmark_test.go`:

- creates a real application through `ctx.CreateContextualizedApplication`;
- initializes a fresh HTTP server and route through go-ctx dependency injection;
- binds an ephemeral `127.0.0.1` listener and sends HTTP/1.1 requests through `net/http`;
- reuses keep-alive connections with an explicitly sized connection pool;
- returns `200 OK` with `{"status":"ok"}` and validates every status and response size;
- excludes application startup, route registration, readiness, and shutdown from the timer;
- creates a separate application and connection pool for every serial or parallel leaf;
- includes routing, the request-size wrapper, response writing, client transport work, and
  allocations on both the client and server sides;
- reports both a serial one-in-flight baseline and parallel throughput.

The `instrumented` server is `NewRestServer`, including access-log processing, Prometheus
request metrics, timer, recorder, and panic recovery. The benchmark pins `SLOG_LEVEL=warn`,
`SLOG_HANDLER=text`, and source/common-tag output off, so debug access-log and info lifecycle
output are deterministic and disabled. The access-log middleware itself remains in the timed
instrumented stack. The `silent` server is `NewRestServerSilent`, which retains panic recovery
but omits the other standard middleware. The framework JSON handler constructs a `Response`
and marshals a small struct; the raw handler writes the same static JSON bytes.

The measured client has a five-second dial timeout and five-second response-header timeout,
but no whole-request `http.Client.Timeout`; the separate readiness client has a five-second
whole-request timeout. Parallel worker requests are created before the benchmark timer starts.

## Measurement environment

- Source state: parent commit `e09820d1c01607687b47b47bcf2c93679a386245` plus the benchmark
  and documentation in this change
- Measured: 2026-07-26
- Go: `go1.26.5 windows/amd64`, `GOAMD64=v1`; module language version `go 1.26`
- Runtime contract: `github.com/sedmess/go-ctx v0.12.0`
- Database stack: `gorm.io/gorm v1.31.2`, `github.com/glebarez/sqlite v1.11.0`,
  `modernc.org/sqlite v1.54.0`, embedded SQLite 3.53.3
- OS: Microsoft Windows 10 Enterprise 10.0.19045, 64-bit
- CPU: AMD Ryzen 7 4800H, 8 physical cores, 16 logical processors, reported 2.9 GHz maximum
- Memory: 39.4 GiB visible; approximately 18.7 GiB free when recorded
- Power plan: Windows Balanced
- `GOGC`: unset, Go default 100
- `GOMEMLIMIT`: unset, Go default off
- `GODEBUG`: unset
- Logging forced by the benchmark: level `warn`, text handler, source and common tags disabled
- Topology: native client, server, and embedded SQLite in the same test process over IPv4
  loopback, no TLS, proxy, VM, or container
- Sample policy: five samples per point, two seconds of calibrated benchmark time per sample;
  medians are reported rather than best results

## Raw samples and aggregation

The exact benchmark result lines and commands are preserved in
[2026-07-26-windows-amd64.txt](performance-results/2026-07-26-windows-amd64.txt). For each
five-sample metric, the values were sorted and the third value was selected as the median. The
range is the minimum through maximum. `B/op` and `allocs/op` were aggregated independently in
the same way; they are not copied from whichever row contained the median throughput.

## Reproducing the baseline

Run the correctness tests first:

```shell
go test ./httpserver -count=1
```

Measure every parallel variant at the host's chosen eight-thread point:

```shell
go test ./httpserver -run '^$' \
  -bench '^BenchmarkRestServerLoopback$/^.*$/^.*$/^parallel$' \
  -benchmem -benchtime=2s -count=5 -cpu=8
```

Measure the representative path across execution-thread counts. Quoting the comma-containing
argument is required in PowerShell:

```shell
go test ./httpserver -run '^$' \
  -bench '^BenchmarkRestServerLoopback$/^instrumented$/^json$/^parallel$' \
  -benchmem -benchtime=2s -count=5 '-cpu=1,2,4,8,16'
```

Measure the one-in-flight round trip:

```shell
go test ./httpserver -run '^$' \
  -bench '^BenchmarkRestServerLoopback$/^instrumented$/^json$/^serial$' \
  -benchmem -benchtime=2s -count=5 -cpu=8
```

Measure SQLite read scaling and the one-in-flight read:

```shell
go test ./httpserver -run '^$' \
  -bench '^BenchmarkRestServerSQLite$/^point-read$/^parallel$' \
  -benchmem -benchtime=2s -count=5 '-cpu=1,2,4,8'

go test ./httpserver -run '^$' \
  -bench '^BenchmarkRestServerSQLite$/^point-read$/^serial$' \
  -benchmem -benchtime=2s -count=5 -cpu=8
```

Measure the serialized SQLite transaction with one and eight HTTP workers:

```shell
go test ./httpserver -run '^$' \
  -bench '^BenchmarkRestServerSQLite$/^serialized-transactional-update$/^.*$' \
  -benchmem -benchtime=2s -count=5 -cpu=8
```

On PowerShell, place each command on one line or replace the shell continuation characters
shown above with PowerShell backticks.

## Interpretation and production testing

The HTTP-only benchmark does not include database calls. Neither benchmark includes a real
network, TLS, reverse proxy, load balancer, container limits, authentication, request bodies,
response compression, actuator scraping, logging output, downstream I/O, application
business logic, or realistic payload sizes. The SQLite extension also excludes disk I/O,
durable WAL and checkpoint behavior, filesystem synchronization, cold caches, growing or
vacuumed data sets, multiple processes, and concurrent writers. Zero `database/sql` pool
waits on the read workload does not prove an absence of lower-level SQLite contention.

Because the load generator shares the process and `GOMAXPROCS` with the server, it also
consumes the CPU being measured. Two-second samples do not establish soak-test stability.
The reported rates are end-to-end HTTP request rates for these exact workloads, not SQLite
engine operations per second. Subtracting independently sampled medians would not isolate
the database layer.

Production capacity must therefore be measured on the intended deployment with an external
load generator. Use representative routes, payloads, middleware, logging, TLS, database and
downstream behavior; sweep concurrency through saturation; record error rate, p50/p95/p99
latency, CPU, memory, garbage collection, and connection-pool utilization; and run long enough
to expose thermal, GC, and resource-limit effects. Set the operating limit from the service's
latency and error objectives with explicit headroom, not from the approximately 37k local
loopback figure.
