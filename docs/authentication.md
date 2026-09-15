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

## iOS client

The app (`ios/DinnerOS/Core/Auth`, `Core/Networking`) uses only Apple frameworks:
`AuthenticationServices`, `CryptoKit`, `Security`, and `URLSession`. There is no
Google SDK.

### Sign in with Apple

1. `SignInWithAppleButton` asks for `.fullName` and `.email`.
2. The app generates a raw nonce (32 bytes from `SecRandomCopyBytes`, base64url)
   and sets `request.nonce` to its **lowercase hex SHA-256**. Apple copies that
   hash into the identity token's `nonce` claim.
3. The app sends `{identityToken, nonce: <raw nonce>, fullName?}` to
   `/auth/apple`. The API hashes the raw nonce and compares it to the claim.
   `fullName` is formatted with `PersonNameComponentsFormatter`, capped at 100
   characters, and omitted when Apple shares no name. Apple only shares it on the
   first authorization.
4. If the API call fails (for example, offline), **Try Again** resends the same
   credential without another Apple prompt.

Capability: `ios/Config/DinnerOS.entitlements` has
`com.apple.developer.applesignin = [Default]`. The App ID must have Sign in with
Apple enabled.

### Sign in with Google (OAuth 2.0 + PKCE, no SDK)

The client ID is the iOS OAuth client from Google Cloud. It's a public
identifier, set as `GOOGLE_IOS_CLIENT_ID` in `ios/Config/Shared.xcconfig` and
read through `Info.plist` → `AppConfiguration.googleIOSClientID`. An empty
value hides the button. The redirect URI comes from the client ID:
`com.googleusercontent.apps.<id>:/oauth2redirect` (the reversed client ID as the
scheme).

```text
app                                    ASWebAuthenticationSession / Google
───                                    ───────────────────────────────────
state, raw nonce, code_verifier  (32 random bytes each, base64url)
code_challenge = BASE64URL(SHA256(code_verifier))
open https://accounts.google.com/o/oauth2/v2/auth
  ?client_id&redirect_uri&response_type=code&scope=openid email profile
  &code_challenge&code_challenge_method=S256&state&nonce        ──▶ user consents
callback <reversed-client-id>:/oauth2redirect?code&state        ◀──
reject unless scheme matches and state == ours
POST https://oauth2.googleapis.com/token (form-encoded)
  client_id, code, code_verifier, grant_type=authorization_code, redirect_uri
                                                                 ──▶ { id_token, … }
POST /api/v1/auth/google { idToken, nonce: <raw nonce> }
```

- `state` is checked **before** the callback's `error` or `code` is read.
  `error=access_denied` counts as a cancellation, so no error is shown.
- Google puts the raw nonce in the ID token unchanged. The API requires the
  claim to equal the `nonce` in the request.
- There's no client secret. iOS clients are public, and PKCE proves this app
  started the authorization. Google's access token is discarded.
- `prefersEphemeralWebBrowserSession = false`, so Safari's Google cookies allow
  one-tap sign-in. The presentation anchor is the key window.
- The backend's `GOOGLE_CLIENT_ID` must be this iOS client ID, because it is the
  ID token's `aud`.

### Session lifecycle (`AuthSession`)

`AuthSession` is an `@Observable`, main-actor-isolated object. It is the only
code that holds DinnerOS tokens. Its states are `restoring`, `signedOut`,
`signedIn(user)`, and `configurationError`. The last one appears when the build
has no valid `API_BASE_URL`, so the app shows an error screen instead of crashing.

- **Launch:** reads the Keychain. No item, or an expired refresh token, means
  `signedOut`. Otherwise the app shows the cached user right away and refreshes
  if the access token expires within 30 seconds. If the server **rejects** the
  refresh (`400`/`401`), the app clears the Keychain and signs out. If the refresh
  fails for a network or `5xx` reason, the app keeps the cached session and the
  next authorized request tries again. Signing out when offline would throw away
  a refresh token that may still be valid.
- **Authorized requests** (`authorized { token in … }`): attach the bearer token,
  refreshing first if it's expired. On `401` (`token_expired` or
  `unauthenticated`), refresh once and retry once. A rejected refresh, or a
  second `401` after a successful refresh, signs out.
- **Refresh serialization:** all state is on the main actor, and the session
  holds at most one in-flight refresh `Task`. Every caller that needs a refresh
  awaits that same task. A caller whose rejected access token has already been
  replaced reuses the new tokens without refreshing. Two refreshes with the same
  token would look like reuse to the API and revoke the sign-in, so this is
  required. Unit tests check that ten concurrent requests getting `401` cause
  exactly one `/auth/refresh`.
- A generation counter changes on every sign-in and sign-out. A refresh that
  finishes after sign-out can't write tokens back.
- **Sign out:** clears the Keychain and in-memory tokens first, then calls
  `POST /auth/logout {refreshToken}` on a best-effort basis. The user is signed out
  locally even if that call fails.

### Keychain

`KeychainTokenStore` saves one generic-password item. The service is
`<bundle id>.auth` and the account is `session`. It holds JSON with the access
token, refresh token, both expiries, and the user summary. It uses
`kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly` and the data-protection
keychain, so it doesn't sync to iCloud or move to another device. An item that
can't be decoded is deleted and treated as signed out. If a Keychain write fails,
the session still works but doesn't survive a relaunch. Tests use
`InMemoryTokenStore`.

### Logging

The app logs with `os.Logger`. It logs error codes, HTTP statuses, and request
IDs. It never logs tokens, nonces, authorization codes, or request bodies.
Every API request sends a generated `X-Request-ID`, and an `APIError` includes
the request ID for support.

