"use client";

import TextField from "@mui/material/TextField";
import { useState } from "react";
import { testIdAttr } from "../../shared/dom";
import type { TextareaProps } from "./Textarea.types";

export function Textarea({
  value,
  defaultValue,
  onChange,
  onBlur,
  label,
  placeholder,
  helperText,
  error,
  name,
  required,
  disabled,
  readOnly,
  rows,
  minRows = 3,
  maxRows,
  maxLength,
  showCount,
  fullWidth = true,
  ref,
  className,
  id,
  testId,
}: TextareaProps) {
  // Only used for the counter when uncontrolled.
  const [uncontrolledLength, setUncontrolledLength] = useState(defaultValue?.length ?? 0);
  const length = value !== undefined ? value.length : uncontrolledLength;

  const message = typeof error === "string" ? error : helperText;
  const counter = showCount && maxLength ? `${length} / ${maxLength}` : undefined;

  return (
    <TextField
      multiline
      value={value}
      defaultValue={defaultValue}
      onChange={(event) => {
        setUncontrolledLength(event.target.value.length);
        onChange?.(event.target.value);
      }}
      onBlur={onBlur ? () => onBlur() : undefined}
      label={label}
      placeholder={placeholder}
      helperText={
        message || counter ? (
          <span style={{ display: "flex", justifyContent: "space-between", gap: 8 }}>
            <span>{message}</span>
            {counter && <span>{counter}</span>}
          </span>
        ) : undefined
      }
      error={Boolean(error)}
      name={name}
      required={required}
      disabled={disabled}
      rows={rows}
      minRows={rows ? undefined : minRows}
      maxRows={rows ? undefined : maxRows}
      fullWidth={fullWidth}
      inputRef={ref}
      className={className}
      id={id}
      slotProps={{
        input: { readOnly },
        htmlInput: { maxLength, ...testIdAttr(testId) },
      }}
    />
  );
}
