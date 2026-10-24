"use client";

import { useState, type FormEvent, type ReactNode } from "react";
import { ErrorNote, StickyFooter } from "@/components/admin/AdminShell";
import { ChipToggle } from "@/components/form/ChipToggle";
import { FieldError } from "@/components/form/FieldError";
import { FieldLabel } from "@/components/form/FieldLabel";
import { Button, Input, SingleSelect } from "@/components/ui";
import { ApiError, type AdminVenue, type EventDraft, type EventFormat } from "@/lib/api";
import { maxMatchesFor, venueDate } from "@/lib/eventTime";
import { useFormErrors, type FieldErrors } from "@/lib/useFormErrors";

const FORMATS: { value: EventFormat; label: string }[] = [
  { value: "singles", label: "Singles" },
  { value: "doubles", label: "Doubles" },
  { value: "mixed", label: "Mixed doubles" },
];

// Must match the backend's defaults (defaultSlotMinutes in admin.go).
const DEFAULT_SLOT: Record<EventFormat, number> = { singles: 30, doubles: 40, mixed: 40 };

const CLOSE_OPTIONS = [
  { value: "0", label: "At the start" },
  { value: "30", label: "30 minutes before" },
  { value: "60", label: "1 hour before" },
  { value: "120", label: "2 hours before" },
  { value: "240", label: "4 hours before" },
  { value: "720", label: "12 hours before" },
  { value: "1440", label: "1 day before" },
];

function activeCourtIds(venue: AdminVenue | undefined) {
  return venue?.courts.filter((c) => c.active).map((c) => c.id) ?? [];
}

/** A new event's starting values: tomorrow evening, ₹50, all courts. */
export function newEventDraft(venues: AdminVenue[]): EventDraft {
  const venue = venues[0];
  const tomorrow = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString();
  return {
    venueId: venue?.id ?? "",
    title: "",
    formats: ["singles"],
    slotMinutes: {},
    date: venueDate(tomorrow, venue?.timezone ?? "Asia/Kolkata"),
    startTime: "19:00",
    endTime: "21:00",
    registrationClosesMinutesBefore: 120,
    feePaise: 5000,
    capacity: 16,
    maxMatches: 8,
    ratingMin: null,
    ratingMax: null,
    courtIds: activeCourtIds(venue),
  };
}

/** A number input's text as a whole number, or null when empty. */
function wholeNumber(text: string): number | null {
  const n = Number.parseInt(text, 10);
  return Number.isNaN(n) ? null : n;
}

type FieldKey = "venue" | "courts" | "title" | "formats" | "slots" | "date" | "times" | "capacity" | "maxMatches" | "fee" | "rating";

const toMinutes = (t: string) => {
  const [h, m] = t.split(":").map(Number);
  return h * 60 + m;
};

/**
 * What the admin still has to fix, per field. The server checks the same
 * things (and more); this catches them before a round trip, next to the
 * field instead of at the bottom of the form.
 */
function validate(draft: EventDraft, feeText: string, limit: number): FieldErrors<FieldKey> {
  const errors: FieldErrors<FieldKey> = {};
  const fee = Number.parseFloat(feeText);
  if (!draft.venueId) errors.venue = "Pick a venue.";
  if (draft.courtIds.length === 0) errors.courts = "Pick at least one court.";
  if (!draft.title.trim()) errors.title = "Give the event a title.";
  if (draft.formats.length === 0) errors.formats = "Pick at least one format.";
  else if (draft.formats.some((f) => !((draft.slotMinutes[f] ?? DEFAULT_SLOT[f]) > 0))) {
    errors.slots = "Each format needs a slot length in minutes.";
  }
  if (!draft.date) errors.date = "Pick a date.";
  if (!draft.startTime || !draft.endTime) errors.times = "Set when it starts and ends.";
  else if (toMinutes(draft.endTime) <= toMinutes(draft.startTime)) {
    errors.times = "It has to end after it starts, on the same day.";
  }
  if (!(draft.capacity >= 2)) errors.capacity = "At least 2 entries.";
  if (draft.courtIds.length > 0) {
    if (!(draft.maxMatches >= 1)) errors.maxMatches = "At least 1 match.";
    else if (draft.maxMatches > limit) errors.maxMatches = `At most ${limit} on these courts and times.`;
  }
  if (feeText.trim() === "" || Number.isNaN(fee) || fee < 0) errors.fee = "Enter a fee, or 0 for free.";
  if (draft.ratingMin !== null && draft.ratingMax !== null && draft.ratingMin > draft.ratingMax) {
    errors.rating = "The minimum can't be above the maximum.";
  }
  return errors;
}

