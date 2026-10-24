"use client";

import { useRouter } from "next/navigation";
import { AdminShell } from "@/components/admin/AdminShell";
import { VenueForm } from "@/components/admin/VenueForm";

export default function NewVenuePage() {
  const router = useRouter();
  return (
    <AdminShell title="New venue" back={{ href: "/admin/venues", label: "Venues" }}>
      {() => <VenueForm onCreated={() => router.replace("/admin/venues")} />}
    </AdminShell>
  );
}
