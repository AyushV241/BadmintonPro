# Backend package structure

**Status: Proposed, not started.** Written 2026-10-23. Pick this up once no
other work is in flight in `backend/` (see [When to do it](#when-to-do-it)).

## The problem

The whole backend is one `package main`: about 60 files and 17k lines at the
top of `backend/`. It is already grouped by feature, but only by naming
convention. Each domain has:

- `<domain>.go`: types, validation and its store interface
- `<domain>_handlers.go`: HTTP handlers, as methods on `*API`
- `<domain>_postgres.go`: Postgres queries, as methods on `*PostgresStore`
- `<domain>_test.go`

Nothing stops one domain reaching into another's internals. `API` has grown to
about 20 fields (one store per domain plus providers), and `PostgresStore`
carries about 60 methods across every domain. Every handler depends on the
whole `API` struct.

## Target layout

Packages by **domain**, not by layer. Each domain keeps the same pieces it has
today, in its own package.

```
backend/
├── cmd/server/main.go        # flags (-migrate, -seed-demo), config, wiring only
└── internal/
    ├── platform/             # shared plumbing, no business logic
    │   ├── config/           # env.go (.env loading), typed config
    │   ├── db/               # pgx pool, querier helpers, migrate.go, migrations/
    │   └── httpx/            # WriteJSON/WriteError, JSON decoding, rate limiter,
    │                         #   validation errors
    ├── auth/                 # users, sessions, phone + Google login, profile,
    │   │                     #   RequireUser middleware
    │   ├── oauth/            # moved from internal/oauth
    │   └── otp/              # moved from internal/otp
    ├── ratings/              # Elo tables: loadRatings, bandRatings, inBand
    ├── events/               # venues, courts, events (public + admin management)
    ├── entries/              # joining an event, username search
    ├── matching/             # matcher.go: the pure scheduling algorithm
    ├── matches/              # schedule, desk, player matches, live scoring
    ├── judges/               # judge accounts, judge home
    ├── challenges/
    ├── payments/             # handlers + store
    │   └── provider/         # moved from internal/payments (fake, Razorpay)
    ├── seed/                 # demo data for -seed-demo
    └── server/               # builds the mux: mounts each domain's routes,
                              #   the /api/admin guard
```

Each domain package exposes a `Handler` with a `Register(mux)` method, and its
own Postgres store that satisfies its own interface. `server/` composes them.
The big `API` struct and the single `PostgresStore` go away.

### Where today's files go

| Today (`backend/`) | Moves to |
| --- | --- |
| `main.go` | `cmd/server/main.go` (wiring), `internal/server/` (routes) |
| `api.go` | `internal/server/` (mux), `internal/platform/httpx/` (JSON helpers) |
| `env.go` | `internal/platform/config/` |
| `migrate.go`, `migrations/` | `internal/platform/db/` |
| `ratelimit.go` | `internal/platform/httpx/` |
| `store.go`, `postgres_store.go`, `memory_store.go`, `session.go` | `internal/auth/` |
| `phone.go`, `phone_handlers.go`, `oauth_handlers.go`, `profile_handlers.go` | `internal/auth/` |
| `events*.go` | `internal/events/` |
| `admin*.go` | split: venue and event management to `internal/events/`, judge list to `internal/judges/`, the guard to `internal/server/` |
| `entries*.go`, `search*.go` | `internal/entries/` |
| `matcher*.go` | `internal/matching/` |
| `matches*.go` | `internal/matches/` |
| `judges*.go` | `internal/judges/` |
| `challenges*.go` | `internal/challenges/` |
| `payments*.go`, `mockwebhook*.go` | `internal/payments/` |
| `seed.go` | `internal/seed/` |
| `internal/oauth`, `internal/otp` | `internal/auth/oauth`, `internal/auth/otp` |
| `internal/payments` | `internal/payments/provider` |

## Decisions

- **By domain, not by layer.** Folders like `handlers/`, `models/` and
  `store/` would make every package import every other one. Grouping by domain
  keeps a change to challenges inside `challenges/`.
- **No separate `admin` package.** Admin work belongs to the domain it
  changes. `server/` mounts every `/api/admin/*` route behind one `RequireAdmin`
  guard, so adding the `role_assignments` check later (see
  [events-and-matches.md](events-and-matches.md), section 10) is still a change
  to one function.
- **Auth becomes middleware.** `requireUser`, `requireAdmin` and `requireJudge`
  are methods on `API` today, which is what ties every handler to that struct.
  They become middleware that puts the user in the request context, read with
  something like `auth.UserFrom(ctx)`.
- **`matching/` stands alone.** `runMatcher` is pure (no database, no clock,
  no randomness), so it has no dependencies and the most to gain from staying
  isolated.
- **`ratings/` is its own package.** `loadRatings` is used by entries, matches
  and challenges. Leaving it in any one of them would create an import cycle.
- **Shared helpers that become exported:** `writeJSON`, `writeError`,
  `validationError`, `normaliseUsername`, `loadRatings` and the `querier` /
  `rowsQuerier` interfaces. Each gets a home in `platform/` or `ratings/`.

## Costs

- **Many renames.** Anything used across packages has to be exported
  (`httpx.WriteJSON`, `ratings.Load`, …). This is most of the diff, and it is
  mechanical.
- **Tests move with their code.** Some reach into package internals today and
  may need small test helpers in their new package.
- **Commands change.** `go run . -migrate` becomes
  `go run ./cmd/server -migrate`. Update `backend/.air.toml` (build command),
  `.mise.toml`, the root and backend READMEs, CI, and file references in
  `events-and-matches.md`.
- **No behaviour change.** No API, database or frontend changes.

## When to do it

Every file in `backend/` gets moved, so this can't overlap with other work
there. Uncommitted changes would conflict on every file.

1. Everyone working in `backend/` commits and pauses.
2. Do the move on a branch, in steps. Each step must build and pass
   `go vet ./...` and `go test ./...`, and is its own commit:
   1. `cmd/server/` plus everything else into one temporary `internal/app`
      package, with no other changes;
   2. `platform/`, `matching/` and `ratings/` (the leaf packages);
   3. `auth/`, including the middleware change;
   4. one domain at a time: events, entries, matches, judges, challenges,
      payments, seed;
   5. delete `internal/app`; update docs, air, mise and CI.
3. Use `git mv` so `git log --follow` keeps each file's history.
4. Merge, then everyone pulls before starting new backend work.
