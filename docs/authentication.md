# Authentication and authorization

Status: planned for Phase 2 (authentication) and Phase 3 (household roles).

## Providers

V1 supports **Sign in with Apple** and **Sign in with Google**. There is no
email/password authentication.

```text
iOS app                                  DinnerOS API                         Apple / Google
───────                                  ────────────                         ──────────────
generate nonce ─┐
                ├─ provider sign-in ─────────────────────────────────────────▶ identity token (JWT)
                │
POST /api/v1/auth/{apple|google}  ──▶  verify JWT signature via provider JWKS
  { identityToken, nonce }             verify iss, aud, exp, nonce
                                       find-or-create AuthIdentity(provider, subject)
                                       find-or-create User
                                ◀──    { accessToken, refreshToken, user }
store tokens in Keychain
```

### Token verification rules

| Check | Apple | Google |
| --- | --- | --- |
| Signing keys | `https://appleid.apple.com/auth/keys` (cached, refreshed on unknown `kid`) | `https://www.googleapis.com/oauth2/v3/certs` |
| `iss` | `https://appleid.apple.com` | `accounts.google.com` or `https://accounts.google.com` |
| `aud` | `APPLE_BUNDLE_ID` | `GOOGLE_CLIENT_ID` (iOS OAuth client) |
| `exp` / `iat` | required, small clock skew | required, small clock skew |
| nonce | SHA-256 of the client's raw nonce must match | nonce claim when supplied |
| email | trust only from a verified token; Apple may return a private relay address and sends email only on first sign-in | trust only if `email_verified` is true |

The client never sends a bare email or user ID. Identity is established only from
a verified provider token.

## Identity model

```text
User (internal id)  1 ──── *  AuthIdentity { provider, subject, email, emailVerified }
```

- `(provider, subject)` is unique. `subject` is the provider's stable user ID.
- A user can attach more login methods later. Accounts are **not** automatically
  merged by email; linking is an explicit, authenticated action.
- Provider tokens are verified and then discarded. They are never stored.

## DinnerOS sessions

| Token | Form | Lifetime | Storage |
| --- | --- | --- | --- |
| Access token | JWT (HS256, `AUTH_TOKEN_SIGNING_KEY`), claims `sub`=userId, `aud`, `exp`, `jti` | ~15 minutes | iOS Keychain; sent as `Authorization: Bearer` |
| Refresh token | 256-bit random opaque string | ~60 days, sliding | iOS Keychain; server stores only its SHA-256 hash |

- **Rotation:** each refresh issues a new refresh token and invalidates the old one.
- **Reuse detection:** presenting an already-rotated refresh token revokes the
  whole token family (every session descended from that sign-in).
- **Logout** revokes the current session server-side and clears the Keychain.
- **Session restoration** on launch reads the Keychain, then refreshes if the
  access token is expired. Refresh failure returns the user to sign-in.
- Keychain items use `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`.

## Authorization

- Every request is authorized **server-side**. Client-side UI hiding is only a
  convenience.
- Household access requires a `HouseholdMembership`. Queries are always scoped by
  `householdId`. A resource in a household the caller doesn't belong to returns
  `404`, so its existence is never revealed.
- Roles map to permissions in one place:

```go
type Role string // "admin", "member" (viewer, shopper, child, guest later)

type Permission string // "household.invite", "plan.edit", "pantry.edit", ...

func (r Role) Can(p Permission) bool
```

Handlers ask `membership.Role.Can(PermissionX)` instead of checking for
`admin` directly.

## Secrets

- Nothing secret ships in the iOS app. Apple and Google client IDs are public
  identifiers.
- `AUTH_TOKEN_SIGNING_KEY` and provider settings live in environment
  configuration (see [deployment.md](deployment.md)).
- Tokens, cookies, and identity tokens are never logged.
