# Configuration Contract: HTTP Header-Value Count

## Key and precedence

The key is `HTTP_MAX_HEADER_VALUE_COUNT`. For prefix `PREFIX`, resolution is:

1. `PREFIX_HTTP_MAX_HEADER_VALUE_COUNT` when present.
2. `HTTP_MAX_HEADER_VALUE_COUNT` as global fallback.
3. Default `500` when neither is present.

The default server accepts `BASE_HTTP_MAX_HEADER_VALUE_COUNT`; independent actuator and profiler
servers accept their prefixes and otherwise inherit the global key.

## Validation

- Positive decimal integer: accepted.
- Missing: default 500.
- Malformed integer: visible initialization failure through existing conversion.
- Zero or negative integer: descriptive initialization error before readiness.

## Request behavior

The effective count is assigned before serving. Separately transmitted header lines count
separately; comma-separated items on one line count once. An over-limit request is rejected before
go-ctx-base middleware or handlers execute. `HTTP_MAX_HEADER_SIZE` remains independent.
