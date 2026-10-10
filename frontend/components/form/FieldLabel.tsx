/**
 * The brand's field label: small uppercase text above the field, optionally
 * with "OPTIONAL" on the right. Pass the field's id as `htmlFor`.
 */
export function FieldLabel({
  htmlFor,
  children,
  optional = false,
}: {
  htmlFor: string;
  children: string;
  optional?: boolean;
}) {
  return (
    <div className="flex items-center justify-between px-0.5 text-[11px] font-semibold tracking-[0.14em] uppercase">
      <label htmlFor={htmlFor} className="text-muted">
        {children}
      </label>
      {optional && <span className="text-muted/80">Optional</span>}
    </div>
  );
}
