# BadmintonPro

A badminton club and match management app — Next.js frontend, Go backend, Postgres.

> **Status:** early scaffold. Sign-in works end to end with a phone code or
> Google, against a real database. Players, matches and rankings are not built
> yet.

## Stack

| Layer | Choice |
| --- | --- |
| Frontend | Next.js 16 (App Router), React 19, TypeScript, Tailwind v4 |
| Backend | Go 1.26, standard library `net/http` |
| Database | Postgres 17 via Docker Compose locally, or a hosted Postgres such as Neon; `pgx` driver |
| Migrations | golang-migrate, embedded in the binary |
| Auth | httpOnly session cookie; phone codes via Twilio Verify (`internal/otp`); Google via OpenID Connect (`x/oauth2`, `go-oidc`) |

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

**No rate limiting of our own yet** (see `TODO.md`, "Rate limiting"). Only
the vendor's limits apply: Twilio Verify allows 5 sends per number per 10
minutes and 5 checks per code, blocks suspicious traffic with Fraud Guard,
and only sends to the countries in its geo permissions. A request Twilio
refuses as too many gets `429` with `Retry-After`. The console provider only
limits wrong guesses (5 per code). Add our own limits back before relying on
`OTP_PROVIDER=twilio` anywhere public: every SMS costs money.

### Payments (optional)

Entry fees go through an adapter chosen by `PAYMENT_PROVIDER`
(`backend/internal/payments`), like OTP. Unset, paid entries can't be paid
for (`503 "payments aren't set up"`); free events still confirm.

**Locally, no account needed:** `PAYMENT_PROVIDER=fake` in `backend/.env`.
The frontend's fake checkout makes up a `pay_fake_…` payment and signs it
`fake:<orderId>|<paymentId>`; every payment is captured and every refund
succeeds. Its orders live in memory, so restarting the backend forgets them
(pay again from a new entry). `fake` refuses to start when
`COOKIE_SECURE=true`.

**Razorpay test mode** (no real money):

