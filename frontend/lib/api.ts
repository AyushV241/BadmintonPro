// All requests go to this app's own origin; next.config.ts proxies /api/* to
// the Go backend. The session is an httpOnly cookie the browser attaches
// automatically, so no token is ever handled in JavaScript.

export type User = {
  id: string;
  name: string;
  username: string;
  /** Contact details, never ways to sign in. */
  email: string;
  emailVerified: boolean;
  phone: string;
  phoneVerified: boolean;
  /** How the account signs in: "phone", "google", ... */
  signInMethod: string;
  /** False until "Set up your profile" has a name and username. */
  profileComplete: boolean;
};

/** Thrown for any non-2xx response, carrying the API's message. */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    /** Seconds to wait before retrying, from a 429's Retry-After header. */
    readonly retryAfter?: number,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      ...init,
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", ...init.headers },
    });
  } catch {
    throw new ApiError("Cannot reach the server. Check your connection.", 0);
  }

  if (res.status === 204) {
    return undefined as T;
  }

  const body = await res.json().catch(() => null);

  if (!res.ok) {
    // The dev proxy answers 500 with no JSON when the Go server is down.
    const message =
      body && typeof body.error === "string"
        ? body.error
        : res.status >= 500
          ? "The server is unavailable. Is the backend running?"
          : `Request failed (${res.status})`;
    const retryAfter = Number(res.headers.get("Retry-After")) || undefined;
    throw new ApiError(message, res.status, retryAfter);
  }

  return body as T;
}

export async function me(): Promise<User> {
  const { user } = await request<{ user: User }>("/api/me");
  return user;
}

export function logout(): Promise<void> {
  return request<void>("/api/logout", { method: "POST" });
}

export type LoginOptions = {
  /** Redirect logins, each shown as a "Continue with …" button. */
  providers: string[];
  /** Whether sign-in with a code sent to a mobile number is enabled. */
  phone: boolean;
};

/** Which sign-in methods the backend has enabled. */
export async function loginOptions(): Promise<LoginOptions> {
  return request<LoginOptions>("/api/auth/providers");
}

/**
 * Sends a one-time code to a mobile number. Resolves to the number the code
 * went to, in international format (+919876543210), which verifyPhoneLogin
 * needs.
 */
export async function startPhoneLogin(phone: string): Promise<string> {
  const res = await request<{ phone: string }>("/api/auth/phone/start", {
    method: "POST",
    body: JSON.stringify({ phone }),
  });
  return res.phone;
}

/** Checks a code and signs in, creating the account on first login. */
export async function verifyPhoneLogin(
  phone: string,
  code: string,
): Promise<User> {
  const { user } = await request<{ user: User }>("/api/auth/phone/verify", {
    method: "POST",
    body: JSON.stringify({ phone, code }),
  });
  return user;
}

/**
 * Where to send the browser to sign in with a provider. This is a full-page
 * navigation, not a fetch: the provider's consent screen must load in the
 * top-level window.
 */
export function providerLoginUrl(provider: string): string {
  return `/api/auth/${encodeURIComponent(provider)}/start`;
}

export type ProfileUpdate = {
  name: string;
  username: string;
  /**
   * Contact email, for accounts without a verified one. Omit to leave it
   * unchanged; "" clears it. Stored unverified.
   */
  email?: string;
};

/** Saves the profile step. 409 means the username is taken. */
export async function updateProfile(profile: ProfileUpdate): Promise<User> {
  const { user } = await request<{ user: User }>("/api/me/profile", {
    method: "PUT",
    body: JSON.stringify(profile),
  });
  return user;
}

/**
 * Sends a code to a phone a signed-in user wants on their profile. Resolves
 * to the number in international format, which verifyContactPhone needs.
 */
export async function startContactPhone(phone: string): Promise<string> {
  const res = await request<{ phone: string }>("/api/me/phone/start", {
    method: "POST",
    body: JSON.stringify({ phone }),
  });
  return res.phone;
}

/** Checks the code and saves the phone as verified contact information. */
export async function verifyContactPhone(
  phone: string,
  code: string,
): Promise<User> {
  const { user } = await request<{ user: User }>("/api/me/phone/verify", {
    method: "POST",
    body: JSON.stringify({ phone, code }),
  });
  return user;
}

// --- Events -----------------------------------------------------------------

export type EventFormat = "singles" | "doubles" | "mixed";
export type EventStatus = "draft" | "published" | "cancelled" | "completed";

export type Venue = {
  id: string;
  name: string;
  address: string;
  city: string;
  lat: number | null;
  lng: number | null;
  /** IANA zone; show event times in it. */
  timezone: string;
};

