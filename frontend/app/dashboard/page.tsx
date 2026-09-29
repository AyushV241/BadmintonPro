"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { logout, me, type User } from "@/lib/api";

export default function DashboardPage() {
  const router = useRouter();
  const [user, setUser] = useState<User | null>(null);

  useEffect(() => {
    // The session cookie is httpOnly, so the only way to know whether we are
    // signed in is to ask the server.
    me()
      .then((u) => {
        // The profile step comes first.
        if (!u.profileComplete) {
          router.replace("/setup-profile");
          return;
        }
        setUser(u);
      })
      .catch(() => router.replace("/login"));
  }, [router]);

  async function handleSignOut() {
    await logout().catch(() => {
      /* the cookie is cleared server-side; move on regardless */
    });
    router.replace("/login");
  }

  if (!user) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-slate-50 dark:bg-slate-950">
        <p className="text-sm text-slate-500 dark:text-slate-400">Loading…</p>
      </main>
    );
  }

  return (
    <main className="min-h-screen bg-slate-50 px-4 py-12 dark:bg-slate-950">
      <div className="mx-auto max-w-lg">
        <div className="rounded-xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
          <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-50">
            Welcome, {user.name}
          </h1>
          <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">@{user.username}</p>
          {user.email && <ContactLine value={user.email} verified={user.emailVerified} />}
          {user.phone && <ContactLine value={user.phone} verified={user.phoneVerified} />}

          <p className="mt-6 text-sm text-slate-600 dark:text-slate-300">
            You are signed in. Players, matches and rankings will live here.
          </p>

          <button
            onClick={handleSignOut}
            className="mt-6 rounded-lg border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700 transition-colors hover:bg-slate-100 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
          >
            Sign out
          </button>
        </div>
      </div>
    </main>
  );
}

function ContactLine({ value, verified }: { value: string; verified: boolean }) {
  return (
    <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
      {value}
      <span
        className={
          verified
            ? "ml-2 rounded bg-emerald-50 px-1.5 py-0.5 text-xs text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-400"
            : "ml-2 rounded bg-slate-100 px-1.5 py-0.5 text-xs text-slate-500 dark:bg-slate-800 dark:text-slate-400"
        }
      >
        {verified ? "verified" : "unverified"}
      </span>
    </p>
  );
}
