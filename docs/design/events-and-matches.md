# Events, matches and ratings

The core logic of BadmintonPro: how players find an event, pay for a slot,
get matched, play a judged match with live scoring, and have their rating
change. Read this before working on events, registrations, payments,
matchmaking, scoring or ratings.

Each rule below is tagged as one of:

- **Decided:** the product owner chose it. Don't change it without asking.
- **Proposed:** a default chosen while writing this doc. Build it this way
  unless it's revisited.
- **Open:** not decided yet. Ask before building anything that depends on it.

Last updated 2026-10-10.

---

## 1. The idea in one paragraph

Admins host **events** at venues on courts they've already booked. A player
sees the events near them, picks one, and **pays a small slot fee (₹50) to
join its player pool**, saying which formats they want to play. Before the
event, a **matching algorithm** turns the pool into matches. Each match has
an opponent (or a team for doubles), a court, a time and a **judge**. The
judge scores the match live on their phone, and players and spectators see
the score update in real time. When the match ends, both sides' **Elo
ratings** change. Players who turn up at the venue without registering can
still join if there are free slots.

Matches outside events, where two players arrange a game themselves on a
normal day, are **future work** (section 12). The model below keeps matches
separate from events so that can be added without a redesign.

---

## 2. Glossary

| Term | Meaning |
| --- | --- |
| **Venue** | A place with badminton courts. It has a city, coordinates and a time zone. |
| **Court** | One court at a venue, e.g. "Court 3". |
| **Event** | A session an admin runs at one venue, on a set of booked courts, over a time window on one day. It is the unit a player joins and pays for. |
| **Format** | `singles`, `doubles` or `mixed` (mixed doubles). |
| **Registration** | A player's place in an event's pool: which formats they want, payment status, and whether they joined online or as a walk-in. |
| **Pool** | All confirmed (paid) registrations for an event. The matcher works from the pool. |
| **Slot** | One court for one block of time, e.g. Court 2, 19:30–20:00. Each match uses exactly one slot. |
| **Match** | Two sides playing each other in one slot. A side is one player (singles) or two (doubles). |
| **Judge** | The person who scores a match live. Each match needs one. |
| **Rating** | A player's Elo rating for a discipline. The expected range is about 1200–2000. |
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
   registrations and matches are cancelled (their status changes) instead.
6. **Capacity is checked inside a database transaction.** Two people can't
   both get the last spot.
7. **A match that players have already been told about is never quietly
   reshuffled.** Late changes, such as walk-ins or no-shows, only fill
   empty slots or replace a missing player. They never move someone who
   has already been notified.
8. **The server is the source of truth for scores.** The judge's app sends
   point events; the server checks them against the scoring rules and works
   out the score.

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
  - spots left and the slot fee.
- The API (Proposed): `GET /api/events?lat=&lng=&radiusKm=`. Distance is
  computed in SQL with the haversine formula on `venues.lat`/`lng`; the
  city filter stays as a fallback. PostGIS is not needed at this scale.

### 4.2 Join and pay (Decided: ₹50 slot fee to secure the place)

1. The player opens an event and picks:
   - **formats**: one or more of singles, doubles and mixed;
   - **partner**, optional: they can pick a doubles partner, or leave it
     to the matcher.
2. **The place is held while they pay** (Proposed: for 10 minutes):
   - A `registration` is created as `pending_payment`, and the event's
     capacity check and the insert happen in one transaction.
   - If payment doesn't succeed before the hold expires, the place is
     released.
3. **Payment:**
   - It goes through a payment-provider adapter, built the same way as the
     OTP adapter (Proposed: Razorpay first).
   - The **provider's webhook** confirms the payment, not the browser
     redirect. Webhooks are idempotent: a repeated webhook changes nothing.
4. Once paid, the registration is `confirmed` and the player joins the pool.
   They see: **"You're in. We'll notify you once your match is scheduled."**

**Event limits** (Decided: per venue, per day):

- `capacity`: the most players the event takes.
- `max_matches`: the most matches it can host. This can't exceed the
  number of slots: courts × (event length ÷ slot length).

### 4.3 Scheduling (Decided: done by the matching algorithm)

- The matcher runs per event (section 6) and publishes a schedule.
- Each player is notified of:
  - their opponent or team;
  - the court and time;
  - the judge.
- Notifications are in-app first. SMS or WhatsApp come through a
  notification adapter later.

### 4.4 Match day

