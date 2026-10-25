import { Avatar, avatarColors } from "@/components/app/Avatar";
import type { GameScore, LiveScore, MatchSide, Side } from "@/lib/api";
import { initials } from "@/lib/initials";

/** "Rohan" in singles, "Rohan & Priya" in doubles; "You" for the viewer. */
export function sideName(side: MatchSide | undefined, meId?: string) {
  if (!side) return "TBC";
  return side.players.map((p) => (p.userId === meId ? "You" : p.name.split(" ")[0])).join(" & ");
}

/**
 * Whether a match that's over headlines the games won. One that ended before
 * any game was won (retired early, voided) headlines the points instead.
 */
export const headlinesGames = (score: LiveScore) => score.gamesA + score.gamesB > 0;

export function sideOf(sides: MatchSide[], s: Side) {
  return sides.find((x) => x.side === s);
}

/** One or two overlapping initials badges for a side. */
export function SideAvatars({ side, highlight, size = 48 }: { side: MatchSide | undefined; highlight?: boolean; size?: number }) {
  const players = side?.players ?? [];
  const pair = players.length > 1;
  return (
    <span className="flex">
      {players.map((p, i) => (
        <span key={p.userId} className={`rounded-full ${pair ? "ring-2 ring-ink" : ""} ${i ? "-ml-1" : ""}`}>
          <Avatar
            text={initials(p.name, p.username)}
            color={highlight ? avatarColors.lime : side?.side === "A" ? avatarColors.sand : avatarColors.blue}
            size={pair ? Math.round(size * 0.8) : size}
          />
        </span>
      ))}
    </span>
  );
}

/** "21–14 · 18–16 · –" chips; the game in play is lime. */
export function GameChips({ games, decided }: { games: GameScore[]; decided: boolean }) {
  const shown = [...games];
  while (shown.length < 3 && !decided) shown.push({ a: -1, b: -1 });
  const current = decided ? -1 : games.length - 1;
  return (
    <div className="flex justify-center gap-2">
      {shown.map((g, i) => (
        <span
          key={i}
          className={`rounded-[10px] px-3 py-1.5 text-[13px] font-semibold tabular-nums ${
            i === current ? "bg-lime text-ink" : g.a < 0 ? "bg-[#262920] text-[#a3a596]" : "bg-[#262920] text-[#f5f5f0]"
          }`}
        >
          {g.a < 0 ? "–" : `${g.a}–${g.b}`}
        </span>
      ))}
    </div>
  );
}

/** The red "LIVE" dot. */
export function LiveDot() {
  return <span aria-hidden="true" className="size-[7px] shrink-0 rounded-full bg-[#ff5a4e] shadow-[0_0_0_3px_rgba(255,90,78,0.25)]" />;
}
