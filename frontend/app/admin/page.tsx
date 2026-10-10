"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { AdminShell, ErrorNote, StatusChip } from "@/components/admin/AdminShell";
import { PlusIcon } from "@/components/icons";
import { Button } from "@/components/ui";
import { ApiError, adminListEvents, type BadmintonEvent, type EventStatus } from "@/lib/api";
import { formatWhen, rupees } from "@/lib/eventTime";

const FILTERS: { value: EventStatus | "all"; label: string }[] = [
  { value: "all", label: "All" },
  { value: "draft", label: "Drafts" },
  { value: "published", label: "Live" },
  { value: "cancelled", label: "Cancelled" },
];

function EventList() {
  const [filter, setFilter] = useState<EventStatus | "all">("all");
  const [events, setEvents] = useState<BadmintonEvent[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    adminListEvents(filter === "all" ? undefined : filter)
      .then((list) => active && setEvents(list))
      .catch((err) => active && setError(err instanceof ApiError ? err.message : "Couldn't load events."));
    return () => {
      active = false;
    };
  }, [filter]);

  return (
    <div className="flex flex-col gap-4">
      <div role="tablist" aria-label="Filter events" className="grid grid-cols-4 gap-1 rounded-2xl bg-line/60 p-1">
        {FILTERS.map((f) => (
          <button
            key={f.value}
            type="button"
            role="tab"
            aria-selected={filter === f.value}
            onClick={() => {
              setEvents(null);
              setFilter(f.value);
            }}
            className={`min-h-11 rounded-xl text-sm font-semibold ${
              filter === f.value ? "bg-surface text-foreground shadow-sm" : "text-muted"
            }`}
          >
            {f.label}
          </button>
        ))}
      </div>

      <ErrorNote message={error} />
      {events === null && !error && <p className="text-sm text-muted">Loading…</p>}
      {events?.length === 0 && (
        <p className="rounded-[22px] border border-line bg-surface p-5 text-sm text-muted">
          No events here yet. Create one with New event.
        </p>
      )}

      <ul className="flex flex-col gap-3">
        {events?.map((e) => (
          <li key={e.id}>
            <Link
              href={`/admin/events/${e.id}`}
              className="flex flex-col gap-2 rounded-[20px] border border-line bg-surface p-4"
            >
              <div className="flex items-start justify-between gap-3">
                <span className="text-[15px] font-semibold tracking-[-0.01em]">{e.title}</span>
                <StatusChip status={e.status} />
              </div>
              <span className="text-sm text-muted">
                {formatWhen(e.startsAt, e.venue.timezone)} · {e.venue.name}
              </span>
              <span className="text-xs text-muted">
                {e.formats.join(", ")} · {e.capacity} players · {e.maxMatches} matches · {rupees(e.feePaise)}
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}

export default function AdminHomePage() {
  return (
    <AdminShell
      title="Events"
      actions={
        <div className="flex gap-2">
          <Button href="/admin/venues" variant="secondary" size="sm">
            Venues
          </Button>
          <Button href="/admin/events/new" variant="accent" size="sm" startIcon={<PlusIcon />}>
            New event
          </Button>
        </div>
      }
    >
      {() => <EventList />}
    </AdminShell>
  );
}
