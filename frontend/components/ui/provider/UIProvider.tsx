"use client";

// Mounted once in app/layout.tsx. Pages never import MUI's providers directly,
// so swapping libraries only changes this file.

import type { ReactNode } from "react";
import { AppRouterCacheProvider } from "@mui/material-nextjs/v16-appRouter";
import { ThemeProvider } from "@mui/material/styles";
import { muiTheme } from "../theme/muiTheme";

export function UIProvider({ children }: { children: ReactNode }) {
  return (
    // enableCssLayer puts MUI styles in `@layer mui`, which globals.css orders
    // before Tailwind's utilities, so a `className` always wins over MUI.
    <AppRouterCacheProvider options={{ enableCssLayer: true }}>
      <ThemeProvider theme={muiTheme}>{children}</ThemeProvider>
    </AppRouterCacheProvider>
  );
}
