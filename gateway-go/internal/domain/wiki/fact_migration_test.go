package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/memory"
)

func TestLegacyFactMigrationCanonicalLabelsShareLiveIdentity(t *testing.T) {
	tests := []struct {
		name     string
		legacy   string
		live     string
		wantKey  string
		wantKind FactKind
	}{
		{
			name:     "name",
			legacy:   "**이름**: 민수",
			live:     "앞으로 나를 영희라고 불러줘",
			wantKey:  "identity.address",
			wantKind: FactKindIdentity,
		},
		{
			name:     "form of address",
			legacy:   "**호칭**: 민수님",
			live:     "앞으로 호칭은 영희님이라고 불러줘",
			wantKey:  "identity.address",
			wantKind: FactKindIdentity,
		},
		{
			name:     "language",
			legacy:   "**언어**: 영어",
			live:     "앞으로 한국어로 답변해줘",
			wantKey:  "communication.language",
			wantKind: FactKindPreference,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, _, _ := newFactTestStore(t)
			workspace := t.TempDir()
			legacyFile := "# MEMORY\n\n- " + tt.legacy + "\n"
			if err := os.WriteFile(filepath.Join(workspace, "MEMORY.md"), []byte(legacyFile), 0o600); err != nil {
				t.Fatal(err)
			}

			if fallback := memory.FactKeyFromText(tt.legacy); fallback == tt.wantKey {
				t.Fatalf("fixture no longer exercises legacy label normalization: fallback=%q", fallback)
			}
			if imported, err := store.ImportLegacyFactFiles(workspace); err != nil || imported != 1 {
				t.Fatalf("imported=%d err=%v", imported, err)
			}
			legacyHistory := store.FactHistory("self", tt.wantKey)
			if len(legacyHistory) != 1 || legacyHistory[0].Value != tt.legacy ||
				legacyHistory[0].Kind != tt.wantKind || legacyHistory[0].Status != FactStatusCurrent {
				t.Fatalf("canonical legacy history=%+v", legacyHistory)
			}

			live := memory.ClassifyHeuristics(tt.live)
			if live.Target != memory.TargetProfile || live.FactKey != tt.wantKey || FactKind(live.FactKind) != tt.wantKind {
				t.Fatalf("live classification=%+v, want key=%q kind=%q", live, tt.wantKey, tt.wantKind)
			}
			if _, err := store.UpsertFact(FactInput{
				Subject: "self", Key: live.FactKey, Value: tt.live,
				Kind: tt.wantKind, Authority: FactAuthorityDirectUser,
			}); err != nil {
				t.Fatal(err)
			}

			history := store.FactHistory("self", tt.wantKey)
			if len(history) != 2 || history[0].Value != tt.legacy || history[0].Status != FactStatusSuperseded ||
				history[1].Value != tt.live || history[1].Status != FactStatusCurrent {
				t.Fatalf("legacy to live history=%+v", history)
			}
			active := store.ActiveFacts("self")
			if len(active) != 1 || active[0].Key != tt.wantKey || active[0].Value != tt.live {
				t.Fatalf("active facts retained parallel legacy identity: %+v", active)
			}
		})
	}
}

