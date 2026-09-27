// Translations from our vocabulary to MUI's. Only implementation files
// (`<Name>.tsx`) use these; when a primitive moves off MUI, its imports of
// this file go with it.

import type { Size, Tone } from "./types";

export type MuiSize = "small" | "medium" | "large";
export type MuiColor = "primary" | "secondary" | "success" | "warning" | "error" | "inherit";

export function toMuiSize(size: Size): MuiSize {
  return size === "sm" ? "small" : size === "lg" ? "large" : "medium";
}

/** For MUI inputs that only support small/medium. */
export function toMuiFieldSize(size: Size): "small" | "medium" {
  return size === "sm" ? "small" : "medium";
}

export function toMuiColor(tone: Tone): MuiColor {
  switch (tone) {
    case "danger":
      return "error";
    case "neutral":
      return "secondary";
    default:
      return tone;
  }
}