export type Court = { id: string; label: string };

export type BadmintonEvent = {
  id: string;
  title: string;
  formats: EventFormat[];
  /** Minutes one match occupies a court, for each format offered. */
  slotMinutes: Partial<Record<EventFormat, number>>;
  status: EventStatus;
  /** ISO 8601, UTC. */
  startsAt: string;
  endsAt: string;
  registrationClosesAt: string;
  /** Slot fee in paise. */
  feePaise: number;
  currency: string;
  capacity: number;
  maxMatches: number;
  spotsLeft: number;
  /** Elo band; null means no limit. */
  ratingMin: number | null;
  ratingMax: number | null;
  /** Only on location searches. */
  distanceKm?: number;
  venue: Venue;
  /** Only on single-event responses. */
  courts?: Court[];
};

/** Where to look: a point and radius when the browser shares location, else a city. */
export type EventQuery = { lat: number; lng: number; radiusKm?: number } | { city: string };

/** Published, upcoming events in start order. Public: no session needed. */
export async function listEvents(query: EventQuery, limit = 20): Promise<BadmintonEvent[]> {
  const params = new URLSearchParams({ limit: String(limit) });
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined) params.set(key, String(value));
  }
  const { events } = await request<{ events: BadmintonEvent[] }>(`/api/events?${params}`);
  return events;
}

export async function getEvent(id: string): Promise<BadmintonEvent> {
  const { event } = await request<{ event: BadmintonEvent }>(`/api/events/${encodeURIComponent(id)}`);
  return event;
}

// --- Entries ----------------------------------------------------------------
// One entry is one paid match in an event's pool.

/** What other players may see of someone. */
export type PublicUser = { id: string; username: string; name: string };

export type EntryStatus =
  | "pending_payment"
  | "confirmed"
  | "standby"
  | "scheduled"
  | "cancelled"
  | "refunded"
  | "no_show";

export type Entry = {
  id: string;
  /** "open": the matcher picks the opponent; "locked": a challenge fixed it. */
  kind: "open" | "locked";
  formats: EventFormat[];
  partner: PublicUser | null;
  source: "online" | "walk_in";
  status: EntryStatus;
  /** While pending payment: when the held spot is released. */
  holdExpiresAt: string | null;
  holdExpired: boolean;
  /** Set on a locked entry: the challenge it pays for. */
  challengeId: string | null;
  createdAt: string;
  event: Pick<
    BadmintonEvent,
    "id" | "title" | "status" | "startsAt" | "endsAt" | "registrationClosesAt" | "feePaise" | "currency" | "venue"
  >;
};

/** Holds a spot for 10 minutes while the player pays. */
export async function createEntry(eventId: string, formats: EventFormat[], partnerUsername?: string): Promise<Entry> {
  const { entry } = await request<{ entry: Entry }>(`/api/events/${encodeURIComponent(eventId)}/entries`, {
    method: "POST",
    body: JSON.stringify(partnerUsername ? { formats, partnerUsername } : { formats }),
  });
  return entry;
}

/** The signed-in player's entries, newest first. */
export async function myEntries(): Promise<Entry[]> {
  const { entries } = await request<{ entries: Entry[] }>("/api/me/entries");
  return entries;
}

