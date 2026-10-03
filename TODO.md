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

### 3. Rate limiting for phone codes (removed for now)

It was built and then taken out to make local testing easier. Only the OTP
vendor's limits apply today (Twilio Verify: 5 sends per number per 10
minutes, Fraud Guard, geo permissions). **Restore it before using
`OTP_PROVIDER=twilio` anywhere public**: every SMS costs money, and SMS
pumping fraud sends thousands.

The working version is in git: `backend/ratelimit.go` and
`ratelimit_test.go`, and the limits in `phone_handlers.go`, as of commit
`1fa9ccc` (they were first added in `755e31f`). Restoring it is mostly
`git checkout 1fa9ccc -- backend/ratelimit.go backend/ratelimit_test.go`
plus re-adding the checks in `sendCode`/`checkCode`, `NewPhoneLogin`'s
parameters and the config below.

The design:

- an in-memory sliding-window limiter, with `allowAll` checking every rule
  before recording any, so a request refused by one rule doesn't use up the
  others;
- sends:
  - per number: one every 30 seconds and 5 an hour;
  - overall: `OTP_SENDS_PER_HOUR` (default 100), which bounds the
    worst-case bill however many numbers or IPs an attacker uses;
  - per IP: 10 an hour, **only when `CLIENT_IP_HEADER` is set**;
- checks: 10 per number every 10 minutes, and 30 per IP an hour;
- limited requests get `429` with `Retry-After`, and the page counts down
  (the UI countdown is still in place);
- **the client-IP finding:** the Next.js rewrite proxy forwards the browser's
  own `X-Forwarded-For` unchanged and adds nothing. So trusting that header
  lets anyone fake their IP, and `RemoteAddr` is always the proxy. Per-IP
  limits need a header written by infrastructure in front of everything (a
  load balancer), taking its last entry;
- with several backend instances, move the counters to Postgres.

The same limiter would also serve any future public endpoint.

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

- **Verify a phone account's contact email.** It's stored unverified today.
  Send a code or link by email (Twilio Verify's email channel needs SendGrid;
  or an email adapter such as Resend or Postmark behind the same
  `otp.Provider` interface), then set `emailVerified`.
- **Edit the profile after setup.** A settings page reusing the setup form;
  today `/setup-profile` only works until the profile is complete.
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
