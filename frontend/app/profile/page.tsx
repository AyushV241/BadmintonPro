"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { Avatar, avatarColors } from "@/components/app/Avatar";
import { BottomNav } from "@/components/app/BottomNav";
import { Switch } from "@/components/form/Switch";
import { ArrowRightIcon, LogoutIcon, MailIcon, PhoneIcon, ShieldIcon } from "@/components/icons";
import { setAdminMode, useAdminMode } from "@/lib/adminMode";
import { logout, me, myStats, type PlayerStats, type User } from "@/lib/api";
import { initials } from "@/lib/initials";

function Eyebrow({ children }: { children: string }) {
  return <p className="text-[11px] font-semibold tracking-[0.14em] text-muted uppercase">{children}</p>;
}

function PlayerCard({ user, stats }: { user: User; stats: PlayerStats | null }) {
  const winRate = stats && stats.played > 0 ? `${Math.round((stats.won / stats.played) * 100)}%` : "–";
  return (
    <section
      aria-label="Player card"
      className="flex flex-col gap-4 rounded-3xl bg-ink bg-[image:radial-gradient(120%_80%_at_100%_0%,rgba(226,251,108,0.12)_0%,rgba(226,251,108,0)_55%)] p-[18px] text-[#f5f5f0] dark:border dark:border-[#30342a] dark:bg-[#1c1f18]"
    >
      <div className="flex items-center gap-3.5">
        <Avatar text={initials(user.name, user.username)} color={avatarColors.lime} size={64} />
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="truncate text-[21px] font-semibold tracking-[-0.035em]">{user.name}</span>
          <span className="truncate text-[13px] text-[#a3a596]">@{user.username}</span>
        </div>
      </div>
      <div className="flex flex-wrap gap-2">
        {(["singles", "doubles", "mixed"] as const).map((f, i) => (
          <span
            key={f}
            className={`flex items-baseline gap-1.5 rounded-full px-3 py-1.5 ${i === 0 ? "bg-lime text-ink" : "bg-white/10"}`}
          >
            <span className={`text-[10px] font-semibold tracking-[0.12em] uppercase ${i === 0 ? "" : "text-[#a3a596]"}`}>{f}</span>
            <span className="text-sm font-semibold">{stats?.ratings[f] ?? "–"}</span>
          </span>
        ))}
      </div>
      <div className="grid grid-cols-3 border-t border-white/10 pt-3.5">
        {[
          [stats ? String(stats.played) : "–", "Matches"],
          [stats ? String(stats.won) : "–", "Wins"],
          [winRate, "Win rate"],
        ].map(([value, label], i) => (
          <div key={label} className={`flex flex-col items-center gap-0.5 ${i ? "border-l border-white/10" : ""}`}>
            <span className="text-[22px] font-semibold tracking-[-0.035em]">{value}</span>
            <span className="text-xs text-[#a3a596]">{label}</span>
          </div>
        ))}
      </div>
    </section>
  );
}

function ContactRow({ icon, value, verified }: { icon: ReactNode; value: string; verified: boolean }) {
  return (
    <div className="flex min-h-14 items-center gap-3 border-b border-line px-4 last:border-b-0">
      <span className="text-muted">{icon}</span>
      <span className="min-w-0 flex-1 truncate text-[15px]">{value}</span>
      <span
        className={`shrink-0 rounded-full px-2.5 py-1 text-xs font-semibold ${verified ? "bg-lime text-ink" : "bg-line text-muted"}`}
      >
        {verified ? "Verified" : "Unverified"}
      </span>
    </div>
  );
}

function ProfileView({ user }: { user: User }) {
  const router = useRouter();
  const adminMode = useAdminMode();
  const [stats, setStats] = useState<PlayerStats | null>(null);

  useEffect(() => {
    myStats()
      .then(setStats)
      .catch(() => {
        /* the card shows dashes */
      });
  }, []);

  async function signOut() {
    await logout().catch(() => {
      /* the cookie is cleared server-side; move on regardless */
    });
    router.replace("/login");
  }

  return (
    <main className="min-h-dvh bg-background text-foreground">
      <div className="mx-auto flex w-full max-w-[480px] flex-col gap-3.5 px-5 pt-6 pb-36">
        <header className="flex flex-col gap-1.5 pb-1">
          <Eyebrow>Your player card</Eyebrow>
          <h1 className="text-[32px] leading-tight font-medium tracking-[-0.04em]">Profile</h1>
        </header>

        <PlayerCard user={user} stats={stats} />

        {(user.phone || user.email) && (
          <section className="flex flex-col gap-2">
            <Eyebrow>Contact</Eyebrow>
            <div className="overflow-hidden rounded-[20px] border border-line bg-surface">
              {user.phone && <ContactRow icon={<PhoneIcon className="size-[18px]" />} value={user.phone} verified={user.phoneVerified} />}
              {user.email && <ContactRow icon={<MailIcon className="size-[18px]" />} value={user.email} verified={user.emailVerified} />}
            </div>
            <p className="px-1 text-xs text-muted">
              You sign in with {user.signInMethod === "google" ? "Google" : "your phone number"}.
            </p>
          </section>
        )}

        <section className="flex flex-col gap-2">
          <Eyebrow>Settings</Eyebrow>
          {/* Open to everyone for now; later only people with the admin role see it. */}
          <Switch
            checked={adminMode}
            onChange={setAdminMode}
            label="Admin mode"
            description="Create venues and events, and assign judges."
          />
        </section>

        <nav aria-label="Account" className="overflow-hidden rounded-[20px] border border-line bg-surface">
          {adminMode && (
            <Link href="/admin" className="flex min-h-14 items-center gap-3 border-b border-line px-4 text-[15px] font-semibold">
              <ShieldIcon className="size-[19px]" />
              <span className="flex-1">Admin</span>
              <ArrowRightIcon className="size-[18px] text-muted" />
            </Link>
          )}
          <button
            type="button"
            onClick={() => void signOut()}
            className="flex min-h-14 w-full items-center gap-3 px-4 text-left text-[15px] font-semibold text-danger"
          >
            <LogoutIcon className="size-[19px]" />
            Sign out
          </button>
        </nav>
      </div>
      <BottomNav active="profile" />
    </main>
  );
}

/** The player's own page: their card, contact details and settings. */
export default function ProfilePage() {
  const router = useRouter();
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
  return <ProfileView user={user} />;
}
