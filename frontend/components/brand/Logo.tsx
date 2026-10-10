/** BadmintonPro mark and wordmark. `onPhoto` for use over the dark hero image. */
export function Logo({ onPhoto = false }: { onPhoto?: boolean }) {
  return (
    <span className="flex items-center gap-2.5">
      <span
        aria-hidden="true"
        className="flex size-9 items-center justify-center rounded-[11px] bg-lime text-xl font-bold tracking-[-0.04em] text-ink"
      >
        B
      </span>
      <span
        className={`text-[19px] font-semibold tracking-[-0.03em] ${onPhoto ? "text-white" : "text-foreground"}`}
      >
        Badminton<span className={onPhoto ? "text-lime" : "text-lime-text"}>Pro</span>
      </span>
    </span>
  );
}