/** Releases an entry that hasn't been paid for. */
export async function cancelEntry(id: string): Promise<Entry> {
  const { entry } = await request<{ entry: Entry }>(`/api/me/entries/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  return entry;
}

/** Players by username: exact, then starts-with, then contains, then similar. */
export async function searchUsers(q: string, signal?: AbortSignal): Promise<PublicUser[]> {
  const { users } = await request<{ users: PublicUser[] }>(`/api/users/search?q=${encodeURIComponent(q)}`, {
    signal,
  });
  return users;
}

// --- Matches ----------------------------------------------------------------

/** How a match was made: the matcher, an admin by hand, a challenge, on arrival, or at the desk. */
export type MatchSource = "matcher" | "admin" | "challenge" | "arrival" | "desk";

export type MatchStatus = "scheduled" | "ready" | "live" | "completed" | "retired" | "walkover" | "cancelled" | "void";

export type MatchPlayer = { userId: string; name: string; username: string; rating: number };
export type MatchSide = { side: "A" | "B"; players: MatchPlayer[]; rating: number };

/** One of the signed-in player's matches. */
export type MyMatch = {
  id: string;
  status: MatchStatus;
  format: EventFormat;
  event: { id: string; title: string } | null;
  venue: Venue;
  court: Court;
  startsAt: string;
  endsAt: string;
  judge: Judge | null;
  sides: MatchSide[];
  mySide: "A" | "B";
  winnerSide: "A" | "B" | null;
  /** Set when a challenge fixed this match. */
  challengeId: string | null;
  /** How it was made. */
  source: MatchSource;
};

/** The signed-in player's matches, soonest first. */
export async function myMatches(): Promise<MyMatch[]> {
  const { matches } = await request<{ matches: MyMatch[] }>("/api/me/matches");
  return matches;
}

// --- Judges -----------------------------------------------------------------
// A judge is an ordinary phone account an admin has registered. "Log in as
// judge" lands on /judge; signing in normally lands on the player home, and
// a judge can switch between the two. The server checks the judge list on
// every judge request; the mode only picks the home page.

export type SessionMode = "player" | "judge";

/** GET /api/me: the account, and what its session is for. */
export type MeSession = { user: User; mode: SessionMode; isJudge: boolean };

export function meSession(): Promise<MeSession> {
  return request<MeSession>("/api/me");
}

/**
 * "Log in as judge": like verifyPhoneLogin, but only a number an admin has
 * registered gets in (403 otherwise), and the session starts in judge mode.
 */
export async function verifyJudgeLogin(phone: string, code: string): Promise<User> {
  const { user } = await request<MeSession>("/api/auth/phone/verify", {
    method: "POST",
    body: JSON.stringify({ phone, code, as: "judge" }),
  });
  return user;
}

/** Switches the home page. "judge" needs a registered judge (403 otherwise). */
export function setSessionMode(mode: SessionMode): Promise<MeSession> {
  return request<MeSession>("/api/me/mode", { method: "POST", body: JSON.stringify({ mode }) });
}

/** One entry in the admin's judge list. */
export type RegisteredJudge = {
  userId: string;
  /** As the admin entered them; phone in E.164. */
  name: string;
  phone: string;
  /** Empty until they set up a player profile. */
  username: string;
  createdAt: string;
};

/** An event whose judge list includes the signed-in judge. */
export type JudgeEvent = {
  id: string;
  title: string;
  status: EventStatus;
  startsAt: string;
  endsAt: string;
  venue: Venue;
};

/** A match the judge scores: a player match with no side of their own. */
export type JudgeMatch = Omit<MyMatch, "mySide">;

export type JudgeHome = {
  judge: RegisteredJudge;
  /** Not yet ended or cancelled, soonest first. */
  events: JudgeEvent[];
  /** Assigned to the judge, soonest first, including today's finished ones. */
  matches: JudgeMatch[];
};

/** 403 unless the signed-in account is a registered judge. */
export function judgeHome(): Promise<JudgeHome> {
  return request<JudgeHome>("/api/judge/home");
}

// --- Admin mode -------------------------------------------------------------
// Every /api/admin call needs a session. Admin mode is open to every
// signed-in user for now; later only people with the role.

export type AdminCourt = Court & { active: boolean };
export type AdminVenue = Venue & { courts: AdminCourt[] };
export type Judge = { userId: string; name: string; username: string };
export type AdminEvent = { event: BadmintonEvent; judges: Judge[] };

export type VenueDraft = {
  name: string;
  address: string;
  /** Slug, e.g. "bangalore". */
  city: string;
  lat: number | null;
  lng: number | null;
  /** Default "Asia/Kolkata". */
  timezone?: string;
  /** Creates "Court 1" … "Court n". */
  courts: number;
};

/** An event as the admin form sends it: the venue's local date and times. */
export type EventDraft = {
  venueId: string;
  title: string;
  formats: EventFormat[];
  slotMinutes: Partial<Record<EventFormat, number>>;
  /** "2026-10-17" */
  date: string;
  /** "19:30" */
  startTime: string;
  endTime: string;
  registrationClosesMinutesBefore: number;
  feePaise: number;
  capacity: number;
  maxMatches: number;
  ratingMin: number | null;
  ratingMax: number | null;
  courtIds: string[];
};

export async function adminListVenues(): Promise<AdminVenue[]> {
  const { venues } = await request<{ venues: AdminVenue[] }>("/api/admin/venues");
  return venues;
}

export async function adminCreateVenue(venue: VenueDraft): Promise<AdminVenue> {
  const res = await request<{ venue: AdminVenue }>("/api/admin/venues", {
    method: "POST",
    body: JSON.stringify(venue),
  });
  return res.venue;
}

/** 409 if the venue already has a court with that label. */
export async function adminAddCourt(venueId: string, label: string): Promise<AdminCourt> {
  const res = await request<{ court: AdminCourt }>(
    `/api/admin/venues/${encodeURIComponent(venueId)}/courts`,
    { method: "POST", body: JSON.stringify({ label }) },
  );
  return res.court;
}

/** Events in every status, latest start first. */
export async function adminListEvents(status?: EventStatus): Promise<BadmintonEvent[]> {
  const query = status ? `?status=${status}` : "";
  const { events } = await request<{ events: BadmintonEvent[] }>(`/api/admin/events${query}`);
  return events;
}

export function adminGetEvent(id: string): Promise<AdminEvent> {
  return request<AdminEvent>(`/api/admin/events/${encodeURIComponent(id)}`);
}

/** Saves a new event as a draft. */
export function adminCreateEvent(draft: EventDraft): Promise<AdminEvent> {
  return request<AdminEvent>("/api/admin/events", { method: "POST", body: JSON.stringify(draft) });
}

/** Only drafts can be edited (409 otherwise). */
export function adminUpdateEvent(id: string, draft: EventDraft): Promise<AdminEvent> {
  return request<AdminEvent>(`/api/admin/events/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: JSON.stringify(draft),
  });
}

