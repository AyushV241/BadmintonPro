# Events, matches and ratings

The core logic of BadmintonPro: how players find an event, pay for a slot,
get matched, play a judged match with live scoring, and have their rating
change. Read this before working on events, entries, payments,
matchmaking, scoring or ratings.

Each rule below is tagged as one of:

- **Decided:** the product owner chose it. Don't change it without asking.
- **Proposed:** a default chosen while writing this doc. Build it this way
  unless it's revisited.
- **Open:** not decided yet. Ask before building anything that depends on it.

Last updated 2026-10-12.

---

## 1. The idea in one paragraph

Admins host **events** at venues on courts they've already booked. Every
match in an event comes from the event's **pool**. A player gets into the
pool by buying an **entry** (₹50), and **each entry is one match**. There
are two kinds:

- an **open entry**: the player says which formats they'll play, and the
  **matching algorithm** picks their opponent (or team for doubles);
- a **locked entry**: the player has **challenged** a friend (section 12),
  so the opponent is already fixed and the matcher only finds them a slot.

A player can hold several entries in the same event, such as one open
entry and one challenge, and pays for each. Every match gets a court, a
time and a **judge**. The judge scores the match live on their phone, and
players and spectators see the score update in real time. When the match
ends, both sides' **Elo ratings** change. Players who turn up at the venue
can still buy an entry if there are free slots.

Matches with no event at all, where two players arrange a game themselves
on a normal day, are **future work** (section 14). The model below keeps
matches separate from events so that can be added without a redesign.

---

## 2. Glossary

| Term | Meaning |
| --- | --- |
| **Venue** | A place with badminton courts. It has a city, coordinates and a time zone. |
| **Court** | One court at a venue, e.g. "Court 3". |
| **Event** | A session an admin runs at one venue, on a set of booked courts, over a time window on one day. Players buy entries into it. |
| **Format** | `singles`, `doubles` or `mixed` (mixed doubles). |
| **Entry** | One paid match for one player in an event. **Open**: the matcher picks the opponent. **Locked**: the opponent is fixed by a challenge. It also records whether it was bought online or as a walk-in. |
| **Pool** | All confirmed (paid) entries for an event. Every match in the event comes from the pool. |
| **Slot** | One court for one block of time, e.g. Court 2, 19:30–20:00. Each match uses exactly one slot. |
| **Match** | Two sides playing each other in one slot. A side is one player (singles) or two (doubles). |
| **Judge** | The person who scores a match live. Each match needs one. |
| **Rating** | A player's Elo rating for a discipline. The expected range is about 1200–2000. |
| **Challenge** | One player's request to play a named opponent at a chosen event and time. Once accepted and paid, it becomes two locked entries (section 12). |
| **Admin mode** | The part of the app for running events (section 10). |

---

## 3. Rules that must always hold

1. **Matches are central; events are one way matches are made.**
   (Decided.) A match row never assumes it belongs to an event:
   `matches.event_id` is nullable.
2. **Ratings only change because of a finished, scored match.** Every
   change is recorded with its before and after values against the match,
   so ratings can be audited and recalculated. Ratings are never edited
   directly.
3. **Money is always whole paise in integer columns**, never floating-point
   rupees.
4. **Times are stored as `timestamptz` in UTC** and shown in the venue's
   time zone.
5. **Nothing a payment or a match points at is ever deleted.** Events,
   entries and matches are cancelled (their status changes) instead.
6. **Capacity is checked inside a database transaction.** Two people can't
   both get the last spot.
7. **A match that players have already been told about is never quietly
   reshuffled.** Late changes, such as walk-ins or no-shows, only fill
   empty slots or replace a missing player. They never move someone who
   has already been notified. Only an admin moves a published match, by
   hand (section 10, "Editing fixtures by hand", built); the players see
   the change in the app, and are told once notifications exist.
8. **The server is the source of truth for scores.** The judge's app sends
   point events; the server checks them against the scoring rules and works
   out the score.
9. **One entry, one match, one payment** (Decided). An entry is never
   used for a second match, and a player pays for every match they're in.

---

## 4. Player flow, step by step

### 4.1 Discover events (Decided: by distance)

- The player sets a distance, e.g. within 5 km. The app asks the browser
  for the player's location:
  - if they allow it, events are sorted by distance;
  - if not, the app falls back to a city filter.
- The list shows the upcoming events in range (for example 5 events across
  different venues), each with:
  - venue, distance, date and time;
  - the formats on offer;
  - spots left and the entry fee.
- The API (built): `GET /api/events?lat=&lng=&radiusKm=` keeps events whose
  venue is within the radius (default 10 km, at most 100), **in start
  order**, and gives each event its `distanceKm`. The distance is computed
  in SQL with the haversine formula on `venues.lat`/`lng`; venues without
  coordinates drop out. `?city=` stays as the fallback when location is
  refused. PostGIS isn't needed at this scale. A "nearest first" sort can be
  added later, with a cursor on distance.

### 4.2 Join and pay (Decided: ₹50 per entry, one entry per match)

1. The player opens an event and taps **Join**, which buys an **open
   entry**. They pick:
   - **formats**: one or more of singles, doubles and mixed. The entry
     gets one match, in one of those formats;
   - **partner**, optional, for doubles: a partner who already holds their
     own live entry in this event for the same team format (Decided: every
     player pays for their own match). Without one, the matcher makes the
     team.

   To play a chosen opponent instead, they send a challenge (section 12).
2. **The spot is held while they pay** (Proposed: for 10 minutes):
   - An `entry` is created as `pending_payment`, and the event's capacity
     check and the insert happen in one transaction.
   - If payment doesn't succeed before the hold expires, the spot is
     released.
