package wiki

import (
	"os"
	"path/filepath"
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
