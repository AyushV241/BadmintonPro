# BadmintonPro backend

Go API server on port 8080. The frontend proxies `/api/*` to it, so in the
browser you always use http://localhost:3000, never this port directly.

See the [root README](../README.md) for the whole project, the API and the
sign-in design.

## Prerequisites

- Go 1.26+
- Docker Desktop, for the local Postgres. Not needed if you only use the shared
  hosted database.
- Optional: [mise](https://mise.jdx.dev), which installs `air` for hot reload.
  Run `mise install` once in the repo root.

## Starting the server

Run these from `backend/` unless noted.

**1. Start Postgres** (from the repo root, first time and after a reboot):

```bash
docker compose up -d
```

Skip this if you use the shared hosted database.

**2. Configure (optional).** With nothing set, the server uses the local Docker
database and has password login only. To change that:

```bash
cp .env.example .env    # then fill in what you need
```

At startup the server loads `backend/.env`, then the repo-root `.env`. Earlier
files win over later ones, and variables already set in your shell win over
both. Google credentials usually live in the repo-root `.env`; see
[Google sign-in](../README.md#google-sign-in-optional).

**3. Apply migrations** (first time, and whenever someone adds a migration):

```bash
go run . -migrate
```

This applies pending migrations to `DATABASE_URL` and exits. The server
**never** migrates on its own startup, because the database can be shared.

If your `.env` points at the shared hosted database, this migrates everyone's
database. Do that only for migrations that are merged. To migrate just your
local Docker database, override the URL for that one command:

```bash
DATABASE_URL='postgres://badminton:badminton@localhost:5432/badmintonpro?sslmode=disable' go run . -migrate
```

**4. Run the server:**

```bash
air          # hot reload: rebuilds and restarts on every save
go run .     # or: no reload
```

`air` watches `.go` and `.env` files (see [.air.toml](.air.toml)). A build
error keeps the last good server running. From the repo root, `mise run server`
does the same as `air`, and `mise run dev` also starts the frontend.

A healthy start logs:

```
loaded environment from ../.env
demo login: player@badmintonpro.local (already seeded)
external login providers: [google]
BadmintonPro API listening on :8080
```

`no external login providers configured` means Google credentials weren't
found. Password login still works.

**5. Sign in** through the frontend at http://localhost:3000 with the demo
account `player@badmintonpro.local / smash123`.

## Environment variables

All optional.

| Variable | Default |
| --- | --- |
| `DATABASE_URL` | local Docker database |
| `PORT` | `8080` |
| `COOKIE_SECURE` | `false`. Set `true` anywhere served over HTTPS |
| `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | unset, so Google sign-in is off |
| `GOOGLE_REDIRECT_URL` | `http://localhost:3000/api/auth/google/callback` |
| `DEMO_EMAIL`, `DEMO_PASSWORD` | `player@badmintonpro.local`, `smash123` |

Full descriptions are in [Configuration](../README.md#configuration).

## Tests

```bash
go test ./...
```

Tests use the in-memory store and need no database. To also run the store tests
against a throwaway Postgres, see [Tests](../README.md#tests).

## Troubleshooting

- **`connection refused` on port 5432.** Postgres isn't running. Run
  `docker compose up -d` from the repo root.
- **`relation "users" does not exist`.** The database has no schema yet. Run
  `go run . -migrate`.
- **`address already in use` on port 8080.** Another server is still running.
  Stop it, or start with `PORT=8081`, and set `BACKEND_URL` in the frontend to
  match.
