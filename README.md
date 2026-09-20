# BadmintonPro

A badminton club and match management app — Next.js frontend, Go backend.

> **Status:** early scaffold. Authentication works end to end against an in-memory
> user store. Players, matches and rankings are not built yet.

## Stack

| Layer | Choice |
| --- | --- |
| Frontend | Next.js 16 (App Router), React 19, TypeScript, Tailwind v4 |
| Backend | Go 1.26, standard library `net/http`, bcrypt for password hashing |
| Storage | In-memory (no database yet) |

## Prerequisites

- Go 1.26+
- Node.js 20+

## Running locally

The two halves run as separate processes. Start the backend first.

**Terminal 1 — backend (port 8080):**

```bash
cd backend
go run .
```

**Terminal 2 — frontend (port 3000):**

```bash
cd frontend
npm install     # first time only
npm run dev
```

Open http://localhost:3000 — you'll be redirected to the login page.

### Demo account

The backend seeds one user at startup so there is something to log in with:

```
player@badmintonpro.local / smash123
```

Override with the `DEMO_EMAIL` and `DEMO_PASSWORD` environment variables.

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
  "token": "3f895b45…",
  "user": { "id": "usr_1", "name": "Demo Player", "email": "player@badmintonpro.local" }
}
```

Authenticated requests pass the token as `Authorization: Bearer <token>`.

Failed logins return `401` with the same message whether the email is unknown or
the password is wrong, so the endpoint can't be used to discover which accounts
exist.

## Configuration

**Backend** (environment variables, all optional):

| Variable | Description | Default |
| --- | --- | --- |
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

Covers the login, session and logout paths. The frontend has no tests yet.

## Project structure

```
BadmintonPro/
├── backend/
│   ├── main.go          # server setup, config, demo user seeding
│   ├── api.go           # routes, handlers, CORS middleware
│   ├── store.go         # in-memory users and sessions, bcrypt hashing
│   └── api_test.go
└── frontend/
    ├── app/
    │   ├── page.tsx           # redirects to /login
    │   ├── login/page.tsx     # login form
    │   └── dashboard/page.tsx # signed-in landing page
    └── lib/api.ts             # typed API client + token storage
```

## Known limitations

These are deliberate shortcuts for local development, not oversights:

- **Users and sessions live in memory** — everything is lost when the backend
  restarts. Replacing `Store` with a database is the next real step.
- **Tokens are opaque random strings**, not JWTs, and are held in a map.
- **The frontend stores its token in `localStorage`**, which any script on the
  page can read. An httpOnly cookie is the right answer before this is exposed
  to anyone.
- **No signup, password reset, or rate limiting** on the login endpoint.

## License

Not yet chosen. All rights reserved until one is added.
