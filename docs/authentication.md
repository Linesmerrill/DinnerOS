# Authentication and authorization

Status: authentication is implemented (Phase 2: `api/internal/auth`,
`api/internal/users`). Household authorization and invitations are implemented
(Phase 3: `api/internal/households`, `api/internal/invitations`).

## Providers

V1 supports **Sign in with Apple** and **Sign in with Google**. There is no
email/password authentication.

```text
iOS app                                  DinnerOS API                         Apple / Google
───────                                  ────────────                         ──────────────
generate nonce ─┐
                ├─ provider sign-in ─────────────────────────────────────────▶ identity token (JWT)
                │
POST /api/v1/auth/apple                verify JWT signature via provider JWKS
  { identityToken, nonce, fullName? }  verify iss, aud, exp, iat, nonce
POST /api/v1/auth/google               find AuthIdentity(provider, subject)
  { idToken, nonce? }             ──▶    or create User + AuthIdentity
                                       start a new session (token family)
                                ◀──    { accessToken, accessTokenExpiresAt,
                                         refreshToken, refreshTokenExpiresAt,
                                         user, isNewUser }
store tokens in Keychain
```

### Token verification rules

| Check | Apple | Google |
| --- | --- | --- |
| Signing keys | `https://appleid.apple.com/auth/keys` | `https://www.googleapis.com/oauth2/v3/certs` |
| Algorithm | `RS256` only (anything else, including `HS256` and `none`, is rejected) | `RS256` only |
| `iss` | `https://appleid.apple.com` | `accounts.google.com` or `https://accounts.google.com` |
| `aud` | `APPLE_BUNDLE_ID` | `GOOGLE_CLIENT_ID` (iOS OAuth client) |
| `exp` / `iat` | both required, 60s clock skew | both required, 60s clock skew |
| `sub` | required | required |
| nonce | **required**: the claim must equal the hex SHA-256 of the raw `nonce` in the request | optional: when the request includes `nonce`, the claim must equal it exactly |
| email | trusted only when `email_verified` is true (Apple sends `"true"` as a string or a boolean; both are handled). Apple may return a private relay address. | trusted only when `email_verified` is true |
| name | the token has none; the app sends `fullName` (Apple provides it only on first authorization) | `name` claim |

