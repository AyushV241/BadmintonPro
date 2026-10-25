"use client";

import { useEffect, useState, type ReactNode } from "react";
import { Avatar, avatarColors } from "@/components/app/Avatar";
import { Input } from "@/components/ui";
import { searchUsers, type PublicUser } from "@/lib/api";
import { initials } from "@/lib/initials";

const DEBOUNCE_MS = 250;

/** "@Rohan" → "rohan", as the server normalises it. */
function normalise(q: string) {
  return q.trim().replace(/^@/, "").toLowerCase();
}

/** text with the part matching q in bold, matched without letter case. */
function Highlighted({ text, q }: { text: string; q: string }) {
  const at = q ? text.toLowerCase().indexOf(q) : -1;
  if (at < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, at)}
      <strong className="font-bold text-foreground">{text.slice(at, at + q.length)}</strong>
      {text.slice(at + q.length)}
    </>
  );
}

type Results = { q: string; users: PublicUser[] } | { q: string; error: string };

/**
 * Search players by name or username (design doc, section 13). Waits for
 * typing to pause, cancels the request it replaces, and ignores late answers.
 * `empty` is shown while nothing is typed, e.g. suggested players.
 */
export function PlayerSearch({
  id,
  label = "Search by name or username",
  onPick,
  empty,
}: {
  id: string;
  label?: string;
  onPick: (u: PublicUser) => void;
  empty?: ReactNode;
}) {
  const [text, setText] = useState("");
  const [results, setResults] = useState<Results | null>(null);
  const q = normalise(text);

  useEffect(() => {
    if (!q) return;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      searchUsers(q, controller.signal)
        .then((users) => setResults({ q, users }))
        .catch((err: unknown) => {
          if (controller.signal.aborted) return;
          setResults({ q, error: err instanceof Error ? err.message : "Search failed." });
        });
    }, DEBOUNCE_MS);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [q]);

  // Only show answers for what's typed now; anything else is stale.
  const current = q && results?.q === q ? results : null;

  return (
    <div className="flex flex-col gap-2">
      <label htmlFor={id} className="text-[13px] font-semibold text-muted">
        {label}
      </label>
      <Input
        id={id}
        type="search"
        value={text}
        onChange={setText}
        placeholder="Name or @username"
        autoComplete="off"
        maxLength={41}
      />
      {q && !current && <p className="px-1 text-sm text-muted">Searching…</p>}
      {current && "error" in current && (
        <p role="alert" className="px-1 text-sm text-danger">
          {current.error}
        </p>
      )}
      {current && "users" in current && current.users.length === 0 && (
        <p className="px-1 text-sm text-muted">No player matches &ldquo;{text.trim()}&rdquo;</p>
      )}
      {current && "users" in current && current.users.length > 0 && (
        <ul className="overflow-hidden rounded-[20px] border border-line bg-surface">
          {current.users.map((u) => (
            <li key={u.id} className="border-b border-line last:border-b-0">
              <button
                type="button"
                onClick={() => onPick(u)}
                className="flex min-h-16 w-full items-center gap-3 px-3.5 py-2 text-left"
              >
                <Avatar text={initials(u.name, u.username)} color={avatarColors.blue} size={40} />
                <span className="flex min-w-0 flex-col">
                  <span className="truncate text-[15px] font-semibold">
                    <Highlighted text={u.name} q={q} />
                  </span>
                  <span className="truncate text-[13px] text-muted">
                    @<Highlighted text={u.username} q={q} />
                  </span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {!q && empty}
    </div>
  );
}
