"use client";

// MUI has no OTP input. One MUI OutlinedInput per character.
//
// The code is kept contiguous: focusing a box past the end jumps to the first
// empty one, so the value never has holes.

import Box from "@mui/material/Box";
import FormHelperText from "@mui/material/FormHelperText";
import OutlinedInput from "@mui/material/OutlinedInput";
import { useRef, useState, type ClipboardEvent, type KeyboardEvent } from "react";
import { testIdAttr } from "../../shared/dom";
import type { OTPInputProps } from "./OTPInput.types";

const ALLOWED = {
  numeric: /[^0-9]/g,
  alphanumeric: /[^a-zA-Z0-9]/g,
} as const;

export function OTPInput({
  length = 6,
  value,
  defaultValue = "",
  onChange,
  onComplete,
  type = "numeric",
  mask = false,
  error,
  disabled,
  autoFocus,
  ariaLabel = "Verification code",
  name,
  className,
  id,
  testId,
}: OTPInputProps) {
  const [uncontrolled, setUncontrolled] = useState(defaultValue);
  const code = (value ?? uncontrolled).slice(0, length);
  const inputs = useRef<(HTMLInputElement | null)[]>([]);
  // True while we move focus ourselves. Our focus moves happen before React
  // re-renders with the new code, so the "jump to first empty box" guard in
  // onFocus would see a stale code and pull focus back.
  const movingFocus = useRef(false);

  const focusBox = (i: number) => {
    const el = inputs.current[Math.max(0, Math.min(length - 1, i))];
    movingFocus.current = true;
    el?.focus();
    movingFocus.current = false;
    el?.select();
  };

  function commit(next: string) {
    if (value === undefined) setUncontrolled(next);
    onChange?.(next);
    if (next.length === length && next !== code) onComplete?.(next);
  }

  /** Writes `text` starting at box `i` (typing, paste or OS autofill). */
  function writeAt(i: number, text: string) {
    const clean = text.replace(ALLOWED[type], "");
    if (!clean) return;
    const start = Math.min(i, code.length);
    const next = (code.slice(0, start) + clean + code.slice(start + clean.length)).slice(0, length);
    commit(next);
    focusBox(start + clean.length);
  }

  function handleInput(i: number, raw: string) {
    // Android keyboards often delete without a usable Backspace keydown.
    if (raw === "") {
      if (code[i]) commit(code.slice(0, i) + code.slice(i + 1));
      return;
    }
    // Select-on-focus normally makes a keystroke replace the box's char. If the
    // selection was lost, the box holds old + new; keep the new one.
    if (raw.length === 2 && code[i]) {
      writeAt(i, raw[0] === code[i] ? raw[1] : raw[0]);
      return;
    }
    // Anything longer is a paste or OS one-time-code autofill.
    writeAt(i, raw);
  }

  function handleKeyDown(i: number, event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Backspace") {
      event.preventDefault();
      const target = code[i] ? i : i - 1;
      if (target < 0) return;
      commit(code.slice(0, target) + code.slice(target + 1));
      focusBox(target);
    } else if (event.key === "ArrowLeft") {
      event.preventDefault();
      focusBox(i - 1);
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      focusBox(Math.min(i + 1, code.length));
    }
  }

  function handlePaste(i: number, event: ClipboardEvent<HTMLInputElement>) {
    event.preventDefault();
    writeAt(i, event.clipboardData.getData("text"));
  }

  const message = typeof error === "string" ? error : undefined;

  return (
    <Box className={className} id={id} {...testIdAttr(testId)}>
      <Box role="group" aria-label={ariaLabel} sx={{ display: "flex", gap: 1 }}>
        {Array.from({ length }, (_, i) => (
          <OutlinedInput
            key={i}
            inputRef={(el: HTMLInputElement | null) => {
              inputs.current[i] = el;
            }}
            value={code[i] ?? ""}
            onChange={(event) => handleInput(i, event.target.value)}
            onKeyDown={(event) => handleKeyDown(i, event as KeyboardEvent<HTMLInputElement>)}
            onPaste={(event) => handlePaste(i, event as ClipboardEvent<HTMLInputElement>)}
            onFocus={(event) => {
              if (!movingFocus.current && i > code.length) focusBox(code.length);
              else (event.target as HTMLInputElement).select();
            }}
            error={Boolean(error)}
            disabled={disabled}
            autoFocus={autoFocus && i === 0}
            type={mask ? "password" : "text"}
            sx={{ width: 48, height: 56 }}
            slotProps={{
              input: {
                "aria-label": `${ariaLabel}, character ${i + 1} of ${length}`,
                inputMode: type === "numeric" ? "numeric" : "text",
                pattern: type === "numeric" ? "[0-9]*" : undefined,
                autoComplete: i === 0 ? "one-time-code" : "off",
                autoCapitalize: "off",
                spellCheck: false,
                style: { textAlign: "center", fontSize: "1.25rem", fontWeight: 600, padding: 0, height: "100%" },
              },
            }}
          />
        ))}
      </Box>
      {name && <input type="hidden" name={name} value={code} />}
      {message && <FormHelperText error>{message}</FormHelperText>}
    </Box>
  );
}
