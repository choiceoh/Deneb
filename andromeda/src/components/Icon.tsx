import type { ReactNode } from "react";

// Dependency-free line-icon set (Tabler-style outline, 24 viewBox, currentColor).
// Used by the nav rail and chrome. Inherits color from the parent; size via prop.
export type IconName =
  | "today"
  | "chat"
  | "attach"
  | "projects"
  | "progress"
  | "todo"
  | "notebook"
  | "mail"
  | "calendar"
  | "wiki"
  | "files"
  | "search"
  | "people"
  | "crons"
  | "fleet"
  | "workfeed"
  | "approvals"
  | "groupware"
  | "skills"
  | "rsi"
  | "observe"
  | "sitemap"
  | "code"
  | "send"
  | "bell"
  | "arrow-right"
  | "close"
  | "win-min"
  | "win-max"
  | "settings"
  | "check"
  | "copy"
  | "pencil"
  | "stop"
  | "refresh"
  | "history"
  | "plus"
  | "trash"
  | "chevron-down"
  | "chevron-up"
  | "expand-panel"
  | "collapse-panel"
  | "printer"
  | "warning"
  | "coin";

const PATHS: Record<IconName, ReactNode> = {
  today: (
    <>
      <circle cx="12" cy="12" r="3.6" />
      <path d="M12 2.5v2.4M12 19.1v2.4M2.5 12h2.4M19.1 12h2.4M5.1 5.1l1.7 1.7M17.2 17.2l1.7 1.7M18.9 5.1l-1.7 1.7M6.8 17.2l-1.7 1.7" />
    </>
  ),
  chat: (
    <path d="M5 5.5h14a1.5 1.5 0 0 1 1.5 1.5v8a1.5 1.5 0 0 1-1.5 1.5H9l-4 3v-3H5A1.5 1.5 0 0 1 3.5 15V7A1.5 1.5 0 0 1 5 5.5z" />
  ),
  attach: (
    <path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l8.57-8.57A4 4 0 0 1 18 8.84l-8.59 8.57a2 2 0 0 1-2.83-2.83l8.49-8.48" />
  ),
  projects: (
    <>
      <path d="M4.5 19.5V6.8A2.3 2.3 0 0 1 6.8 4.5h4.1l1.9 2.2h4.4a2.3 2.3 0 0 1 2.3 2.3v10.5" />
      <path d="M4.5 19.5h15M8 11h8M8 14.5h5.5" />
    </>
  ),
  todo: (
    <>
      <rect x="3.5" y="3.5" width="17" height="17" rx="4" />
      <path d="M8.5 12.2l2.4 2.4 4.6-4.9" />
    </>
  ),
  notebook: (
    <>
      <path d="M6 3.5h12a1 1 0 0 1 1 1v15a1 1 0 0 1-1 1H6A1.5 1.5 0 0 1 4.5 19V5A1.5 1.5 0 0 1 6 3.5z" />
      <path d="M9 3.5v17M12 8.5h4M12 12h4" />
    </>
  ),
  mail: (
    <>
      <rect x="3" y="5" width="18" height="14" rx="2.5" />
      <path d="M4 7.5l7.3 5a1.2 1.2 0 0 0 1.4 0l7.3-5" />
    </>
  ),
  calendar: (
    <>
      <rect x="3.5" y="5" width="17" height="15.5" rx="2.5" />
      <path d="M3.5 9.5h17M8 3.2v3.4M16 3.2v3.4" />
    </>
  ),
  wiki: (
    <>
      <path d="M5 4.5h9a2.5 2.5 0 0 1 2.5 2.5v12.5H7.5A2.5 2.5 0 0 1 5 19V4.5z" />
      <path d="M16.5 19.5H7.5A2.5 2.5 0 0 0 5 22M8.5 8.5h5M8.5 12h5" />
    </>
  ),
  files: (
    <>
      <path d="M3.5 7.5A2.5 2.5 0 0 1 6 5h4l2 2h6A2.5 2.5 0 0 1 20.5 9.5v7A2.5 2.5 0 0 1 18 19H6a2.5 2.5 0 0 1-2.5-2.5z" />
      <path d="M3.7 10h16.6" />
    </>
  ),
  search: (
    <>
      <circle cx="10.5" cy="10.5" r="6.5" />
      <path d="M15.5 15.5l4 4" />
    </>
  ),
  people: (
    <>
      <circle cx="9" cy="8" r="3.2" />
      <path d="M3.5 20c0-3.3 2.5-5.6 5.5-5.6s5.5 2.3 5.5 5.6" />
      <path d="M15.5 5.2a3.2 3.2 0 0 1 0 6M17 14.6c2.3.4 4 2.5 4 5.4" />
    </>
  ),
  crons: (
    <>
      <circle cx="12" cy="12" r="8.2" />
      <path d="M12 7.5V12l3 2" />
    </>
  ),
  fleet: (
    <>
      <circle cx="6.5" cy="7" r="2.4" />
      <circle cx="17.5" cy="7" r="2.4" />
      <circle cx="12" cy="17" r="2.4" />
      <path d="M8.7 8.7l2.1 5.9M15.3 8.7l-2.1 5.9M8.9 7h6.2" />
    </>
  ),
  workfeed: <path d="M3 12h3.5l2.2 6 4-13 2.4 9 1.8-2h4" />,
  // Clipboard with check — 전자결재 browse/act.
  approvals: (
    <>
      <path d="M9 4.5h6a1 1 0 0 1 1 1V6h1.5A1.5 1.5 0 0 1 19 7.5v12A1.5 1.5 0 0 1 17.5 21h-11A1.5 1.5 0 0 1 5 19.5v-12A1.5 1.5 0 0 1 6.5 6H8V5.5a1 1 0 0 1 1-1z" />
      <path d="M9 6h6M8.5 12.2l2.2 2.2 4.4-4.6" />
    </>
  ),
  // Warehouse shelves — Amaranth ERP read-only hub.
  groupware: (
    <>
      <path d="M4 7.5h16v2H4zM4 12h16v2H4zM4 16.5h16v2H4z" />
      <path d="M7 7.5V18.5M12 7.5V18.5M17 7.5V18.5" />
    </>
  ),
  // Sparkle — reads as a skill/capability the agent can wield.
  skills: (
    <>
      <path d="M12 3.5l1.7 4.8 4.8 1.7-4.8 1.7L12 16.5l-1.7-4.8L5.5 10l4.8-1.7z" />
      <path d="M18.5 16l.7 2 2 .7-2 .7-.7 2-.7-2-2-.7 2-.7z" />
    </>
  ),
  rsi: (
    <>
      <path d="M18.5 8A7.5 7.5 0 0 0 5 9.5M5.5 16A7.5 7.5 0 0 0 19 14.5" />
      <path d="M18.5 4.5V8H15M5.5 19.5V16H9" />
    </>
  ),
  // Eye — read-only observation of gateway behavior / logs.
  observe: (
    <>
      <path d="M2.5 12s3.5-6.5 9.5-6.5S21.5 12 21.5 12s-3.5 6.5-9.5 6.5S2.5 12 2.5 12z" />
      <circle cx="12" cy="12" r="2.8" />
    </>
  ),
  sitemap: (
    <>
      <path d="M12 21s6-5.7 6-10.5a6 6 0 1 0-12 0C6 15.3 12 21 12 21z" />
      <circle cx="12" cy="10.5" r="2.3" />
    </>
  ),
  code: <path d="M8.5 8l-4 4 4 4M15.5 8l4 4-4 4" />,
  progress: (
    <>
      <path d="M3.5 20.5h17" />
      <path d="M6.5 20.5V14M12 20.5V8M17.5 20.5V4.5" />
    </>
  ),
  send: <path d="M12 19V6M6.5 11.5L12 5.8l5.5 5.7" />,
  bell: (
    <>
      <path d="M6 9a6 6 0 1 1 12 0c0 5 2 6.5 2 6.5H4S6 14 6 9z" />
      <path d="M10 19a2 2 0 0 0 4 0" />
    </>
  ),
  "arrow-right": <path d="M5 12h13M13 6.5l5.5 5.5-5.5 5.5" />,
  close: <path d="M6 6l12 12M18 6L6 18" />,
  "win-min": <path d="M6 12h12" />,
  "win-max": <rect x="5.5" y="5.5" width="13" height="13" rx="2.5" />,
  // Sliders/controls glyph — distinct from the `today` sun, reads as "settings".
  settings: (
    <>
      <path d="M4 7h10M18 7h2M4 12h2M10 12h10M4 17h6M14 17h6" />
      <circle cx="16" cy="7" r="2" />
      <circle cx="8" cy="12" r="2" />
      <circle cx="12" cy="17" r="2" />
    </>
  ),
  check: <path d="M5 12.5l4.2 4.2L19 7" />,
  pencil: <path d="M12 20h8.5M16.7 3.8a2.1 2.1 0 0 1 3 3L7.5 19l-4 1 1-4Z" />,
  copy: (
    <>
      <rect x="8" y="8" width="11.5" height="12.5" rx="2.2" />
      <path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2" />
    </>
  ),
  stop: <rect x="6.5" y="6.5" width="11" height="11" rx="2.4" />,
  refresh: (
    <>
      <path d="M19.6 12a7.6 7.6 0 1 1-2.3-5.4" />
      <path d="M19.8 4.4V9h-4.6" />
    </>
  ),
  history: (
    <>
      <path d="M3.6 12a8.4 8.4 0 1 0 2.7-6.2" />
      <path d="M3.2 4.6v3.7h3.7" />
      <path d="M12 7.7V12l3 1.8" />
    </>
  ),
  plus: <path d="M12 5v14M5 12h14" />,
  trash: (
    <>
      <path d="M4.5 7h15M9.4 7V5.3A1.3 1.3 0 0 1 10.7 4h2.6a1.3 1.3 0 0 1 1.3 1.3V7" />
      <path d="M6.6 7l.8 12.1A1.6 1.6 0 0 0 9 20.6h6a1.6 1.6 0 0 0 1.6-1.5L17.4 7" />
    </>
  ),
  "chevron-down": <path d="M5 9l7 7 7-7" />,
  "chevron-up": <path d="M5 15l7-7 7 7" />,
  // Widen the chat panel leftward over the work area — double chevron «.
  "expand-panel": <path d="M13 6.5L7.5 12l5.5 5.5M19 6.5L13.5 12l5.5 5.5" />,
  // Shrink the chat panel back to the right — double chevron ».
  "collapse-panel": <path d="M11 6.5L16.5 12 11 17.5M5 6.5L10.5 12 5 17.5" />,
  printer: (
    <>
      <path d="M17 9V5a1 1 0 0 0-1-1H8a1 1 0 0 0-1 1v4" />
      <path d="M17 17h2a2 2 0 0 0 2-2v-4a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v4a2 2 0 0 0 2 2h2" />
      <rect x="7" y="13" width="10" height="8" rx="1.5" />
    </>
  ),
  warning: (
    <>
      <path d="M10.3 4.6a2 2 0 0 1 3.4 0l7.5 13a2 2 0 0 1-1.7 3H4.5a2 2 0 0 1-1.7-3z" />
      <path d="M12 9.6v4.2M12 17.2v.1" />
    </>
  ),
  coin: (
    <>
      <circle cx="12" cy="12" r="8.2" />
      <path d="M14.5 9.3a2.4 2.4 0 0 0-2-1h-.9a2.2 2.2 0 0 0 0 4.4h.8a2.2 2.2 0 0 1 0 4.4h-.9a2.4 2.4 0 0 1-2-1M12 6.7v1.6M12 15.7v1.6" />
    </>
  ),
};

export function Icon({
  name,
  size = 16,
  className,
  strokeWidth = 1.85,
}: {
  name: IconName;
  size?: number;
  className?: string;
  strokeWidth?: number;
}) {
  return (
    <svg
      className={className}
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {PATHS[name]}
    </svg>
  );
}