3. **Payment:**
   - It goes through a payment-provider adapter, built the same way as the
     OTP adapter (Proposed: Razorpay first).
   - The **provider's webhook** confirms the payment, not the browser
     redirect. Webhooks are idempotent: a repeated webhook changes nothing.
   - **Built (step 5)**, with these readings of the rules:
     - The page's verify call (signed pair from Checkout) also confirms,
       but only after the server re-fetches the payment from the provider
       (and captures it if only authorized). Verify and webhook share one
       confirmation that locks event → entry → payment, so whichever comes
       second is a no-op; the webhook still works if the page never returns.
     - A payment that lands after the hold expired is confirmed if the
       event still has room (the desk's checks); otherwise, or if the event
       was cancelled or the player cancelled meanwhile, the entry becomes
       `refunded` and the payment is refunded automatically.
     - A second payment for an entry already paid (online or at the desk)
       is refunded and leaves the entry alone (rule 9).
     - A refund counts as done once the provider accepts it (Razorpay
       "processed" or "pending"). One the provider refuses stays
       `refund_pending` for a retry job that isn't built yet.
     - Free events (`fee_paise` 0) confirm at once from the pay call, with
       no payment row, even when no provider is configured.
4. Once paid, the entry is `confirmed` and joins the pool. The player sees:
   **"You're in. We'll notify you once your match is scheduled."**
5. **Another match the same day** means buying another entry, with the
   same steps (Proposed: at most 3 entries per player per event).

**Event limits** (Decided: per venue, per day):

- `capacity`: the most entries the event takes.
- `max_matches`: the most matches it can host. This can't exceed the
  number of slots: courts × (event length ÷ slot length).

### 4.3 Scheduling (Decided: done by the matching algorithm)

- Locked entries (challenges) get their slot as soon as both players have
  paid (section 12).
- Open entries are matched when the matcher runs (section 6), which then
  publishes the schedule.
- For each match, the player is notified of:
  - their opponent or team;
  - the court and time;
  - the judge.
- Notifications are in-app first. SMS or WhatsApp come through a
  notification adapter later.

### 4.4 Match day

1. **Check-in.** (Proposed.) The player checks in once at the venue, by
   scanning the venue's QR code or being marked by the judge. That covers
   all their entries for the day. Check-in closes 10 minutes before their
   first slot.
2. **No-shows.** A player who misses a match is a no-show for that match:
   - its slot opens up for a walk-in or someone on standby (section 7);
   - their opponent gets a **walkover**, which doesn't change anyone's
     rating (Proposed);
   - the no-show loses that entry's fee.
3. **Live scoring.** The judge starts the match and scores each point
   (section 8). Players and spectators watch it live.

### 4.5 After the match (Decided: the rating changes)

- The judge ends the match, and the server records the result.
- Ratings update in the same transaction (section 9).
- Each player sees their rating change, e.g. 1480 → 1498 (+18), and the
  leaderboard moves.

---

## 5. Capacity and slots

- An event has:
  - booked courts;
  - a time window, `starts_at` to `ends_at`;
  - a **slot length** per format (Proposed defaults: singles 30 min,
    doubles and mixed 40 min, editable per event).
- **Each entry is exactly one match** (Decided). A player who wants more
  matches buys more entries; there are no free extra matches.
- Total slots = the sum over courts of how many slot lengths fit in the
  window.
- **Joining closes** when either:
  - confirmed and held entries reach `capacity`, or
  - the matches those entries need would exceed the available slots or
    `max_matches`. A locked pair needs exactly one match. An open entry is
    counted as half a match, the most it can need (singles); doubles uses
    less.

  The join endpoint enforces this inside a transaction.
- Slots the pool doesn't need are **spare**. They're used for walk-ins and
  replacements after no-shows.

---

## 6. Matching algorithm

**When it runs** (Decided with Ayush: rolling runs; not built yet):

1. **Joining opens when the event is published,** normally 4 days before
   it starts (question 21).
2. **Each category has its own pool and its own maximum,** e.g. men's
   singles or mixed doubles (question 20).
3. **A category that reaches its maximum closes to new entries and is
   matched straight away,** from the full pool.
4. **24 hours before the start,** every category that isn't full is
   matched with what it has, **complete matches only**: pairs in singles,
   groups of four in doubles and mixed. Anyone left over stays in the pool,
   and the unused slots stay open for more players.
5. **In the last 24 hours** joining stays open (question 22), and the
   matcher runs **every 4 hours** per category. Each run matches the new
   entries and anyone left over, but only where there are good enough
   opponents (question 23). The rest wait for the next run.
6. **On the spot:** players can join at the venue (walk-ins, section 7).
   Rather than waiting up to 4 hours, a confirmed walk-in triggers a run of
   its own (Proposed).
7. **Admins can edit fixtures by hand:** pair players, replace a player,
   move a match (section 10).
8. **Every run only fills gaps** (rule 7): published matches never move
   unless an admin moves them. **Each run publishes its matches straight
   away;** players see them in the app.
9. **Whoever is still unmatched when the event ends is refunded
   automatically.**

Why rolling runs: players in a full category get certainty early, and
late joiners can still play. The cost is that early runs pair from a
smaller pool, and late joiners get the slots that are left, so their
preferred time is more often missed.

**Running safely:** the runs are a background job inside the server. With
several servers on one database (developers sharing the hosted Postgres),
only one may run at a time. The job takes a Postgres advisory lock, and
it's switched on by a setting that is off by default for local
development.

**What it receives:**

- the open entries in the pool: each player's ratings per format, the
  entry's formats, partner if any, and gender if mixed was chosen;
- the slots already taken by scheduled challenges, which it never moves;
- the event's free slots;
- the event's judges.

**Pairing** (Proposed), per format, for open entries only:

1. **Singles:**
   1. Sort entries by the player's singles rating.
   2. Pair neighbours, so the rating gap is as small as possible.
   3. Pairs must be no more than about 200 apart. If that's impossible,
      allow the closest pair and flag it for the admin.
   4. Never pair a player with themselves (two entries of the same
      player), and no two players meet twice in the same event through
      open entries.
2. **Doubles:**
   1. Keep the partners who joined together.
   2. Make teams from the solo entries. Sort them by doubles rating; in
      each group of four, team the highest with the lowest and the middle
      two together, then those two teams play.
   3. A team's rating is the average of its two players.
3. **Mixed:** like doubles, but each team is one man and one woman. This
   needs a gender field on the profile, asked only when someone chooses
   mixed (Open: wording and options).
4. **Every confirmed entry gets exactly one match.** An entry that allows
   several formats is placed in whichever format pairs it best.
5. **Leftover entries** that couldn't be matched, such as an odd one out:
   - first, they go on **standby** for walk-ins and no-shows;
   - if still unmatched at the end, they're **refunded automatically**
     (Proposed).

**Location isn't an input** (Decided). Players already chose the event by
distance (section 4.1), and every match is at the event's venue.

**Preferred time** (Decided, not built):

- Joining asks for an optional **preferred 30-minute window** inside the
  event's time, e.g. 19:00–19:30, on the half hour in the venue's time
  zone. Left empty, any time suits.
- It's **soft**: the matcher tries to start the match inside its players'
  windows, and falls back to any free slot rather than leaving them out.
- A match has two to four players. Among the slots that meet every hard
  rule, it takes the one whose start suits the **most** of their windows,
  then the earliest.
- Pairing doesn't look at windows: windows only choose the slot.
- Stored on the entry, e.g. `entries.preferred_start` (the window's start;
  NULL for any time).

**Priorities when goals conflict** (Decided):

1. Hard rules, never broken: no overlapping or back-to-back slots for a
   player, a free court, a free judge who isn't playing, and `max_matches`.
2. Close ratings: who plays whom.
3. Preferred time: which slot they get.

**Slots and judges:**

- Fill slots in time order, except where a preferred time picks a later
  slot.
- **A player with several entries never gets overlapping or back-to-back
  slots** (Decided): leave at least one slot between their matches,
  including any challenge match they already have.
- Each slot needs a free judge who isn't playing in that match. If there
  aren't enough judges, leave the slot empty and flag it for the admin.

**Determinism:** the same input must give the same schedule. Break ties by
entry ID, so runs can be tested and compared.

**How it's built** (`backend/matcher.go`, a pure function; Proposed where
the rules above leave room):

