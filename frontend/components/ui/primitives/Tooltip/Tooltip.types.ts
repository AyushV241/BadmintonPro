import type { ReactNode } from "react";
import type { BaseProps } from "../../shared/types";

export type TooltipPlacement =
  | "top"
  | "bottom"
  | "left"
  | "right"
  | "top-start"
  | "top-end"
  | "bottom-start"
  | "bottom-end";

export interface TooltipProps extends BaseProps {
  /** What the tooltip shows. */
  content: ReactNode;
  /** The trigger. Any element works, including a disabled Button. */
  children: ReactNode;
  /** Default `top`. */
  placement?: TooltipPlacement;
  /** Default `true`. */
  arrow?: boolean;
  /** Milliseconds before showing on hover. */
  delay?: number;
  /** Controlled visibility. Omit to show on hover/focus. */
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  /** Keeps the trigger but never shows the tooltip. */
  disabled?: boolean;
}
