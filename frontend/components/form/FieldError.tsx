/**
 * A field's error message, under the field. Point the field's
 * aria-describedby at `id`. `className` overrides the colour, e.g. on a dark
 * card where the usual red is too dark to read.
 */
export function FieldError({ id, message, className = "text-danger" }: { id: string; message?: string; className?: string }) {
  if (!message) return null;
  return (
    <p id={id} role="alert" className={`text-sm font-medium ${className}`}>
      {message}
    </p>
  );
}