Signing keys are cached for 1 hour. A token with an unknown `kid` triggers a
refetch (the provider may have rotated keys), at most once per minute, so bogus
key IDs can't flood the provider. If a refresh fails, cached keys keep working.
If keys were never fetched, sign-in returns `503 provider_unavailable`. A
rejected token returns `401 unauthenticated`. The reason (for example "token
has invalid audience") is logged, but the token never is.

The client never sends a bare email or user ID. Identity is established only from
a verified provider token. `fullName` is only a display-name hint for new users.

## Identity model

```text
User (internal id)  1 ──── *  AuthIdentity { provider, subject, email, emailVerified }
```

- `(provider, subject)` is unique. `subject` is the provider's stable user ID.
- A user can attach more login methods later. Accounts are **not** automatically
  merged by email; linking is an explicit, authenticated action.
- Provider tokens are verified and then discarded. They are never stored.
- Emails are recorded only when verified. On each sign-in, `lastUsedAt` and any
  newly verified email are updated on the identity. The user's `displayName` and
  `primaryEmail` are set only when the user is created.
- If two first sign-ins for the same identity race, the unique index lets one
  win. The other deletes the user it just created and signs in to the winner's.

## DinnerOS sessions

| Token | Form | Lifetime | Storage |
| --- | --- | --- | --- |
| Access token | JWT, HS256 signed with `AUTH_TOKEN_SIGNING_KEY`. Claims: `sub`=userId (hex), `iss`=`dinneros`, `aud`=`dinneros-api`, `iat`, `exp`, `jti` | 15 minutes | iOS Keychain; sent as `Authorization: Bearer` |
| Refresh token | 32 random bytes, base64url (43 chars), opaque | 60 days, sliding (each refresh restarts the 60 days) | iOS Keychain; the server stores only its SHA-256 hash in `sessions` |

- **Rotation:** each refresh marks the presented session `rotatedAt`, then issues a
  new access token and a new refresh token in the same family. The rotation is an
  atomic conditional update, so two requests can't both rotate the same session.
- **Reuse detection:** presenting a refresh token that was already rotated or
  revoked sets `revokedAt` on every session in its family and returns 401. This
  includes two *concurrent* refreshes with the same token, so the iOS client
  must serialize refreshes (one in-flight refresh shared by all waiting requests).
- **Logout** (`POST /auth/logout {refreshToken}`) revokes the token's whole family,
  which is that sign-in. It is idempotent (unknown or already revoked tokens still
  get `204`) and doesn't need an access token. The client also clears the Keychain.
- **Expiry:** a session past `expiresAt` is rejected, and MongoDB's TTL index
  deletes it later.
- **Session restoration** on launch reads the Keychain, then refreshes if the
  access token is expired. Refresh failure returns the user to sign-in.
- Keychain items use `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`.

### Authenticated requests

`auth.RequireAuth` wraps protected routes. It requires `Authorization: Bearer`,
accepts only `HS256` tokens signed with our key with the expected `iss` and `aud`
and a valid `exp` (30s leeway), and stores the user ID in the request context
(`auth.UserIDFromContext`). Failures return `401` with a `WWW-Authenticate`
header:

| Code | Meaning | Client action |
| --- | --- | --- |
| `token_expired` | the access token was valid but has expired | refresh, then retry once |
| `unauthenticated` | missing, malformed, forged, or wrong-audience token; or the user no longer exists | refresh; if that fails, return to sign-in |

## Endpoints

| Method and path | Body | Success |
| --- | --- | --- |
| `POST /api/v1/auth/apple` | `{identityToken, nonce, fullName?}` | `200` session |
| `POST /api/v1/auth/google` | `{idToken, nonce?}` | `200` session (`503 provider_unavailable` if `GOOGLE_CLIENT_ID` is unset) |
| `POST /api/v1/auth/refresh` | `{refreshToken}` | `200 {accessToken, accessTokenExpiresAt, refreshToken, refreshTokenExpiresAt}` |
| `POST /api/v1/auth/logout` | `{refreshToken}` | `204` |
| `POST /api/v1/auth/dev` | `{subject, email?, displayName?}` | `200` session. Development only (see below). |
| `GET /api/v1/me` | bearer token | `200 {user, identities: [{provider, email?}]}` |

A session response is
`{accessToken, accessTokenExpiresAt, refreshToken, refreshTokenExpiresAt, user: {id, displayName, primaryEmail?, createdAt}, isNewUser}`.
The full contract is in [`api/openapi.yaml`](../api/openapi.yaml).

### Rate limiting

Every `/api/v1/auth/*` route is rate limited per client IP with an in-memory
token bucket: a burst of 10, then 1 request every 6 seconds. Over the limit
returns `429 rate_limited` with `Retry-After`. The client IP is the **last**
`X-Forwarded-For` entry, which Heroku's router appends, or the connection address
when that header is absent. Idle buckets are evicted after 10 minutes. `/me` is
not rate limited.

### Development login

`POST /api/v1/auth/dev` signs in with an **unverified** identity (provider `dev`)
for the simulator and integration tests. The same `subject` always maps to the
same user. The route is mounted only when `APP_ENV=development` **and**
`AUTH_DEV_LOGIN_ENABLED=true`, and the API refuses to start in production with
that flag set.

```sh
curl -s localhost:8080/api/v1/auth/dev -H 'Content-Type: application/json' \
  -d '{"subject":"simulator-ada","email":"ada@example.com","displayName":"Ada"}'
```

## Authorization

Status: implemented in Phase 3 (`api/internal/households`,
`api/internal/invitations`).

- Every request is authorized **server-side**. Client-side UI hiding is only a
  convenience. Household responses include the caller's `role` and
  `permissions` so the app can hide actions the server would refuse.
- Household access requires a `HouseholdMembership`. Queries are always scoped
  by `householdId`.
- `households.RequirePermission(authorizer, permission, logger)` guards every
  route with a `{householdId}` parameter. It runs after `auth.RequireAuth`,
  loads the caller's membership, and stores it in the request context
  (`households.MembershipFromContext`):
  - not a member, unknown household, or malformed ID → `404 not_found`, so a
    household's existence is never revealed
  - a member whose role lacks the permission → `403 forbidden`
- Service methods take that membership and check the specific permission
  again, so the rules don't depend on the HTTP layer.

### Roles and permissions

One table maps roles to permissions (`rolePermissions` in
`api/internal/households/roles.go`):

```go
type Role string       // "admin", "member"
type Permission string // "household.view", "members.invite", "plan.edit", ...

func (r Role) Can(p Permission) bool
func (r Role) Covers(other Role) bool // r has every permission other has
```

| Permission | admin | member |
| --- | --- | --- |
| `household.view` | ✅ | ✅ |
| `household.update` | ✅ | — |
| `members.view` | ✅ | ✅ |
| `members.invite` | ✅ | — |
| `members.remove` | ✅ | — |
| `members.changeRole` | ✅ | — |
| `plan.edit` | ✅ | ✅ |
| `pantry.edit` | ✅ | ✅ |
| `recipes.edit` | ✅ | ✅ |
| `recipes.import` | ✅ | ✅ |

No code checks `role == admin` to decide access; it asks `Role.Can`. Adding
`viewer`, `shopper`, `child`, or `guest` means adding a constant and a row in
that table. Unknown roles grant nothing. `plan.edit`, `pantry.edit`, and the
`recipes.*` permissions are reserved for later phases.

Rules beyond the table:

- **Granting:** inviting someone with a role, or changing someone to a role,
  requires a role that `Covers` it. You can only change or remove members
  whose role yours covers. Nobody can hand out more access than they have.
- **Leaving:** any member can remove themselves
  (`DELETE .../members/{yourUserId}`) without `members.remove`.
- **Last admin:** a household always has at least one admin. Demoting or
  removing the last admin returns `409 last_admin`. That includes the last
  admin leaving, even when they're the only member, because households can't
  be deleted or left empty yet. The guard is atomic without transactions; see
  [database.md](database.md#households).

### Invitations

- An admin invites an email address with a role. The response includes a
  **code** (`XXXXX-XXXXX`: 10 Crockford base32 characters, 50 bits) that is
  shown only once, so the admin can share it directly. The email contains the
  same code and a link with a separate **token** (32 random bytes, base64url).
  Only SHA-256 hashes of both are stored. Neither is logged or returned again.
- The link is `APP_INVITE_URL_BASE` + token (default `dinneros://invite?token=`;
  universal links can replace it later through configuration). The app posts
  the token to `POST /api/v1/invitations/accept` in the body, never in a URL.
- Invitations expire after 7 days. Re-inviting an address revokes its pending
  invitation. Admins can revoke invitations, and revoking is idempotent.
- **Accepting requires a signed-in user, but not a matching email.** Sign in
  with Apple can hide the real address behind a private relay, and people often
  accept from a different account than the one that received the email.
  Holding the secret is the proof, as with any invite link.
- Unknown, expired, revoked, and used invitations all return
  `404 invitation_invalid`. The reason is only logged. Codes are matched
  case-insensitively, spaces and dashes are ignored, and O, I, and L are read as
  0, 1, and 1.
- Acceptance is rate limited per client IP before authentication runs: a burst
  of 10, then 1 request every 6 seconds, the same budget as sign-in. That makes
  guessing a 50-bit code infeasible. Creating invitations sends email, so it has
  its own limit: a burst of 20, then 1 every 30 seconds.
- The invitation is marked accepted with a conditional update *before* the
  membership is created, so two people can't both use it. If the user is
  already a member, the invitation is used up and their existing role is kept.
  If the same user retries an invitation they already accepted (for example
  after a lost response), they get their membership back. After they leave the
  household, the used invitation stops working.
- If the email can't be sent, the invitation is kept and the response has
  `emailDelivered: false`, so the admin can still share the code.

### Email

`invitations.EmailProvider` is the email seam:

- `resend` posts to the Resend API. Its errors include the HTTP status and
  Resend's message but never the API key.
- `log` sends nothing. It logs the recipient and household, plus the code only
  when `APP_ENV=development`. It never logs the token or link.

Production refuses to start unless `EMAIL_PROVIDER=resend` and `EMAIL_FROM` is
set. Email copy uses `APP_NAME`, escapes every interpolated value, and includes
a plain-text part.

## Secrets

- Nothing secret ships in the iOS app. Apple and Google client IDs are public
  identifiers.
- `AUTH_TOKEN_SIGNING_KEY` and provider settings live in environment
  configuration (see [deployment.md](deployment.md) and `.env.example`). The key
  must be base64 that decodes to at least 32 bytes, and it is required in
  production. In development an empty key is replaced by a random per-process
  key, with a warning at startup. The key is never logged.
- Rotating `AUTH_TOKEN_SIGNING_KEY` invalidates all access tokens. Clients
  recover by refreshing, because refresh tokens don't depend on the key.
- Tokens, refresh tokens, identity tokens, nonces, and cookies are never logged.
  Sign-ins log provider, user ID, and `isNewUser`. Reuse detection logs user ID
  and family ID at warn level.
