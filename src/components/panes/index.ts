// Pane registry — the workstation's navigation is DERIVED from this list. Adding a
// Phase 2 pane (wiki, search, …) means: build the pane component, add its resource
// to resources.ts (if data-backed), and add one entry here. Nav button, keyboard
// shortcut, and routing all follow automatically.
import type { ComponentType } from "react";
import type { View } from "../../types";
import { TodoPane } from "./TodoPane";
import { DocPane } from "./DocPane";
import { MailPane } from "./MailPane";
import { CalendarPane } from "./CalendarPane";

export interface PaneDef {
  key: View;
  label: string;
  shortcut: string; // ⌘/Ctrl + this key
  Component: ComponentType;
}

export const PANES: PaneDef[] = [
  { key: "todo", label: "할일", shortcut: "1", Component: TodoPane },
  { key: "doc", label: "문서", shortcut: "2", Component: DocPane },
  { key: "mail", label: "메일", shortcut: "3", Component: MailPane },
  { key: "calendar", label: "일정", shortcut: "4", Component: CalendarPane },
];

export const paneLabel = (key: View): string => PANES.find((p) => p.key === key)?.label ?? key;
