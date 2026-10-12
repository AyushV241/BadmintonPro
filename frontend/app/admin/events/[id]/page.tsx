"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";
import { AdminShell, ErrorNote, StatusChip } from "@/components/admin/AdminShell";
import { EventForm } from "@/components/admin/EventForm";
import { FieldLabel } from "@/components/form/FieldLabel";
import { ArrowRightIcon, XIcon } from "@/components/icons";
import { Button, Input } from "@/components/ui";
import {
  ApiError,
  adminAddJudge,
  adminCancelEvent,
  adminGetEvent,
  adminListVenues,
  adminPublishEvent,
  adminRemoveJudge,
  adminUpdateEvent,
  type AdminEvent,
  type AdminVenue,
  type BadmintonEvent,
  type EventDraft,
} from "@/lib/api";
import { formatWhen, rupees, venueDate, venueTime } from "@/lib/eventTime";

const message = (err: unknown) => (err instanceof ApiError ? err.message : "Something went wrong.");

/** The event as the form edits it: the venue's local date and times. */
function draftFromEvent(e: BadmintonEvent): EventDraft {
  const tz = e.venue.timezone;
  return {
    venueId: e.venue.id,
    title: e.title,
    formats: e.formats,
    slotMinutes: e.slotMinutes,
    date: venueDate(e.startsAt, tz),
    startTime: venueTime(e.startsAt, tz),
    endTime: venueTime(e.endsAt, tz),
    registrationClosesMinutesBefore: Math.round((Date.parse(e.startsAt) - Date.parse(e.registrationClosesAt)) / 60000),
    feePaise: e.feePaise,
    capacity: e.capacity,
    maxMatches: e.maxMatches,
    ratingMin: e.ratingMin,
    ratingMax: e.ratingMax,
    courtIds: e.courts?.map((c) => c.id) ?? [],
  };
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-4 border-b border-line py-2.5 text-sm last:border-b-0">
      <span className="text-muted">{label}</span>
      <span className="text-right font-semibold">{value}</span>
    </div>
  );
}

/** Read-only details, once an event is no longer a draft. */
function Summary({ e }: { e: BadmintonEvent }) {
  const band =
    e.ratingMin === null && e.ratingMax === null ? "Any" : `${e.ratingMin ?? "Any"} – ${e.ratingMax ?? "Any"}`;
  return (
    <div className="rounded-[22px] border border-line bg-surface px-4 py-2">
      <Row label="Formats" value={e.formats.map((f) => `${f} (${e.slotMinutes[f]} min)`).join(", ")} />
      <Row label="Courts" value={e.courts?.map((c) => c.label).join(", ") ?? "–"} />
      <Row label="Ends" value={formatWhen(e.endsAt, e.venue.timezone)} />
      <Row label="Joining closes" value={formatWhen(e.registrationClosesAt, e.venue.timezone)} />
      <Row label="Entries" value={`${e.capacity} (${e.spotsLeft} left)`} />
      <Row label="Max matches" value={String(e.maxMatches)} />
      <Row label="Slot fee" value={rupees(e.feePaise)} />
      <Row label="Rating band" value={band} />
    </div>
  );
}

