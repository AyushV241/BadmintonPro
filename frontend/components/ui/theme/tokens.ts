// Design tokens: the single source of truth for colour, radius and type.
// Library-agnostic — `muiTheme.ts` reads these today; a different library's
// theme adapter would read the same values tomorrow.
//
// Values match the Tailwind slate palette the existing pages use.

export const tokens = {
  radius: {
    sm: 6,
    md: 8,
    lg: 12,
    xl: 16,
  },
  font: {
    sans: "var(--font-geist-sans), Arial, Helvetica, sans-serif",
    mono: "var(--font-geist-mono), ui-monospace, monospace",
  },
  color: {
    light: {
      primary: "#0f172a", // slate-900
      primaryContrast: "#ffffff",
      secondary: "#475569", // slate-600
      success: "#15803d", // green-700
      warning: "#b45309", // amber-700
      danger: "#b91c1c", // red-700
      background: "#f8fafc", // slate-50
      surface: "#ffffff",
      border: "#e2e8f0", // slate-200
      text: "#0f172a",
      textMuted: "#64748b", // slate-500
    },
    dark: {
      primary: "#f1f5f9", // slate-100
      primaryContrast: "#0f172a",
      secondary: "#94a3b8", // slate-400
      success: "#4ade80", // green-400
      warning: "#fbbf24", // amber-400
      danger: "#f87171", // red-400
      background: "#020617", // slate-950
      surface: "#0f172a", // slate-900
      border: "#1e293b", // slate-800
      text: "#f8fafc",
      textMuted: "#94a3b8",
    },
  },
} as const;
