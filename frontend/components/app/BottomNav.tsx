import Link from "next/link";
import type { ComponentType, SVGProps } from "react";
import { HomeIcon, MatchesIcon, SparkleIcon, TrophyIcon, UserIcon } from "@/components/icons";

export type NavTab = "home" | "matches" | "rank" | "profile";

type Tab = {
  key: NavTab;
  label: string;
  Icon: ComponentType<SVGProps<SVGSVGElement>>;
  /** Unset while the screen isn't built: the tab shows as disabled. */
  href?: string;
};

const TABS: Tab[] = [
  { key: "home", label: "Home", Icon: HomeIcon, href: "/dashboard" },
  { key: "matches", label: "Matches", Icon: MatchesIcon },
  { key: "rank", label: "Rank", Icon: TrophyIcon },
  { key: "profile", label: "Profile", Icon: UserIcon },
];

const tabClass =
  "flex min-h-14 flex-col items-center justify-end gap-[3px] text-[11px] tracking-[-0.01em]";

function TabItem({ tab, active, onClick }: { tab: Tab; active: boolean; onClick?: () => void }) {
  const content = (
    <>
      <tab.Icon className="size-[22px]" strokeWidth={active ? 2.2 : 1.8} />
      <span>{tab.label}</span>
      <span aria-hidden="true" className={`size-[5px] rounded-full ${active ? "bg-lime-text" : ""}`} />
    </>
  );
  const state = active ? "font-semibold text-foreground" : "font-medium text-muted";

  if (onClick) {
    return (
      <button type="button" onClick={onClick} className={`${tabClass} ${state}`}>
        {content}
      </button>
    );
  }
  if (tab.href) {
    return (
      <Link href={tab.href} aria-current={active ? "page" : undefined} className={`${tabClass} ${state}`}>
        {content}
      </Link>
    );
  }
  return (
    <span aria-disabled="true" title="Coming soon" className={`${tabClass} ${state} opacity-40`}>
      {content}
    </span>
  );
}

/**
 * The app's bottom tab bar: Home, Matches, the raised lime Match button, Rank,
 * Profile. Fixed to the bottom of the phone-width column.
 */
export function BottomNav({ active, onProfile }: { active: NavTab; onProfile?: () => void }) {
  const tab = (key: NavTab) => {
    const t = TABS.find((x) => x.key === key)!;
    return <TabItem tab={t} active={active === key} onClick={key === "profile" ? onProfile : undefined} />;
  };

  return (
    <nav
      aria-label="Main"
      className="fixed bottom-0 left-1/2 z-10 grid w-full max-w-[480px] -translate-x-1/2 grid-cols-5 items-end border-t border-line bg-surface px-2 pt-1.5 pb-[max(22px,env(safe-area-inset-bottom))]"
    >
      {tab("home")}
      {tab("matches")}
      {/* Find a match: not built yet. */}
      <span
        aria-disabled="true"
        title="Coming soon"
        className="flex flex-col items-center gap-[3px] text-[11px] font-semibold text-foreground"
      >
        <span className="-mt-7 flex size-14 items-center justify-center rounded-[18px] bg-lime text-ink shadow-[0_8px_20px_rgba(143,174,18,0.35)] ring-[5px] ring-surface">
          <SparkleIcon className="size-6" />
        </span>
        <span>Match</span>
        <span aria-hidden="true" className="size-[5px]" />
      </span>
      {tab("rank")}
      {tab("profile")}
    </nav>
  );
}