export function adminPublishEvent(id: string): Promise<AdminEvent> {
  return request<AdminEvent>(`/api/admin/events/${encodeURIComponent(id)}/publish`, { method: "POST" });
}

export function adminCancelEvent(id: string): Promise<AdminEvent> {
  return request<AdminEvent>(`/api/admin/events/${encodeURIComponent(id)}/cancel`, { method: "POST" });
}

/** Adds a registered judge (from adminListJudges); 400 for anyone else. */
export async function adminAddJudge(eventId: string, userId: string): Promise<Judge> {
  const res = await request<{ judge: Judge }>(
    `/api/admin/events/${encodeURIComponent(eventId)}/judges`,
    { method: "POST", body: JSON.stringify({ userId }) },
  );
  return res.judge;
}

export function adminRemoveJudge(eventId: string, userId: string): Promise<void> {
  return request<void>(
    `/api/admin/events/${encodeURIComponent(eventId)}/judges/${encodeURIComponent(userId)}`,
    { method: "DELETE" },
  );
}

/** Everyone who can "Log in as judge", by name. */
export async function adminListJudges(): Promise<RegisteredJudge[]> {
  const { judges } = await request<{ judges: RegisteredJudge[] }>("/api/admin/judges");
  return judges;
}

/**
 * Registers a judge by name and phone. A number that has never signed in
 * gets an account; one that has keeps it, so the person can also play.
 * 409 if the number is already a judge.
 */
export async function adminRegisterJudge(name: string, phone: string): Promise<RegisteredJudge> {
  const { judge } = await request<{ judge: RegisteredJudge }>("/api/admin/judges", {
    method: "POST",
    body: JSON.stringify({ name, phone }),
  });
  return judge;
}

/**
 * Removes a judge. They come off every event still to come; matches
 * already scheduled keep them. Their account is untouched.
 */
export function adminDeleteJudge(userId: string): Promise<void> {
  return request<void>(`/api/admin/judges/${encodeURIComponent(userId)}`, { method: "DELETE" });
}

// Schedule: the matcher's proposal for an event, then the published matches.

export type ScheduledMatch = {
  /** Null in a preview; set once published. */
  id: string | null;
  format: EventFormat;
  court: Court;
  startsAt: string;
  endsAt: string;
  judge: Judge | null;
  sides: { side: "A" | "B"; players: (MatchPlayer & { entryId: string })[]; rating: number }[];
  /** E.g. "rating gap 240", "no judge", "gender balance not checked". */
  flags: string[];
  /** Saved matches only. */
  status?: MatchStatus;
  /** How it was made. */
  source?: MatchSource;
  /** Set when a challenge fixed it: the players chose each other, so it can be moved but not re-paired or cancelled. */
  challengeId?: string | null;
};

export type Schedule = {
  eventId: string;
  publishedAt: string | null;
  matches: ScheduledMatch[];
  standby: { entryId: string; user: PublicUser; reason: string }[];
  warnings: string[];
};

/** The published schedule, or null while it isn't published. */
export async function adminGetSchedule(eventId: string): Promise<Schedule | null> {
  try {
    const { schedule } = await request<{ schedule: Schedule }>(`/api/admin/events/${encodeURIComponent(eventId)}/schedule`);
    return schedule;
  } catch (err) {
    if (err instanceof ApiError && err.status === 404 && err.message.includes("isn't published")) return null;
    throw err;
  }
}