func TestLegacyFactMigrationKeepsUnmatchedMarkdownInActiveTier1AcrossCutover(t *testing.T) {
	root := t.TempDir()
	wikiDir, diaryDir, workspace := filepath.Join(root, "wiki"), filepath.Join(root, "diary"), filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	memoryRaw := `# MEMORY

승인자가 확인하기 전에는 운영 배포를 시작하지 않는다.

1. 계약 검토를 먼저 수행한다.
2. 승인 기록을 남긴다.

* 별표 목록도 지속 컨텍스트다.

- 답변은 짧게 유지한다.
`
	userRaw := `# USER

평일 오전에는 집중 업무를 우선한다.
`
	if err := os.WriteFile(filepath.Join(workspace, "MEMORY.md"), []byte(memoryRaw), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "USER.md"), []byte(userRaw), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	if imported, importErr := store.ImportLegacyFactFiles(workspace); importErr != nil || imported != 1 {
		t.Fatalf("imported=%d err=%v", imported, importErr)
	}
	if imported, retryErr := store.ImportLegacyFactFiles(workspace); retryErr != nil || imported != 0 {
		t.Fatalf("idempotent retry imported=%d err=%v", imported, retryErr)
	}
	assertActive := func(label string, current *Store) {
		t.Helper()
		pages := make(map[string]*Page)
		for _, result := range current.Tier1Pages(0.9) {
			pages[result.Path] = result.Page
		}
		memoryPage := pages[legacyActiveContextPagePath("MEMORY.md")]
		if memoryPage == nil {
			t.Fatalf("%s legacy MEMORY is absent from Tier1: %+v", label, pages)
		}
		for _, want := range []string{
			"승인자가 확인하기 전에는 운영 배포를 시작하지 않는다.",
			"1. 계약 검토를 먼저 수행한다.",
			"2. 승인 기록을 남긴다.",
			"* 별표 목록도 지속 컨텍스트다.",
		} {
			if !strings.Contains(memoryPage.Body, want) {
				t.Fatalf("%s legacy MEMORY missing %q:\n%s", label, want, memoryPage.Body)
			}
		}
		if strings.Contains(memoryPage.Body, "답변은 짧게 유지한다") {
			t.Fatalf("%s imported fact row was duplicated into legacy active context:\n%s", label, memoryPage.Body)
		}
		userPage := pages[legacyActiveContextPagePath("USER.md")]
		if userPage == nil || !strings.Contains(userPage.Body, "평일 오전에는 집중 업무를 우선한다") {
			t.Fatalf("%s legacy USER is absent from Tier1: %+v", label, userPage)
		}
	}
	assertActive("before workspace projection", store)
	if err := store.SetFactProjectionDir(workspace); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"MEMORY.md", "USER.md"} {
		raw, readErr := os.ReadFile(filepath.Join(workspace, name))
		if readErr != nil || !bytesContainGeneratedMarker(raw) {
			t.Fatalf("%s projection=%q err=%v", name, raw, readErr)
		}
	}
	if err := store.UpdatePage(legacyActiveContextPagePath("MEMORY.md"), func(current *Page) (*Page, error) {
		if current == nil {
			return nil, os.ErrNotExist
		}
		current.Body += "\n사용자가 cutover 뒤 추가한 메모.\n"
		return current, nil
	}); err != nil {
		t.Fatalf("ordinary legacy context edit: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	assertActive("after restart", reopened)
	edited, err := reopened.ReadPage(legacyActiveContextPagePath("MEMORY.md"))
	if err != nil || !strings.Contains(edited.Body, "사용자가 cutover 뒤 추가한 메모") {
		t.Fatalf("ordinary legacy context edit did not survive restart: page=%+v err=%v", edited, err)
	}
}

func TestLegacyFactMigrationDoesNotCollapseBareSectionBullets(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	workspace := t.TempDir()
	legacyFile := "# MEMORY\n\n## 언어\n\n- Go\n- Kotlin\n"
	if err := os.WriteFile(filepath.Join(workspace, "MEMORY.md"), []byte(legacyFile), 0o600); err != nil {
		t.Fatal(err)
	}

	if imported, err := store.ImportLegacyFactFiles(workspace); err != nil || imported != 2 {
		t.Fatalf("imported=%d err=%v", imported, err)
	}
	active := store.ActiveFacts("self")
	if len(active) != 2 {
		t.Fatalf("bare bullets under a generic section collapsed into one identity: %+v", active)
	}
	for _, claim := range active {
		if claim.Key == "communication.language" {
			t.Fatalf("bare section bullet was over-aliased to a structured field: %+v", claim)
		}
	}
}
