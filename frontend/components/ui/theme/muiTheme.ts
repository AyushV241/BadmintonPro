// MUI adapter for `tokens.ts`. The only file that knows how MUI is themed.

import { createTheme } from "@mui/material/styles";
import { tokens } from "./tokens";

function palette(mode: "light" | "dark") {
  const c = tokens.color[mode];
  return {
    primary: { main: c.primary, contrastText: c.primaryContrast },
    secondary: { main: c.secondary },
    success: { main: c.success },
    warning: { main: c.warning },
    error: { main: c.danger },
    background: { default: c.background, paper: c.surface },
    divider: c.border,
    text: { primary: c.text, secondary: c.textMuted },
  };
}

export const muiTheme = createTheme({
  // Follow the OS setting, like the Tailwind `dark:` variants in the pages.
  cssVariables: { colorSchemeSelector: "media" },
  colorSchemes: {
    light: { palette: palette("light") },
    dark: { palette: palette("dark") },
  },
  shape: { borderRadius: tokens.radius.md },
  typography: {
    fontFamily: tokens.font.sans,
    h1: { fontSize: "2.25rem", fontWeight: 700, lineHeight: 1.2 },
    h2: { fontSize: "1.875rem", fontWeight: 600, lineHeight: 1.25 },
    h3: { fontSize: "1.5rem", fontWeight: 600, lineHeight: 1.3 },
    h4: { fontSize: "1.25rem", fontWeight: 600, lineHeight: 1.35 },
    h6: { fontSize: "1rem", fontWeight: 600, lineHeight: 1.4 },
    body1: { fontSize: "0.9375rem" },
    body2: { fontSize: "0.875rem" },
    subtitle2: { fontSize: "0.875rem", fontWeight: 500 },
    button: { textTransform: "none", fontWeight: 500 },
  },
  components: {
    MuiButton: { defaultProps: { disableElevation: true } },
    MuiButtonBase: { defaultProps: { disableRipple: false } },
    MuiPaper: { defaultProps: { elevation: 0 } },
    MuiTooltip: { defaultProps: { arrow: true } },
  },
});
