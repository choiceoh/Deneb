package recall

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

func TestRecallCorrectedFactKeyBlocksParaphrasedOldDiaryEvidence(t *testing.T) {
	root := t.TempDir()
	store := openRecallFactLifecycleStore(t, filepath.Join(root, "wiki"), filepath.Join(root, "diary"))
	t.Cleanup(func() { _ = store.Close() })

	const (
		oldClaim = "나는 답변을 길고 상세하게 받는 것을 선호해"
		newClaim = "앞으로 답변은 짧고 간결하게 해줘"
		oldDiary = "사용자는 장황하고 자세한 답변을 원한다고 했다"
	)
	if err := store.AppendDiary(oldDiary + "."); err != nil {
		t.Fatalf("AppendDiary: %v", err)
	}
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	upsertRecallFact(t, store, wiki.FactInput{
		Subject: "self", Key: "communication.response_length", Value: oldClaim,
		Kind: wiki.FactKindPreference, Authority: wiki.FactAuthorityDirectUser, At: base,
	})
	upsertRecallFact(t, store, wiki.FactInput{
		Subject: "self", Key: "communication.response_length", Value: newClaim,
		Kind: wiki.FactKindPreference, Authority: wiki.FactAuthorityDirectUser, At: base.Add(time.Minute),
	})

	out := buildRecallFactLifecycle(t, store, "내 답변 길이 선호 기억나?")
	if strings.Contains(out, oldDiary) || strings.Contains(out, oldClaim) {
		t.Fatalf("semantic stale evidence survived correction:\n%s", out)
	}
	if !strings.Contains(out, newClaim) {
		t.Fatalf("current claim missing:\n%s", out)
	}
}

func TestRecallLegacySupersededPageBlocksVagueDiaryFallback(t *testing.T) {
	root := t.TempDir()
	store := openRecallFactLifecycleStore(t, filepath.Join(root, "wiki"), filepath.Join(root, "diary"))
	t.Cleanup(func() { _ = store.Close() })

	const (
		oldValue = "아틀라스 담당자는 최민수다"
		newValue = "아틀라스 담당자는 윤지혜다"
	)
	if err := store.AppendDiary(oldValue + "."); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendDiary(newValue + "."); err != nil {
		t.Fatal(err)
	}
	oldPage := wiki.NewPage("아틀라스 담당자 이전", "프로젝트", nil)
	oldPage.Body = oldValue
	newPage := wiki.NewPage("아틀라스 담당자", "프로젝트", nil)
	newPage.Body = newValue
	if err := store.WritePage("프로젝트/atlas-old.md", oldPage); err != nil {
		t.Fatal(err)
	}
	if err := store.WritePage("프로젝트/atlas.md", newPage); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSuperseded("프로젝트/atlas-old.md", "프로젝트/atlas.md"); err != nil {
		t.Fatal(err)
	}
	out := buildRecallFactLifecycle(t, store, "그거 기억나?")
	if strings.Contains(out, oldValue) {
		t.Fatalf("vague fallback revived legacy superseded value:\n%s", out)
	}
}