1. **Check-in.** (Proposed.) The player checks in at the venue by scanning
   the venue's QR code or being marked by the judge. Check-in closes 10
   minutes before their slot.
2. **No-shows.** A player who hasn't checked in by then is a no-show:
   - their slot opens up for a walk-in or someone on standby (section 7);
   - their opponent gets a **walkover**, which doesn't change anyone's
     rating (Proposed);
   - the no-show loses their fee.
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
    doubles and mixed 40 min, editable per event);
  - a **minimum and maximum matches per player** (Open: how many matches
    does the ₹50 fee buy? Proposed: at least 1 guaranteed, more when slots
    are free).
- Total slots = the sum over courts of how many slot lengths fit in the
  window.
- Registration closes when either:
  - confirmed registrations reach `capacity`, or
  - the matches the pool needs at the guaranteed minimum would exceed the
    available slots or `max_matches`.

  The registration endpoint enforces this inside a transaction.
- Slots the minimum doesn't need are **spare**. They're used for extra
  matches, walk-ins, or replacements after no-shows.

---

## 6. Matching algorithm

**When it runs** (Proposed):

- **A main run at the registration cutoff,** e.g. 2 hours before the start
  (configurable). It builds the whole schedule.
- **Small extra runs afterwards** for walk-ins, no-shows and spare slots.
  These follow rule 7: they only fill gaps.
- Admins can also run it by hand from admin mode, to preview the schedule
  and publish it.

**What it receives:**

- the pool: each player's ratings per format, chosen formats, partner if
  any, and gender if mixed was chosen;
- the event's slots;
- the event's judges.

**Pairing** (Proposed), per format:

1. **Singles:**
   1. Sort players by singles rating.
   2. Pair neighbours, so the rating gap is as small as possible.
   3. Pairs must be no more than about 200 apart. If that's impossible,
      allow the closest pair and flag it for the admin.
   4. No pair plays twice in the same event.
2. **Doubles:**
   1. Keep the pairs who registered together.
   2. Make teams from the solo players. Sort them by doubles rating; in
      each group of four, team the highest with the lowest and the middle
      two together, then those two teams play.
   3. A team's rating is the average of its two players.
3. **Mixed:** like doubles, but each team is one man and one woman. This
   needs a gender field on the profile, asked only when someone chooses
   mixed (Open: wording and options).
4. **Fairness across the event:** first make sure everyone gets their
   guaranteed matches, then hand out spare slots to whoever has played
   least.
5. **Leftover players** who couldn't be matched, such as an odd one out:
   - first, they go on **standby** for walk-ins and no-shows;
   - if still unmatched at the end, they're **refunded automatically**
     (Proposed).

**Slots and judges:**

- Fill slots in time order, and don't give a player two matches in a row
  unless they asked for it.
- Each slot needs a free judge who isn't playing in that match. If there
  aren't enough judges, leave the slot empty and flag it for the admin.

**Determinism:** the same input must give the same schedule. Break ties by
user ID, so runs can be tested and compared.

---

## 7. Walk-ins: players who turn up at the venue (Decided: allowed when slots are free)

**Two ways in (Proposed: build both):**

1. **QR self-join (preferred).** The venue shows an event QR code. The
   player scans it, signs in with their phone, and joins in the app:
   - same flow as 4.2, but allowed after the online cutoff while the event
     is running;
   - it's open only while spare slots exist;
   - the registration is marked `source = walk_in`.
2. **Desk-assisted** (for players without the app). An admin, in admin mode:
   1. looks the player up by phone number, or creates their account (they
      confirm with a phone code on their own phone, or the admin enters
      their name and number and they claim the account later);
   2. records the payment as **offline** (cash or UPI to the venue), noting
      which admin took it, or sends them an in-app payment link.

**Getting them a match:**

- An extra matching run (section 6) pairs walk-ins with:
  - each other;
  - standby players;
  - players left without an opponent by a no-show.
- They only go into empty slots, never into a slot already given out
  (rule 7).
- Walk-ins without a slot wait in a **live queue**. The admin and the
  player both see their place in it.

**Limits:**

- Walk-ins count towards the event's `capacity` and `max_matches` like
  everyone else.
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
- **The check:** every admin endpoint goes under `/api/admin/…` behind one
  `requireAdmin` check. Today it only requires being signed in; later it
  will check a `role_assignments` table. Only that function changes.
  **While it's open to everyone, anyone signed in can create events on
  whatever database the server uses.**

**What admins do:**

- Create and edit venues and courts.
- Create, publish and cancel events: courts, time window, slot lengths,
  formats, capacity, `max_matches` and fee.
