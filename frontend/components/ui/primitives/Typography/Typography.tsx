"use client";

import MuiTypography from "@mui/material/Typography";
import { testIdAttr } from "../../shared/dom";
import type { TypographyElement, TypographyProps, TypographyVariant } from "./Typography.types";

const VARIANT_MAP = {
  display: "h1",
  h1: "h2",
  h2: "h3",
  h3: "h4",
  title: "h6",
  body: "body1",
  bodySmall: "body2",
  label: "subtitle2",
  caption: "caption",
  overline: "overline",
} as const;

const DEFAULT_ELEMENT: Record<TypographyVariant, TypographyElement> = {
  display: "h1",
  h1: "h1",
  h2: "h2",
  h3: "h3",
  title: "h4",
  body: "p",
  bodySmall: "p",
  label: "span",
  caption: "span",
  overline: "span",
};

const COLOR_MAP = {
  default: "text.primary",
  muted: "text.secondary",
  primary: "primary.main",
  success: "success.main",
  danger: "error.main",
  inherit: "inherit",
} as const;

const WEIGHT_MAP = { regular: 400, medium: 500, semibold: 600, bold: 700 } as const;

export function Typography({
  children,
  variant = "body",
  as,
  color = "default",
  align,
  weight,
  truncate,
  htmlFor,
  className,
  id,
  testId,
}: TypographyProps) {
  const lineClamp =
    typeof truncate === "number"
      ? {
          display: "-webkit-box",
          WebkitLineClamp: truncate,
          WebkitBoxOrient: "vertical" as const,
          overflow: "hidden",
        }
      : undefined;

  return (
    <MuiTypography
      variant={VARIANT_MAP[variant]}
      component={as ?? DEFAULT_ELEMENT[variant]}
      color={COLOR_MAP[color]}
      align={align}
      noWrap={truncate === true}
      htmlFor={htmlFor}
      className={className}
      id={id}
      sx={{ fontWeight: weight ? WEIGHT_MAP[weight] : undefined, ...lineClamp }}
      {...testIdAttr(testId)}
    >
      {children}
    </MuiTypography>
  );
}
