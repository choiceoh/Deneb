// Code-session status → display. Shared by the Sidebar rail dots and the CodePane
// task-detail header so the color and label never drift apart.
//   working → 진행중 (초록)   passed → 멈춤/완료 (검정)   failed·missing → 문제 (빨강)

// Dot color for a session status (3 buckets — the rail glance + detail header).
export function codeStatusColor(status?: string): string {
  switch (status) {
    case "working":
      return "var(--online)";
    case "failed":
    case "missing":
      return "var(--danger)";
    default:
      return "var(--ink)";
  }
}

// Glance label for the rail dot tooltip — matches the 3 dot colors.
export function codeStatusGlance(status?: string): string {
  switch (status) {
    case "working":
      return "진행중";
    case "failed":
      return "문제";
    case "missing":
      return "문제 (워크트리 없음)";
    default:
      return "멈춤";
  }
}

// Precise label for the task-detail header (distinguishes passed from idle).
export function codeStatusLabel(status?: string): string {
  switch (status) {
    case "working":
      return "작업중";
    case "passed":
      return "검증 통과";
    case "failed":
      return "검증 실패";
    case "missing":
      return "워크트리 없음";
    default:
      return status || "—";
  }
}
