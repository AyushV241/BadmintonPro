"use client";

import MenuItem from "@mui/material/MenuItem";
import TextField from "@mui/material/TextField";
import { testIdAttr } from "../../shared/dom";
import { toMuiFieldSize } from "../../shared/muiAdapters";
import type { SingleSelectProps } from "./SingleSelect.types";

export function SingleSelect<V extends string = string>({
  options,
  value,
  defaultValue,
  onChange,
  label,
  placeholder,
  helperText,
  error,
  name,
  required,
  disabled,
  size = "md",
  fullWidth = true,
  className,
  id,
  testId,
}: SingleSelectProps<V>) {
  const labelFor = (selected: unknown) =>
    options.find((option) => option.value === selected)?.label;

  return (
    <TextField
      select
      // MUI's Select uses "" for "nothing selected".
      value={value === undefined ? undefined : (value ?? "")}
      defaultValue={value === undefined ? (defaultValue ?? "") : undefined}
      onChange={(event) => onChange?.(event.target.value as V)}
      label={label}
      helperText={typeof error === "string" ? error : helperText}
      error={Boolean(error)}
      name={name}
      required={required}
      disabled={disabled}
      size={toMuiFieldSize(size)}
      fullWidth={fullWidth}
      className={className}
      id={id}
      slotProps={{
        inputLabel: placeholder ? { shrink: true } : undefined,
        select: placeholder
          ? {
              displayEmpty: true,
              renderValue: (selected) =>
                selected === "" ? (
                  <span style={{ opacity: 0.5 }}>{placeholder}</span>
                ) : (
                  labelFor(selected)
                ),
            }
          : undefined,
        htmlInput: testIdAttr(testId),
      }}
    >
      {options.map((option) => (
        <MenuItem key={option.value} value={option.value} disabled={option.disabled}>
          {option.label}
        </MenuItem>
      ))}
    </TextField>
  );
}
