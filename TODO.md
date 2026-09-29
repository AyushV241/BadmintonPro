# TODO

## Backend hardening (from the compass review)

CI is in place (`.github/workflows/ci.yml`), so each of these lands with
lint, race tests and a vulnerability scan behind it. The order below lets
each one build on the previous. All three rework `backend/api.go`, so check
which other sessions are editing it first.

### 1. Request logging

Today the backend only calls `log.Printf` when something fails. There's no
record of requests and no way to follow one user's actions.

- Switch to Go's standard structured logger, `log/slog`: readable text
  locally, JSON when deployed (for example a `LOG_FORMAT` variable).
- Middleware, outermost first:
  - panic recovery: log the stack trace and return a generic JSON 500;
  - request ID: accept `X-Request-Id` or generate one, echo it in the
    response, and attach it to every log line for the request;
  - access log: one line per request with method, route pattern, status,
    duration, user ID and request ID. Skip `/api/health`.
- Never log request bodies, cookies, tokens or query strings. The OAuth
  callback's query carries the one-time `code` and `state`, so log
  `r.Pattern` (e.g. `/api/auth/{provider}/callback`), not `r.URL`.
- Set the missing `ReadTimeout`, `WriteTimeout` and `IdleTimeout` on the
  `http.Server`.
- When this lands, delete the G706 exclusion in `backend/.golangci.yml`.

### 2. Route auth table and its test

Today each handler decides for itself whether you must be signed in
(`handleMe` checks the cookie inline). A new route that forgets the check is
silently public.

- Declare every route in one table with its auth requirement:
  `{"GET /api/me", a.handleMe, authUser}`, `{"POST /api/login", a.handleLogin, authPublic}`.
  Register routes only from this table.
- A `requireUser` middleware looks up the session once and puts the user in
  the request context. Handlers read it instead of handling cookies.
- A test walks the table:
  - every non-public route returns 401 without a session cookie;
  - the set of public routes exactly matches an explicit allowlist (health,
    signup, login, logout, providers, OAuth start and callback). Making a
    route public then means editing the allowlist, which is visible in
    review.

### 3. Login and signup rate limiting

Today `/api/login` and `/api/signup` accept unlimited attempts. That allows
password guessing and makes signup's "already registered" 409 easy to abuse
(see `handleSignup`).

The limiter now exists (`backend/ratelimit.go`, used by phone sign-in), so
this is mostly wiring:

- per email: after 5 failed logins, exponential backoff (1s, 2s, 4s … up to
  15 minutes), reset by a successful login;
- per client IP: about 10 requests a minute across login and signup, **only
  when `CLIENT_IP_HEADER` is set**. The Next.js proxy forwards the browser's
  own `X-Forwarded-For` unchanged and adds nothing, so without a trusted
  header every request looks like it comes from the proxy;
- respond `429` with `Retry-After` via `writeRateLimited`, and show "Too many
  attempts, try again in N seconds" on the login and signup pages;
- if the backend ever runs as several instances, move the counters to
  Postgres.

### Also suggested (smaller)

- Store session tokens as SHA-256 hashes, so a database leak doesn't expose
  working sessions.
- `/healthz` (process is up) and `/readyz` (pings the database with a
  timeout), with readiness turned off when shutdown starts.
- Load all settings into one typed `Config` struct with a `Validate()` that
  fails at startup, instead of `os.Getenv` calls spread across `main.go`.
- A CI job with gitleaks, to catch committed secrets.

## Phone sign-in follow-ups

Phone sign-in works with `OTP_PROVIDER=console` locally and `twilio` for real
SMS. Still to do:

- **Name on first sign-in.** Phone accounts start as "Player". After the first
  phone login, ask for a name (needs a `PATCH /api/me` and a store method).
- **Decide whether to retire email/password.** The plan was phone first,
  Google second, password removed. Removing it also shrinks "Password flows"
  below to a note.
- **Test numbers for staging.** An allowlist of numbers with fixed codes, so
  demos don't send real SMS. It's a login bypass, so the backend must refuse
  to start with it configured when `COOKIE_SECURE=true`.
- **WhatsApp as a second channel.** `otp.WhatsApp` exists, and Twilio Verify
  supports it; often cheaper and more reliable than SMS in India.
- **Add or change the phone number on a signed-in account,** so an email or
  Google account can gain phone login without creating a second account.
- **Real delivery test.** Nobody has sent a real SMS through the Twilio
  adapter yet; it's tested against a fake Verify server.

## Password flows (deferred)

Today a password account that later uses "Continue with Google" with the same
email loses its password. Every signup is unverified, so the linking rules
treat Google's sign-in as a takeover: the password is deleted and its sessions
revoked. The user still gets in with Google, but nothing tells them their
password is gone, and they have no way to set a new one.

### Target design

Two mechanisms cover three user-facing flows:

| Flow | Trigger | Proof of identity | Endpoint | Page |
| --- | --- | --- | --- | --- |
| First setup, signed in | Google-only user adds a password | Recent sign-in | `POST /api/password` | `/account/password` |
| Re-enable after takeover | The Google callback just removed the password | The Google sign-in that just happened | same | same page, with a notice |
| Reset, signed out | "Forgot password?" | Link sent to the inbox | `POST /api/password/forgot`, then `POST /api/password/reset` | `/forgot-password`, `/reset-password?token=…` |

**Signed in (first setup and re-enable)**

- After a takeover, the Google callback redirects to
  `/account/password?reason=removed` instead of `/dashboard`.
- The notice stays calm and doesn't alarm. For example: "For security, the
  password on this account was removed because the email hadn't been
  verified. Set a new one to keep signing in with email." It should not
  suggest the account was hijacked, since almost every takeover is the honest
  owner.
- `/api/me` returns `hasPassword`, and the dashboard shows "Add a password"
  until one is set. Skipping the prompt is fine; Google keeps working.
- Setting a password requires a session created in the last ~10 minutes, so a
  stolen session cookie can't add a password and keep access for good.
- Changing an existing password requires the current password.
- Setting or changing a password signs out every other session.

**Signed out (reset)**

- Always reply "If an account exists, we've emailed a link", so the endpoint
  never reveals which emails have accounts.
- Reset tokens are random, stored hashed, single-use and expire in 30–60
  minutes. Rate-limit requests per email and per IP.
- Completing a reset marks the email verified, because the link proves inbox
  ownership, and it revokes all existing sessions.

### Related: email verification at signup

This uses the same email-sending setup and removes most of the need for the
re-enable flow:

- A verified password account gets *attach* instead of *takeover* when it
  uses Google, so it keeps its password.
- Signup can reply "check your inbox" instead of `409 email already
  registered`, which removes the account-enumeration tradeoff documented on
  `handleSignup`.

### Open decisions

- Email provider for reset and verification mail (Resend, Postmark, AWS SES,
  …). In local development, log the link to the backend console instead.

### Suggested order

1. Signed-in set password, the takeover redirect, and `hasPassword`. No email
   needed.
2. Forgot/reset, logging the link to the console.
3. Email verification at signup.
4. Connect a real email provider.

These steps touch `backend/api.go`, the store (plus a migration for reset
tokens), and the login/signup pages. Check which other sessions are editing
those files before starting.
