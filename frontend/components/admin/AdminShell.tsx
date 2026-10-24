"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { Button } from "@/components/ui";
import { ChevronLeftIcon } from "@/components/icons";
import { setAdminMode, useAdminMode } from "@/lib/adminMode";
import { me, type EventStatus, type User } from "@/lib/api";

/**
 * Frame for every admin page: signed-in check, admin-mode check, and a header
 * with a back link. The server enforces access on its own; this only decides
 * what to show.
 */
export function AdminShell({
  title,
  back = { href: "/dashboard", label: "Exit admin" },
  actions,
  children,
}: {
  title: string;
  back?: { href: string; label: string };
  actions?: ReactNode;
  children: (user: User) => ReactNode;
}) {
  const router = useRouter();
  const adminMode = useAdminMode();
  const [user, setUser] = useState<User | null>(null);

  useEffect(() => {
    me()
      .then((u) => {
        if (!u.profileComplete) {
          router.replace("/setup-profile");
          return;
        }
        setUser(u);
      })
      .catch(() => router.replace("/login"));
  }, [router]);

  if (!user) {
    return (
      <main className="flex min-h-dvh items-center justify-center bg-background">
        <p className="text-sm text-muted">Loading…</p>
      </main>
    );
  }

  return (
    <main className="min-h-dvh bg-background text-foreground">
      <div className="mx-auto flex w-full max-w-[480px] flex-col gap-6 px-5 pt-4 pb-16">
        <header className="flex flex-col gap-3">
          <Link
            href={back.href}
            className="-ml-2 flex min-h-11 items-center gap-1 self-start pr-2 text-sm font-semibold text-muted"
          >
            <ChevronLeftIcon className="size-5" />
            {back.label}
          </Link>
          <div className="flex items-end justify-between gap-3">
            <div className="flex flex-col gap-1">
              <p className="flex items-center gap-2 text-[11px] font-semibold tracking-[0.14em] text-muted uppercase">
                <span className="size-[7px] rounded-full bg-lime shadow-[0_0_0_3px_rgba(226,251,108,0.3)]" />
                Admin mode
              </p>
              <h1 className="text-[30px] leading-tight font-semibold tracking-[-0.04em]">{title}</h1>
            </div>
            {adminMode && actions}
          </div>
        </header>

        {adminMode ? (
          children(user)
        ) : (
          <div className="flex flex-col gap-3 rounded-[22px] border border-line bg-surface p-5">
            <p className="text-[15px] font-semibold">Admin mode is off</p>
            <p className="text-sm text-muted">
              Turn it on to create venues and events. You can switch it off any time from your Profile.
            </p>
            <Button variant="accent" size="lg" fullWidth onClick={() => setAdminMode(true)}>
              Turn on admin mode
            </Button>
          </div>
        )}
      </div>
    </main>
  );
}

const statusStyles: Record<EventStatus, string> = {
  draft: "bg-line text-muted",
  published: "bg-lime text-ink",
  cancelled: "bg-danger/15 text-danger",
  completed: "bg-foreground/10 text-foreground",
};

export function StatusChip({ status }: { status: EventStatus }) {
  return (
    <span
      className={`shrink-0 rounded-full px-2.5 py-1 text-[11px] font-semibold tracking-[0.08em] uppercase ${statusStyles[status]}`}
    >
      {status}
    </span>
  );
}

/**
 * Pinned to the bottom of the screen, for a page's main action. Leaves room
 * below the page's content so nothing hides behind it.
 */
export function StickyFooter({ children }: { children: ReactNode }) {
  return (
    <>
      <div aria-hidden="true" className="h-20" />
      <div className="fixed bottom-0 left-1/2 z-10 w-full max-w-[480px] -translate-x-1/2 border-t border-line bg-background/95 px-5 pt-3 pb-[max(20px,env(safe-area-inset-bottom))] backdrop-blur">
        {children}
      </div>
    </>
  );
}

/** Puts the message from a failed request in front of the admin. */
export function ErrorNote({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <p role="alert" className="rounded-xl bg-danger/10 px-3 py-2 text-sm text-danger">
      {message}
    </p>
  );
}