/** What the matcher would do now. Saves nothing. */
export async function adminPreviewSchedule(eventId: string): Promise<Schedule> {
  const { schedule } = await request<{ schedule: Schedule }>(
    `/api/admin/events/${encodeURIComponent(eventId)}/schedule/preview`,
    { method: "POST" },
  );
  return schedule;
}

/** Runs the matcher again and saves the matches. Only once per event for now. */
export async function adminPublishSchedule(eventId: string): Promise<Schedule> {
  const { schedule } = await request<{ schedule: Schedule }>(
    `/api/admin/events/${encodeURIComponent(eventId)}/schedule/publish`,
    { method: "POST" },
  );
  return schedule;
}

export type AdminEntry = {
  id: string;
  user: PublicUser;
  ratings: Partial<Record<EventFormat, number>>;
  kind: "open" | "locked";
  formats: EventFormat[];
  partner: PublicUser | null;
  source: "online" | "walk_in";
  status: EntryStatus;
  holdExpiresAt: string | null;
  holdExpired: boolean;
  matchId: string | null;
  standbyReason: string | null;
  confirmedBy: PublicUser | null;
  confirmedAt: string | null;
  createdAt: string;
};

/** An event's entries in join order. */
export async function adminListEntries(eventId: string): Promise<AdminEntry[]> {
  const { entries } = await request<{ entries: AdminEntry[] }>(
    `/api/admin/events/${encodeURIComponent(eventId)}/entries`,
  );
  return entries;
}

/** Records a cash or UPI payment taken at the desk. */
export async function adminMarkPaid(entryId: string): Promise<AdminEntry> {
  const { entry } = await request<{ entry: AdminEntry }>(
    `/api/admin/entries/${encodeURIComponent(entryId)}/mark-paid`,
    { method: "POST" },
  );
  return entry;
}

// Fixture editing: the admin pairs players by hand, swaps a player, moves or
// cancels a match. Every change is checked like the matcher's own (court
// free, players never back to back, a judge who isn't playing) and logged.

/** A court and time a match could take. */
export type FreeSlot = { court: Court; startsAt: string; endsAt: string };

/** Where a new match would go, and the judge it would get. */
export type SuggestedSlot = FreeSlot & { judge: Judge | null; flags: string[] };

export type NewMatch = {
  format: EventFormat;
  sideA: string[];
  sideB: string[];
  /** Both or neither: without them the earliest slot that suits everyone is used. */
  courtId?: string;
  startsAt?: string;
};

export async function adminCreateMatch(eventId: string, match: NewMatch): Promise<ScheduledMatch> {
  const { match: created } = await request<{ match: ScheduledMatch }>(
    `/api/admin/events/${encodeURIComponent(eventId)}/matches`,
    { method: "POST", body: JSON.stringify(match) },
  );
  return created;
}

/** What creating these entries' match would do, without saving it. */
export async function adminSuggestSlot(
  eventId: string,
  format: EventFormat,
  entryIds: string[],
  at?: { courtId: string; startsAt: string },
): Promise<SuggestedSlot> {
  const { slot } = await request<{ slot: SuggestedSlot }>(
    `/api/admin/events/${encodeURIComponent(eventId)}/matches/suggest`,
    { method: "POST", body: JSON.stringify({ format, entryIds, ...at }) },
  );
  return slot;
}

/** Court-free starts in the event's window; `matchId`'s own time counts as free (for moving it). */
export async function adminFreeSlots(eventId: string, format: EventFormat, matchId?: string): Promise<FreeSlot[]> {
  const params = new URLSearchParams({ format });
  if (matchId) params.set("matchId", matchId);
  const { slots } = await request<{ slots: FreeSlot[] }>(
    `/api/admin/events/${encodeURIComponent(eventId)}/free-slots?${params}`,
  );
  return slots;
}

/** "no_show": the player missed it and keeps no refund. "back_to_pool": they wait for another match. */
export type ReplaceOutcome = "no_show" | "back_to_pool";

export async function adminReplacePlayer(
  matchId: string,
  outEntryId: string,
  inEntryId: string,
  outcome: ReplaceOutcome,
): Promise<ScheduledMatch> {
  const { match } = await request<{ match: ScheduledMatch }>(`/api/admin/matches/${encodeURIComponent(matchId)}/replace`, {
    method: "POST",
    body: JSON.stringify({ outEntryId, inEntryId, outcome }),
  });
  return match;
}

export async function adminMoveMatch(matchId: string, courtId: string, startsAt: string): Promise<ScheduledMatch> {
  const { match } = await request<{ match: ScheduledMatch }>(`/api/admin/matches/${encodeURIComponent(matchId)}/move`, {
    method: "POST",
    body: JSON.stringify({ courtId, startsAt }),
  });
  return match;
}

