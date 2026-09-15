# API conventions

The DinnerOS API is REST + JSON. The contract lives in
[`api/openapi.yaml`](../api/openapi.yaml) and is updated in the same commit as
any endpoint change.

## Routing and versioning

- Product endpoints: `/api/v1/...`. Breaking changes go to `/api/v2`. Additive
  changes (new fields or endpoints) stay in v1.
- Operational endpoints are unversioned:
  - `GET /health`: liveness. The process is serving HTTP; dependencies are not checked.
  - `GET /ready`: readiness. Returns `200 {"status":"ready","checks":{"mongodb":"ok"}}`,
    or `503` with `"unavailable"` when a dependency is down. Failure details go
    to the logs only.

## Requests

- Content type: `application/json`. Bodies are limited by `HTTP_MAX_BODY_BYTES`.
- Authentication: `Authorization: Bearer <access token>`. See
  [authentication.md](authentication.md) for sign-in, refresh, and error codes.
- Unknown JSON fields are rejected on write endpoints to catch client bugs early.
- Request IDs: the server accepts a well-formed `X-Request-ID` or generates one,
  echoes it in the response, and includes it in every log line for that request.

## Responses

- JSON field names are camelCase. IDs are strings.
- Timestamps are RFC 3339 in UTC (`2026-09-14T18:30:00Z`). Plan dates are
  `YYYY-MM-DD` in the household's time zone.
- Lists use cursor pagination: `?limit=50&cursor=...` returns
  `{ "items": [...], "nextCursor": "..." }`.

### Errors

Every non-2xx response has the same shape:

```json
{
  "error": {
    "code": "not_found",
    "message": "resource not found",
    "requestId": "5f0c1e9a2b7d4c3e8f6a1b2c"
  }
}
```

Handlers decode request bodies with `httpx.DecodeJSON`. It rejects empty
bodies, malformed JSON, unknown fields, wrong types, and trailing data with
`400 invalid_request`, and bodies over the limit with `413 payload_too_large`.

`code` is stable and machine-readable. `message` is for humans and may change.

| Status | Typical codes |
| --- | --- |
| 400 | `invalid_request`, `validation_failed` |
| 401 | `unauthenticated`, `token_expired` |
| 403 | `forbidden` (the caller can see the resource but not perform the action) |
| 404 | `not_found` (also used when the caller may not know the resource exists) |
| 405 | `method_not_allowed` |
| 409 | `conflict` |
| 413 | `payload_too_large` |
| 429 | `rate_limited` |
| 500 | `internal` (details only in server logs) |
| 503 | `provider_unavailable` (sign-in method not configured, or provider keys unreachable) |

## Endpoints

| Method | Path | Auth | Phase | Status |
| --- | --- | --- | --- | --- |
| GET | `/health` | — | 0 | ✅ |
| GET | `/ready` | — | 1 | ✅ |
| POST | `/api/v1/auth/apple` `{identityToken, nonce, fullName?}` → session | rate limited | 2 | ✅ |
| POST | `/api/v1/auth/google` `{idToken, nonce?}` → session | rate limited | 2 | ✅ |
| POST | `/api/v1/auth/refresh` `{refreshToken}` → token pair | rate limited | 2 | ✅ |
| POST | `/api/v1/auth/logout` `{refreshToken}` → `204` | rate limited | 2 | ✅ |
| POST | `/api/v1/auth/dev` `{subject, email?, displayName?}` → session (development only) | rate limited | 2 | ✅ |
| GET | `/api/v1/me` → `{user, identities}` | bearer | 2 | ✅ |
| … | households, invitations, recipes, plans, grocery, events | 3–9 | planned |