1. In the [Razorpay dashboard](https://dashboard.razorpay.com), switch to
   **Test Mode**, then **Account & Settings → API Keys → Generate Test Key**.
2. In `backend/.env` (gitignored):

   ```bash
   PAYMENT_PROVIDER=razorpay
   RAZORPAY_KEY_ID=rzp_test_…
   RAZORPAY_KEY_SECRET=…
   ```

   The backend logs `payments: razorpay, test mode, webhook disabled …`.
3. Pay in Checkout with Razorpay's
   [test cards or UPI IDs](https://razorpay.com/docs/payments/payments/test-card-details/).
   The Mastercard `5267 3181 8797 5449` (any future expiry, any CVV) goes
   through to the mock bank page; pick **Success** there. UPI
   `success@razorpay` skips the bank page. Untick "Save this card", or
   Razorpay texts a real OTP to verify your number. The page's
   `POST /api/payments/verify` confirms the entry.
4. **Webhooks** (optional locally, required in production, because the
   page may never get back to verify): Razorpay must reach the backend, so
   expose it with a tunnel such as `ngrok http 8080`. In the dashboard,
   **Webhooks → Add New Webhook**: URL
   `https://<your-tunnel>/api/payments/webhook`, a secret of your choice,
   events `payment.captured` and `order.paid`. Put the same secret in
   `RAZORPAY_WEBHOOK_SECRET` and restart. Without it the webhook answers
   `503`.
5. **Mock webhook** (local, no tunnel): set `RAZORPAY_WEBHOOK_SECRET` in
   `.env` to any value and restart the backend. Start a payment in the app
   and close Checkout without paying; the backend logs
   `payments: order order_… for entry ent_…`. Then, from `backend/`:

   ```sh
   go run . -mock-webhook order_…
   ```

   It reads the order's amount from Razorpay, signs a `payment.captured`
   event with your secret exactly as Razorpay does, and posts it to
   `http://localhost:8080/api/payments/webhook` (change with
   `-webhook-url`). The entry is confirmed as if the player had paid and
   closed the tab. Running it again resends the same payment, so it
   answers `already applied`, as a real Razorpay retry would. It refuses
   live keys. A made-up payment can't be refunded: if the backend decides
   to refund it (the hold ran out and the event filled up), Razorpay
   answers 404 and the payment stays `refund_pending`.

Live keys (`rzp_live_…`) work the same way; never commit either kind.

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

### Demo data

```bash
go run . -seed-demo    # three Bangalore venues with courts, events in the coming week, and 16 demo players with paid entries in "Midweek Rally Night"
```

It rewrites the same rows each time and moves the events to the next matching
days, so re-run it whenever the demo events have passed. Like `-migrate` it
never runs on startup. It also **refuses a `DATABASE_URL` that isn't on this
machine**, because the repo-root `.env` may point at the shared database; add
`-allow-remote` only if you mean to write demo data there.

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

"admin" routes need a session and go through one `requireAdmin` check. For
now that only requires being signed in, so **anyone signed in can use admin
mode**; it will check roles later (see the design doc, section 10).

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| `GET` | `/api/health` | — | Liveness check |
| `POST` | `/api/logout` | cookie | Revokes the session and clears the cookie |
| `GET` | `/api/me` | cookie | `{"user","mode","isJudge"}`. `user`: the signed-in user, including `username`, contact `email`/`phone` with `emailVerified`/`phoneVerified`, `signInMethod` and `profileComplete`. `mode`: `"player"` or `"judge"`, the home page this session lands on. `isJudge`: whether the account is a registered judge right now |
| `POST` | `/api/me/mode` | cookie | `{"mode":"player"\|"judge"}`: switches the home page without signing in again; returns the same as `GET /api/me`. `"judge"` needs a registered judge (`403` otherwise) |
| `GET` | `/api/judge/home` | judge | `{"judge","events","matches"}`: the registered judge (`userId`, `name`, `phone`, `username`, `createdAt`), the events they're listed for that haven't ended or been cancelled, and the matches assigned to them that aren't cancelled (including today's finished ones), soonest first, in the shape of `/api/me/matches` without `mySide`. `403` for anyone who isn't a registered judge, checked on every request |
| `PUT` | `/api/me/profile` | cookie | `{"name","username","email"?}`: the profile step. `email` only for accounts without a verified one (stored unverified; `""` clears it). `409` if the username is taken |
| `POST` | `/api/me/phone/start` | cookie | `{"phone"}`: texts a code to a phone a Google account wants on its profile. Same validation as phone sign-in. `400` for phone accounts |
| `POST` | `/api/me/phone/verify` | cookie | `{"phone","code"}`: saves it as a **verified contact phone**. It does not become a way to sign in |
| `GET` | `/api/auth/providers` | — | Enabled sign-in methods, e.g. `{"providers":["google"],"phone":true}`. `providers` are redirect logins; `phone` is separate because it isn't one |
| `POST` | `/api/auth/phone/start` | — | `{"phone"}` in any common format; texts a code and returns `{"phone"}` in E.164. The reply is the same whether or not the number has an account. `429` with `Retry-After` if the OTP provider refuses as too many. Only when `OTP_PROVIDER` is set |
| `POST` | `/api/auth/phone/verify` | — | `{"phone","code","as"?}`; signs in (creating the account on first login) and sets the session cookie. `401` for a wrong or expired code. With `"as":"judge"` ("Log in as judge") only a number an admin registered as a judge gets in, the session starts in judge mode, and the reply is `{"user","mode","isJudge"}`; any other number gets `403` and **no account is created**. The judge check happens after the code, so only the number's owner learns whether it's registered |
| `GET` | `/api/events` | — | Published events that haven't ended, in start order: `{"events":[…],"nextCursor"}`. `?city=bangalore` filters by venue city. `?lat=&lng=&radiusKm=` keeps venues within that distance (default 10 km, at most 100) and adds `distanceKm` to each event; venues without coordinates drop out. `?limit=` is 1–50 (default 20), and `?cursor=` takes the previous page's `nextCursor` (`null` on the last page). Each event has its `formats`, `slotMinutes` per format, `feePaise`, `capacity`, `maxMatches`, `spotsLeft` (capacity minus live entries), `registrationClosesAt` and an optional Elo band (`ratingMin`/`ratingMax`). Public: it says nothing about who joined |
| `GET` | `/api/events/{id}` | — | One event with its venue and courts. Drafts are `404`; cancelled and completed events stay visible |
| `POST` | `/api/events/{id}/entries` | cookie | `{"formats":["singles","doubles"],"partnerUsername"?:"@asha"}`: joins with an **open entry** (one match), `201 {"entry"}` as `pending_payment` with a 10-minute `holdExpiresAt`. A partner only with doubles or mixed. `400` bad formats or partner, `403` profile not set up, `404` unknown event, draft or partner, `409` closed, cancelled, outside the Elo band, already 3 entries, full, or no matches left. Pay for it with `POST /api/me/entries/{id}/pay` |
| `GET` | `/api/me/entries` | cookie | `{"entries":[…]}`, newest first. Each entry: `id`, `kind`, `formats`, `partner` (`{"id","username","name"}` or `null`), `source`, `status`, `holdExpiresAt`, `holdExpired` (an unpaid entry whose hold ran out; it no longer counts), `createdAt`, `event` (`id`, `title`, `status`, times, `feePaise`, `currency`, `venue`), and `challengeId` (a challenge's `locked` entry; `null` for an open one) |
| `DELETE` | `/api/me/entries/{id}` | cookie | Cancels your own `pending_payment` entry: `200 {"entry"}` with `status: "cancelled"`; the row is kept, and its open order is marked failed (a payment that still arrives is refunded). `404` if it isn't yours, `409` once paid or already cancelled, or for a challenge's entry (withdraw the challenge instead) |
| `POST` | `/api/me/entries/{id}/pay` | cookie | Starts paying for your held entry (open, or a challenge's locked entry, held until the challenge's deadline): `200 {"checkout":{"provider":"razorpay"\|"fake","keyId","orderId","amountPaise","currency","name":"BadmintonPro","description":"<event title> · 1 entry","prefill":{"name","email","contact"}}}`. Creates a provider order, or reuses the entry's open one for the same amount. A free event (`feePaise` 0) confirms at once and returns `200 {"entry"}` instead. `404` not yours, `409` not `pending_payment`, hold expired or event not published, `503` payments not set up, `502` the provider failed |
| `POST` | `/api/payments/verify` | cookie | `{"orderId","paymentId","signature"}` from the checkout: checks the signature, fetches (and if only authorized, captures) the payment, then confirms: `200 {"entry"}`, normally `confirmed`. A payment that arrives after the hold ran out is confirmed if the event still has room, else the entry comes back `refunded` and the payment is refunded. A second payment for an entry already paid for is refunded. Repeats change nothing. `400` bad body or signature, `404` unknown order or not yours, `409` payment failed or for another order or amount, `502` provider error, `503` not set up |
| `POST` | `/api/payments/webhook` | signature | The provider's webhook (no session): `X-Razorpay-Signature` must be the HMAC of the raw body (at most 64 KB) with `RAZORPAY_WEBHOOK_SECRET`. `payment.captured` and `order.paid` confirm like verify; repeats and out-of-order events are no-ops. `200 {"status"}`: `applied`, `already applied` or `ignored` (other events, unknown orders). `400` bad signature, `503` no webhook secret or payments not set up, `500` (retried by the provider) on our own failure |
| `GET` | `/api/me/matches` | cookie | `{"matches":[…]}`, soonest first, every status. Each: `id`, `status`, `source` (how it was made: `matcher`, `admin`, `challenge`; `arrival` and `desk` are reserved), `format`, `event` (`{"id","title"}`, `null` without one), `venue`, `court` (`{"id","label"}`), `startsAt`, `endsAt`, `judge` (`{"userId","name","username"}` or `null`), `sides` (`[{"side":"A","players":[{"userId","name","username","rating"}],"rating"}]`, ratings current, side rating the average), `mySide`, `winnerSide` and `challengeId` (the challenge it was fixed for, or `null`) |
| `POST` | `/api/challenges` | cookie | `{"eventId","opponentUsername":"@ravi","preferredStart":"2026-10-17T19:30:00+05:30"}`: sends a **singles challenge**, `201 {"challenge"}` as `pending`. `preferredStart` is RFC 3339, cut to the minute, and must leave a singles slot inside the event's window. The opponent must accept and both must pay by `expiresAt`: 4 hours after sending, or the registration cutoff if sooner. Checked now and again on accepting: both profiles complete, event published and before its cutoff, offers singles, both singles ratings in its band, neither at 3 entries counting this one, room for two entries and one match, and no other open (`pending`/`accepted`) challenge between the two for the event. `400` bad body, time, yourself or no singles, `403` your profile isn't set up, `404` unknown event, draft or opponent, `409` the rest |
| `GET` | `/api/me/challenges` | cookie | `{"challenges":[Challenge]}`, sent and received, at most 50: `pending` and `accepted` first, then the rest, newest first. Any past their deadline are expired (and refunded) first. A Challenge: `id`, `status` (`pending`, `accepted`, `scheduled`, `declined`, `withdrawn`, `expired`, `failed`), `role` (`challenger` or `opponent`, yours), `format` (`singles`), `event` (`id`, `title`, `startsAt`, `endsAt`, `registrationClosesAt`, `feePaise`, `currency`, `venue`), `challenger` and `opponent` (`{"id","username","name","rating"}`, singles rating), `preferredStart`, `expiresAt`, `createdAt`, `respondedAt` (or `null`), `myEntry` (`{"id","status","holdExpiresAt"}` once accepted, else `null`; pay it with `POST /api/me/entries/{id}/pay`), `theirEntryPaid`, and `match` (`{"id","court":{"id","label"},"startsAt","endsAt","judge":{"userId","name","username"} or null}` once fixed, else `null`) |
| `GET` | `/api/challenges/{id}` | cookie | `{"challenge"}`. Only its two players; `404` for anyone else. Expires it first if its deadline has passed |
| `POST` | `/api/challenges/{id}/accept` | cookie | The opponent accepts a `pending` challenge: the rules are checked again under the event lock, and a `locked` `pending_payment` entry is made for each player, held until `expiresAt` (two entries against capacity, one match against `maxMatches`). `200 {"challenge"}`. When the second of the two payments is confirmed, the **match is fixed at once**: the free slot nearest `preferredStart` (earlier on a tie) on the event's courts, keeping a slot's gap from each player's other matches, with a free judge or none. No slot left: `failed`, and both are refunded. Not both paid by `expiresAt`: `expired`, and whoever paid is refunded (the server sweeps every 30 seconds, and reading or acting on a challenge expires it too). `403` you sent it or your profile isn't set up, `404` not yours, `409` not pending, expired, or a rule fails |
| `POST` | `/api/challenges/{id}/decline` | cookie | The opponent declines a `pending` challenge: `200 {"challenge"}` as `declined`. `403` you sent it, `409` not pending or expired |
| `POST` | `/api/challenges/{id}/withdraw` | cookie | Pulls out before the match is fixed: the challenger while `pending` or `accepted`, the opponent once `accepted`. `200 {"challenge"}` as `withdrawn`; unpaid entries are cancelled and paid ones refunded. `409` otherwise (scheduled, already over, or the opponent before accepting: decline instead) |
| `GET` | `/api/me/recent-players` | cookie | `{"players":[{"id","username","name","rating"}]}`: up to 10 people you've shared a match with or challenged (either way), most recent first (a match by its start, a challenge by when it was sent). Complete profiles only |
| `GET` | `/api/players/nearby?lat=&lng=&radiusKm=` | cookie | `{"players":[…]}` as above: up to 10 other players with an entry that isn't cancelled from the last 60 days at a venue within the radius (default 10 km, at most 100; `lat` and `lng` required, `400` otherwise), closest singles rating to yours first, then the most recent entry. For Home's "Challenge people near you" |
| `GET` | `/api/users/search?q=` | cookie | `{"users":[{"id","username","name"}]}`, at most 20: exact, prefix, then contains matches, plus fuzzy ones when fewer than 5 and `q` has 3+ characters. `q` ignores case and a leading `@`; empty gives `[]`. Leaves out you and incomplete profiles, never shows phone or email. 30 searches a minute per user, then `429` with `Retry-After` |
| `GET` | `/api/admin/venues` | admin | Every venue with its courts |
| `POST` | `/api/admin/venues` | admin | `{"name","address","city","lat"?,"lng"?,"timezone"?,"courts"}`: creates a venue and `courts` courts named "Court 1" … "Court n". Time zone defaults to `Asia/Kolkata` |
| `POST` | `/api/admin/venues/{id}/courts` | admin | `{"label"}`: adds a court. `409` if the venue already has that label |
| `GET` | `/api/admin/events` | admin | Events in every status, latest first; `?status=draft` and so on to filter |
| `POST` | `/api/admin/events` | admin | Creates a **draft**: `{"venueId","title","formats","slotMinutes"?,"date":"2026-10-17","startTime":"19:30","endTime":"22:30","registrationClosesMinutesBefore"?,"feePaise","capacity","maxMatches","ratingMin"?,"ratingMax"?,"courtIds"}`. Times are the venue's local times; `maxMatches` can't exceed whole slots on the chosen courts. `400` with a message on invalid input; `409` naming the court and event if another event that isn't cancelled uses one of the courts at an overlapping time (also checked on update and publish) |
| `GET` | `/api/admin/events/{id}` | admin | `{"event","judges"}`, drafts included |
| `PUT` | `/api/admin/events/{id}` | admin | Same body as create. Drafts only: `409` once published |
| `POST` | `/api/admin/events/{id}/publish` | admin | Draft → published (players can see it). `409` from any other status or once the start has passed |
| `POST` | `/api/admin/events/{id}/cancel` | admin | Draft or published → cancelled. Events are never deleted |
| `POST` | `/api/admin/events/{id}/judges` | admin | `{"userId"}`: adds a **registered judge** (from `GET /api/admin/judges`). `400` for anyone who isn't registered |
| `DELETE` | `/api/admin/events/{id}/judges/{userId}` | admin | Removes a judge from the event |
| `GET` | `/api/admin/judges` | admin | `{"judges":[…]}`: everyone who can log in as judge, by name. Each: `userId`, `name` and `phone` (E.164) as the admin entered them, `username` (empty until they set up a player profile), `createdAt` |
| `POST` | `/api/admin/judges` | admin | `{"name","phone"}`: registers a judge. The number finds its phone account, or creates one (named after the judge) if it has never signed in, so it's the same account the person plays with. `201 {"judge"}`; `400` bad name or number, `409` already a judge, `503` phone login disabled |
| `DELETE` | `/api/admin/judges/{userId}` | admin | Removes a judge: they come off the judge list of every event that hasn't ended (matches already scheduled keep them), and judge-mode sessions fall back to player mode. Their account is untouched. `404` if not a judge |
| `GET` | `/api/admin/events/{id}/entries` | admin | `{"entries":[…]}`, every status, in join order. Each: `id`, `user` (`{"id","username","name"}`), `ratings` (per format of the entry), `kind`, `formats`, `partner`, `source`, `status`, `holdExpiresAt`, `holdExpired`, `matchId`, `standbyReason`, `confirmedBy` (the admin, or `null`), `confirmedAt`, `createdAt` |
| `POST` | `/api/admin/entries/{id}/mark-paid` | admin | The desk's offline payment (cash or UPI to the venue) for an open entry (`409` for a challenge's, which is paid in the app): `pending_payment` → `confirmed`, recording who and when, and an offline payment for the event's fee (none for a free event), `200 {"entry"}`. An expired hold is fine if the event still has room (capacity, `maxMatches`, 3 per player, checked under the event lock); the registration cutoff doesn't apply. `404` unknown, `409` not pending, event not published or over, or no room |
| `GET` | `/api/admin/events/{id}/schedule` | admin | The saved schedule, in the same shape as the preview: every match that isn't cancelled (published ones and any made by hand), with each player's current rating and the flags worked out again. Each match also has `status`, `source` (`matcher`, `admin` for one made by hand, `challenge`; `matcher` in a preview) and `challengeId` (or `null`; a challenge's match can be moved but not replaced or cancelled). Before a publish it shows the matches made by hand with `publishedAt` `null`. `404` for an unknown event, or while nothing is published and no match has been made by hand |
| `POST` | `/api/admin/events/{id}/schedule/preview` | admin | Runs the matcher on the confirmed open entries (never a challenge's locked ones), working around matches already fixed (challenges: their court time is taken, their players and judge are busy, and they count towards `maxMatches`), and returns `{"schedule"}` without saving: `eventId`, `publishedAt`, `matches` (`id` (`null` in a preview), `format`, `court` `{"id","label"}`, `startsAt`, `endsAt`, `judge` or `null`, `sides` `[{"side","players":[{"userId","name","username","rating","entryId"}],"rating"}]`, `flags` such as `"rating gap 240"`, `"gender balance not checked"`, `"no judge"`), `standby` (`[{"entryId","user","reason"}]`, reason `"no opponent"`, `"no free slot"` or `"format not offered"`) and `warnings` (strings). `404` unknown event |
| `POST` | `/api/admin/events/{id}/schedule/publish` | admin | Same, in one transaction with the event locked, and saves it: matches and sides, entries to `scheduled` (with `matchId`) or `standby` (with the reason), the publish time. `201 {"schedule"}` with match IDs. Published events only; once per event in version 1. `409` not published, already published, or the entries make no match |
| `POST` | `/api/admin/events/{id}/matches` | admin | Makes a match by hand (published events, before or after the schedule is published): `{"format","sideA":[entryId],"sideB":[entryId],"courtId"?,"startsAt"?}` (RFC 3339), one entry a side in singles, two in doubles and mixed, each a waiting paid open entry of the event (`confirmed` or `standby`, in no match) that allows the format. A given court and start must be the event's court, inside the window, free, and keep every player a slot's gap from their other matches; left out, the earliest such slot (first court on a tie). The judge is picked as the matcher does, or none with the `"no judge"` flag. `201 {"match"}`, a schedule match with `id`, `status`, `challengeId` and `flags`; the entries become `scheduled`. `400` bad body or rule (format, sizes, an entry twice, a player twice or on both sides, an entry not waiting, a court not the event's, a start outside the window), `404` unknown event or entry, `409` court taken ("Court 2 is taken at 8:30 PM"), a player's gap ("Rohan already plays at 8:00 PM"), `maxMatches` reached, no free slot, event not published |
| `POST` | `/api/admin/events/{id}/matches/suggest` | admin | `{"format","entryIds":[…],"courtId"?,"startsAt"?}`: the same checks and choice, saving nothing. `200 {"slot":{"court":{"id","label"},"startsAt","endsAt","judge":{"userId","name","username"} or null,"flags":[…]}}` (a rating gap is only flagged in singles: the sides aren't chosen yet). Same errors |
| `GET` | `/api/admin/events/{id}/free-slots?format=singles&matchId=` | admin | `{"slots":[{"court":{"id","label"},"startsAt","endsAt"}]}`: every start from the event's start, stepping by the format's slot length, at which a court is free, by time then court. With `matchId` that match's own time counts as free (for moving it). Players aren't checked. `400` no format or one the event doesn't offer, `404` unknown event or match |
| `POST` | `/api/admin/matches/{id}/replace` | admin | `{"outEntryId","inEntryId","outcome":"no_show"\|"back_to_pool"}`: swaps an entry of a `scheduled` or `ready` match for a waiting paid entry of the event that allows its format, whose player isn't in the match and keeps the gap. The outgoing entry becomes `no_show` (no refund) or goes back to the pool (`confirmed`); a judge who comes in as a player is replaced. `200 {"match"}`. `400` bad body, entry not in the match or not waiting, player already in it; `404` unknown match or entry; `409` a challenge's match, not scheduled, the gap |
| `POST` | `/api/admin/matches/{id}/move` | admin | `{"courtId","startsAt"}`: a new court and start for a `scheduled` or `ready` match (a challenge's too), checked like making one, ignoring the match itself. The judge stays if still free, else another or none. `200 {"match"}`. `400`, `404`, `409` as above |
| `POST` | `/api/admin/matches/{id}/cancel` | admin | Cancels a `scheduled` or `ready` match: `200 {"match"}` with `status` `cancelled`; its open entries go back to the pool (`confirmed`). `404` unknown, `409` not scheduled or a challenge's (challenges end by withdrawing or refunding) |
| `GET` | `/api/admin/events/{id}/changes` | admin | `{"changes":[{"id","matchId","action","admin":{"id","username","name"},"details","createdAt"}]}`, newest first, at most 100: one per fixture edit, `action` `create`, `replace`, `move` or `cancel`, `details` the before and after (court, times and judge; entries in and out with the outcome; entries released). `404` unknown event |
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

**Judges are phone accounts an admin has registered** (`judges`), not a
separate kind of user. An admin adds them under Admin → Judges by name and
number; they sign in with the quiet "Log in as judge" link on the sign-in
page (phone only) and land on `/judge`. Signing in normally with the same
number reaches the same account in player mode, so judges can play too, and
can switch views from either home page. The session's mode only picks the
home page; judge endpoints check `judges` on every request. See section 10
of the design doc.

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
| `PAYMENT_PROVIDER` | Enables payments: `fake` (made-up payments; local only) or `razorpay` (see *Payments*) | — (disabled) |
| `RAZORPAY_KEY_ID` | Razorpay key ID, `rzp_test_…` or `rzp_live_…`; sent to the browser's checkout | — |
| `RAZORPAY_KEY_SECRET` | Razorpay key secret: API calls and checkout signatures | — |
| `RAZORPAY_WEBHOOK_SECRET` | The secret set on the Razorpay webhook; enables `/api/payments/webhook` | — (webhook disabled) |

**Frontend** — copy `frontend/.env.example` to `frontend/.env.local`:

| Variable | Description | Default |
| --- | --- | --- |
| `BACKEND_URL` | Where the dev server proxies `/api/*`. Server-side only | `http://localhost:8080` |
| `NEXT_PUBLIC_GOOGLE_MAPS_EMBED_KEY` | Optional. A Google Maps Embed API key for the venue maps; without one they use Google's keyless embed. Restrict the key to your site's domains, since it is visible in the browser | unset |

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
│   ├── phone_handlers.go    # phone sign-in: send and check codes ("as": "judge" too)
│   ├── judges*.go           # judge accounts: registry, judge sign-in, session mode, judge home
│   ├── profile_handlers.go  # profile step, contact-phone verification
│   ├── events.go            # venues, courts, events; EventStore interface
│   ├── events_postgres.go   # events queries (Postgres only, no in-memory copy)
│   ├── events_handlers.go   # public GET /api/events and /api/events/{id}
│   ├── seed.go              # -seed-demo data
│   ├── admin.go             # admin mode: AdminStore interface, venue/event validation
│   ├── admin_postgres.go    # admin queries
│   ├── admin_handlers.go    # /api/admin/*, behind requireAdmin
│   ├── entries*.go          # joining events: entries, holds, the capacity check
│   ├── matcher.go           # the matching algorithm: a pure function, no database
│   ├── matches*.go          # schedule preview and publish, mark-paid, a player's matches
│   ├── fixtures*.go         # fixture editing by hand: make, suggest, replace, move, cancel; the audit
│   ├── payments*.go         # pay, verify, webhook; the one confirmation path
│   ├── challenges*.go       # challenges: send, accept, pay, the fixed slot, expiry; player suggestions
│   ├── search*.go           # username search (pg_trgm)
│   ├── ratelimit.go         # in-memory sliding-window limiter
│   ├── phone.go             # phone number normalisation to E.164
│   ├── store.go             # Store interface, hashing, IDs
│   ├── memory_store.go      # in-memory implementation (tests)
│   ├── postgres_store.go    # Postgres implementation
│   ├── migrate.go           # embedded migration runner
│   ├── migrations/
│   ├── internal/oauth/      # Provider interface, registry, Google adapter
│   ├── internal/otp/        # OTP Provider interface; Twilio and in-memory adapters
│   ├── internal/payments/   # payment Provider interface; Razorpay and fake adapters
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
- **No rate limiting of our own on phone codes.** Only the OTP vendor's
  limits apply (see *Phone sign-in*); ours are planned in `TODO.md`.
- **A refund the provider refuses isn't retried.** The payment stays
  `refund_pending` (and is logged) until a retry job exists; refund it from
  the Razorpay dashboard meanwhile. Refunds for cancelling a paid entry wait
  for the refund policy (design doc, open question 2).
- **Apple will need `SameSite=None; Secure` on the OAuth flow cookie**, and so
  HTTPS. Its callback is a cross-site POST, which `Lax` cookies aren't sent on.
  Google is unaffected.
- **`sslmode=disable`** in the default connection string. Correct for a local
  container, wrong anywhere else.

## License

Not yet chosen. All rights reserved until one is added.
