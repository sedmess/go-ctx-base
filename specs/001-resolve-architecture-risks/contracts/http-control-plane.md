# HTTP and Control-Plane Contract

**Feature**: `001-resolve-architecture-risks`

## Common Access Behavior

When a component token policy is active, every route owned by that component requires:

```text
Authorization: Bearer <component-token>
```

| Condition | Status | Handler invoked | Credential stored |
|---|---:|---|---|
| Header absent or not a valid Bearer scheme | 401 | No | No |
| Bearer token is present but not accepted | 403 | No | No |
| Bearer token is accepted | Route result | Yes | Deterministic numeric value |

Token checks use secret-safe comparison, and operational output never contains the presented or configured token. Authorization is component-specific: an actuator token does not implicitly authorize profiler routes and vice versa.

When a component is bound only to loopback and no component token policy is configured, its existing unauthenticated local behavior remains available.

## Actuator Routes

Existing successful route contracts remain unchanged:

| Method | Path | Successful response |
|---|---|---|
| GET | `/actuator/health` | 200 with the existing aggregate health JSON shape |
| GET | `/actuator/health/plain` | 200 `text/plain; charset=utf-8` with the aggregate status text |
| GET | `/actuator/services` | 200 with the existing service-description map JSON shape |
| GET | `/actuator/metrics` | Existing Prometheus exposition response |

Access failures occur before health aggregation, topology lookup, or metric gathering.

## Profiler Routes

### Trace

```text
GET /profiler/trace?duration=<duration>
```

### CPU Profile

```text
GET /profiler/cpu_profile?duration=<duration>
```

Duration contract for both routes:

| Input | Result |
|---|---|
| Parameter absent | Existing 15-second duration |
| Valid duration greater than zero and at most 30 seconds | Capture runs for that duration unless canceled |
| Malformed, zero, negative, or greater than 30 seconds | 400 before capture starts |

Successful trace response:

- status 200;
- `Content-Type: application/octet-stream`;
- `Content-Disposition: attachment; filename="trace.pprof"`;
- body is the captured runtime trace.

Successful CPU response:

- status 200;
- `Content-Type: application/octet-stream`;
- `Content-Disposition: attachment; filename="profile.pprof"`;
- body is the captured CPU profile.

### Named Profile

```text
GET /profiler/named_profile?name=<runtime-profile>&debug=<0|1|2>
```

| Input | Result |
|---|---|
| Existing, header-safe runtime profile name | Accepted |
| Missing, unknown, or header-unsafe name | 400 |
| `debug` absent | Existing default `0` |
| `debug` equal to `0`, `1`, or `2` | Accepted |
| Any other or malformed `debug` | 400 |

Success returns status 200, `application/octet-stream`, a safe `<name>.pprof` download filename, and the existing runtime profile representation.

### Admission, Cancellation, and Failures

One process-wide nonblocking gate covers all HTTP trace, CPU, and named-profile work.

| Condition | Result |
|---|---|
| Another profiling endpoint owns the gate | 429, `Retry-After: 1`, no profiling work starts |
| Client cancels the request | Active timer/capture stops and the gate is released |
| Server shutdown begins | Server request context is canceled, active capture stops, and the gate is released |
| Runtime capture fails after valid admission | Existing 500 error path, with a secret-safe structured log |

For new 400 and 429 outcomes, the response body is empty; status and documented headers are the contract. A started trace or CPU profile is stopped exactly once on every success, cancellation, and error path.

## HTTP Request Metrics

Metric names and label keys remain:

| Metric | Type | Labels |
|---|---|---|
| `httpserver_requests_total` | Counter | `server`, `code`, `method`, `path` |
| `httpserver_request_duration` | Histogram | `server`, `code`, `method`, `path` |
| `httpserver_request_bytes` | Histogram | `server`, `code`, `method`, `path` |

The existing duration observation unit and histogram configuration remain unchanged in this feature.

### Label Value Contract

| Label | Value source |
|---|---|
| `server` | Stable server service name |
| `code` | Recorded response status code |
| `method` | `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS`, or `OTHER` |
| `path` | Registered route expression, or `unmatched` when no registered handler ran |

Examples:

| Request | Metric `path` |
|---|---|
| `GET /messages/123?expand=true` matched by `/messages/:id` | `/messages/:id` |
| `GET /static/a/b/c.txt` matched by `/static/*` | `/static/*` |
| `GET /random/5f42...` with no matching route | `unmatched` |
| A global middleware rejects a request before routing | `unmatched` |

Raw paths, identifiers, query strings, authorization values, and credentials are prohibited from metric labels.

## Listener Failure Contract

- Effective listen configuration is resolved using the existing prefix and fallback rules.
- The real TCP listener is reserved during initialization.
- A bind failure is returned from initialization with the stable server name and effective non-secret address.
- go-ctx performs its existing startup cleanup and fatal creation behavior; this feature does not replace that framework boundary with a new return signature.
- No HTTP route is reported ready and no listener begins serving after an initialization failure.
- An already initialized sibling listener is disposed during startup rollback.
