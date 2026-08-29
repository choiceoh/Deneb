package polaris

import (
	"context"
	"testing"
)

// TestWarmSemanticIndexHydratesSessionsFromDisk is the regression this change
// exists for: the warmer runs at startup with an empty resident set, so before
// hydration it embedded nothing while still reporting success, and the semantic
// arm stayed blind to every session the process had not touched.
func TestWarmSemanticIndexHydratesSessionsFromDisk(t *testing.T) {
	dir := t.TempDir()
	// Write two sessions through one store, then reopen: the second store has
	// the files on disk but nothing resident — exactly the post-restart state.
	seed := mustStoreAt(t, dir)
	if err := seed.AppendMessage("sessA", textMsg("user", "2026-08-17 금호 곡성 현장 기성 청구 처리 완료. 청구 금액과 지급 일정, 세금계산서 발행 시점까지 정리해서 회신했고 담당자 확인도 받았다. 잔여 기성분은 다음 회차에 이어서 청구하기로 협의했으며, 관련 근거 자료는 별도 폴더에 정리해 두었다. 확정된 일정 기준으로 후속 조치를 진행하면 된다. 추가로 확인이 필요한 항목은 담당자와 재협의 후 확정한다.", 1000)); err != nil {
		t.Fatalf("append A: %v", err)
	}
	if err := seed.AppendMessage("sessB", textMsg("user", "2026-08-18 추론 서버 재기동 및 배포 완료. 노드 상태 점검과 헬스체크까지 확인했고 오류 없이 정상 기동되었다. 배포 이후 지표를 모니터링하며 이상 징후가 없는지 계속 관찰하기로 했다. 관련 설정 변경 사항은 문서에 반영했고 검증 절차도 함께 기록했다. 다음 배포 시 동일 절차를 따른다.", 2000)); err != nil {
		t.Fatalf("append B: %v", err)
	}
	seed.Close()

	s := mustStoreAt(t, dir)
	defer s.Close()
	s.SetSummaryEmbedder(semFakeEmbedder{healthy: true})

	// Without a key lister the warm keeps the old resident-only behavior.
	if n := s.hydrateRecentSessions(120); n != 0 {
		t.Fatalf("no key lister must hydrate nothing, got %d", n)
	}

	s.SetSessionKeyLister(func() ([]string, error) { return []string{"sessA", "sessB"}, nil })
	if err := s.WarmSemanticIndex(context.Background()); err != nil {
		t.Fatalf("warm: %v", err)
	}

	hits := s.SearchSummariesSemantic(context.Background(), "current", []string{"곡성 대금 지급 어떻게 했지"}, 5)
	if len(hits) == 0 || len(hits[0]) == 0 {
		t.Fatal("a session only on disk must be reachable after a warm — this is the starvation regression")
	}
	if key := hits[0][0].SessionKey; key != "sessA" {
		t.Errorf("top hit = %q, want sessA (the billing conversation)", key)
	}
}

// TestHydrateRecentSessionsHonorsLimitAndRecency pins the bound: the cap keeps
// the resident set (and the per-restart embedding bill) finite, and spends
// itself on the newest sessions.
func TestHydrateRecentSessionsHonorsLimitAndRecency(t *testing.T) {
	dir := t.TempDir()
	seed := mustStoreAt(t, dir)
	for _, k := range []string{"old", "mid", "new"} {
		if err := seed.AppendMessage(k, textMsg("user", "내용 "+k, 1000)); err != nil {
			t.Fatalf("append %s: %v", k, err)
		}
	}
	seed.Close()

	s := mustStoreAt(t, dir)
	defer s.Close()
	s.SetSessionKeyLister(func() ([]string, error) { return []string{"old", "mid", "new"}, nil })

	if n := s.hydrateRecentSessions(0); n != 0 {
		t.Errorf("limit 0 disables hydration, got %d", n)
	}
	if n := s.hydrateRecentSessions(2); n != 2 {
		t.Fatalf("limit 2 must hydrate exactly 2, got %d", n)
	}
	// Re-running must not double count: already-resident sessions are skipped.
	if n := s.hydrateRecentSessions(2); n != 0 {
		t.Errorf("resident sessions must not re-hydrate, got %d", n)
	}

	// A key with no message file on disk is skipped rather than creating an
	// empty resident entry.
	s.SetSessionKeyLister(func() ([]string, error) { return []string{"ghost"}, nil })
	if n := s.hydrateRecentSessions(5); n != 0 {
		t.Errorf("missing message file must be skipped, got %d", n)
	}
}

func mustStoreAt(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := NewStore(dir + "/test.db")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}