- **Fixed teams.** An entry naming a partner teams with one of the
  partner's confirmed entries that shares a team format. Mutual requests
  are settled first, then one-sided ones where the partner's entry names
  no one, each in entry-ID order; a partner's entry that names someone else
  stays theirs. An entry whose partner can't be found plays solo, with a
  warning to the admin.
- **Format for a flexible entry** (rule 4 made concrete). Head counts
  decide, not ratings: entries and teams allowing several formats are
  spread so the fewest players are left over (singles wants an even count,
  doubles and mixed a multiple of four players, a fixed team counting as
  two). An exact search over the count residues finds that; among equally
  good choices, flexible entries take singles, then doubles, then mixed, in
  entry-ID order.
- **Who sits out** an odd pool: the entry that joined last, unless benching
  another lets more matches happen (one player's two entries can't play
  each other). In doubles and mixed, solo entries sit out before a fixed
  team. They go on standby, "no opponent".
- **Rating gaps** over 200 are flagged on every format, by side rating.
- **Mixed** is paired like doubles until profiles have a gender, and every
  mixed match is flagged "gender balance not checked".
- **One player on both sides** never happens: groups of four are four
  different players, and teams sharing a player don't meet.
- **Slots.** Matches whose players have the most matches are placed first,
  then doubles and mixed before singles (four entries a slot instead of
  two), each at the earliest free court time, "Court 1" first. "At least one
  slot between" a player's matches means a gap of the shorter of the two
  slot lengths. Matches that don't fit, or exceed `max_matches`, put their
  entries on standby, "no free slot".
- **Judges** are assigned in time order: a judge not playing in or during
  the match and not judging another at the same time, with the fewest
  matches so far (then lowest user ID). None free: no judge, flagged.
- **Matches already fixed** (scheduled challenges, and other events'
  matches on the event's courts) are passed in, never moved: their court
  time is taken, their players keep the gap, their judges are busy and
  their load counts. The event's own count towards `max_matches`. Locked
  entries are never in the pool.
- **Publishing** happens once per event in version 1, and not when the
  pool makes no match at all. The rolling runs above replace this when
  they're built: each run publishes the matches it adds. Later runs that only fill gaps (walk-ins,
  no-shows, entries confirmed after publishing) are future work.

---

## 7. Walk-ins: players who turn up at the venue (Decided: allowed when slots are free)

**Two ways in (Proposed: build both):**

1. **QR self-join (preferred).** The venue shows an event QR code. The
   player scans it, signs in with their phone, and buys an entry in the
   app:
   - same flow as 4.2, but allowed after the online cutoff while the event
     is running;
   - it's open only while spare slots exist;
   - the entry is marked `source = walk_in`.
2. **Desk-assisted** (for players who come to the admin). An admin, in
   admin mode:
   1. **finds the player's existing account first**, and only creates one
      if none is found (below);
   2. records the payment as **offline** (cash or UPI to the venue), noting
      which admin took it, or sends them an in-app payment link.

**Finding the account at the desk** (Proposed). A second account would
restart the player at 1200 and split their history, so the desk always
searches before it creates:

1. **Look them up**, best first:
   - the player shows their **player card** in the app (a QR code and their
     @username), which identifies the account exactly;
   - search by **phone number**, matching phone sign-in accounts and the
     verified contact phone on Google accounts. If more than one account
     matches, the admin sees each one's name, @username and rating and
     picks;
   - search by **@username** (section 13).
2. **Confirm it's them.** Unless they showed the player card in the app, a
   one-time code goes to the account's phone and the player reads it out.
   This stops someone playing on another person's rating.
3. **Check what they already have in this event:**
   - entries already paid: check them in. A further match is a new entry,
     paid again;
   - an entry waiting for payment: turn it into an offline payment;
   - nothing yet: a new walk-in entry.
