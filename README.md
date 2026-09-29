# BadmintonPro

A badminton club and match management app — Next.js frontend, Go backend, Postgres.

> **Status:** early scaffold. Sign-in works end to end with a phone code or
> Google, against a real database. Players, matches and rankings are not built
> yet.

## Stack

| Layer | Choice |
| --- | --- |
| Frontend | Next.js 16 (App Router), React 19, TypeScript, Tailwind v4 |
| Backend | Go 1.26, standard library `net/http`, bcrypt |
| Database | Postgres 17 via Docker Compose, `pgx` driver |
| Migrations | golang-migrate, embedded in the binary |
| Auth | httpOnly session cookie; Google via OpenID Connect (`x/oauth2`, `go-oidc`) |

## Prerequisites

- Go 1.26+
- Node.js 20+
- Docker Desktop (for Postgres)

## Running locally

Three steps, in order. Each half has fuller startup notes and
troubleshooting in [backend/README.md](backend/README.md) and
[frontend/README.md](frontend/README.md).

**1. Database:**

```bash
docker compose up -d
```

**2. Backend (port 8080):**

```bash
cd backend
air        # hot reload: rebuilds and restarts on save
# or: go run .   (no reload)
```

`air` comes from mise (`mise install` in the repo root). `mise run dev` starts
the backend and frontend together.