/** Its players go back to waiting for a match. Not for challenge matches. */
export async function adminCancelMatch(matchId: string): Promise<ScheduledMatch> {
  const { match } = await request<{ match: ScheduledMatch }>(`/api/admin/matches/${encodeURIComponent(matchId)}/cancel`, {
    method: "POST",
  });
  return match;
}

export type MatchChange = {
  id: string;
  matchId: string;
  action: "create" | "replace" | "move" | "cancel";
  admin: PublicUser;
  details: Record<string, unknown>;
  createdAt: string;
};

/** Every hand edit to the event's matches, newest first. */
export async function adminListChanges(eventId: string): Promise<MatchChange[]> {
  const { changes } = await request<{ changes: MatchChange[] }>(
    `/api/admin/events/${encodeURIComponent(eventId)}/changes`,
  );
  return changes;
}

// --- Payments ---------------------------------------------------------------
// The server creates an order with the payment provider; the browser opens
// the provider's checkout; the server verifies what comes back. The
// provider's webhook confirms too, so a closed tab still counts.

export type Checkout = {
  provider: "razorpay" | "fake";
  /** Razorpay's public key id; empty for the fake provider. */
  keyId: string;
  orderId: string;
  amountPaise: number;
  currency: string;
  name: string;
  description: string;
  prefill: { name: string; email: string; contact: string };
};

/** A checkout to open, or the entry itself when nothing was due (a free event). */
export type PayStart = { checkout: Checkout; entry?: undefined } | { entry: Entry; checkout?: undefined };

export function startPayment(entryId: string): Promise<PayStart> {
  return request<PayStart>(`/api/me/entries/${encodeURIComponent(entryId)}/pay`, { method: "POST" });
}

export type CheckoutResult = { orderId: string; paymentId: string; signature: string };

/** Hands the checkout's result to the server, which checks it and confirms the entry. */
export async function verifyPayment(result: CheckoutResult): Promise<Entry> {
  const { entry } = await request<{ entry: Entry }>("/api/payments/verify", {
    method: "POST",
    body: JSON.stringify(result),
  });
  return entry;
}

// --- Challenges -------------------------------------------------------------
// One player challenges another to a singles match inside an event. Within
// 4 hours of sending, the opponent accepts and both pay; only then is the
// match fixed (design doc, section 12).

/** A player as challenges show them, with their singles rating. */
export type Player = PublicUser & { rating: number };

export type ChallengeStatus = "pending" | "accepted" | "scheduled" | "declined" | "withdrawn" | "expired" | "failed";

export type Challenge = {
  id: string;
  status: ChallengeStatus;
  role: "challenger" | "opponent";
  format: "singles";
  event: Pick<
    BadmintonEvent,
    "id" | "title" | "startsAt" | "endsAt" | "registrationClosesAt" | "feePaise" | "currency" | "venue"
  >;
  challenger: Player;
  opponent: Player;
  preferredStart: string;
  /** Accept and both pay before this, or the challenge expires. */
  expiresAt: string;
  createdAt: string;
  respondedAt: string | null;
  /** The caller's locked entry, once the challenge is accepted. */
  myEntry: { id: string; status: EntryStatus; holdExpiresAt: string | null } | null;
  theirEntryPaid: boolean;
  match: { id: string; court: Court; startsAt: string; endsAt: string; judge: Judge | null } | null;
};

export async function sendChallenge(eventId: string, opponentUsername: string, preferredStart: string): Promise<Challenge> {
  const { challenge } = await request<{ challenge: Challenge }>("/api/challenges", {
    method: "POST",
    body: JSON.stringify({ eventId, opponentUsername, preferredStart }),
  });
  return challenge;
}

export async function getChallenge(id: string): Promise<Challenge> {
  const { challenge } = await request<{ challenge: Challenge }>(`/api/challenges/${encodeURIComponent(id)}`);
  return challenge;
}

/** Sent and received: open ones first, then the rest, newest first. */
export async function myChallenges(): Promise<Challenge[]> {
  const { challenges } = await request<{ challenges: Challenge[] }>("/api/me/challenges");
  return challenges;
}

async function challengeAction(id: string, action: "accept" | "decline" | "withdraw"): Promise<Challenge> {
  const { challenge } = await request<{ challenge: Challenge }>(
    `/api/challenges/${encodeURIComponent(id)}/${action}`,
    { method: "POST" },
  );
  return challenge;
}

export const acceptChallenge = (id: string) => challengeAction(id, "accept");
export const declineChallenge = (id: string) => challengeAction(id, "decline");
export const withdrawChallenge = (id: string) => challengeAction(id, "withdraw");

