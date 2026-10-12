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

Last updated 2026-10-11.

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
   has already been notified. (Later, an admin will be able to move or
   delete a slot by hand, and the players are told. Not in version 1.)
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

**When it runs** (Proposed):

- **A main run at the registration cutoff,** e.g. 2 hours before the start
  (configurable). It matches the open entries and builds the schedule.
- **Small extra runs afterwards** for walk-ins, no-shows and spare slots.
  These follow rule 7: they only fill gaps.
- Admins can also run it by hand from admin mode, to preview the schedule
  and publish it.

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

**Slots and judges:**

- Fill slots in time order.
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
- **Publishing** happens once per event in version 1, and not when the
  pool makes no match at all. Later runs that only fill gaps (walk-ins,
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
- **The toggle (built):** "Admin mode" in the account sheet on Home. It's
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
  `created_by`.

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
- Assign judges to an event.
- Preview, run and publish the schedule.
- Later: move or delete scheduled slots, telling the players affected.
- Run the walk-in desk.
- Mark no-shows.
- Void matches.
- Resolve disputes.

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
and one open (`created`) order per entry. The rest is proposed.

| Table | Key columns |
| --- | --- |
| `venues` | id, name, address, city, lat, lng, timezone |
| `courts` | id, venue_id, label, active |
| `events` | id, venue_id, title, starts_at, ends_at, status, fee_paise (per entry), currency, capacity (most entries), max_matches, formats (array), slot_minutes per format, registration_closes_at, rating_min/max (whole-number Elo) |
| `event_courts` | event_id, court_id, venue_id (composite keys keep courts at the event's venue) |
| `event_judges` | event_id, user_id, added_at (built, `000006`) |
| `entries` | id, event_id, user_id, kind (`open` or `locked`), formats, partner_user_id (open doubles), challenge_id (locked), source (`online` or `walk_in`), status (`pending_payment`, `confirmed`, `standby`, `scheduled`, `cancelled`, `refunded`, `no_show`), hold_expires_at, match_id (once scheduled), created_at. **Not** unique on (event_id, user_id): a player can hold several entries |
| `event_checkins` | event_id, user_id, checked_in_at. One check-in per player per event covers all their entries |
| `payments` | id, entry_id, user_id, provider (`razorpay`, `fake`, `offline`), kind (`online` or `offline`), provider_order_id, provider_payment_id, amount_paise, currency, status (`created`, `paid`, `failed`, `refund_pending`, `refunded`), recorded_by (admin, for offline), refund_id, paid_at, refunded_at (built, `000009`). One paid payment per entry |
| `challenges` | id, event_id, format, challenger_id, opponent_id, preferred_start, status (`pending`, `accepted`, `scheduled`, `declined`, `withdrawn`, `expired`, `failed`), expires_at, pay_by, responded_at, created_at. At most one open challenge per pair per event |
| `matches` | id, event_id (nullable), format, court_id, starts_at, slot_minutes, judge_id, status, winner_side. Every match is rated; a voided match is reversed (section 9) |
| `match_sides` | match_id, side (`A` or `B`), user_id, entry_id |
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
5. Once both have paid, the **match is fixed straight away**: the free slot
   closest to the preferred time (the earlier one on a tie), a court, and a
   judge from the event's judges. Both players are told the venue, court,
   date, time and judge.

**On Home** (Decided): a "Challenge people near you" row shows a few
players to challenge, each with their rating, in place of the old weekly
activity chart. "Near you" means players with recent entries at venues near
the player, closest rating first (Proposed). It's built with challenges
(build step 7); until then Home shows only the player's rating.

**Rules** (Proposed unless marked):

- **Who can challenge whom.** Checked when the challenge is sent and again
  when it's accepted: both profiles are complete; the event is published
  and still open for joining; both players are inside its rating band; it
  offers the format; nobody challenges themselves; neither player is over
  the per-event entry limit; and there's at most one open challenge
  between the same two players for the same event.
- **Expiry.** An unanswered challenge expires after 24 hours or at the
  event's registration cutoff, whichever comes first.
- **Holding the spot.** Accepting creates both locked entries as
  `pending_payment`, in the same transaction as the capacity check. They
  count as two entries against `capacity` and one match against
  `max_matches`, so the event can't fill up while they pay. Both must pay
  within 2 hours of accepting, and before the registration cutoff.
- **Only one pays.** The challenge fails and the payer is refunded
  automatically (Open: or turn their entry into an open one instead).
- **Every match is paid for** (Decided). A player who already has an open
  entry in the event still pays for the challenge: it's a separate entry
  and a separate match.
- **Fixing the slot.** The slot must not overlap or sit right next to
  another match the player already has in the event (section 6). The main
  matcher run treats a scheduled challenge's slot as taken. Rule 7
  applies: it's never moved once the players have been told. If no judge
  is free at that time, the slot is still fixed and the admin is asked to
  assign one.
  - TODO (later): admins will be able to move and delete scheduled slots,
    telling the players affected. Not built in version 1.
- **Rated** (Decided): like every match we schedule. There's no cap on
  rated matches between the same two players.
- **Pulling out.** Either player can withdraw before both have paid; anyone
  who already paid is refunded. After the match is fixed, the normal refund
  policy applies (open question 2), and a no-show gives the other player a
  walkover.

**Statuses:** `pending` → `accepted` → `scheduled`, or it ends as
`declined`, `withdrawn`, `expired` or `failed` (payment not completed, the
event cancelled, or no slot left).

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
   the venue on a Google map with directions.
7. Challenges (section 12): request, accept, both pay, fixed slot.
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
| 13 | A challenge where only one player pays: refund, or turn their entry into an open one? | Refund automatically |
| 14 | How long do both players have to pay after a challenge is accepted? | 2 hours, and before the registration cutoff |
| 15 | ~~Rated challenges between the same two players~~ | Decided: every scheduled match is rated, with no cap |
| 16 | ~~Doubles and mixed challenges~~ | Decided: singles only in version 1 |
| 17 | Most entries one player can hold in one event | 3 |