On a fresh local database, apply the schema once with `go run . -migrate`
(see [Migrations](#migrations)). Starting the server never migrates.

**3. Frontend (port 3000):**

```bash
cd frontend
npm install     # first time only
npm run dev
```

Open http://localhost:3000 and sign in with any mobile number: with
`OTP_PROVIDER=console` (see *Phone sign-in*) the code appears in the backend's
log, so no SMS or account is needed.

### Google sign-in (optional)

The "Continue with Google" button appears only when the backend has Google
credentials. Without them, phone sign-in still works.

1. In [Google Cloud Console](https://console.cloud.google.com), using a
   **personal** Google account rather than a work one, create a project. No
   billing account is needed.
2. **APIs & Services → OAuth consent screen**: user type *External*, scopes
   `openid`, `email`, `profile`. Leave it in *Testing* and add yourself under
   *Test users*. Only listed test users can sign in until it is published.
3. **Credentials → Create credentials → OAuth client ID**, type *Web
   application*, with one authorized redirect URI:

   ```
   http://localhost:3000/api/auth/google/callback
   ```

   It must match exactly: `http`, `localhost` (not `127.0.0.1`), port `3000`,
   no trailing slash. Leave *Authorized JavaScript origins* empty.
4. Put the credentials in the repo-root `.env` (gitignored):

   ```bash
   GOOGLE_CLIENT_ID=….apps.googleusercontent.com
   GOOGLE_CLIENT_SECRET=GOCSPX-…
   GOOGLE_REDIRECT_URL=http://localhost:3000/api/auth/google/callback
   ```

On startup the backend logs `external login providers: [google]`. Google
shows the client secret only once, so keep a copy in a password manager.

### Phone sign-in (optional)

Players type a mobile number and the 6-digit code texted to it; the account
is created on the first login. Codes are sent through an adapter chosen by
`OTP_PROVIDER` (`backend/internal/otp`), so the vendor can change without
touching handlers.

**Locally, no SMS or account needed.** Put this in `backend/.env`:

```bash
OTP_PROVIDER=console
```

The backend prints each code in its log instead of sending it:

```
phone login code for +919876543210: 482913 (console provider: not sent)
```

`console` refuses to start when `COOKIE_SECURE=true`, so it can't reach a
real deployment by accident.

**Real SMS with Twilio Verify:**

1. Create a [Twilio](https://www.twilio.com) account. The trial includes 40
   free Verify checks for 30 days, sending only to up to 5 numbers you've
   verified, in your sign-up country.
2. **Verify → Services → Create new**, and copy its Service SID (`VA…`).
3. **Verify → Settings → Geo permissions**: allow only the countries in
   `PHONE_REGIONS`, so SMS fraud is also stopped at Twilio's end. Leave
   Fraud Guard on (the default).
4. In `backend/.env` (gitignored):

   ```bash
   OTP_PROVIDER=twilio
   TWILIO_ACCOUNT_SID=AC…
   TWILIO_AUTH_TOKEN=…
   TWILIO_VERIFY_SERVICE_SID=VA…
   ```

On startup the backend logs `phone login: enabled via twilio for [IN]`.

**Limits.** Every SMS costs money, and SMS pumping fraud triggers thousands,
so sends are capped:

- per number: one every 30 seconds and 5 an hour;
- overall: `OTP_SENDS_PER_HOUR` (default 100), which bounds the worst-case
  bill however many numbers or IPs an attacker uses;
- per IP: 10 an hour, **only when `CLIENT_IP_HEADER` is set**. The Next.js
  proxy forwards the browser's own `X-Forwarded-For` unchanged and adds
  nothing, so the backend can't tell users apart by IP on its own. Set it to
  a header your production load balancer writes.

Checking codes is limited per number and per IP as well, on top of Twilio's
own limit of 5 checks per code. Limited requests get `429` with
`Retry-After`, and the login page counts down before offering to resend.

## Database credentials

**The credentials in `docker-compose.yml` are not secrets and are committed on
purpose.** Every developer runs their own Postgres container locally, so the
values are identical on every machine and there is nothing to share — a
teammate clones the repo, runs `docker compose up -d`, and has the same
database. The port is bound to `127.0.0.1`, so the container is not reachable
from the network.

The local Docker database's *data* isn't shared: each developer's container starts empty and is populated by migrations and whoever signs in. For shared data, use the hosted database below. Shared fixtures still belong in a migration or a seed command committed to the repo, not in a database dump someone passes around.

Real environments never use these values. They set `DATABASE_URL`, which should
come from the deployment platform's secret store and never be committed.

To override locally, put values in a `.env` file next to `docker-compose.yml`
(already gitignored):

```bash
POSTGRES_USER=someone
POSTGRES_PASSWORD=something
```

### Useful commands

```bash
docker compose ps                    # container status
docker compose logs -f db            # database logs
docker compose down                  # stop, keep data
docker compose down -v               # stop and wipe data
docker exec -it badmintonpro-db psql -U badminton -d badmintonpro
```

## Shared hosted database

The Docker database is private to each machine. To work against the same data from several machines, point the backend at a hosted Postgres instead. Any provider works; [Neon](https://neon.tech) has a free tier.

1. Create a database and copy its **direct** connection string, not the pooled one. `-migrate` needs a direct session.
2. Copy `backend/.env.example` to `backend/.env` (gitignored) and set `DATABASE_URL`:

   ```bash
   DATABASE_URL=postgres://USER:PASSWORD@HOST/DBNAME?sslmode=require
   ```

3. Start the backend as usual. It loads `backend/.env`, then the repo-root `.env`. A variable already set in your shell overrides both.

Leave `DATABASE_URL` unset to keep using the local Docker database.

**Starting the backend never migrates**, so pointing it at the shared database is safe. `go run . -migrate` applies migrations to whatever `DATABASE_URL` points at, which on the shared database means everyone's. Run it there only once a migration is merged. Never edit a migration after it has run there: golang-migrate won't re-run it, so the shared schema and the code would silently disagree. While writing a new migration, test it on the local database by overriding the URL for that command:

```bash
DATABASE_URL=postgres://badminton:badminton@localhost:5432/badmintonpro?sslmode=disable go run . -migrate
```


## Migrations

SQL files live in `backend/migrations/` and are embedded into the binary, so
the compiled server carries its own schema. They never run on startup. Apply
them explicitly, from `backend/`:

```bash
go run . -migrate    # applies pending migrations to DATABASE_URL, then exits
```

golang-migrate records the applied version in a `schema_migrations` table and
skips anything already done.

To add one, create the next numbered pair:

```
backend/migrations/000003_add_matches.up.sql
backend/migrations/000003_add_matches.down.sql
```

## API

The browser only talks to `http://localhost:3000`. Next.js proxies `/api/*`
to the Go server on `:8080` (see `frontend/next.config.ts`), so the session
cookie is on the same origin as the app and no CORS is involved. All
responses are JSON except the OAuth redirects.

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| `GET` | `/api/health` | — | Liveness check |
| `POST` | `/api/logout` | cookie | Revokes the session and clears the cookie |
| `GET` | `/api/me` | cookie | The signed-in user, including `username`, contact `email`/`phone` with `emailVerified`/`phoneVerified`, `signInMethod` and `profileComplete` |
| `PUT` | `/api/me/profile` | cookie | `{"name","username","email"?}`: the profile step. `email` only for accounts without a verified one (stored unverified; `""` clears it). `409` if the username is taken |
| `POST` | `/api/me/phone/start` | cookie | `{"phone"}`: texts a code to a phone a Google account wants on its profile. Same limits as phone sign-in. `400` for phone accounts |
| `POST` | `/api/me/phone/verify` | cookie | `{"phone","code"}`: saves it as a **verified contact phone**. It does not become a way to sign in |
| `GET` | `/api/auth/providers` | — | Enabled sign-in methods, e.g. `{"providers":["google"],"phone":true}`. `providers` are redirect logins; `phone` is separate because it isn't one |
| `POST` | `/api/auth/phone/start` | — | `{"phone"}` in any common format; texts a code and returns `{"phone"}` in E.164. The reply is the same whether or not the number has an account. `429` with `Retry-After` when limited. Only when `OTP_PROVIDER` is set |
| `POST` | `/api/auth/phone/verify` | — | `{"phone","code"}`; signs in (creating the account on first login) and sets the session cookie. `401` for a wrong or expired code |
| `GET` | `/api/auth/{provider}/start` | — | Redirects to the provider's sign-in page |
| `GET`, `POST` | `/api/auth/{provider}/callback` | — | Provider redirects back here; signs in, then redirects to `/setup-profile` on a first sign-in or `/dashboard` after |

```bash
curl -X POST http://localhost:3000/api/auth/phone/start \
  -H 'Content-Type: application/json' -d '{"phone":"98765 43210"}'
# the code is in the backend log with OTP_PROVIDER=console
curl -c cookies.txt -X POST http://localhost:3000/api/auth/phone/verify \
  -H 'Content-Type: application/json' -d '{"phone":"+919876543210","code":"482913"}'

curl -b cookies.txt http://localhost:3000/api/me
```

The session is an httpOnly, `SameSite=Lax` cookie named `bp_session`. It never
appears in a response body, and page scripts can't read it.

When a provider sign-in fails, the callback redirects to `/login?error=<code>`
with one of `cancelled`, `expired` (bad or missing `state`) or `failed`. The details go to the backend log, never the URL.

## Accounts and sign-in methods

A user (`users`) is separate from the way they sign in (`user_identities`):
a phone number, or a provider's stable subject ID, **never an email**. Emails
change, Apple hands out relay addresses, and Facebook may not return one at
all.

**Each sign-in method is its own account.** The first sign-in with a phone
number or a Google account creates an account; later sign-ins with the same
one return to it. Nothing is ever joined by email or phone number, so signing
in with a phone and later with Google gives two separate accounts, even for
the same person. That rules out a whole class of account-hijacking bugs, at
the cost of possible duplicates.

A provider's email is stored only if the provider vouches for it
(`EmailVerified`), and then only as contact information.

### Set up your profile

After the first sign-in, the frontend sends the user to `/setup-profile`, and
keeps doing so from the dashboard until the profile is complete (a name and a
username):

| | Phone sign-in | Google sign-in |
| --- | --- | --- |
| Name | required | required, prefilled from Google |
| Username | required | required |
| Email | optional, stored **unverified** | Google's, **verified**, read-only |
| Phone | the sign-in number, verified | optional, **verified by a code** before it's saved |

Usernames are 3–20 characters (letters, digits, `_` and `.`, starting with a
letter or digit), stored lowercase and unique regardless of case, with a few
reserved names such as `admin`. Contact email and phone are never used to sign
in or to find an account, so neither is unique.

### Adding a provider

Each provider is an adapter implementing `oauth.Provider`
(`backend/internal/oauth/provider.go`):

```go
type Provider interface {
    Name() string
    AuthURL(state, nonce, verifier string) string
    Exchange(ctx context.Context, code, verifier, nonce string) (Identity, error)
}
```

To add Facebook or Apple, write the adapter, register it in
`configureProviders` in `main.go`, and add its label in
`frontend/app/login/page.tsx`. Routes and storage don't change, and the
button appears once `/api/auth/providers` lists it. The adapter must verify
the provider's response itself (Google's checks the ID token's signature,
issuer, audience, expiry and nonce) and report `EmailVerified` honestly, since
it decides whether the email is kept.

Every login uses PKCE, a `state` value checked against a short-lived httpOnly
cookie scoped to `/api/auth/`, and a nonce. Nothing from the provider is stored
beyond the identity. Access and refresh tokens are discarded.

## Configuration

**Backend** (environment variables, all optional). At startup the backend
loads `backend/.env`, then the repo-root `.env`. Earlier files win over later
ones, and variables already set in the shell win over both:

| Variable | Description | Default |
| --- | --- | --- |
| `DATABASE_URL` | Postgres connection string. Set it in `backend/.env` to use the shared hosted database (see *Shared hosted database*); unset means the local Docker container | `postgres://badminton:badminton@localhost:5432/badmintonpro?sslmode=disable` |
| `PORT` | Port the API listens on | `8080` |
| `COOKIE_SECURE` | `true` marks cookies `Secure`; required anywhere served over HTTPS | `false` |
| `GOOGLE_CLIENT_ID` | Enables Google sign-in when set | — |
| `GOOGLE_CLIENT_SECRET` | Required with the client ID | — |
| `GOOGLE_REDIRECT_URL` | Must match the URI registered with Google | `http://localhost:3000/api/auth/google/callback` |
| `OTP_PROVIDER` | Enables phone sign-in: `console` (codes printed to the log; local only) or `twilio` | — (disabled) |
| `TWILIO_ACCOUNT_SID` | Twilio Account SID (or an API key SID) | — |
| `TWILIO_AUTH_TOKEN` | Twilio Auth Token (or the API key secret) | — |
| `TWILIO_VERIFY_SERVICE_SID` | The Verify Service that sends codes (`VA…`) | — |
| `PHONE_REGIONS` | Comma-separated ISO country codes allowed to sign in by phone | `IN` |
| `PHONE_DEFAULT_REGION` | Country assumed for numbers typed without a country code | first of `PHONE_REGIONS` |
| `OTP_SENDS_PER_HOUR` | Cap on codes sent per hour across all numbers | `100` |
| `CLIENT_IP_HEADER` | Header, set by trusted infrastructure, carrying the client IP. Enables per-IP limits | — (off) |

**Frontend** — copy `frontend/.env.example` to `frontend/.env.local`:

| Variable | Description | Default |
| --- | --- | --- |
| `BACKEND_URL` | Where the dev server proxies `/api/*`. Server-side only | `http://localhost:8080` |

## Tests

```bash
cd backend && go test ./...
```

By default the tests use the in-memory store and need no database. The store
contract tests, which cover how sign-ins map to accounts, can also run against
real Postgres. Point them at a **throwaway** database, because they truncate every
table:

```bash
docker exec badmintonpro-db createdb -U badminton badmintonpro_test
TEST_DATABASE_URL='postgres://badminton:badminton@localhost:5432/badmintonpro_test?sslmode=disable' \
  go test ./...
```

OAuth flows are tested with a fake provider, so they never call Google. The
frontend has no tests yet.

## Project structure

```
BadmintonPro/
├── docker-compose.yml       # Postgres 17
├── backend/
│   ├── main.go              # startup, provider setup, graceful shutdown
│   ├── env.go               # loads backend/.env and ../.env
│   ├── api.go               # routes, /api/me, logout
│   ├── session.go           # session cookie
│   ├── oauth_handlers.go    # provider-agnostic start + callback
│   ├── phone_handlers.go    # phone sign-in: send and check codes, limits
│   ├── profile_handlers.go  # profile step, contact-phone verification
│   ├── phone.go             # phone number normalisation to E.164
│   ├── ratelimit.go         # in-memory sliding-window limiter
│   ├── store.go             # Store interface, hashing, IDs
│   ├── memory_store.go      # in-memory implementation (tests)
│   ├── postgres_store.go    # Postgres implementation
│   ├── migrate.go           # embedded migration runner
│   ├── migrations/
│   ├── internal/oauth/      # Provider interface, registry, Google adapter
│   ├── internal/otp/        # OTP Provider interface; Twilio and in-memory adapters
│   └── *_test.go
└── frontend/
    ├── app/
    │   ├── page.tsx           # redirects to /login
    │   ├── login/page.tsx     # login page: phone and Google
    │   ├── setup-profile/page.tsx # "Set up your profile" after the first sign-in
    │   └── dashboard/page.tsx # signed-in landing page
    ├── components/auth/PhoneCodeForm.tsx # number + code steps, for sign-in and profile
    ├── lib/api.ts             # typed API client
    └── next.config.ts         # proxies /api/* to the backend
```

## Known limitations

Deliberate shortcuts, not oversights:

- **Tokens are opaque random strings**, not JWTs — fine, but it means every
  authenticated request hits the database.
- **One person can have two accounts:** signing in once by phone and once
  with Google gives two unrelated accounts (see *Accounts and sign-in
  methods*).
- **Phone accounts are only as safe as the SIM.** Carriers reassign numbers,
  so whoever gets a recycled number can sign in to the previous owner's
  account, and SIM-swap scams can hijack one. Don't let phone login alone
  unlock anything sensitive.
- **A phone account's contact email is unverified.** Don't send it anything
  important until email verification exists; people mistype emails, or type
  someone else's.
- **Profiles can't be edited after setup yet**: `/setup-profile` redirects to
  the dashboard once the profile is complete.
- **Rate-limit counters live in process memory**, which is correct for one
  backend instance. Several instances would need them in Postgres.
- **Apple will need `SameSite=None; Secure` on the OAuth flow cookie**, and so
  HTTPS. Its callback is a cross-site POST, which `Lax` cookies aren't sent on.
  Google is unaffected.
- **`sslmode=disable`** in the default connection string. Correct for a local
  container, wrong anywhere else.

## License

Not yet chosen. All rights reserved until one is added.
