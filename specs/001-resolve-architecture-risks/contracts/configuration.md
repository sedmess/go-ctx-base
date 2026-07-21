# Configuration Contract

**Feature**: `001-resolve-architecture-risks`

## Inherited Precedence

Every existing setting continues to use `go-ctx` v0.12.0 precedence:

1. exact process environment key;
2. uppercase process environment key when the exact spelling is absent;
3. `--NAME=value` argument;
4. `.env_custom`;
5. `.env`;
6. defaults registered before the first lookup.

A present empty process value remains present and suppresses lower-precedence sources. `GetEnvCustomOrDefault(prefix, key)` continues checking `PREFIX_KEY` before `KEY`.

## Listener Settings

| Component | Primary setting | Existing fallback | Default |
|---|---|---|---|
| Default HTTP server | `BASE_HTTP_LISTEN` | `HTTP_LISTEN` | `127.0.0.1:8088` |
| Actuator server | `ACTUATOR_HTTP_LISTEN` | `HTTP_LISTEN` | `127.0.0.1:8089` |
| Profiler server | `PROFILER_HTTP_LISTEN` | `HTTP_LISTEN` | `127.0.0.1:8099` |

The feature does not remove or reorder fallback. The effective value is validated by reserving the real listener during initialization.

- Distinct addresses start independently.
- Repeated loopback port `0` values each receive a distinct operating-system port.
- Two incompatible real binds fail the later initialization before any listener serves.
- A present-empty listen address keeps the current `net/http` empty-address behavior rather than being treated as absent.
- The failure identifies the server name and effective address but includes no secret configuration.

The existing `BASE_HTTP_MAX_HEADER_SIZE`, `BASE_HTTP_READ_TIMEOUT`, `BASE_HTTP_WRITE_TIMEOUT`, component-prefixed equivalents, and global `HTTP_*` fallbacks remain unchanged. `HTTP_MAX_REQUEST_SIZE` remains global.

## Control-Plane Token Settings

| Component | New exact setting | Fallback | Default |
|---|---|---|---|
| Actuator | `ACTUATOR_HTTP_AUTH_TOKENS` | None | Absent |
| Profiler | `PROFILER_HTTP_AUTH_TOKENS` | None | Absent |

Token parsing contract:

- the value is a comma-separated list;
- surrounding whitespace is removed from each entry;
- at least one non-empty token is required when the setting is present;
- any empty entry makes the policy invalid and fails controller initialization;
- duplicate entries are harmless and collapse to one accepted token;
- token comparison does not emit token values to logs, errors, health, metrics, or diagnostics;
- no `HTTP_AUTH_TOKENS` or other global key is consulted as a fallback.

Effective exposure policy:

| Bound scope | Token setting | Result |
|---|---|---|
| Loopback-only | Absent | Existing unauthenticated local access |
| Loopback-only | Valid | Bearer policy enforced |
| Non-loopback | Valid | Bearer policy enforced |
| Non-loopback | Absent or invalid | Startup refused |
| Unknown/custom server | Valid | Bearer policy enforced |
| Unknown/custom server | Absent or invalid | Startup refused |

A reverse proxy can expose a loopback listener without changing its local bind. Such a deployment is contractually external and must configure the component token setting.

## Database and Scheduler Settings

All existing `BASE_DB_*`, `DB_*`, `SCHEDULER_LOCK_PROVIDER`, and `SCHEDULER_DB_*` names and fallback rules remain unchanged. No new connection secret is introduced.

- The default database continues using `BASE_DB_*` with `DB_*` fallback.
- The scheduler's private PostgreSQL connection continues using exact `SCHEDULER_DB_*` settings without global database fallback.
- `SCHEDULER_LOCK_PROVIDER` accepts `LOCAL` or `POSTGRES` case-insensitively and fails visibly for any other value.
- DSNs, usernames/password combinations, and token settings are never copied into failure messages.

## Test Configuration

Consumer composition tests use process-scoped values:

```text
BASE_HTTP_LISTEN=127.0.0.1:0
ACTUATOR_HTTP_LISTEN=127.0.0.1:0
PROFILER_HTTP_LISTEN=127.0.0.1:0
```

This avoids ambient port conflicts and proves namespace isolation. Tests separately cover the retained single-server global fallback.

## Migration Rules

- Existing safe loopback deployments require no configuration change.
- Existing non-loopback actuator or profiler deployments must add their component token setting before upgrading to the remediated release.
- Deployments that intentionally use a reverse proxy must add tokens even though the process binds loopback.
- Existing global listener fallback remains supported, but a composition that resolves two servers to one real endpoint now fails early and must assign distinct prefixed values.