/** The field a server error is about, so it shows there instead of at the bottom. */
function fieldForServerError(message: string): FieldKey | null {
  const m = message.toLowerCase();
  if (m.includes("court")) return "courts";
  if (m.includes("title")) return "title";
  if (m.includes("slot")) return "slots";
  if (m.includes("format")) return "formats";
  if (m.includes("match")) return "maxMatches";
  if (m.includes("capacity") || m.includes("entries")) return "capacity";
  if (m.includes("fee")) return "fee";
  if (m.includes("rating")) return "rating";
  if (m.includes("date") || m.includes("future")) return "date";
  if (m.includes("time") || m.includes("start") || m.includes("end")) return "times";
  if (m.includes("venue")) return "venue";
  return null;
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-4 rounded-[22px] border border-line bg-surface p-4">
      <h2 className="text-[17px] font-semibold tracking-[-0.02em]">{title}</h2>
      {children}
    </section>
  );
}

function Field({ id, label, optional, children }: { id: string; label: string; optional?: boolean; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-2">
      <FieldLabel htmlFor={id} optional={optional}>
        {label}
      </FieldLabel>
      {children}
    </div>
  );
}

/**
 * Create or edit an event. Times are the venue's local date and times; the
 * server applies the venue's time zone. The server checks everything again,
 * so the limits here only guide the admin.
 */
