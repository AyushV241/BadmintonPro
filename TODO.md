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
  `{"GET /api/me", a.handleMe, authUser}`, `{"POST /api/auth/phone/start", a.handlePhoneStart, authPublic}`.
  Register routes only from this table.
- A `requireUser` middleware looks up the session once and puts the user in
  the request context. Handlers read it instead of handling cookies.
- A test walks the table:
  - every non-public route returns 401 without a session cookie;
  - the set of public routes exactly matches an explicit allowlist (health,
    logout, providers, phone start and verify, OAuth start and callback). Making a
    route public then means editing the allowlist, which is visible in
    review.

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
- **Test numbers for staging.** An allowlist of numbers with fixed codes, so
  demos don't send real SMS. It's a login bypass, so the backend must refuse
  to start with it configured when `COOKIE_SECURE=true`.
- **WhatsApp as a second channel.** `otp.WhatsApp` exists, and Twilio Verify
  supports it; often cheaper and more reliable than SMS in India.
- **Maybe: let a verified phone sign in to its Google account.** Today each
  sign-in method is its own account (decided on purpose), so one person can
  end up with two. Once a Google account has verified a phone by OTP, that
  number could safely become a second way into it. Revisit if duplicates
  become a problem.
- **Real delivery test.** Nobody has sent a real SMS through the Twilio
  adapter yet; it's tested against a fake Verify server.