/** People the caller has played or challenged, most recent first. */
export async function recentPlayers(): Promise<Player[]> {
  const { players } = await request<{ players: Player[] }>("/api/me/recent-players");
  return players;
}

/** Players active at venues near a point, closest rating first. */
export async function nearbyPlayers(lat: number, lng: number, radiusKm = 10): Promise<Player[]> {
  const params = new URLSearchParams({ lat: String(lat), lng: String(lng), radiusKm: String(radiusKm) });
  const { players } = await request<{ players: Player[] }>(`/api/players/nearby?${params}`);
  return players;
}

/** The signed-in player's ratings (1200 until they've played) and finished matches. */
export type PlayerStats = { ratings: Record<EventFormat, number>; played: number; won: number };

export function myStats(): Promise<PlayerStats> {
  return request<PlayerStats>("/api/me/stats");
}

// --- Live scoring -----------------------------------------------------------
// The judge starts the match and scores it rally by rally; players watch it
// live. The score is computed by the server from its point log, so a client
// only ever displays what it's sent (design doc, section 8).

export type Side = "A" | "B";

/** One game; winner is missing while it's being played. */
export type GameScore = { a: number; b: number; winner?: Side };

/** A point that counts, with the score it made. */
export type Rally = { seq: number; side: Side; game: number; a: number; b: number; at: string };

export type LiveScore = {
  /** Finished games, then the one in play. */
  games: GameScore[];
  gamesA: number;
  gamesB: number;
  /** Serving the next rally; "" before the start. */
  server: Side | "";
  /** Set once decided, before the judge finishes the match. */
  winner?: Side;
  /** Newest first. */
  rallies: Rally[];
};

export type LiveMatch = {
  id: string;
  status: MatchStatus;
  format: EventFormat;
  event: { id: string; title: string } | null;
  venue: Venue;
  court: Court;
  startsAt: string;
  endsAt: string;
  startedAt: string | null;
  completedAt: string | null;
  judge: Judge | null;
  sides: MatchSide[];
  firstServer: Side | "";
  winnerSide: Side | null;
  score: LiveScore;
  /** The number the judge's next point or undo must carry. */
  nextSeq: number;
  /** The earliest the judge may start it. */
  canStartFrom: string;
  /** The side that retired (injury); null unless it did. */
  retiredSide: Side | null;
  /** Set once an admin voids the match. */
  void: { at: string; reason: string; from: MatchStatus } | null;
  /** The rating system players see, e.g. "glicko-1". */
  ratingSystem: string;
  /**
   * Every system's changes, oldest first: one per player and system if the
   * match was rated, then one each if it was voided. Show ratingSystem's.
   */
  ratingChanges: RatingChange[];
};

export type RatingChange = {
  userId: string;
  /** Which system made it: "glicko-1" or "elo-1". */
  system: string;
  kind: "match" | "void";
  before: number;
  after: number;
  change: number;
  /** Glicko's uncertainty; null for Elo. */
  rdBefore: number | null;
  rdAfter: number | null;
  /** The system's prediction, before the match, that this player's side would win; null on a void. */
  winProbability: number | null;
};

/** A match that has ended one way or another: nothing more will happen. */
export const matchOver = (status: MatchStatus) =>
  status === "completed" ||
  status === "retired" ||
  status === "walkover" ||
  status === "cancelled" ||
  status === "void";

/** The player's net change from a match in the rating they see: 0 once voided, unless later matches were replayed. */
export function ratingChangeFor(m: LiveMatch, userId: string): RatingChange | null {
  const mine = m.ratingChanges.filter((c) => c.userId === userId && c.system === m.ratingSystem);
  if (mine.length === 0) return null;
  const rated = mine.find((c) => c.kind === "match")!;
  const last = mine[mine.length - 1];
  return { ...rated, after: last.after, change: mine.reduce((sum, c) => sum + c.change, 0) };
}

export async function liveMatch(id: string): Promise<LiveMatch> {
  const { match } = await request<{ match: LiveMatch }>(`/api/matches/${encodeURIComponent(id)}/live`);
  return match;
}

/**
 * Follows a match as it's scored, over server-sent events. Calls onChange
 * with every state, starting with the current one, and stops once the match
 * is over. EventSource reconnects by itself after a dropped connection.
 * Returns a function that stops watching.
 */
