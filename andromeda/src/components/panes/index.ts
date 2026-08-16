// Pane registry — the workstation's navigation is DERIVED from this list. Adding a
// pane means: build the pane component, add its resource to resources.ts (if
// data-backed), and add one entry here. Nav button, keyboard shortcut, and routing
// all follow automatically.
import type { ComponentType } from "react";
import type { View } from "@/types";
import { orderedItems } from "@/listReorder";
import { ProjectHomePane } from "./ProjectHomePane";
import { ProgressPane } from "./ProgressPane";
import { TodoPane } from "./TodoPane";
import { NotebookPane } from "./NotebookPane";
import { MailPane } from "./MailPane";
import { CalendarPane } from "./CalendarPane";
import { WikiPane } from "./WikiPane";
import { SearchPane } from "./SearchPane";
import { PeoplePane } from "./PeoplePane";
import { CronsPane } from "./CronsPane";
import { FleetPane } from "./FleetPane";
import { WorkfeedPane } from "./WorkfeedPane";
import { ApprovalsPane } from "./ApprovalsPane";
import { GroupwareERPPane } from "./GroupwareERPPane";
import { SkillsPane } from "./SkillsPane";
import { RsiPane } from "./RsiPane";
import { ObservePane } from "./ObservePane";
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
  // 채팅 — 비업무 전용 대화 탭. Workstation이 view==="chat"를 가로채 <ChatView/>(중앙 채팅 +
  // 우측 세션)를 그리므로, 여기 Component는 렌더되지 않는 placeholder다. 레일 버튼·⌘T 단축키만
  // 레지스트리에서 파생된다.
  { key: "chat", label: "채팅", shortcut: "t", Component: () => null },
  { key: "projects", label: "프로젝트", shortcut: "j", Component: ProjectHomePane },
  // Digits 0–9 are taken; this dashboard-style overview gets a letter shortcut (⌘P).
  { key: "progress", label: "진행", shortcut: "p", Component: ProgressPane },
  { key: "todo", label: "할일", shortcut: "1", Component: TodoPane },
  { key: "notebook", label: "노트북", shortcut: "2", Component: NotebookPane },
  { key: "mail", label: "메일", shortcut: "3", Component: MailPane },
  { key: "calendar", label: "일정", shortcut: "4", Component: CalendarPane },
  { key: "wiki", label: "위키", shortcut: "5", Component: WikiPane },
  // 파일 — Workstation이 항상 마운트해(열린 탭·미저장 편집 보존) 별도로 렌더하므로, chat 처럼
  // 여기 Component는 placeholder다. 레일 버튼·⌘F 단축키만 레지스트리에서 파생된다.
  { key: "files", label: "파일", shortcut: "f", Component: () => null },
  { key: "search", label: "검색", shortcut: "6", Component: SearchPane },
  { key: "people", label: "연락처", shortcut: "7", Component: PeoplePane },
  { key: "crons", label: "크론", shortcut: "8", Component: CronsPane },
  { key: "fleet", label: "플릿", shortcut: "l", Component: FleetPane },
  { key: "workfeed", label: "피드", shortcut: "9", Component: WorkfeedPane },
  { key: "approvals", label: "결재", shortcut: "a", Component: ApprovalsPane },
  { key: "groupware", label: "그룹웨어", shortcut: "u", Component: GroupwareERPPane },
  // ⌘K is the command palette (Workstation intercepts it), so skills sits on I.
  { key: "skills", label: "스킬", shortcut: "i", Component: SkillsPane },
  { key: "rsi", label: "자가개선", shortcut: "r", Component: RsiPane },
  { key: "observe", label: "관찰", shortcut: "o", Component: ObservePane },
  // App settings live at the bottom of the rail; ⌘, mirrors the OS settings shortcut.
  { key: "settings", label: "설정", shortcut: ",", Component: SettingsPane },
];

export const paneLabel = (key: View): string => PANES.find((p) => p.key === key)?.label ?? key;

// The non-settings pane keys in the user's saved rail order; any registry pane
// missing from the saved order is appended in registry order (new panes appear,
// removed ones drop). Settings is excluded — it's pinned to the bottom of the rail.
export function orderedViews(saved: View[]): View[] {
  const keys = PANES.filter((p) => p.key !== "settings").map((p) => p.key);
  return orderedItems(saved, keys);
}