function Judges({ data, onChange }: { data: AdminEvent; onChange: (d: AdminEvent) => void }) {
  const [username, setUsername] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const eventId = data.event.id;

  async function add(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setBusy(true);
    try {
      const judge = await adminAddJudge(eventId, username);
      if (!data.judges.some((j) => j.userId === judge.userId)) {
        onChange({ ...data, judges: [...data.judges, judge] });
      }
      setUsername("");
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  async function remove(userId: string) {
    setError(null);
    try {
      await adminRemoveJudge(eventId, userId);
      onChange({ ...data, judges: data.judges.filter((j) => j.userId !== userId) });
    } catch (err) {
      setError(message(err));
    }
  }

  return (
    <section className="flex flex-col gap-3 rounded-[22px] border border-line bg-surface p-4">
      <div>
        <h2 className="text-[17px] font-semibold tracking-[-0.02em]">Judges</h2>
        <p className="text-sm text-muted">They score this event&rsquo;s matches live.</p>
      </div>
      {data.judges.length === 0 && <p className="text-sm text-muted">No judges yet.</p>}
      <ul className="flex flex-col gap-2">
        {data.judges.map((j) => (
          <li key={j.userId} className="flex items-center justify-between gap-3 rounded-2xl bg-background px-3 py-2">
            <span className="flex flex-col">
              <span className="text-sm font-semibold">{j.name}</span>
              <span className="text-xs text-muted">@{j.username}</span>
            </span>
            <button
              type="button"
              aria-label={`Remove ${j.name}`}
              onClick={() => void remove(j.userId)}
              className="flex size-11 items-center justify-center rounded-full text-muted"
            >
              <XIcon className="size-5" />
            </button>
          </li>
        ))}
      </ul>
      <form onSubmit={add} className="flex flex-col gap-2">
        <FieldLabel htmlFor="judge-username">Add by username</FieldLabel>
        <div className="flex gap-2">
          <Input
            id="judge-username"
            value={username}
            onChange={setUsername}
            placeholder="priya"
            startAdornment="@"
            autoComplete="off"
          />
          <Button type="submit" variant="secondary" loading={busy} disabled={!username.trim()}>
            Add
          </Button>
        </div>
      </form>
      <ErrorNote message={error} />
    </section>
  );
}

function EventAdmin({ id }: { id: string }) {
  const [data, setData] = useState<AdminEvent | null>(null);
  const [venues, setVenues] = useState<AdminVenue[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<"publish" | "cancel" | null>(null);
  // Remounts the form after a save, so it shows what the server stored.
  const [formKey, setFormKey] = useState(0);

  useEffect(() => {
    Promise.all([adminGetEvent(id), adminListVenues()])
      .then(([d, v]) => {
        setData(d);
        setVenues(v);
      })
      .catch((err) => setError(message(err)));
  }, [id]);

  async function act(kind: "publish" | "cancel") {
    if (kind === "cancel" && !window.confirm("Cancel this event? Players will no longer see it as open.")) return;
    setError(null);
    setBusy(kind);
    try {
      setData(await (kind === "publish" ? adminPublishEvent(id) : adminCancelEvent(id)));
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(null);
    }
  }

  if (!data || !venues) {
    return error ? <ErrorNote message={error} /> : <p className="text-sm text-muted">Loading…</p>;
  }

  const e = data.event;
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2 rounded-[22px] border border-line bg-surface p-4">
        <div className="flex items-start justify-between gap-3">
          <h2 className="text-[20px] leading-tight font-semibold tracking-[-0.03em]">{e.title}</h2>
          <StatusChip status={e.status} />
        </div>
        <p className="text-sm text-muted">
          {formatWhen(e.startsAt, e.venue.timezone)} · {e.venue.name}
        </p>
        {e.status === "draft" && (
          <p className="text-xs text-muted">Players can&rsquo;t see a draft. Publish it when the details are right.</p>
        )}
        <div className="mt-2 flex flex-col gap-2">
          {e.status === "draft" && (
            <Button variant="accent" size="lg" fullWidth loading={busy === "publish"} onClick={() => void act("publish")}>
              Publish
            </Button>
          )}
          {(e.status === "draft" || e.status === "published") && (
            <Button variant="ghost" fullWidth className="text-danger" loading={busy === "cancel"} onClick={() => void act("cancel")}>
              Cancel event
            </Button>
          )}
        </div>
        <ErrorNote message={error} />
      </div>

      {e.status !== "draft" && (
        <Link
          href={`/admin/events/${e.id}/schedule`}
          className="flex items-center gap-3 rounded-[22px] border border-line bg-surface p-4"
        >
          <span className="flex flex-1 flex-col">
            <span className="text-[15px] font-semibold">Entries &amp; schedule</span>
            <span className="text-sm text-muted">
              {e.capacity - e.spotsLeft} of {e.capacity} taken · mark payments, preview and publish matches
            </span>
          </span>
          <ArrowRightIcon className="size-5 text-muted" />
        </Link>
      )}

      {e.status === "draft" ? (
        <EventForm
          key={formKey}
          venues={venues}
          initial={draftFromEvent(e)}
          submitLabel="Save changes"
          onSubmit={async (draft) => {
            setData(await adminUpdateEvent(id, draft));
            setFormKey((k) => k + 1);
          }}
        />
      ) : (
        <Summary e={e} />
      )}

      <Judges data={data} onChange={setData} />
    </div>
  );
}

export default function AdminEventPage() {
  const { id } = useParams<{ id: string }>();
  return (
    <AdminShell title="Event" back={{ href: "/admin", label: "Events" }}>
      {() => <EventAdmin id={id} />}
    </AdminShell>
  );
}