export function watchMatch(id: string, onChange: (m: LiveMatch) => void, onError?: () => void): () => void {
  const source = new EventSource(`/api/matches/${encodeURIComponent(id)}/live/stream`);
  source.addEventListener("match", (e) => {
    const m = JSON.parse((e as MessageEvent<string>).data) as LiveMatch;
    onChange(m);
    // The server ends the stream after a final state; without this the
    // browser would reconnect to it every few seconds.
    if (matchOver(m.status)) source.close();
  });
  source.onerror = () => onError?.();
  return () => source.close();
}

/** Judge only: starts a scheduled match, with who serves first. */
export async function judgeStartMatch(id: string, firstServer: Side): Promise<LiveMatch> {
  const { match } = await request<{ match: LiveMatch }>(`/api/judge/matches/${encodeURIComponent(id)}/start`, {
    method: "POST",
    body: JSON.stringify({ firstServer }),
  });
  return match;
}

export type PointSend = { seq: number; kind: "point"; side: Side; clientId: string } | { seq: number; kind: "undo"; clientId: string };

/**
 * Judge only: records a point or an undo. A clientId the server has seen is
 * a resend and changes nothing. 409 when seq is out of turn: reload the
 * match for its nextSeq and resend.
 */
export async function judgeSendPoint(id: string, point: PointSend): Promise<LiveMatch> {
  const { match } = await request<{ match: LiveMatch }>(`/api/judge/matches/${encodeURIComponent(id)}/points`, {
    method: "POST",
    body: JSON.stringify(point),
  });
  return match;
}

/** Judge only: records the result of a decided match. */
export async function judgeFinishMatch(id: string): Promise<LiveMatch> {
  const { match } = await request<{ match: LiveMatch }>(`/api/judge/matches/${encodeURIComponent(id)}/finish`, {
    method: "POST",
  });
  return match;
}

/**
 * Judge only: ends a live match because a side can't go on (injury). The
 * other side wins; it's rated on the points played so far.
 */
export async function judgeRetireMatch(id: string, retiredSide: Side): Promise<LiveMatch> {
  const { match } = await request<{ match: LiveMatch }>(`/api/judge/matches/${encodeURIComponent(id)}/retire`, {
    method: "POST",
    body: JSON.stringify({ retiredSide }),
  });
  return match;
}

/** Judge only: gives a match that hasn't started to the side that turned up. Not rated. */
export async function judgeWalkover(id: string, winnerSide: Side): Promise<LiveMatch> {
  const { match } = await request<{ match: LiveMatch }>(`/api/judge/matches/${encodeURIComponent(id)}/walkover`, {
    method: "POST",
    body: JSON.stringify({ winnerSide }),
  });
  return match;
}

/** Admin: records a walkover on any match that hasn't started. */
export async function adminWalkover(id: string, winnerSide: Side): Promise<LiveMatch> {
  const { match } = await request<{ match: LiveMatch }>(`/api/admin/matches/${encodeURIComponent(id)}/walkover`, {
    method: "POST",
    body: JSON.stringify({ winnerSide }),
  });
  return match;
}

/**
 * Admin: strikes off a match that started or ended, with a reason (up to
 * 200 characters). Its rating changes are reversed.
 */
export async function adminVoidMatch(id: string, reason: string): Promise<LiveMatch> {
  const { match } = await request<{ match: LiveMatch }>(`/api/admin/matches/${encodeURIComponent(id)}/void`, {
    method: "POST",
    body: JSON.stringify({ reason }),
  });
  return match;
}

export type PlayerRating = {
  system: string;
  discipline: EventFormat;
  rating: number;
  matchesPlayed: number;
  provisional: boolean;
  /** Glicko's uncertainty after their last match, and grown since; null for Elo. */
  rd: number | null;
  rdNow: number | null;
  lastRatedAt: string | null;
};

export type RatingHistoryEntry = {
  matchId: string;
  system: string;
  discipline: EventFormat;
  /** "adjust": a void replayed later matches and moved this player's rating. */
  kind: "match" | "void" | "adjust";
  before: number;
  after: number;
  change: number;
  rdBefore: number | null;
  rdAfter: number | null;
  winProbability: number | null;
  at: string;
};

/** What every rating system says about a player: current ratings and recent history, newest first. */
export async function playerRatings(
  userId: string,
): Promise<{ userId: string; default: string; ratings: PlayerRating[]; history: RatingHistoryEntry[] }> {
  return request(`/api/players/${encodeURIComponent(userId)}/ratings`);
}

/**
 * Players at the signed-in player's level, closest singles rating first, so
 * picking someone never starts from an empty list.
 */
export async function suggestedPlayers(): Promise<Player[]> {
  const { players } = await request<{ players: Player[] }>("/api/players/suggested");
  return players;
}
