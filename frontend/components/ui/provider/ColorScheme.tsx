"use client";

// Light and dark mode. The scheme lives on <html data-mui-color-scheme>, so
// MUI's colours and Tailwind's `dark:` variants switch together.

import InitColorSchemeScript from "@mui/material/InitColorSchemeScript";
import { useColorScheme } from "@mui/material/styles";

export type Appearance = "light" | "dark";

/**
 * Sets the scheme before the page paints, so a saved Light or Dark choice
 * never flashes the other one. Render it first thing in <body>.
 */
export function ColorSchemeScript() {
  return <InitColorSchemeScript attribute="data-mui-color-scheme" defaultMode="system" />;
}

const ATTRIBUTE = "data-mui-color-scheme";
const REVEAL_MS = 550;

/** Where the reveal starts, e.g. the centre of the button that was tapped. */
export type RevealOrigin = { x: number; y: number };

/**
 * Light or dark, remembered on this device. Until the player picks one, it's
 * whatever their phone uses. `appearance` is undefined until the browser has
 * said which it is (it's unknown on the server).
 *
 * `setAppearance` with an origin plays a circular reveal of the new theme
 * from that point, where the browser supports view transitions and the
 * player hasn't asked for reduced motion; otherwise it switches at once.
 */
export function useAppearance(): {
  appearance: Appearance | undefined;
  setAppearance: (a: Appearance, from?: RevealOrigin) => void;
} {
  const { mode, systemMode, setMode } = useColorScheme();
  const appearance = mode === "system" ? systemMode : mode;

  function setAppearance(next: Appearance, from?: RevealOrigin) {
    const root = document.documentElement;
    const apply = () => {
      // Set the attribute ourselves so the new theme is in the DOM inside the
      // transition's callback; MUI sets the same value and remembers it.
      root.setAttribute(ATTRIBUTE, next);
      setMode(next);
    };
    const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (!from || next === appearance || reduceMotion || !document.startViewTransition) {
      apply();
      return;
    }
    const transition = document.startViewTransition(apply);
    // Grow the new theme from `from` until it covers the farthest corner.
    const radius = Math.hypot(Math.max(from.x, innerWidth - from.x), Math.max(from.y, innerHeight - from.y));
    transition.ready
      .then(() =>
        root.animate(
          { clipPath: [`circle(0px at ${from.x}px ${from.y}px)`, `circle(${radius}px at ${from.x}px ${from.y}px)`] },
          { duration: REVEAL_MS, easing: "cubic-bezier(0.4, 0, 0.2, 1)", pseudoElement: "::view-transition-new(root)" },
        ),
      )
      .catch(() => {
        /* the switch itself already happened */
      });
  }

  return { appearance, setAppearance };
}
