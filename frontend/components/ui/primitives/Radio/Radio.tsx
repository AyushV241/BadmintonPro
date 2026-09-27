"use client";

import FormControl from "@mui/material/FormControl";
import FormControlLabel from "@mui/material/FormControlLabel";
import FormHelperText from "@mui/material/FormHelperText";
import FormLabel from "@mui/material/FormLabel";
import MuiRadio from "@mui/material/Radio";
import RadioGroup from "@mui/material/RadioGroup";
import { useId } from "react";
import { testIdAttr } from "../../shared/dom";
import { toMuiFieldSize } from "../../shared/muiAdapters";
import type { RadioProps } from "./Radio.types";

export function Radio<V extends string = string>({
  options,
  value,
  defaultValue,
  onChange,
  label,
  ariaLabel,
  helperText,
  error,
  name,
  required,
  disabled,
  direction = "vertical",
  size = "md",
  className,
  id,
  testId,
}: RadioProps<V>) {
  const labelId = useId();
  const message = typeof error === "string" ? error : helperText;

  return (
    <FormControl
      component="fieldset"
      error={Boolean(error)}
      required={required}
      disabled={disabled}
      className={className}
      id={id}
      {...testIdAttr(testId)}
    >
      {label && (
        <FormLabel component="legend" id={labelId}>
          {label}
        </FormLabel>
      )}
      <RadioGroup
        row={direction === "horizontal"}
        name={name}
        // `null` means "nothing selected" while staying controlled.
        value={value === undefined ? undefined : (value ?? "")}
        defaultValue={defaultValue}
        onChange={(_, next) => onChange?.(next as V)}
        aria-labelledby={label ? labelId : undefined}
        aria-label={label ? undefined : ariaLabel}
      >
        {options.map((option) => (
          <FormControlLabel
            key={option.value}
            value={option.value}
            label={option.label}
            disabled={option.disabled}
            control={<MuiRadio size={toMuiFieldSize(size)} />}
          />
        ))}
      </RadioGroup>
      {message && <FormHelperText>{message}</FormHelperText>}
    </FormControl>
  );
}
