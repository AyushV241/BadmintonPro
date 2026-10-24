"use client";

import { useEffect, useState, type FormEvent } from "react";
import { AdminShell, ErrorNote } from "@/components/admin/AdminShell";
import { FieldLabel } from "@/components/form/FieldLabel";
import { XIcon } from "@/components/icons";
import { Button, Input } from "@/components/ui";
import {
  ApiError,
  adminDeleteJudge,
  adminListJudges,
  adminRegisterJudge,
  type RegisteredJudge,
} from "@/lib/api";

const message = (err: unknown) => (err instanceof ApiError ? err.message : "Something went wrong.");

/** "+919800000002" shown as "+91 98000 00002"; other numbers as they are. */
function formatPhone(e164: string) {
  const m = /^\+91(\d{5})(\d{5})$/.exec(e164);
  return m ? `+91 ${m[1]} ${m[2]}` : e164;
}

function RegisterForm({ onAdded }: { onAdded: (j: RegisteredJudge) => void }) {
  const [name, setName] = useState("");
  const [phone, setPhone] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setBusy(true);
    try {
      onAdded(await adminRegisterJudge(name.trim(), phone.trim()));
      setName("");
      setPhone("");
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-3 rounded-[22px] border border-line bg-surface p-4">
      <h2 className="text-[17px] font-semibold tracking-[-0.02em]">Add a judge</h2>
      <div className="flex flex-col gap-1.5">
        <FieldLabel htmlFor="judge-name">Name</FieldLabel>
        <Input id="judge-name" value={name} onChange={setName} placeholder="Priya Nair" autoComplete="off" maxLength={100} />
      </div>
      <div className="flex flex-col gap-1.5">
        <FieldLabel htmlFor="judge-phone">Phone number</FieldLabel>
        <Input
          id="judge-phone"
          type="tel"
          inputMode="tel"
          value={phone}
          onChange={setPhone}
          placeholder="98765 43210"
          autoComplete="off"
          helperText="They log in as judge with this number. Without a country code it's read as Indian (+91)."
        />
      </div>
      <Button type="submit" variant="accent" loading={busy} disabled={!name.trim() || !phone.trim()}>
        Add judge
      </Button>
      <ErrorNote message={error} />
    </form>
  );
}

function JudgeList() {
  const [judges, setJudges] = useState<RegisteredJudge[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [removing, setRemoving] = useState<string | null>(null);

  useEffect(() => {
    adminListJudges()
      .then(setJudges)
      .catch((err) => setError(message(err)));
  }, []);

  async function remove(j: RegisteredJudge) {
    if (!window.confirm(`Remove ${j.name} as a judge? They come off every event still to come.`)) return;
    setError(null);
    setRemoving(j.userId);
    try {
      await adminDeleteJudge(j.userId);
      setJudges((list) => list?.filter((x) => x.userId !== j.userId) ?? null);
    } catch (err) {
      setError(message(err));
    } finally {
      setRemoving(null);
    }
  }

  return (
    <div className="flex flex-col gap-5">
      <RegisterForm
        onAdded={(j) =>
          setJudges((list) => [...(list ?? []), j].sort((a, b) => a.name.localeCompare(b.name)))
        }
      />

      <section className="flex flex-col gap-3" aria-labelledby="judge-list">
        <h2 id="judge-list" className="text-[17px] font-semibold tracking-[-0.02em]">
          Judges
        </h2>
        <p className="text-sm text-muted">
          They log in with &ldquo;Log in as judge&rdquo; on the sign-in page. Each is an ordinary account, so
          they can also play. Add them to an event from the event&apos;s page.
        </p>
        {judges === null && !error && <p className="text-sm text-muted">Loading…</p>}
        {judges?.length === 0 && <p className="text-sm text-muted">No judges yet.</p>}
        <ul className="flex flex-col gap-2">
          {judges?.map((j) => (
            <li key={j.userId} className="flex items-center gap-3 rounded-[20px] border border-line bg-surface p-3 pl-4">
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="truncate text-[15px] font-semibold">{j.name}</span>
                <span className="truncate text-xs text-muted">
                  {formatPhone(j.phone)}
                  {j.username && ` · @${j.username}`}
                </span>
              </span>
              <button
                type="button"
                aria-label={`Remove ${j.name}`}
                disabled={removing === j.userId}
                onClick={() => remove(j)}
                className="flex size-11 shrink-0 items-center justify-center rounded-full text-muted hover:bg-foreground/5 disabled:opacity-50"
              >
                <XIcon className="size-5" />
              </button>
            </li>
          ))}
        </ul>
        <ErrorNote message={error} />
      </section>
    </div>
  );
}

export default function AdminJudgesPage() {
  return (
    <AdminShell title="Judges" back={{ href: "/admin", label: "Events" }}>
      {() => <JudgeList />}
    </AdminShell>
  );
}
