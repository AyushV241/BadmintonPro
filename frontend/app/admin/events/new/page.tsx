"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { AdminShell, ErrorNote } from "@/components/admin/AdminShell";
import { EventForm, newEventDraft } from "@/components/admin/EventForm";
import { ApiError, adminCreateEvent, adminListVenues, type AdminVenue } from "@/lib/api";

function NewEvent() {
  const router = useRouter();
  const [venues, setVenues] = useState<AdminVenue[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    adminListVenues()
      .then(setVenues)
      .catch((err) => setError(err instanceof ApiError ? err.message : "Couldn't load venues."));
  }, []);

  if (error) return <ErrorNote message={error} />;
  if (!venues) return <p className="text-sm text-muted">Loading…</p>;

  return (
    <EventForm
      venues={venues}
      initial={newEventDraft(venues)}
      submitLabel="Save as draft"
      onSubmit={async (draft) => {
        const { event } = await adminCreateEvent(draft);
        router.replace(`/admin/events/${event.id}`);
      }}
    />
  );
}

export default function NewEventPage() {
  return (
    <AdminShell title="New event" back={{ href: "/admin", label: "Events" }}>
      {() => <NewEvent />}
    </AdminShell>
  );
}