4. **No account found:** the admin enters their name and number, and the
   account is only created once the player confirms a code sent to that
   phone. A mistyped or someone else's number never gets the account.

The match uses the player's real rating for that format.

**Getting them a match:**

- An extra matching run (section 6) pairs walk-in entries with:
  - each other;
  - standby entries;
  - players left without an opponent by a no-show.
- They only go into empty slots, never into a slot already given out
  (rule 7).
- Walk-ins without a slot wait in a **live queue**. The admin and the
  player both see their place in it.

**Limits:**

- Walk-in entries count towards the event's `capacity` and `max_matches`
  like every other entry.
- The QR self-join closes when no spare slots are left.

---

## 8. Live scoring (Decided: the judge scores, the score updates in real time)

**Rules** (Proposed, BWF standard, configurable per event):

- Rally scoring to 21.
- A game must be won by 2 points, capped at 30.
- Best of 3 games.

**How points are stored:**

- Each point is one row in an **append-only** `match_points` table:
  match, sequence number, which side won it, and the judge's client-side
  ID.
- **The score is calculated from these rows,** never stored as a counter
  that gets edited.
- **Undo** adds a new "undo" row; nothing is deleted.
- The client ID makes retries safe: a resent point isn't counted twice.

**What the server checks:**

- Only the match's assigned judge (or an admin) can add points.
- No points after the match has ended.
- Game and match ends follow the rules above.

**Real-time updates:**

- Players and spectators get updates over **server-sent events**:
  `GET /api/matches/{id}/live`. It's one-way, simpler than WebSockets, and
  works through the Next.js proxy.
- The judge sends points as normal POST requests.

**Bad connections:** the judge's app keeps unsent points in a queue and
sends them in order when it reconnects. The sequence numbers keep the
order right.

**Match statuses:**

| Status | Meaning |
| --- | --- |
| `scheduled` | Assigned a slot, not yet ready |
| `ready` | Both sides checked in |
| `live` | The judge has started scoring |
| `completed` | Finished and scored |
| `walkover` | One side didn't show up |
| `cancelled` | Called off |

---

## 9. Ratings (Decided: Elo, in the 1200–2000 range)

**One rating per discipline** (Proposed): singles, doubles and mixed are
rated separately (as BWF ranks disciplines separately). Each rating stores:

- the rating itself;
- matches played;
- whether it's still provisional.

**Starting rating** (Decided): **every new account starts at 1200** in
each discipline. No self-assessment at signup. The higher K-factor while a
rating is provisional (below) lets strong newcomers climb quickly.

**The Elo update**, for side A against side B:

```
expected_A = 1 / (1 + 10 ^ ((rating_B - rating_A) / 400))
result_A   = 1 if A won, else 0
change_A   = round(K × (result_A - expected_A))
```

- In doubles, a side's rating is the average of its two players, and each
  player gets that side's change.
- **K-factor:** 40 for a player's first 10 rated matches in that
  discipline (while provisional), 20 after that (Proposed).
- The margin of victory doesn't affect the change in version 1.

**When ratings change:**

- In the **same transaction** that marks the match `completed`.
- A `rating_changes` row is written per player: match, discipline, before,
  after, change.
- Each match can change ratings **only once**. A unique key on
  (match, player, discipline) prevents a second update.

**Walkovers and cancelled matches** don't change ratings (Proposed).

**Corrections:**

- An admin can **void** a match.
- That adds reversing `rating_changes` rows; existing rows are never
  edited.
- If later matches depend on the voided one, replay that player's later
  matches in order. Recording before and after values makes this possible.

**Leaderboard:**

- One per discipline, filterable by city.
- Only players with at least 5 rated matches appear on it (Proposed).

---

## 10. Admin mode (Decided: a settings toggle, open to everyone for now)

- **The toggle:** a "Switch to admin mode" option in Settings shows the
  admin screens.
  - For now, any signed-in user can turn it on.
  - Later, only people given the role will see it.
- **The check (built):** every admin endpoint goes under `/api/admin/…`
  behind one `requireAdmin` check (`backend/admin_handlers.go`). Today it
  only requires being signed in; later it will check a `role_assignments`
  table. Only that function changes. **While it's open to everyone, anyone
  signed in can create events on whatever database the server uses.**
- **The toggle (built):** "Admin mode" in Settings on the Profile page. It's
  remembered per browser and only changes what the app shows; the server
  decides access.

**Rules for when roles arrive:**

- Check the role on the server on **every request**, never cached in the
  session, so revoking it takes effect at once. Return 403 without it.
- Grant roles only outside user-editable paths: a CLI flag, a migration or
  SQL. Never through the profile or any request body. Request bodies reject
  unknown fields, so a stray `"role"` can't slip in.
- Accounts are per sign-in method, so a role granted to someone's Google
  account doesn't apply when they sign in by phone.
- Prefer Google accounts for admins: recycled numbers and SIM swaps would
  hand a phone account's role to whoever gets the number.
- Anything that changes data stays POST, PUT or DELETE. The session cookie
  is SameSite=Lax, which still rides along on cross-site GET navigations.
- Consider an audit row per admin action. Events already record
  `created_by`; fixture edits write one to `match_changes` (below).

### Judge accounts (Decided, built: migration `000011`)

A judge is an **ordinary phone account plus a row in `judges`**, not a
separate kind of user. That's what lets a judge also play.

- **Registering:** in admin mode, Judges → name and phone number
  (`POST /api/admin/judges`). The number is normalised to E.164 and finds
  its phone account the same way a phone sign-in does. A number that has
  never signed in gets an account then, named after the judge. An existing
  account keeps the name its owner chose; the admin's name is kept on the
  `judges` row. The role is held by the **account** (`user_id`), not the
  number.
- **Signing in:** "Log in as judge" is a deliberately quiet link on the
  sign-in page. It's phone only. It sends the same code as a normal sign-in
  and verifies with `"as": "judge"`. The judge list is checked **after** the
  code: a number nobody registered gets a 403, no session, and **no account
  is created**. Because the check needs a valid code, only the number's
  owner learns whether it's registered.
