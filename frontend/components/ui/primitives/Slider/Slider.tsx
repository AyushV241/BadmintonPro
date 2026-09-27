"use client";

import MuiSlider from "@mui/material/Slider";
import { testIdAttr } from "../../shared/dom";
import { toMuiFieldSize } from "../../shared/muiAdapters";
import type { SliderProps, SliderValue } from "./Slider.types";

const SHOW_VALUE_MAP = { auto: "auto", always: "on", never: "off" } as const;

export function Slider<T extends SliderValue = number>({
  value,
  defaultValue,
  onChange,
  onChangeEnd,
  min = 0,
  max = 100,
  step = 1,
  marks,
  showValue = "auto",
  formatValue,
  ariaLabel,
  disabled,
  size = "md",
  name,
  className,
  id,
  testId,
}: SliderProps<T>) {
  return (
    <MuiSlider
      value={value}
      defaultValue={defaultValue}
      onChange={(_, next) => onChange?.(next as T)}
      onChangeCommitted={(_, next) => onChangeEnd?.(next as T)}
      min={min}
      max={max}
      step={step}
      marks={marks}
      valueLabelDisplay={SHOW_VALUE_MAP[showValue]}
      valueLabelFormat={formatValue}
      getAriaValueText={formatValue}
      aria-label={ariaLabel}
      disabled={disabled}
      size={toMuiFieldSize(size)}
      name={name}
      className={className}
      id={id}
      {...testIdAttr(testId)}
    />
  );
}