- Assign judges to an event.
- Preview, run and publish the schedule.
- Run the walk-in desk.
- Mark no-shows.
- Void matches.
- Resolve disputes.

---

## 11. Data model (target)

Built so far (uncommitted, migration `000005_events`): `venues`, `courts`,
`events`, `event_courts`. The rest is proposed.

| Table | Key columns |
| --- | --- |
| `venues` | id, name, address, city, lat, lng, timezone |
| `courts` | id, venue_id, label, active |
| `events` | id, venue_id, title, starts_at, ends_at, status, fee_paise, currency, capacity, max_matches, formats (array), slot_minutes per format, registration_closes_at, rating_min/max (whole-number Elo) |
| `event_courts` | event_id, court_id, venue_id (composite keys keep courts at the event's venue) |
| `event_judges` | event_id, user_id |
| `registrations` | id, event_id, user_id, formats, partner_user_id, source (`online` or `walk_in`), status (`pending_payment`, `confirmed`, `standby`, `cancelled`, `refunded`, `no_show`), hold_expires_at, checked_in_at. Unique on (event_id, user_id) |
| `payments` | id, registration_id, provider, provider_ref, amount_paise, currency, status, kind (`online` or `offline`), recorded_by (admin, for offline), idempotency_key |
| `matches` | id, event_id (nullable), format, court_id, starts_at, slot_minutes, judge_id, status, winner_side, rated (bool) |
| `match_sides` | match_id, side (`A` or `B`), user_id |
| `match_points` | match_id, seq, side, kind (`point` or `undo`), client_id, created_at. Unique on (match_id, seq) and (match_id, client_id) |
| `player_ratings` | user_id, discipline, rating, matches_played, provisional. Key: (user_id, discipline) |
| `rating_changes` | match_id, user_id, discipline, before, after, delta. Unique on (match_id, user_id, discipline) |
| `role_assignments` | user_id, role, scope (later) |

**Changes to make to `000005` before it runs on the shared Neon database**
(after that it can't be edited):

- `rating_min`/`rating_max` → whole-number Elo (`SMALLINT`).
- Add `max_matches`, `formats` and the slot-length columns.

---

## 12. Future: matches outside events

These are everyday rated matches, with no event: two players arrange a game
themselves.

The model above already allows them (`matches.event_id` is null). When
building them, decide:

- how results are confirmed without a judge (likely: one player enters the
  score, the other confirms);
- who books the court;
- limits on how often the same two players can play rated matches
  (anti-boosting).

---

## 13. Build order

1. **Done, uncommitted:** venues, courts and events; public
   `GET /api/events` and `/api/events/{id}`; the `-seed-demo` command.
2. Change `000005` as in section 11, and add distance search to
   `/api/events`.
3. Admin endpoints and the admin-mode toggle: venues, courts, events,
   judges.
4. Registrations with holds and the capacity check, plus the event
   details and join screens.
5. The payment adapter (Razorpay) and webhooks, with a fake provider for
   local development, the same way OTP has a console provider.
6. Player ratings (starting at 1200), matches, and the matching algorithm,
   with its preview and publish steps.
7. Live scoring: the judge console, the point log and server-sent events.
8. Rating updates, the result screen and the leaderboard.
9. Walk-ins: QR self-join, the desk, standby and the live queue.
10. Notifications beyond in-app.

---

## 14. Open questions

| # | Question | Default if unanswered |
| --- | --- | --- |
| 1 | How many matches does the ₹50 fee guarantee? | 1 guaranteed; more if slots are free |
| 2 | Refund policy: unmatched players, cancellations, and how late a player can cancel | Unmatched players are refunded automatically; no refund within 24 h of the event |
| 3 | When does registration close before the event? | 2 hours before the start |
| 4 | Slot length per format | 30 min singles; 40 min doubles and mixed |
| 5 | Largest rating gap allowed in a match | About 200, with closer matches preferred |
| 6 | ~~Starting rating~~ | Decided: 1200 for everyone (section 9) |
| 7 | Separate ratings per discipline? | Yes |
| 8 | Gender field for mixed doubles: wording and options | Asked only when choosing mixed |
| 9 | Who are the judges: staff, volunteers, or other players between their matches? | Assigned by admins from the event's judge list |
| 10 | Does a walkover change ratings? | No |
| 11 | Payment provider | Razorpay |
| 12 | Notification channels after in-app | WhatsApp, then SMS |
