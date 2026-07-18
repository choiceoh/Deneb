// 컨텍스트 팔로우 모드의 실행부 — followMode가 켜졌을 때만 마운트된다 (엔티티
// 목록 fetch도 그때만). 데네브 답변이 완료되면 언급된 프로젝트/인물 위키를 옆
// 타일로 따라 연다. 렌더 출력 없음.
import { useEffect, useRef } from "react";
import { useCachedList } from "@/cachedList";
import { matchEntities } from "@/entityMatch";
import type { Person, ProjectDigest } from "@/types";
import { useWorkspace } from "@/workspaceContext";

export function ContextFollow({ turn }: { turn: { id: string; text: string } | null }) {
  const { connected, splitWiki } = useWorkspace();
  const projects = useCachedList<ProjectDigest>("progress", connected);
  const people = useCachedList<Person>("people", connected);
  const lastFollowedRef = useRef<string | null>(null);

  useEffect(() => {
    if (!turn?.text.trim()) return;
    const source = {
      projects: (projects.result?.data ?? []).map((d) => ({ name: d.project, path: d.path })),
      people: (people.result?.data ?? []).map((p) => ({ name: p.name, path: p.wikiPath })),
    };
    const hit = matchEntities(turn.text, source, 1)[0];
    // 같은 페이지 반복 이동은 소음 — 마지막으로 따라간 경로는 건너뛴다.
    if (!hit || hit.path === lastFollowedRef.current) return;
    lastFollowedRef.current = hit.path;
    splitWiki(hit.path);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [turn?.id, projects.result, people.result]);

  return null;
}
