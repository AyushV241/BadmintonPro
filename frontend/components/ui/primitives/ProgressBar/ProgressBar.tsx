"use client";

import Box from "@mui/material/Box";
import LinearProgress from "@mui/material/LinearProgress";
import MuiTypography from "@mui/material/Typography";
import { testIdAttr } from "../../shared/dom";
import { toMuiColor } from "../../shared/muiAdapters";
import type { ProgressBarProps } from "./ProgressBar.types";

const HEIGHT = { sm: 4, md: 6, lg: 10 } as const;

export function ProgressBar({
  value,
  tone = "primary",
  size = "md",
  showValue,
  ariaLabel,
  className,
  id,
  testId,
}: ProgressBarProps) {
  const determinate = value !== undefined;
  const clamped = determinate ? Math.min(100, Math.max(0, value)) : undefined;

  return (
    <Box
      className={className}
      id={id}
      sx={{ display: "flex", alignItems: "center", gap: 1.5 }}
      {...testIdAttr(testId)}
    >
      <LinearProgress
        variant={determinate ? "determinate" : "indeterminate"}
        value={clamped}
        color={toMuiColor(tone)}
        aria-label={ariaLabel}
        sx={{ flex: 1, height: HEIGHT[size], borderRadius: 999 }}
      />
      {showValue && determinate && (
        <MuiTypography variant="body2" color="text.secondary" sx={{ minWidth: 36, textAlign: "right" }}>
          {Math.round(clamped!)}%
        </MuiTypography>
      )}
    </Box>
  );
}
