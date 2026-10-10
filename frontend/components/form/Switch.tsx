/** An on/off switch with its label and an optional description. */
export function Switch({
  checked,
  onChange,
  label,
  description,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: string;
  description?: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className="flex min-h-14 w-full items-center gap-4 rounded-2xl border border-line bg-surface px-4 py-3 text-left"
    >
      <span className="flex flex-1 flex-col gap-0.5">
        <span className="text-[15px] font-semibold tracking-[-0.01em]">{label}</span>
        {description && <span className="text-xs text-muted">{description}</span>}
      </span>
      <span
        aria-hidden="true"
        className={`flex h-7 w-12 shrink-0 items-center rounded-full p-0.5 transition-colors ${checked ? "bg-lime" : "bg-line"}`}
      >
        <span
          className={`size-6 rounded-full shadow-sm transition-transform ${checked ? "translate-x-5 bg-ink" : "bg-surface"}`}
        />
      </span>
    </button>
  );
}
