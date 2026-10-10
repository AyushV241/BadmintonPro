"use client";

import { useState, type FormEvent, type ReactNode } from "react";
import { ErrorNote } from "@/components/admin/AdminShell";
import { ChipToggle } from "@/components/form/ChipToggle";
import { FieldLabel } from "@/components/form/FieldLabel";
import { Button, Input, SingleSelect } from "@/components/ui";
import { ApiError, type AdminVenue, type EventDraft, type EventFormat } from "@/lib/api";
import { maxMatchesFor, venueDate } from "@/lib/eventTime";

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

  const venue = venues.find((v) => v.id === draft.venueId);
  const set = (patch: Partial<EventDraft>) => setDraft((d) => ({ ...d, ...patch }));
  const slot = (f: EventFormat) => draft.slotMinutes[f] ?? DEFAULT_SLOT[f];
  const limit = maxMatchesFor(
    draft.courtIds.length,
    draft.startTime,
    draft.endTime,
    draft.formats.map(slot),
  );

  function toggleFormat(f: EventFormat) {
    const formats = draft.formats.includes(f) ? draft.formats.filter((x) => x !== f) : [...draft.formats, f];
    // Only send slot lengths for formats on offer; the server rejects others.
    const slotMinutes = Object.fromEntries(
      Object.entries(draft.slotMinutes).filter(([k]) => formats.includes(k as EventFormat)),
    );
    set({ formats, slotMinutes });
  }

  function toggleCourt(id: string) {
    set({ courtIds: draft.courtIds.includes(id) ? draft.courtIds.filter((x) => x !== id) : [...draft.courtIds, id] });
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setSaving(true);
    try {
      await onSubmit({ ...draft, feePaise: Math.round((Number.parseFloat(feeText) || 0) * 100) });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Something went wrong.");
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
    <form onSubmit={handleSubmit} className="flex flex-col gap-4">
      <Section title="Where">
        <SingleSelect
          label="Venue"
          value={draft.venueId}
          options={venues.map((v) => ({ value: v.id, label: `${v.name} · ${v.city}` }))}
          onChange={(venueId) =>
            set({ venueId, courtIds: activeCourtIds(venues.find((v) => v.id === venueId)) })
          }
        />
        <div className="flex flex-col gap-2">
          <FieldLabel htmlFor="event-courts">Courts booked</FieldLabel>
          <div id="event-courts" role="group" aria-label="Courts booked" className="flex flex-wrap gap-2">
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
        </div>
      </Section>

      <Section title="What">
        <Field id="event-title" label="Title">
          <Input
            id="event-title"
            value={draft.title}
            onChange={(title) => set({ title })}
            placeholder="Saturday Smash Session"
            maxLength={120}
            required
          />
        </Field>
        <div className="flex flex-col gap-2">
          <FieldLabel htmlFor="event-formats">Formats</FieldLabel>
          <div id="event-formats" role="group" aria-label="Formats" className="flex flex-wrap gap-2">
            {FORMATS.map((f) => (
              <ChipToggle key={f.value} pressed={draft.formats.includes(f.value)} onToggle={() => toggleFormat(f.value)}>
                {f.label}
              </ChipToggle>
            ))}
          </div>
        </div>
        {draft.formats.length > 0 && (
          <div className="grid grid-cols-3 gap-2">
            {FORMATS.filter((f) => draft.formats.includes(f.value)).map((f) => (
              <Field key={f.value} id={`slot-${f.value}`} label={`${f.value} min`}>
                <Input
                  id={`slot-${f.value}`}
                  type="number"
                  inputMode="numeric"
                  value={String(slot(f.value))}
                  onChange={(text) =>
                    set({ slotMinutes: { ...draft.slotMinutes, [f.value]: wholeNumber(text) ?? 0 } })
                  }
                />
              </Field>
            ))}
          </div>
        )}
      </Section>

      <Section title="When">
        <Field id="event-date" label="Date">
          <Input id="event-date" type="date" value={draft.date} onChange={(date) => set({ date })} required />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field id="event-start" label="Starts">
            <Input id="event-start" type="time" value={draft.startTime} onChange={(startTime) => set({ startTime })} required />
          </Field>
          <Field id="event-end" label="Ends">
            <Input id="event-end" type="time" value={draft.endTime} onChange={(endTime) => set({ endTime })} required />
          </Field>
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
          <Field id="event-capacity" label="Entries">
            <Input
              id="event-capacity"
              type="number"
              inputMode="numeric"
              value={String(draft.capacity)}
              onChange={(text) => set({ capacity: wholeNumber(text) ?? 0 })}
            />
          </Field>
          <Field id="event-matches" label="Max matches">
            <Input
              id="event-matches"
              type="number"
              inputMode="numeric"
              value={String(draft.maxMatches)}
              onChange={(text) => set({ maxMatches: wholeNumber(text) ?? 0 })}
              error={draft.maxMatches > limit ? `At most ${limit}` : undefined}
              helperText={`Up to ${limit} on these courts`}
            />
          </Field>
        </div>
      </Section>

      <Section title="Fee and rating band">
        <Field id="event-fee" label="Slot fee (₹)">
          <Input
            id="event-fee"
            type="number"
            inputMode="decimal"
            value={feeText}
            onChange={setFeeText}
            helperText="Paid when joining, to secure a place."
          />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field id="event-rating-min" label="Min rating" optional>
            <Input
              id="event-rating-min"
              type="number"
              inputMode="numeric"
              placeholder="Any"
              value={draft.ratingMin === null ? "" : String(draft.ratingMin)}
              onChange={(text) => set({ ratingMin: wholeNumber(text) })}
            />
          </Field>
          <Field id="event-rating-max" label="Max rating" optional>
            <Input
              id="event-rating-max"
              type="number"
              inputMode="numeric"
              placeholder="Any"
              value={draft.ratingMax === null ? "" : String(draft.ratingMax)}
              onChange={(text) => set({ ratingMax: wholeNumber(text) })}
            />
          </Field>
        </div>
      </Section>

      <ErrorNote message={error} />
      <Button type="submit" variant="accent" size="lg" fullWidth loading={saving}>
        {submitLabel}
      </Button>
    </form>
  );
}
