# BadmintonPro

A badminton club and match management app — Next.js frontend, Go backend, Postgres.

> **Status:** early scaffold. Sign-up and sign-in work end to end, with email +
> password or Google, against a real database. Players, matches and rankings are not
> built yet.

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
(see [Migrations](#migrations)). Starting the server never migrates; it only
seeds a demo user.

**3. Frontend (port 3000):**

```bash
cd frontend
npm install     # first time only
npm run dev
```

Open http://localhost:3000.

### Demo account

```
player@badmintonpro.local / smash123
```

Override with `DEMO_EMAIL` / `DEMO_PASSWORD`. Seeding is idempotent, so
restarting against an existing database is fine.

### Google sign-in (optional)

The "Continue with Google" button appears only when the backend has Google
credentials. Without them, password login works as before.

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

## Database credentials

**The credentials in `docker-compose.yml` are not secrets and are committed on
purpose.** Every developer runs their own Postgres container locally, so the
values are identical on every machine and there is nothing to share — a
teammate clones the repo, runs `docker compose up -d`, and has the same
database. The port is bound to `127.0.0.1`, so the container is not reachable
from the network.

The local Docker database's *data* isn't shared: each developer's container starts empty and is populated by migrations plus the seeded demo user. For shared data, use the hosted database below. Shared fixtures still belong in a migration or a seed command committed to the repo, not in a database dump someone passes around.

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

The seeded demo account exists on the shared database too, so everyone using it shares `player@badmintonpro.local / smash123`.

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
| `POST` | `/api/signup` | — | Name + email + password; creates an unverified account and sets the session cookie. `409` if the email is taken |
| `POST` | `/api/login` | — | Email + password; sets the session cookie |
| `POST` | `/api/logout` | cookie | Revokes the session and clears the cookie |
| `GET` | `/api/me` | cookie | The signed-in user |
| `GET` | `/api/auth/providers` | — | Enabled external providers, e.g. `{"providers":["google"]}` |
| `GET` | `/api/auth/{provider}/start` | — | Redirects to the provider's sign-in page |
| `GET`, `POST` | `/api/auth/{provider}/callback` | — | Provider redirects back here; signs in, then redirects to `/dashboard` |

```bash
curl -c cookies.txt -X POST http://localhost:3000/api/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"player@badmintonpro.local","password":"smash123"}'

curl -b cookies.txt http://localhost:3000/api/me
```

```json
{ "user": { "id": "usr_1", "name": "Demo Player", "email": "player@badmintonpro.local", "emailVerified": false } }
```

The session is an httpOnly, `SameSite=Lax` cookie named `bp_session`. It never
appears in a response body, and page scripts can't read it.

Failed password logins return `401` with the same message whether the email is
unknown, the password is wrong, or the account has no password (Google-only),
so the endpoint can't be used to discover which accounts exist.

Signup can't hide that yet: its `409` tells anyone whether an email is
registered. Closing that needs email verification (always answer "check your
inbox" and notify an existing owner by email instead). Until then it's a known
tradeoff, and rate limiting is the first mitigation to add.

Signup validates in the handler: a name of 1–100 characters, a bare email
address, and a password of 8 characters to 72 bytes (bcrypt ignores anything
longer). The email is stored lowercased and **unverified**.

When a provider sign-in fails, the callback redirects to `/login?error=<code>`
with one of `cancelled`, `expired` (bad or missing `state`), `conflict` or
`failed`. The details go to the backend log, never the URL.

## Accounts and sign-in methods

A user (`users`) is separate from the ways they sign in: a password
(`password_credentials`) and any number of external identities
(`user_identities`). External identities are keyed on the provider's stable
subject ID, **never on email**. Emails change, Apple hands out relay
addresses, and Facebook may not return one at all.

### Linking rules

When someone signs in with a provider, `decideLink` in `backend/link.go`
decides what happens:

| Identity already linked? | Account with that email | Provider vouches for email? | Result |
| --- | --- | --- | --- |
| yes | (any) | (any) | Sign in |
| no | none | (any) | Create an account |
| no | verified | yes | Attach to it; its password keeps working |
| no | unverified | yes | **Take over**: attach, delete its password, end all its sessions |
| no | any | no | Refuse (`conflict`) |

Password signup with an email any account already uses is refused, so nobody
can attach a password to someone else's Google account.

Because nothing verifies emails yet, every password signup is unverified. So a
user who signs up with a password and later chooses "Continue with Google" for
the same email hits the takeover row: they get in with Google, but their
password is deleted. Once email verification exists, verified users will keep
their password instead (the attach row).

The takeover row prevents *pre-account hijacking*. Anyone can register a
password account with an email they don't own. If the real owner then signs
in through a provider that proves ownership, keeping the old password would
leave them in an account a stranger can also get into. So the provider wins.
An honest user loses only a password they can set again.

An email a provider doesn't vouch for is never stored on the user, so it can't
claim the address.

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
`frontend/app/login/page.tsx`. Routes, storage and linking rules don't change,
and the button appears once `/api/auth/providers` lists it. The adapter must
verify the provider's response itself (Google's checks the ID token's
signature, issuer, audience, expiry and nonce) and report `EmailVerified`
honestly. That flag decides the linking row.

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
| `DEMO_EMAIL` | Seeded account's email | `player@badmintonpro.local` |
| `DEMO_PASSWORD` | Seeded account's password | `smash123` |

**Frontend** — copy `frontend/.env.example` to `frontend/.env.local`:

| Variable | Description | Default |
| --- | --- | --- |
| `BACKEND_URL` | Where the dev server proxies `/api/*`. Server-side only | `http://localhost:8080` |

## Tests

```bash
cd backend && go test ./...
```

By default the tests use the in-memory store and need no database. The store
contract tests, which include every linking rule, can also run against real
Postgres. Point them at a **throwaway** database, because they truncate every
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
│   ├── api.go               # routes, signup, password login, /api/me
│   ├── session.go           # session cookie
│   ├── oauth_handlers.go    # provider-agnostic start + callback
│   ├── link.go              # account-linking rules
│   ├── store.go             # Store interface, hashing, IDs
│   ├── memory_store.go      # in-memory implementation (tests)
│   ├── postgres_store.go    # Postgres implementation
│   ├── migrate.go           # embedded migration runner
│   ├── migrations/
│   ├── internal/oauth/      # Provider interface, registry, Google adapter
│   └── *_test.go
└── frontend/
    ├── app/
    │   ├── page.tsx           # redirects to /login
    │   ├── login/page.tsx     # login form
    │   ├── signup/page.tsx    # signup form
    │   └── dashboard/page.tsx # signed-in landing page
    ├── lib/api.ts             # typed API client
    └── next.config.ts         # proxies /api/* to the backend
```

## Known limitations

Deliberate shortcuts, not oversights:

- **Tokens are opaque random strings**, not JWTs — fine, but it means every
  authenticated request hits the database.
- **No email verification, password reset, "set a password" or "connect
  Google" settings yet.** The store already enforces the rules those
  flows depend on. Verification emails will need an email service; locally
  the plan is to log the link.
- **No rate limiting** on the login or signup endpoints.
- **Apple will need `SameSite=None; Secure` on the OAuth flow cookie**, and so
  HTTPS. Its callback is a cross-site POST, which `Lax` cookies aren't sent on.
  Google is unaffected.
- **`sslmode=disable`** in the default connection string. Correct for a local
  container, wrong anywhere else.

## License

Not yet chosen. All rights reserved until one is added.