### Developer sign-in (Debug builds only)

When a `DEBUG` build has `AppEnvironment == development`, the sign-in screen shows
**Developer sign-in**. It calls `POST /auth/dev` with subject `dev-simulator`. The
button and the `AuthAPI.signInForDevelopment` method are both inside `#if DEBUG`,
so Release binaries don't contain them. See
[development.md](development.md#developer-sign-in-from-the-simulator).

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
| `shopping.edit` | ✅ | ✅ |

No code checks `role == admin` to decide access; it asks `Role.Can`. Adding
`viewer`, `shopper`, `child`, or `guest` means adding a constant and a row in
that table. Unknown roles grant nothing. `recipes.import` guards recipe
imports, `plan.edit` week plans, `pantry.edit` the pantry (including
confirming a shopping order), and `shopping.edit` store settings, saved
products, and shopping handoffs; `recipes.edit` is reserved for a later phase.

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
- The link is `APP_INVITE_URL_BASE` + `#token=` + token, by default
  `https://api.tlps.dev/invite#token=…` (see [Invitation links](#invitation-links)).
  The token is in the URL fragment, which browsers never send, so it can't
  reach the server or Heroku's router logs. The app posts the token to
  `POST /api/v1/invitations/accept` in the body, never in a URL.
- Invitations expire after 7 days. Re-inviting an address revokes its pending
  invitation. Admins can revoke invitations, and revoking is idempotent.
- **Accepting requires a signed-in user, but not a matching email.** Sign in
  with Apple can hide the real address behind a private relay, and people often
  accept from a different account than the one that received the email.
  Holding the secret is the proof, as with any invite link.
- **Previewing requires no sign-in.** `POST /api/v1/invitations/preview` with
  `{token}` or `{code}` returns `{householdName, inviterName, role, expiresAt}`
  for a usable invitation, so the landing page and a signed-out app can say
  which household an invitation is for before anyone joins. It never uses the
  invitation up. `inviterName` is empty when the inviter has no display name.
- Unknown, expired, revoked, and used invitations all return
  `404 invitation_invalid`, from accept and preview alike, and both reject
  malformed input with the same `400`. Preview therefore can't tell an expired
  invitation from a nonexistent one any more than accept can. The reason is
  only logged. Codes are matched case-insensitively, spaces and dashes are
  ignored, and O, I, and L are read as 0, 1, and 1.
- Acceptance and preview are rate limited per client IP before authentication
  runs, and they **share one bucket**: a burst of 10, then 1 request every 6
  seconds, the same budget as sign-in. Previewing adds no guesses, so guessing a
  50-bit code stays infeasible. Creating invitations sends email, so it has its
  own limit: a burst of 20, then 1 every 30 seconds.
- The invitation is marked accepted with a conditional update *before* the
  membership is created, so two people can't both use it. If the user is
  already a member, the invitation is used up and their existing role is kept.
  If the same user retries an invitation they already accepted (for example
  after a lost response), they get their membership back. After they leave the
  household, the used invitation stops working.
- If the email can't be sent, the invitation is kept and the response has
  `emailDelivered: false`, so the admin can still share the code.

### Invitation links

```text
email: https://api.tlps.dev/invite#token=T
  ├─ app installed → iOS opens the app (universal link)
  │     app: preview → "Join <household>?" → accept
  └─ no app → Safari opens GET /invite (landing page)
        script reads #token → POST /api/v1/invitations/preview {token}
        "Join <household> on DinnerOS" + inviter and role
        Open in DinnerOS → dinneros://invite?token=T
        no app yet → TestFlight steps, or open the app and enter the code
```

- `GET /invite` (`api/internal/applinks`) is a small server-rendered page,
  styled like the email and named with `APP_NAME`. It sends
  `Cache-Control: no-store`, `Referrer-Policy: no-referrer`, and a strict CSP:
  `default-src 'none'`, the one inline script and style allowed by SHA-256 hash,
  and `connect-src 'self'`. Its script writes text only, sends the token nowhere
  but the preview request body, and shows "This invitation is no longer valid"
  when preview returns `400` or `404`. The server never receives the token, so
  it can't log it.
- `GET /.well-known/apple-app-site-association` returns the modern format,
  `{"applinks":{"details":[{"appIDs":["<APPLE_TEAM_ID>.<APPLE_BUNDLE_ID>"],"components":[{"/":"/invite"}]}]}}`,
  as `application/json` with no redirect. It returns `404` until
  `APPLE_TEAM_ID` is set.
- The app's Associated Domains entitlement is `applinks:$(APP_LINK_DOMAIN)`
  (`api.tlps.dev`, set in `ios/Config/Shared.xcconfig`). `InviteLink` accepts
  `https://<APP_LINK_DOMAIN>/invite` with the token in the fragment (or query),
  and the custom scheme `dinneros://invite?token=` that the landing page and
  emails sent before universal links use. The app receives universal links
  through `onContinueUserActivity(NSUserActivityTypeBrowsingWeb)` and custom
  scheme links through `onOpenURL`.
- A link never joins silently. `HouseholdStore` previews it while showing
  progress, then asks "Join <household>?" with "<inviter> invited you to join as
  <role>. Joining shares your name with its members." An invalid or expired
  invitation shows "This invitation is no longer valid" instead of a Join
  button. A link opened while signed out waits in memory until sign-in and is
  previewed then. Typed codes are previewed and confirmed the same way.
- An `APP_INVITE_URL_BASE` ending in `=` (such as the old
  `dinneros://invite?token=`) still gets the token appended directly.

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
