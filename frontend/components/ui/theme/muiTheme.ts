// MUI adapter for `tokens.ts`. The only file that knows how MUI is themed.

import { createTheme } from "@mui/material/styles";
import { tokens } from "./tokens";

// "accent" is the brand lime, the same in light and dark mode.
declare module "@mui/material/styles" {
  interface Palette {
    accent: Palette["primary"];
  }
  interface PaletteOptions {
    accent?: PaletteOptions["primary"];
  }
}
declare module "@mui/material/Button" {
  interface ButtonPropsColorOverrides {
    accent: true;
  }
}

const accent = {
  main: tokens.brand.lime,
  light: tokens.brand.lime,
  dark: tokens.brand.limeHover,
  contrastText: tokens.brand.ink,
};

function palette(mode: "light" | "dark") {
  const c = tokens.color[mode];
  return {
    primary: { main: c.primary, contrastText: c.primaryContrast },
    accent,
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
    // Headings are semibold with tight tracking; bold is kept for display text.
    h1: { fontSize: "2.25rem", fontWeight: 700, lineHeight: 1.05, letterSpacing: "-0.05em" },
    h2: { fontSize: "1.875rem", fontWeight: 600, lineHeight: 1.1, letterSpacing: "-0.04em" },
    h3: { fontSize: "1.625rem", fontWeight: 600, lineHeight: 1.15, letterSpacing: "-0.04em" },
    h4: { fontSize: "1.25rem", fontWeight: 600, lineHeight: 1.25, letterSpacing: "-0.03em" },
    h6: { fontSize: "1rem", fontWeight: 600, lineHeight: 1.4 },
    body1: { fontSize: "0.9375rem" },
    body2: { fontSize: "0.875rem" },
    subtitle2: { fontSize: "0.875rem", fontWeight: 500 },
    button: { textTransform: "none", fontWeight: 600, letterSpacing: "-0.01em" },
  },
  components: {
    MuiButton: {
      defaultProps: { disableElevation: true },
      styleOverrides: {
        root: { borderRadius: tokens.radius.lg },
        sizeMedium: { minHeight: 44 },
        sizeLarge: { minHeight: 54, fontSize: "1rem" },
      },
    },
    MuiButtonBase: { defaultProps: { disableRipple: false } },
    MuiOutlinedInput: {
      styleOverrides: {
        root: ({ theme }) => ({
          borderRadius: tokens.radius.lg,
          backgroundColor: theme.vars?.palette.background.paper,
        }),
      },
    },
    MuiPaper: {
      defaultProps: { elevation: 0 },
      // MUI lightens raised surfaces in dark mode (sheets, menus); the brand
      // keeps them at the surface colour.
      styleOverrides: { root: { backgroundImage: "none" } },
    },
    MuiTooltip: { defaultProps: { arrow: true } },
  },
});
