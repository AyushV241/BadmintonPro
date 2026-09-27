"use client";

import InputAdornment from "@mui/material/InputAdornment";
import TextField from "@mui/material/TextField";
import { testIdAttr } from "../../shared/dom";
import { toMuiFieldSize } from "../../shared/muiAdapters";
import type { InputProps } from "./Input.types";

export function Input({
  value,
  defaultValue,
  onChange,
  onBlur,
  onFocus,
  label,
  placeholder,
  helperText,
  error,
  type = "text",
  name,
  required,
  disabled,
  readOnly,
  autoComplete,
  autoFocus,
  inputMode,
  maxLength,
  size = "md",
  fullWidth = true,
  startAdornment,
  endAdornment,
  ref,
  className,
  id,
  testId,
}: InputProps) {
  return (
    <TextField
      value={value}
      defaultValue={defaultValue}
      onChange={(event) => onChange?.(event.target.value)}
      onBlur={onBlur ? () => onBlur() : undefined}
      onFocus={onFocus ? () => onFocus() : undefined}
      label={label}
      placeholder={placeholder}
      helperText={typeof error === "string" ? error : helperText}
      error={Boolean(error)}
      type={type}
      name={name}
      required={required}
      disabled={disabled}
      autoComplete={autoComplete}
      autoFocus={autoFocus}
      size={toMuiFieldSize(size)}
      fullWidth={fullWidth}
      inputRef={ref}
      className={className}
      id={id}
      slotProps={{
        input: {
          readOnly,
          startAdornment: startAdornment ? (
            <InputAdornment position="start">{startAdornment}</InputAdornment>
          ) : undefined,
          endAdornment: endAdornment ? (
            <InputAdornment position="end">{endAdornment}</InputAdornment>
          ) : undefined,
        },
        htmlInput: { maxLength, inputMode, ...testIdAttr(testId) },
      }}
    />
  );
}
