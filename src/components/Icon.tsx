import type { ReactNode } from "react";

// Dependency-free line-icon set (Tabler-style outline, 24 viewBox, currentColor).
// Used by the nav rail and chrome. Inherits color from the parent; size via prop.
export type IconName =
  | "today"
  | "progress"
  | "todo"
  | "doc"
  | "mail"
  | "calendar"
  | "wiki"
  | "search"
  | "people"
  | "crons"
  | "workfeed"
  | "send"
  | "bell"
  | "arrow-right"
  | "close";

const PATHS: Record<IconName, ReactNode> = {
  today: (
    <>
      <circle cx="12" cy="12" r="3.6" />
      <path d="M12 2.5v2.4M12 19.1v2.4M2.5 12h2.4M19.1 12h2.4M5.1 5.1l1.7 1.7M17.2 17.2l1.7 1.7M18.9 5.1l-1.7 1.7M6.8 17.2l-1.7 1.7" />
    </>
  ),
  todo: (
    <>
      <rect x="3.5" y="3.5" width="17" height="17" rx="4" />
      <path d="M8.5 12.2l2.4 2.4 4.6-4.9" />
    </>
  ),
  doc: (
    <>
      <path d="M6 3.5h7l5 5v12a1.5 1.5 0 0 1-1.5 1.5h-10A1.5 1.5 0 0 1 5 20.5v-15A1.5 1.5 0 0 1 6.5 3.5z" />
      <path d="M13 3.5V8a1 1 0 0 0 1 1h4M8.5 13h7M8.5 16.5h5" />
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
  workfeed: <path d="M3 12h3.5l2.2 6 4-13 2.4 9 1.8-2h4" />,
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
