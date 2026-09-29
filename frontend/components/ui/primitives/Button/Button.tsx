"use client";

import MuiButton from "@mui/material/Button";
import { testIdAttr } from "../../shared/dom";
import { toMuiSize } from "../../shared/muiAdapters";
import type { ButtonProps, ButtonVariant } from "./Button.types";

const VARIANT_MAP = {
  primary: { variant: "contained", color: "primary" },
  secondary: { variant: "outlined", color: "primary" },
  ghost: { variant: "text", color: "primary" },
  danger: { variant: "contained", color: "error" },
} as const satisfies Record<ButtonVariant, object>;

export function Button({
  children,
  variant = "primary",
  size = "md",
  type = "button",
  form,
  disabled,
  loading,
  fullWidth,
  startIcon,
  endIcon,
  href,
  onClick,
  ariaLabel,
  ref,
  className,
  id,
  testId,
}: ButtonProps) {
  return (
    <MuiButton
      {...VARIANT_MAP[variant]}
      size={toMuiSize(size)}
      type={href ? undefined : type}
      form={form}
      disabled={disabled}
      loading={loading}
      fullWidth={fullWidth}
      startIcon={startIcon}
      endIcon={endIcon}
      href={href}
      onClick={onClick}
      aria-label={ariaLabel}
      ref={ref}
      className={className}
      id={id}
      {...testIdAttr(testId)}
    >
      {children}
    </MuiButton>
  );
}
