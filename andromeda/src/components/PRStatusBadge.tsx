// What GitHub says about this conversation's work.
//
// The agent reports its own success, and that report can be wrong — this badge
// exists to show an INDEPENDENT verdict. It reads the pull request for the
// conversation's branch, so a red CI run is visible without leaving the chat.
import type { SessionPR } from "@/gateway";

// Each state gets a distinct mark AND distinct words: colour alone would leave
// the difference invisible to anyone who cannot separate red from green.
const FACES: Record<string, { mark: string; label: string; cls: string }> = {
  running: { mark: "◐", label: "검사 중", cls: "running" },
  failing: { mark: "✕", label: "검사 실패", cls: "failing" },
  passing: { mark: "✓", label: "검사 통과", cls: "passing" },
  merged: { mark: "●", label: "머지됨", cls: "merged" },
  closed: { mark: "○", label: "닫힘", cls: "closed" },
  // ★"물어보지 못함"은 "PR 없음"이 아니다. 둘을 합치면 CI가 깨져 있는데도
  // 추적되지 않는 작업처럼 보인다.
  unknown: { mark: "?", label: "상태 확인 불가", cls: "unknown" },
};

export function PRStatusBadge({ pr }: { pr: SessionPR | null }) {
  // "none" renders nothing: a conversation with no pull request has nothing to
  // report, and a permanent empty badge would be chrome, not information.
  if (!pr || pr.state === "none") return null;
  const face = FACES[pr.state];
  if (!face) return null;

  const counts =
    pr.state === "failing" && pr.failing
      ? ` ${pr.failing}건 실패`
      : pr.state === "running" && pr.pending
        ? ` ${pr.pending}건 남음`
        : "";
  const title = pr.number ? `#${pr.number} ${face.label}${counts}` : face.label;

  const body = (
    <>
      <span className="pr-badge-mark" aria-hidden="true">
        {face.mark}
      </span>
      {pr.number ? <span className="pr-badge-num">#{pr.number}</span> : null}
    </>
  );

  // Linked when we know where to go; plain text when we do not (an unknown
  // status has no pull request to open).
  if (!pr.url) {
    return (
      <span className={`pr-badge ${face.cls}`} role="img" aria-label={title} title={title}>
        {body}
      </span>
    );
  }
  return (
    <a
      className={`pr-badge ${face.cls}`}
      href={pr.url}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={title}
      title={title}
    >
      {body}
    </a>
  );
}
