"use client";

import MuiCheckbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";
import { testIdAttr } from "../../shared/dom";
import { toMuiFieldSize } from "../../shared/muiAdapters";
import type { CheckboxProps } from "./Checkbox.types";

export function Checkbox({
  checked,
  defaultChecked,
  indeterminate,
  onChange,
  label,
  ariaLabel,
  name,
  value,
  required,
  disabled,
  size = "md",
  className,
  id,
  testId,
}: CheckboxProps) {
  const control = (
    <MuiCheckbox
      checked={checked}
      defaultChecked={defaultChecked}
      indeterminate={indeterminate}
      onChange={(_, next) => onChange?.(next)}
      name={name}
      value={value}
      required={required}
      disabled={disabled}
      size={toMuiFieldSize(size)}
      id={id}
      className={label ? undefined : className}
      slotProps={{ input: { "aria-label": ariaLabel, ...testIdAttr(testId) } }}
    />
  );

  if (!label) return control;

  return (
    <FormControlLabel
      control={control}
      label={label}
      disabled={disabled}
      required={required}
      className={className}
    />
  );
}