export function EventForm({
  venues,
  initial,
  submitLabel,
  onSubmit,
}: {
  venues: AdminVenue[];
  initial: EventDraft;
  submitLabel: string;
  onSubmit: (draft: EventDraft) => Promise<void>;
}) {
  const [draft, setDraft] = useState<EventDraft>(initial);
  const [feeText, setFeeText] = useState(String(initial.feePaise / 100));
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const { errors, field, show, clear } = useFormErrors<FieldKey>();

  const venue = venues.find((v) => v.id === draft.venueId);
  /** Updates the draft; `fixes` names the field whose error the change may fix. */
  const set = (patch: Partial<EventDraft>, fixes?: FieldKey) => {
    setDraft((d) => ({ ...d, ...patch }));
    if (fixes) clear(fixes);
  };
  const slot = (f: EventFormat) => draft.slotMinutes[f] ?? DEFAULT_SLOT[f];
  const noCourts = draft.courtIds.length === 0;
  const limit = maxMatchesFor(
    draft.courtIds.length,
    draft.startTime,
    draft.endTime,
    draft.formats.map(slot),
  );

  // Toggles build on the latest draft, not this render's: two quick taps
  // must both count.
  function toggleFormat(f: EventFormat) {
    setDraft((d) => {
      const formats = d.formats.includes(f) ? d.formats.filter((x) => x !== f) : [...d.formats, f];
      // Only send slot lengths for formats on offer; the server rejects others.
      const slotMinutes = Object.fromEntries(Object.entries(d.slotMinutes).filter(([k]) => formats.includes(k as EventFormat)));
      return { ...d, formats, slotMinutes };
    });
    clear("formats");
  }

  function toggleCourt(id: string) {
    setDraft((d) => ({
      ...d,
      courtIds: d.courtIds.includes(id) ? d.courtIds.filter((x) => x !== id) : [...d.courtIds, id],
    }));
    clear("courts");
    clear("maxMatches");
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    // Something missing: show it where it is and go there.
    if (show(validate(draft, feeText, limit))) return;
    setSaving(true);
    try {
      await onSubmit({ ...draft, feePaise: Math.round((Number.parseFloat(feeText) || 0) * 100) });
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : "Something went wrong.";
      const key = err instanceof ApiError ? fieldForServerError(msg) : null;
      if (key) show({ [key]: msg.charAt(0).toUpperCase() + msg.slice(1) });
      else setError(msg);
    } finally {
      setSaving(false);
    }
  }

  if (venues.length === 0) {
    return (
      <p className="rounded-[22px] border border-line bg-surface p-5 text-sm text-muted">
        Add a venue with courts first, under Venues.
      </p>
    );
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-4">
      <Section title="Where">
        <div {...field("venue")}>
          <SingleSelect
            label="Venue"
            value={draft.venueId}
            options={venues.map((v) => ({ value: v.id, label: `${v.name} · ${v.city}` }))}
            onChange={(venueId) => {
              set({ venueId, courtIds: activeCourtIds(venues.find((v) => v.id === venueId)) }, "venue");
              clear("courts");
            }}
            error={errors.venue}
          />
        </div>
        <div {...field("courts")} className="flex flex-col gap-2">
          <FieldLabel htmlFor="event-courts">Courts booked</FieldLabel>
          <div
            id="event-courts"
            role="group"
            aria-label="Courts booked"
            aria-describedby={errors.courts ? "event-courts-error" : undefined}
            className="flex flex-wrap gap-2"
          >
            {venue?.courts.map((c) => (
              <ChipToggle
                key={c.id}
                pressed={draft.courtIds.includes(c.id)}
                onToggle={() => toggleCourt(c.id)}
                disabled={!c.active}
              >
                {c.label}
              </ChipToggle>
            ))}
            {venue?.courts.length === 0 && <p className="text-sm text-muted">This venue has no courts yet.</p>}
          </div>
          <FieldError id="event-courts-error" message={errors.courts} />
        </div>
      </Section>

      <Section title="What">
        <div {...field("title")}>
          <Field id="event-title" label="Title">
            <Input
              id="event-title"
              value={draft.title}
              onChange={(title) => set({ title }, "title")}
              placeholder="Saturday Smash Session"
              maxLength={120}
              error={errors.title}
            />
          </Field>
        </div>
        <div {...field("formats")} className="flex flex-col gap-2">
          <FieldLabel htmlFor="event-formats">Formats</FieldLabel>
          <div
            id="event-formats"
            role="group"
            aria-label="Formats"
            aria-describedby={errors.formats ? "event-formats-error" : undefined}
            className="flex flex-wrap gap-2"
          >
            {FORMATS.map((f) => (
              <ChipToggle key={f.value} pressed={draft.formats.includes(f.value)} onToggle={() => toggleFormat(f.value)}>
                {f.label}
              </ChipToggle>
            ))}
          </div>
          <FieldError id="event-formats-error" message={errors.formats} />
        </div>
        {draft.formats.length > 0 && (
          <div {...field("slots")} className="grid grid-cols-3 gap-2">
            {FORMATS.filter((f) => draft.formats.includes(f.value)).map((f) => (
              <Field key={f.value} id={`slot-${f.value}`} label={`${f.value} min`}>
                <Input
                  id={`slot-${f.value}`}
                  type="number"
                  inputMode="numeric"
                  value={String(slot(f.value))}
                  onChange={(text) =>
                    set({ slotMinutes: { ...draft.slotMinutes, [f.value]: wholeNumber(text) ?? 0 } }, "slots")
                  }
                  error={!!errors.slots && !(slot(f.value) > 0)}
                />
              </Field>
            ))}
            <div className="col-span-3">
              <FieldError id="event-slots-error" message={errors.slots} />
            </div>
          </div>
        )}
      </Section>

      <Section title="When">
        <div {...field("date")}>
          <Field id="event-date" label="Date">
            <Input id="event-date" type="date" value={draft.date} onChange={(date) => set({ date }, "date")} error={errors.date} />
          </Field>
        </div>
        <div {...field("times")} className="flex flex-col gap-2">
          <div className="grid grid-cols-2 gap-3">
            <Field id="event-start" label="Starts">
              <Input
                id="event-start"
                type="time"
                value={draft.startTime}
                onChange={(startTime) => set({ startTime }, "times")}
                error={!!errors.times}
              />
            </Field>
            <Field id="event-end" label="Ends">
              <Input
                id="event-end"
                type="time"
                value={draft.endTime}
                onChange={(endTime) => set({ endTime }, "times")}
                error={!!errors.times}
              />
            </Field>
          </div>
          <FieldError id="event-times-error" message={errors.times} />
        </div>
        <SingleSelect
          label="Joining closes"
          value={String(draft.registrationClosesMinutesBefore)}
          options={CLOSE_OPTIONS}
          onChange={(v) => set({ registrationClosesMinutesBefore: Number(v) })}
          helperText={`Times are at the venue (${venue?.timezone ?? ""}).`}
        />
      </Section>

      <Section title="Entries and matches">
        <div className="grid grid-cols-2 gap-3">
          <div {...field("capacity")}>
            <Field id="event-capacity" label="Entries">
              <Input
                id="event-capacity"
                type="number"
                inputMode="numeric"
                value={String(draft.capacity)}
                onChange={(text) => set({ capacity: wholeNumber(text) ?? 0 }, "capacity")}
                error={errors.capacity}
              />
            </Field>
          </div>
          <div {...field("maxMatches")}>
            <Field id="event-matches" label="Max matches">
              {/* The limit comes from the courts, so there's nothing to set until one is picked. */}
              <Input
                id="event-matches"
                type="number"
                inputMode="numeric"
                value={noCourts ? "" : String(draft.maxMatches)}
                onChange={(text) => set({ maxMatches: wholeNumber(text) ?? 0 }, "maxMatches")}
                disabled={noCourts}
                placeholder={noCourts ? "–" : undefined}
                error={errors.maxMatches ?? (!noCourts && draft.maxMatches > limit ? `At most ${limit}` : undefined)}
                helperText={noCourts ? "Select a court first" : `Up to ${limit} on these courts`}
              />
            </Field>
          </div>
        </div>
      </Section>

      <Section title="Fee and rating band">
        <div {...field("fee")}>
          <Field id="event-fee" label="Entry fee (₹)">
            <Input
              id="event-fee"
              type="number"
              inputMode="decimal"
              value={feeText}
              onChange={(text) => {
                setFeeText(text);
                clear("fee");
              }}
              error={errors.fee}
              helperText="Per entry: one match. 0 for a free event."
            />
          </Field>
        </div>
        <div {...field("rating")} className="grid grid-cols-2 gap-3">
          <Field id="event-rating-min" label="Min rating" optional>
            <Input
              id="event-rating-min"
              type="number"
              inputMode="numeric"
              placeholder="Any"
              value={draft.ratingMin === null ? "" : String(draft.ratingMin)}
              onChange={(text) => set({ ratingMin: wholeNumber(text) }, "rating")}
              error={!!errors.rating}
            />
          </Field>
          <Field id="event-rating-max" label="Max rating" optional>
            <Input
              id="event-rating-max"
              type="number"
              inputMode="numeric"
              placeholder="Any"
              value={draft.ratingMax === null ? "" : String(draft.ratingMax)}
              onChange={(text) => set({ ratingMax: wholeNumber(text) }, "rating")}
              error={!!errors.rating}
            />
          </Field>
          <div className="col-span-2">
            <FieldError id="event-rating-error" message={errors.rating} />
          </div>
        </div>
      </Section>

      <ErrorNote message={error} />
      <StickyFooter>
        <Button type="submit" variant="accent" size="lg" fullWidth loading={saving}>
          {submitLabel}
        </Button>
      </StickyFooter>
    </form>
  );
}
