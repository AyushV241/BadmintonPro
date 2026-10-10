// Design tokens: the single source of truth for colour, radius and type.
// Library-agnostic — `muiTheme.ts` reads these today; a different library's
// theme adapter would read the same values tomorrow.
//
// Brand: lime accent on near-black "ink", warm off-white in light mode. Lime
// stays the same in both modes; in dark mode it becomes the primary colour.

const lime = "#e2fb6c";
const ink = "#151712";

export const tokens = {
  radius: {
    sm: 8,
    md: 12,
    lg: 16,
    xl: 22,
  },
  font: {
    sans: "var(--font-dm-sans), ui-sans-serif, system-ui, sans-serif",
    mono: "var(--font-geist-mono), ui-monospace, monospace",
  },
  brand: {
    lime,
    ink,
    /** Hover shade of lime. */
    limeHover: "#d3ee52",
  },
  color: {
    light: {
      primary: ink,
      primaryContrast: "#ffffff",
      secondary: "#6c6f62",
      success: "#3d7a12",
      warning: "#b45309",
      danger: "#c8321f",
      background: "#f5f5f0",
      surface: "#ffffff",
      border: "#e4e4da",
      text: ink,
      textMuted: "#6c6f62",
    },
    dark: {
      primary: lime,
      primaryContrast: ink,
      secondary: "#9a9d8f",
      success: "#c4ef5a",
      warning: "#fbbf24",
      danger: "#ff7a68",
      background: "#0e0f0c",
      surface: "#181a15",
      border: "#2b2e26",
      text: "#f3f4ee",
      textMuted: "#9a9d8f",
    },
  },
} as const;
