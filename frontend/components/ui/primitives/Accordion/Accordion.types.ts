import type { ReactNode } from "react";
import type { BaseProps } from "../../shared/types";

export interface AccordionItem {
  /** Stable id; used in `expanded` / `onChange`. */
  id: string;
  title: ReactNode;
  subtitle?: ReactNode;
  content: ReactNode;
  disabled?: boolean;
}

export interface AccordionProps extends BaseProps {
  items: AccordionItem[];
  /** Allow more than one item open at once. Default `false`. */
  multiple?: boolean;
  /** Controlled list of open item ids. */
  expanded?: string[];
  defaultExpanded?: string[];
  onChange?: (expanded: string[]) => void;
  /** `outlined` draws each item as a bordered panel; `flush` separates with dividers. Default `outlined`. */
  variant?: "outlined" | "flush";
}
