// Pane registry — the workstation's navigation is DERIVED from this list. Adding a
// pane means: build the pane component, add its resource to resources.ts (if
// data-backed), and add one entry here. Nav button, keyboard shortcut, and routing
// all follow automatically.
import type { ComponentType } from "react";
import type { View } from "@/types";
import { ProgressPane } from "./ProgressPane";
import { TodoPane } from "./TodoPane";
import { NotebookPane } from "./NotebookPane";
import { MailPane } from "./MailPane";
import { CalendarPane } from "./CalendarPane";
import { WikiPane } from "./WikiPane";
import { SearchPane } from "./SearchPane";
import { PeoplePane } from "./PeoplePane";
import { CronsPane } from "./CronsPane";
import { WorkfeedPane } from "./WorkfeedPane";
import { TodayPane } from "./TodayPane";
import { SettingsPane } from "./SettingsPane";

export interface PaneDef {
  key: View;
  label: string;
  shortcut: string; // ⌘/Ctrl + this key
  Component: ComponentType;
}

export const PANES: PaneDef[] = [
  { key: "today", label: "오늘", shortcut: "0", Component: TodayPane },
  // Digits 0–9 are taken; this dashboard-style overview gets a letter shortcut (⌘P).
  { key: "progress", label: "진행", shortcut: "p", Component: ProgressPane },
  { key: "todo", label: "할일", shortcut: "1", Component: TodoPane },
  { key: "notebook", label: "노트북", shortcut: "2", Component: NotebookPane },
  { key: "mail", label: "메일", shortcut: "3", Component: MailPane },
  { key: "calendar", label: "일정", shortcut: "4", Component: CalendarPane },
  { key: "wiki", label: "위키", shortcut: "5", Component: WikiPane },
  { key: "search", label: "검색", shortcut: "6", Component: SearchPane },
  { key: "people", label: "연락처", shortcut: "7", Component: PeoplePane },
  { key: "crons", label: "크론", shortcut: "8", Component: CronsPane },
  { key: "workfeed", label: "작업피드", shortcut: "9", Component: WorkfeedPane },
  // App settings live at the bottom of the rail; ⌘, mirrors the OS settings shortcut.
  { key: "settings", label: "설정", shortcut: ",", Component: SettingsPane },
];

export const paneLabel = (key: View): string => PANES.find((p) => p.key === key)?.label ?? key;
