# Quickstart: Validate the Go 1.27 Upgrade

Run from the repository root with Go 1.27 or later. On Windows, isolate build caches:

```powershell
New-Item -ItemType Directory -Force tmp/gocache,tmp/gotmp | Out-Null
$env:GOCACHE = (Resolve-Path tmp/gocache).Path
$env:GOTMPDIR = (Resolve-Path tmp/gotmp).Path
go version
```

Expected: `go version go1.27.0` or later.

## Baseline and dependency

```powershell
go list -m -f '{{.GoVersion}}' github.com/sedmess/go-ctx-base
go list -m github.com/sedmess/go-ctx
```

Expected: Go 1.27 and `github.com/sedmess/go-ctx v0.12.1`.

## Focused validation

```powershell
go test ./utils/channels -run 'Test(GenericMethod|MapMethod|FlatMapMethod|FlatMapPackage|MapAndFlatMap)' -count=1
go test ./httpserver -run 'TestHeaderValueCount' -count=1
go test ./profiler -run 'TestProfiler.*GoroutineLeak' -count=1
```

Expected: typed, method-reference, `any`, order, empty, cancellation, error, alias, HTTP limit, and
existing protected profile response scenarios pass.

## Go 1.27 modernizer review

```powershell
go fix -diff -atomictypes -embedlit -slicesbackward -unsafefuncs ./...
```

Expected: no diff, or every proposed patch is reviewed and validated. Do not apply blindly.

## Required repository gates

```powershell
gofmt -w utils/channels/streaming.go utils/channels/streaming_test.go httpserver/rest_server.go httpserver/rest_server_test.go profiler/profiler_controller_test.go
go build ./...
go test ./...
go vet ./...
go test -race ./...
git diff --check
```

Record each result. Race is required because streams, servers, and profiling exercise concurrency.
If unavailable locally, report the exact failure; ordinary tests are not a substitute.

## Protected-history check

```powershell
git diff -- specs/001-resolve-architecture-risks docs/performance.md docs/migration-architecture-remediation.md
```

Expected: no feature-caused changes. Pre-existing `go.mod` and `go.sum` edits remain user-owned and
are not reverted.

## Validation record (2026-09-01)

| Gate | Result |
|---|---|
| `go version` | PASS: `go1.27.0 windows/amd64` |
| Module baseline queries | PASS: Go `1.27`, `github.com/sedmess/go-ctx v0.12.1` |
| Focused `utils/channels` tests | PASS |
| Focused `httpserver` tests | PASS |
| Focused `profiler` tests | PASS |
| Go 1.27 modernizers | REVIEWED: two unrelated `embedlit` suggestions not applied; other selected analyzers clean |
| `go build ./...` | PASS |
| `go test ./...` | PASS: all 11 packages |
| `go vet ./...` | PASS |
| Local Windows `go test -race ./...` | BLOCKED: `-race requires cgo`; `CGO_ENABLED=0` and no C compiler installed |
| Docker `golang:1.27` `go test -race ./...` | PASS: all 11 packages, Linux/amd64, read-only workspace mount |
| `git diff --check` | PASS; line-ending conversion warnings only |
| Protected historical paths | PASS: no diff in feature 001, performance evidence, or architecture-remediation migration |
| Compatible dependency refresh | PASS: all updateable direct requirements current; `go-ctx` remains v0.12.1 |
| `go mod verify` / `go mod tidy -diff` | PASS: checksums verified and module files tidy |
| Post-refresh build/test/vet | PASS: all 11 packages |
| Post-refresh Docker race suite | PASS: all 11 packages with official `golang:1.27` |
| Optional live PostgreSQL lock test | SKIPPED: `GO_CTX_BASE_TEST_POSTGRES_DSN` not configured |

The passing Linux race run is the race acceptance evidence. The Windows limitation remains stated
separately and was not replaced by the ordinary Windows test suite.
