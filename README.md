# BadmintonPro

A badminton club and match management app — Next.js frontend, Go backend, Postgres.

> **Status:** early scaffold. Authentication works end to end against a real
> database. Players, matches and rankings are not built yet.

## Stack

| Layer | Choice |
| --- | --- |
| Frontend | Next.js 16 (App Router), React 19, TypeScript, Tailwind v4 |
| Backend | Go 1.26, standard library `net/http`, bcrypt |
| Database | Postgres 17 via Docker Compose, `pgx` driver |
| Migrations | golang-migrate, embedded in the binary |

## Prerequisites

- Go 1.26+
- Node.js 20+
- Docker Desktop (for Postgres)

## Running locally

Three steps, in order.

**1. Database:**

```bash
docker compose up -d
```

**2. Backend (port 8080):**

```bash
cd backend
go run .
```

Migrations run automatically on startup, then a demo user is seeded.

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

## Database credentials

**The credentials in `docker-compose.yml` are not secrets and are committed on
purpose.** Every developer runs their own Postgres container locally, so the
values are identical on every machine and there is nothing to share — a
teammate clones the repo, runs `docker compose up -d`, and has the same
database. The port is bound to `127.0.0.1`, so the container is not reachable
from the network.

What *isn't* shared is the data. Each developer's database starts empty and is
populated by migrations plus the seeded demo user. Shared fixtures belong in a
migration or a seed command, committed to the repo, not in a database someone
passes around.

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

## Migrations

SQL files live in `backend/migrations/` and are embedded into the binary, so
the compiled server carries its own schema. They run on every boot;
golang-migrate records the applied version in a `schema_migrations` table and
skips anything already done.

To add one, create the next numbered pair:

```
backend/migrations/000002_add_matches.up.sql
backend/migrations/000002_add_matches.down.sql
```

## API

Base URL `http://localhost:8080`. All responses are JSON.

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| `GET` | `/api/health` | — | Liveness check |
| `POST` | `/api/login` | — | Exchange email + password for a session token |
| `GET` | `/api/me` | Bearer | Return the signed-in user |
| `POST` | `/api/logout` | Bearer | Revoke the session token |

```bash
curl -X POST http://localhost:8080/api/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"player@badmintonpro.local","password":"smash123"}'
```

```json
{
  "token": "56ac6136…",
  "user": { "id": "usr_1", "name": "Demo Player", "email": "player@badmintonpro.local" }
}
```

Authenticated requests pass the token as `Authorization: Bearer <token>`.

Failed logins return `401` with the same message whether the email is unknown
or the password is wrong, so the endpoint can't be used to discover which
accounts exist.

## Configuration

**Backend** (environment variables, all optional):

| Variable | Description | Default |
| --- | --- | --- |
| `DATABASE_URL` | Postgres connection string | `postgres://badminton:badminton@localhost:5432/badmintonpro?sslmode=disable` |
| `PORT` | Port the API listens on | `8080` |
| `CORS_ORIGIN` | Origin allowed to call the API | `http://localhost:3000` |
| `DEMO_EMAIL` | Seeded account's email | `player@badmintonpro.local` |
| `DEMO_PASSWORD` | Seeded account's password | `smash123` |

**Frontend** — copy `frontend/.env.example` to `frontend/.env.local`:

| Variable | Description | Default |
| --- | --- | --- |
| `NEXT_PUBLIC_API_URL` | Backend base URL | `http://localhost:8080` |

## Tests

```bash
cd backend && go test ./...
```

Tests run against the in-memory store, so they need no database and stay fast.
The frontend has no tests yet.

## Project structure

```
BadmintonPro/
├── docker-compose.yml       # Postgres 17
├── backend/
│   ├── main.go              # startup, migrations, graceful shutdown
│   ├── api.go               # routes, handlers, CORS middleware
│   ├── store.go             # Store interface, password hashing, tokens
│   ├── memory_store.go      # in-memory implementation (tests)
│   ├── postgres_store.go    # Postgres implementation
│   ├── migrate.go           # embedded migration runner
│   ├── migrations/
│   └── api_test.go
└── frontend/
    ├── app/
    │   ├── page.tsx           # redirects to /login
    │   ├── login/page.tsx     # login form
    │   └── dashboard/page.tsx # signed-in landing page
    └── lib/api.ts             # typed API client + token storage
```

## Known limitations

Deliberate shortcuts, not oversights:

- **The frontend stores its token in `localStorage`**, which any script on the
  page can read. An httpOnly cookie is the right answer before this is exposed
  to anyone.
- **Tokens are opaque random strings**, not JWTs — fine, but it means every
  authenticated request hits the database.
- **No signup, password reset, or rate limiting** on the login endpoint.
- **`sslmode=disable`** in the default connection string. Correct for a local
  container, wrong anywhere else.

## License

Not yet chosen. All rights reserved until one is added.