- **Sessions have a mode,** `player` or `judge` (`sessions.mode`). It only
  picks the home page (`/judge` or `/dashboard`) and never grants anything:
  every judge endpoint checks `judges` on each request. A judge can switch
  views without signing in again (`POST /api/me/mode`); switching to judge
  view needs a registered judge.
- **Playing too:** signing in normally with the same number reaches the
  same account in player mode. Judges are never assigned a match they play
  in (section 6).
- **Event judges come from the registry.** An event's judge list
  (`event_judges`) only accepts registered judges, picked by name. A person
  who isn't registered couldn't reach the judge home page to score.
- **Removing a judge** deletes the `judges` row and takes them off the
  judge list of every event that hasn't ended. Matches already scheduled
  keep their judge (rule 7: they're never moved), so the admin should
  reassign those by hand. Their account and player data are untouched, and
  any judge-mode session falls back to player mode.
- **The judge home page** (`GET /api/judge/home`) lists the events they're
  listed for that haven't ended, and the matches assigned to them that
  aren't cancelled, including today's finished ones. Scoring from it comes
  with live scoring (build step 8).

Accounts are per sign-in method, so a judge's Google account isn't a judge.
As with admins, a recycled or SIM-swapped number hands the judge role to
whoever holds the number next; remove judges who change numbers.

**Court bookings are entered by hand** (Decided). Venues have no booking
API, so the admin books courts with the venue directly and enters the
event with the courts it uses. The app can't see bookings made elsewhere.

**No double-booking our own courts** (Proposed): two events that aren't
cancelled can't use the same court at overlapping times. Check it when an
event is saved and when it's published, inside the transaction.

**What admins do:**

- Create and edit venues and courts.
- Create, publish and cancel events: courts, time window, slot lengths,
  formats, capacity, `max_matches` and fee.
- Register judges by phone number, and assign them to events.
- Preview, run and publish the schedule.
- Edit fixtures by hand: make a match, replace a player, move or cancel a
  match (built, below). Telling the players comes with notifications.
- Run the walk-in desk.
- Mark no-shows.
- Void matches.
- Resolve disputes.

### Editing fixtures by hand (built: migration `000012`)

The admin's tools for section 6's "admins can redo any fixture"
(`backend/fixtures*.go`). They work whether or not the schedule has been
published, on a published event (cancelling works in any status). Each runs
in one transaction that locks the event row first, then the match, then
entries in ID order, the same order as joining, the desk and publishing.

- **Make a match** (`POST /api/admin/events/{id}/matches`) from waiting
  paid entries: open, `confirmed` or `standby`, in no match. Singles takes
  one entry a side, doubles and mixed two. Each entry must allow the
  format and the event must offer it; no player twice or on both sides.
  The court and start are optional. Given, the court must be the event's,
  the slot inside the window, the court free (other events' matches on it
  included) and every player clear of their other matches by the gap (the
  shorter slot length, section 6; their matches in other events count,
  and a player can't be judging another match then). Left out, the
  earliest slot that keeps all that, the first court on a tie: the same
  slot search challenges use (`findSlot`). `max_matches` counts the
  event's matches that aren't cancelled. The judge is chosen as the
  matcher does: on the event's list, free, not playing, fewest matches,
  then lowest user ID; none free means no judge and the "no judge" flag.
  Entries move to `scheduled`; the standby reason is cleared. A rating gap
  over 200 is allowed and flagged. The match's `source` is `admin`
  (`matcher` for a publish, `challenge` for a challenge's).
- **Suggest a slot** (`…/matches/suggest`): the same checks and choice,
  saving nothing, so the admin sees the court, time and judge first.
  **Free slots** (`GET …/free-slots`) lists every court-free start for the
  time picker, stepping by the slot length from the event's start; players
  aren't checked there.
- **Replace a player** (`POST /api/admin/matches/{id}/replace`) in a
  `scheduled` or `ready` match, not a challenge's (both players chose each
  other). The incoming entry must be waiting, of the same event, allow the
  match's format, keep the gap, and its player can't already be in the
  match. The outgoing entry becomes `no_show` (no refund, section 4.4; it
  keeps its `match_id` as the match missed) or goes back to the pool
  (`confirmed`), as the admin chooses. A judge who comes in as a player is
  replaced by another judge, or none and the flag.
- **Move a match** (`…/move`): a new court and start for a `scheduled` or
  `ready` match, challenges' included (their players chose each other, not
  the slot). Same checks as making one, ignoring the match itself. The
  judge stays if still on the list and free, otherwise another or none.
- **Cancel a match** (`…/cancel`): `cancelled`, never deleted (rule 5). Its
  open entries go back to the pool; their `match_sides` rows keep the
  player but drop the entry, so the entry can be used again (rule 9: one
  match at a time). A challenge's match can't be cancelled here: its
  locked entries were paid for that pairing, so it ends by withdrawing or
  refunding.
- **Audit:** every action writes a `match_changes` row: who, when, and the
  before and after in `details` (court, times and judge; entries in and out
  with the outcome; entries released). `GET
  /api/admin/events/{id}/changes` lists the latest 100.
- **The schedule** (`GET /api/admin/events/{id}/schedule`) shows matches
  made by hand before any publish (`publishedAt` null). Publishing later
  works around them like challenges: their entries aren't in the pool,
  their court time is taken, and they count towards `max_matches`.

Not yet: telling the players (step 11), and re-pairing a whole category.

---

## 11. Data model (target)

Built so far (migration `000005_events`): `venues`, `courts`, `events`,
`event_courts`. `000006` adds `event_judges`; `000007` adds `entries`
(without `challenge_id` yet) and a `pg_trgm` index on `lower(username)` for
search. `000008` adds `player_ratings` (rows created lazily: no row means
1200), `matches` and `match_sides`, and on `entries` `match_id`,
`standby_reason`, `confirmed_by` and `confirmed_at`, and on `events`
`schedule_published_at` and `schedule_published_by`. `matches` stores
`starts_at` and `ends_at` rather than slot minutes, and an exclusion
constraint (`btree_gist`) keeps a court from holding two matches that
aren't cancelled at overlapping times. `000009` adds `payments` (below):
provider and payment IDs instead of `provider_ref`, the provider's IDs
serving as idempotency keys, plus `refund_id`, `paid_at` and
`refunded_at`; partial unique indexes allow at most one `paid` payment
and one open (`created`) order per entry. `000010` adds `challenges`
(below, with `match_id` and `updated_at`; no separate `pay_by`, since
`expires_at` is the one deadline) and `entries.challenge_id`, with a check
that locked entries have one and open entries don't. A partial unique index
on (event, the pair in either order) allows one open (`pending` or
`accepted`) challenge per pair per event. `000011` adds `judges` and
`sessions.mode` (section 10, "Judge accounts"). `000012` adds
`match_changes`, the audit of fixture edits (section 10, "Editing fixtures
by hand"), and `matches.source`: how a match was made (`matcher`, `admin`,
`challenge`, with `arrival` and `desk` reserved for on-the-spot matching);
existing challenge matches were backfilled as `challenge`, the rest
`matcher`. The rest is proposed.

| Table | Key columns |
| --- | --- |
| `venues` | id, name, address, city, lat, lng, timezone |
| `courts` | id, venue_id, label, active |
| `events` | id, venue_id, title, starts_at, ends_at, status, fee_paise (per entry), currency, capacity (most entries), max_matches, formats (array), slot_minutes per format, registration_closes_at, rating_min/max (whole-number Elo) |
| `event_courts` | event_id, court_id, venue_id (composite keys keep courts at the event's venue) |
| `event_judges` | event_id, user_id, added_at (built, `000006`); only registered judges |
| `judges` | user_id, name, phone, created_by, created_at (built, `000011`) |
| `entries` | id, event_id, user_id, kind (`open` or `locked`), formats, partner_user_id (open doubles), challenge_id (locked), source (`online` or `walk_in`), status (`pending_payment`, `confirmed`, `standby`, `scheduled`, `cancelled`, `refunded`, `no_show`), hold_expires_at, match_id (once scheduled), created_at. **Not** unique on (event_id, user_id): a player can hold several entries |
| `event_checkins` | event_id, user_id, checked_in_at. One check-in per player per event covers all their entries |
| `payments` | id, entry_id, user_id, provider (`razorpay`, `fake`, `offline`), kind (`online` or `offline`), provider_order_id, provider_payment_id, amount_paise, currency, status (`created`, `paid`, `failed`, `refund_pending`, `refunded`), recorded_by (admin, for offline), refund_id, paid_at, refunded_at (built, `000009`). One paid payment per entry |
| `challenges` | id, event_id, format (`singles` only for now), challenger_id, opponent_id, preferred_start, status (`pending`, `accepted`, `scheduled`, `declined`, `withdrawn`, `expired`, `failed`), expires_at (the one deadline), responded_at, match_id (once fixed), created_at, updated_at. At most one open challenge per pair per event (built, `000010`) |
| `matches` | id, event_id (nullable), format, court_id, starts_at, slot_minutes, judge_id, status, winner_side, source (built, `000012`). Every match is rated; a voided match is reversed (section 9) |
| `match_sides` | match_id, side (`A` or `B`), user_id, entry_id |
| `match_changes` | id, match_id, event_id, admin_id, action (`create`, `replace`, `move`, `cancel`), details (JSON: before and after), created_at (built, `000012`) |
| `match_points` | match_id, seq, side, kind (`point` or `undo`), client_id, created_at. Unique on (match_id, seq) and (match_id, client_id) |
| `player_ratings` | user_id, discipline, rating, matches_played, provisional. Key: (user_id, discipline) |
| `rating_changes` | match_id, user_id, discipline, before, after, delta. Unique on (match_id, user_id, discipline) |
| `role_assignments` | user_id, role, scope (later) |

In `events`, the Elo band is `SMALLINT`, `formats` is a text array, and
the slot lengths are `singles_slot_minutes`, `doubles_slot_minutes` and
`mixed_slot_minutes`. `capacity` now means the most entries, not the most
people; the column keeps its name. `000005` must not be edited once it has
run on the shared Neon database; change the schema with a new migration
after that.

---

## 12. Challenges: one player challenges another (Decided flow)

A challenge is how a player gets a **locked entry**: they play the
opponent they chose instead of one the matcher picks. It's still part of
the event's pool, so it gets a venue, court, time and judge like any other
match.

**The flow** (Decided):

1. The challenger taps **Send match request**. A sheet opens with a
   username search at the top (section 13) and their **recent players**
   below: people they've played or challenged. A friends list can replace
   this later.
2. They pick the opponent, then choose:
   - **a venue, from the venues with upcoming published events**;
   - **a date**, from that venue's event dates. The venue and date pick
     the event;
   - **a preferred time**, inside that event's window;
   - the format: **singles only in version 1** (Decided). Doubles needs a
     partner and two opponents to accept, so it comes later.
3. The opponent is notified and **accepts or declines**.
4. Once accepted, a **locked entry is created for each player**, and
   **both pay** the event's entry fee, through the same payment flow as
   section 4.2.
5. **Only once both have paid** (Decided), the **match is fixed straight away**: the free slot
   closest to the preferred time (the earlier one on a tie), a court, and a
   judge from the event's judges. Both players are told the venue, court,
   date, time and judge.

**On Home** (Decided): a "Challenge people near you" row shows a few
players to challenge, each with their rating, in place of the old weekly
activity chart. "Near you" means players with recent entries at venues near
the player, closest rating first (Proposed). It's built with challenges
(build step 7); until then Home shows only the player's rating. The
backend is built: `GET /api/players/nearby` (entries that aren't cancelled
from the last 60 days, Proposed).

**Rules** (Proposed unless marked):

- **Who can challenge whom.** Checked when the challenge is sent and again
  when it's accepted: both profiles are complete; the event is published
  and still open for joining; both players are inside its rating band; it
  offers the format; nobody challenges themselves; neither player is over
  the per-event entry limit; and there's at most one open challenge
  between the same two players for the same event.
- **A 4-hour deadline** (Decided). From the moment a challenge is sent,
  the opponent must accept and both players must pay within 4 hours, or
  before the event's registration cutoff if that comes sooner. One
  deadline covers everything; there's no separate payment window.
- **Holding the spot.** Accepting creates both locked entries as
  `pending_payment`, in the same transaction as the capacity check, held
  until the challenge's deadline. They count as two entries against
  `capacity` and one match against `max_matches`, so the event can't fill
  up while they pay.
- **Not both paid by the deadline** (Decided): the challenge expires and
  anyone who paid is refunded automatically. The entry is never turned
  into an open one.
- **Every match is paid for** (Decided). A player who already has an open
  entry in the event still pays for the challenge: it's a separate entry
  and a separate match.
- **Fixing the slot.** The slot must not overlap or sit right next to
  another match the player already has in the event (section 6). The main
  matcher run treats a scheduled challenge's slot as taken. Rule 7
  applies: it's never moved once the players have been told. If no judge
  is free at that time, the slot is still fixed and the admin is asked to
  assign one.
  - An admin can move a challenge's match by hand (section 10, "Editing
    fixtures by hand"), but not replace a player or cancel it there.
    Telling the players comes with notifications.
- **Rated** (Decided): like every match we schedule. There's no cap on
  rated matches between the same two players.
- **Pulling out.** Either player can withdraw before both have paid; anyone
  who already paid is refunded. After the match is fixed, the normal refund
  policy applies (open question 2), and a no-show gives the other player a
  walkover.

**Statuses:** `pending` → `accepted` → `scheduled`, or it ends as
`declined`, `withdrawn`, `expired` or `failed` (payment not completed, the
event cancelled, or no slot left).

**Built (step 7, backend)**, with these readings of the rules (Proposed):

- **Sending** also needs room in the event for two entries and one match,
  though nothing is held until the opponent accepts. The preferred start is
  cut to the minute and must leave a singles slot inside the event's window.
- **Accepting** rechecks every rule under the event row lock, as joining
  does. A rule that fails leaves the challenge `pending`; the opponent can
  still decline it.
- **Withdrawing**: the challenger while `pending` or `accepted`; the
  opponent once they've accepted (before that, they decline). A challenge's
  entries can't be cancelled on their own, or marked paid at the desk: they
  are paid in the app, so a refund can go back through the provider.
- **The deadline** is enforced three ways: a sweep every 30 seconds, when
  either player reads or acts on the challenge, and when a payment for it
  lands after the deadline (that payment is refunded too).
- **Fixing the slot.** The nearest free start to the preferred one can be
  any minute, not only a multiple of the slot length. Taken time is every
  match that isn't cancelled on the event's courts (another event's
  included), and each player's other matches in the event keep the gap of
  the shorter slot. Among free courts, "Court 1" first. The judge: one of
  the event's judges who isn't playing or judging at that time, with the
  fewest matches already, then the lowest user ID. The challenger is side
  A. Paid entries move straight to `scheduled` with the match.
- **Both payments at once** take turns on the event row, so exactly one
  match is made.
- **An event cancelled** with open challenges: they aren't failed straight
  away; they expire at their deadline (at the latest the registration
  cutoff), refunding anyone who paid. Not built: failing them, and
  refunding scheduled ones, when the event is cancelled.
- **Recent players** are anyone the player shared a match with (either
  side) or challenged either way, most recent first: a match by its start,
  a challenge by when it was sent.

---

## 13. Search: usernames and venues (Proposed)

One search behaviour for players (challenges, adding judges, the walk-in
desk) and later venues.

**Ranking.** Results come back in tiers, best first:

| Tier | Rule | Example for "ash" |
| --- | --- | --- |
| 1. Exact | The username is exactly the query | `ash` |
| 2. Prefix | The username starts with the query | `asha_rao` |
| 3. Contains | The query appears inside the username | `prakash` |
| 4. Fuzzy | Similar spelling | `aash` |

Within a tier: closest similarity first, then shorter names, then
alphabetical. For venues, distance breaks ties within a tier.

**Backend:**

- Tiers 1–3 run first. Fuzzy matches are added only when they return fewer
  than about 5 results, and only for queries of 3 or more characters;
  fuzzy matching on one or two letters is noise.
- Fuzzy matching uses Postgres's `pg_trgm` extension (Neon supports it). A
  trigram GIN index on `lower(username)` serves both the "contains" and
  fuzzy tiers.
- Escape `_`, `%` and `\` in the query before using it in `LIKE`.
  Otherwise "a_b" matches "axb", and `_` is common in usernames.
- Results carry only public fields: name, @username and rating, never phone
  numbers. They leave out the searcher, incomplete profiles and (later)
  people who blocked them. The endpoint needs a session and is
  rate-limited, so it can't be used to list every user.

**Frontend:**

- Search about 250 ms after typing stops.
- Cancel the previous request when a new one starts (`AbortController`),
  and ignore responses that arrive out of order.
- Strip a leading `@` and lowercase the query.
- Bold the matched part of each result.
- Show clear loading, empty and error states.

---

## 14. Future: matches with no event

These are everyday rated matches with no event at all: two players arrange
a game themselves, anywhere.

The model above already allows them (`matches.event_id` is null). When
building them, decide:

- how results are confirmed without a judge (likely: one player enters the
  score, the other confirms);
- who books the court.

Every scheduled match is rated (Decided), so these are too.

---

## 15. Build order

1. **Done:** venues, courts and events; public `GET /api/events` and
   `/api/events/{id}`; the `-seed-demo` command.
2. **Done:** Elo band, formats, slot lengths, `max_matches` and the
   registration cutoff on events; distance search on `/api/events`.
3. **Done:** admin endpoints and the admin-mode toggle: venues, courts,
   events (draft → publish → cancel), judges. Migration `000006` adds
   `event_judges` and `events.created_by`.
4. **Done:** entries (open, one per match) with holds and the capacity
   check, plus the event details and join screens. The username search (section 13) and the
   court double-booking check (section 10). **Backend done** (migration
   `000007`): `POST /api/events/{id}/entries`, `GET /api/me/entries`,
   `DELETE /api/me/entries/{id}`, a real `spotsLeft`, `GET
   /api/users/search`, and the court check on save and publish. The band
   reads `player_ratings` since step 6: every chosen format's rating must
   be inside it. Screens: Home on real
   events (location, else Bangalore) with the rating strip,
   `/events/{id}`, `/events/{id}/join` (formats, partner search) and
   `/entries/{id}` (the held spot, then "finding your rival" once paid).
5. **Backend done:** the payment adapter (Razorpay) and webhooks, with a
   fake provider for local development, the same way OTP has a console
   provider (migration `000009`, `PAYMENT_PROVIDER`): `POST
   /api/me/entries/{id}/pay` (the checkout, or confirmed at once when
   free), `POST /api/payments/verify`, `POST /api/payments/webhook`.
   `mark-paid` now records an offline payment, and cancelling an entry
   closes its open order. Readings of the rules are in 4.2. The checkout
   screen is next.
6. **Done:** player ratings (starting at 1200), matches, and the
   matching algorithm, with its preview and publish steps (migration
   `000008`): `POST /api/admin/events/{id}/schedule/preview` and
   `/publish`, `GET /api/admin/events/{id}/schedule` (the saved one),
   `GET /api/admin/events/{id}/entries`, `GET /api/me/matches`,
   and `POST /api/admin/entries/{id}/mark-paid`, which confirms an entry
   paid offline at the desk (since step 5 it also records the offline
   payment). `-seed-demo` adds 16 rated demo players with paid entries and
   two judges in "Midweek Rally Night". Ratings don't change after a match
   until step 9. Screens: admin "Entries & schedule"
   (`/admin/events/{id}/schedule`: mark paid, preview, publish, the saved
   schedule) and the player's Matches tab (`/matches`). Event pages show
   the venue on a Google map with directions. **Backend done:** fixture
   editing by hand (section 10, migration `000012`): make, suggest, replace,
   move and cancel, free slots, and the audit list; the schedule shows
   matches made by hand before a publish.
7. **Backend done:** challenges (section 12): request, accept, both pay,
   fixed slot (migration `000010`): `POST /api/challenges`, `GET
   /api/me/challenges`, `GET /api/challenges/{id}`, `POST
   /api/challenges/{id}/accept`, `/decline` and `/withdraw`, `GET
   /api/me/recent-players` and `GET /api/players/nearby`. Each player pays
   their locked entry with `POST /api/me/entries/{id}/pay`; the payment that
   confirms the second fixes the match, in the same transaction. A sweep
   every 30 seconds expires challenges past their deadline and refunds. The
   matcher treats fixed matches as taken. Readings of the rules are in
   section 12. The screens are next.
8. Live scoring: the judge console, the point log and server-sent events.
9. Rating updates, the result screen and the leaderboard.
10. Walk-ins: QR self-join, the desk with account lookup, the player-card
    QR, standby and the live queue.
11. Notifications beyond in-app.

---

## 16. Open questions

| # | Question | Default if unanswered |
| --- | --- | --- |
| 1 | ~~How many matches does the ₹50 fee buy?~~ | Decided: one entry is one match; more matches mean more entries (section 5) |
| 2 | Refund policy: unmatched entries, cancellations, and how late a player can cancel | Unmatched entries are refunded automatically; no refund within 24 h of the event |
| 3 | When does joining close before the event? | 2 hours before the start |
| 4 | Slot length per format | 30 min singles; 40 min doubles and mixed |
| 5 | Largest rating gap allowed in a match | About 200, with closer matches preferred |
| 6 | ~~Starting rating~~ | Decided: 1200 for everyone (section 9) |
| 7 | Separate ratings per discipline? | Yes |
| 8 | Gender field for mixed doubles: wording and options | Asked only when choosing mixed |
| 9 | Who are the judges: staff, volunteers, or other players between their matches? | Assigned by admins from the event's judge list |
| 10 | Does a walkover change ratings? | No |
| 11 | Payment provider | Razorpay |
| 12 | Notification channels after in-app | WhatsApp, then SMS |
| 13 | ~~A challenge where only one player pays~~ | Decided: refunded automatically when the deadline passes |
| 14 | ~~How long do players have to accept and pay?~~ | Decided: 4 hours from sending the challenge, or the registration cutoff if sooner |
| 15 | ~~Rated challenges between the same two players~~ | Decided: every scheduled match is rated, with no cap |
| 16 | ~~Doubles and mixed challenges~~ | Decided: singles only in version 1 |
| 17 | Most entries one player can hold in one event | 3 |
| 18 | ~~When a full event's schedule is made, does it publish at once or wait for an admin?~~ | Decided: every run publishes at once; admins can edit fixtures afterwards (section 6) |
| 19 | ~~When an event hasn't filled, when is the schedule made?~~ | Decided: rolling runs, matched at 24 h before with complete matches only, then every 4 h (section 6) |
| 20 | Categories: men's singles, women's singles, men's doubles, women's doubles and mixed doubles? That needs a gender on the profile, a maximum per category, and one category per entry | Open |
| 21 | Does the admin publish about 4 days before by hand, or does the event open on its own at a set time? | Admin publishes; the form suggests 4 days before |
| 22 | When does joining finally close: at the start (on-the-spot entries are walk-ins), or some time before? | At the start |
| 23 | "Good enough" opponents: within 200 rating points? At the last run, pair the closest even if further apart (flagged), or leave them unmatched and refund? | Within 200; the last run pairs the closest and flags it |
