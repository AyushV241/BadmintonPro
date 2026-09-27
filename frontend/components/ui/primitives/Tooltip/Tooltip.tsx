"use client";

import MuiTooltip from "@mui/material/Tooltip";
import { testIdAttr } from "../../shared/dom";
import type { TooltipProps } from "./Tooltip.types";

export function Tooltip({
  content,
  children,
  placement = "top",
  arrow = true,
  delay,
  open,
  onOpenChange,
  disabled,
  className,
  id,
  testId,
}: TooltipProps) {
  // MUI's Tooltip clones its child and attaches a ref plus event handlers.
  // Our primitives deliberately don't accept arbitrary props, so the trigger
  // is always wrapped in a span. This also makes tooltips work on disabled
  // buttons, which don't fire mouse events.
  const trigger = (
    <span className={className} style={{ display: "inline-flex" }} {...testIdAttr(testId)}>
      {children}
    </span>
  );

  if (disabled) return trigger;

  return (
    <MuiTooltip
      title={content}
      placement={placement}
      arrow={arrow}
      // The span has no accessible name of its own; describe the child instead
      // of labelling the span.
      describeChild
      enterDelay={delay}
      open={open}
      onOpen={onOpenChange ? () => onOpenChange(true) : undefined}
      onClose={onOpenChange ? () => onOpenChange(false) : undefined}
      id={id}
    >
      {trigger}
    </MuiTooltip>
  );
}
