"use client";

import { useCallback, useRef, useState } from "react";

/** A message per field that has a problem; fields without one are fine. */
export type FieldErrors<K extends string> = Partial<Record<K, string>>;

const FOCUSABLE = "input:not([disabled]), select:not([disabled]), textarea:not([disabled]), button:not([disabled]), [tabindex]:not([tabindex='-1'])";

function prefersReducedMotion() {
  return typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

/**
 * Field errors for a form, and the jump to the first one.
 *
 * Give each field's wrapper `{...field("title")}`, so the hook knows where
 * it is on the page. When the save button is pressed, run the form's checks
 * and pass the result to `show`: it records the messages, scrolls the first
 * invalid field (in page order, whatever order the checks ran in) to the
 * middle of the screen, and moves focus into it. It returns whether there
 * were any errors, so the caller knows to stop.
 *
 * ```tsx
 * const { errors, field, show, clear } = useFormErrors<"title" | "courts">();
 * if (show(validate(draft))) return; // something's missing: jumped to it
 * <div {...field("title")}>…<FieldError id="title-error" message={errors.title} /></div>
 * ```
 *
 * Errors the server sends back work the same way: `show({ courts: message })`.
 */
export function useFormErrors<K extends string>() {
  const [errors, setErrors] = useState<FieldErrors<K>>({});
  const nodes = useRef(new Map<K, HTMLElement>());

  /** Props for a field's wrapper: a ref and a data attribute. */
  const field = useCallback(
    (key: K) => ({
      "data-field": key,
      ref: (el: HTMLElement | null) => {
        if (el) nodes.current.set(key, el);
        else nodes.current.delete(key);
      },
    }),
    [],
  );

  /** Scrolls to the first field in `next` (in page order) and focuses it. */
  const scrollToFirst = useCallback((next: FieldErrors<K>) => {
    const invalid = (Object.keys(next) as K[])
      .filter((k) => next[k])
      .map((k) => nodes.current.get(k))
      .filter((el): el is HTMLElement => !!el);
    if (invalid.length === 0) return;
    invalid.sort((a, b) => (a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1));
    const target = invalid[0];
    target.scrollIntoView({ behavior: prefersReducedMotion() ? "auto" : "smooth", block: "center" });
    // Focus without a second jump; screen readers then read the field and its error.
    const focusable = target.matches(FOCUSABLE) ? target : target.querySelector<HTMLElement>(FOCUSABLE);
    focusable?.focus({ preventScroll: true });
  }, []);

  /** Shows these errors (replacing the old ones) and jumps to the first. True if there were any. */
  const show = useCallback(
    (next: FieldErrors<K>) => {
      const present = Object.fromEntries(Object.entries(next).filter(([, m]) => m)) as FieldErrors<K>;
      setErrors(present);
      scrollToFirst(present);
      return Object.keys(present).length > 0;
    },
    [scrollToFirst],
  );

  /** Clears one field's error, e.g. once the player changes it. */
  const clear = useCallback((key: K) => {
    setErrors((current) => {
      if (!current[key]) return current;
      const rest = { ...current };
      delete rest[key];
      return rest;
    });
  }, []);

  return { errors, field, show, clear };
}
