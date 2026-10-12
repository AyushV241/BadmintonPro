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

export type MatchStatus = "scheduled" | "ready" | "live" | "completed" | "walkover" | "cancelled";

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
};

/** The signed-in player's matches, soonest first. */
export async function myMatches(): Promise<MyMatch[]> {
  const { matches } = await request<{ matches: MyMatch[] }>("/api/me/matches");
  return matches;
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

/** 404 if no user has that username. */
export async function adminAddJudge(eventId: string, username: string): Promise<Judge> {
  const res = await request<{ judge: Judge }>(
    `/api/admin/events/${encodeURIComponent(eventId)}/judges`,
    { method: "POST", body: JSON.stringify({ username }) },
  );
  return res.judge;
}

export function adminRemoveJudge(eventId: string, userId: string): Promise<void> {
  return request<void>(
    `/api/admin/events/${encodeURIComponent(eventId)}/judges/${encodeURIComponent(userId)}`,
    { method: "DELETE" },
  );
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
