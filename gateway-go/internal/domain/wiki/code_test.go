package wiki

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestValidProjectCodeRejectsInvalid(t *testing.T) {
	valid := []string{"pl0-jdo-wnd-001", "pl3-tri-mod-001", "nde-ztt-cbl-001", "pl2-bs8-epc-002", "etc-gpo-wnd-001", "com-hyu-mod-010"}
	for _, c := range valid {
		if !validProjectCode(c) {
			t.Errorf("validProjectCode(%q) = false, want true", c)
		}
	}
	invalid := []string{
		"",                  // empty
		"pl3-tri-mod",       // no sequence
		"pl3-tri-mod-1",     // sequence not 3-digit
		"xx9-tri-mod-001",   // unknown dept
		"pl3-tri-xyz-001",   // unknown dtype
		"pl3-trina-mod-001", // client not 3-char
		"PL3-TRI-MOD-001",   // not normalized (uppercase)
	}
	for _, c := range invalid {
		if validProjectCode(c) {
			t.Errorf("validProjectCode(%q) = true, want false", c)
		}
	}
}

func TestNormalizeProjectCode(t *testing.T) {
	cases := map[string]string{
		"  PL3-TRI-MOD-001 ":  "pl3-tri-mod-001",
		"[[nde-ztt-cbl-001]]": "nde-ztt-cbl-001",
		"pl2-kia-epc-001":     "pl2-kia-epc-001",
	}
	for in, want := range cases {
		if got := normalizeProjectCode(in); got != want {
			t.Errorf("normalizeProjectCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCodeStemParsesFormats(t *testing.T) {
	cases := map[string]string{
		"pl3-tri-mod":     "pl3-tri-mod", // bare stem
		"pl3-tri-mod-007": "pl3-tri-mod", // full code → sequence dropped (Go owns it)
		"PL3-TRI-MOD":     "pl3-tri-mod", // normalized
		"pl3-tri-xyz":     "",            // unknown dtype
		"zzz-tri-mod":     "",            // unknown dept
		"pl3-trina-mod":   "",            // client not 3-char
		"pl3-tri":         "",            // too few segments
		"":                "",
	}
	for in, want := range cases {
		if got := codeStem(in); got != want {
			t.Errorf("codeStem(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCodeIndex_ResolveCodeCreatesOrInherits(t *testing.T) {
	// Child filed under an existing project inherits the folder's frozen code.
	ci := codeIndex{
		folderCode: map[string]string{"프로젝트/기아-화성": "pl2-kia-epc-001"},
		maxSeq:     map[string]int{},
	}
	if got := ci.resolveCode(wikiUpdate{Path: "프로젝트/기아-화성/new-mail.md"}); got != "pl2-kia-epc-001" {
		t.Errorf("inherit: got %q, want pl2-kia-epc-001", got)
	}

	// New project mints from the LLM stem; Go assigns the next sequence.
	ci2 := codeIndex{folderCode: map[string]string{}, maxSeq: map[string]int{"pl3-new-mod": 2}}
	if got := ci2.resolveCode(wikiUpdate{Path: "프로젝트/새거래/x.md", Code: "pl3-new-mod"}); got != "pl3-new-mod-003" {
		t.Errorf("mint: got %q, want pl3-new-mod-003", got)
	}
	// A sibling filed later in the same batch inherits the freshly minted code.
	if got := ci2.resolveCode(wikiUpdate{Path: "프로젝트/새거래/y.md"}); got != "pl3-new-mod-003" {
		t.Errorf("sibling inherit: got %q, want pl3-new-mod-003", got)
	}

	// Non-project pages are never coded, even if a stem is proposed.
	if got := ci2.resolveCode(wikiUpdate{Path: "인물/foo.md", Code: "pl3-new-mod"}); got != "" {
		t.Errorf("non-project: got %q, want empty", got)
	}
	// A new project with no (or invalid) stem stays uncoded.
	if got := ci2.resolveCode(wikiUpdate{Path: "프로젝트/미상/z.md", Code: "garbage"}); got != "" {
		t.Errorf("invalid stem: got %q, want empty", got)
	}
}

func TestBuildCodeIndexCreatesFolderCodeMap(t *testing.T) {
	s, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ent := NewPage("트리나솔라 2026 모듈 공급계약", "프로젝트", nil)
	ent.Meta.Code = "pl3-tri-mod-001"
	ent.Meta.Type = "entity"
	if err := s.WritePage("프로젝트/트리나솔라-모듈계약/계약.md", ent); err != nil {
		t.Fatal(err)
	}
	wd := &WikiDreamer{store: s, logger: slog.Default()}
	ci := wd.buildCodeIndex()
	if ci.folderCode["프로젝트/트리나솔라-모듈계약"] != "pl3-tri-mod-001" {
		t.Errorf("folderCode = %v, want trina folder mapped", ci.folderCode)
	}
	if ci.maxSeq["pl3-tri-mod"] != 1 {
		t.Errorf("maxSeq[pl3-tri-mod] = %d, want 1", ci.maxSeq["pl3-tri-mod"])
	}
}

// TestBuildCodeIndex_RepCodePreservedOverLaterEntity: the 대표페이지's code owns
// the folder — a child page typed "entity" iterated AFTER the rep (여기서는
// 파일명 정렬상 대표.md 뒤에 오는 이름) must not overwrite it.
func TestBuildCodeIndex_RepCodePreservedOverLaterEntity(t *testing.T) {
	s, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rep := NewPage("기아화성", "프로젝트", nil)
	rep.Meta.Code = "pl3-kia-mod-001"
	if err := s.WritePage(RepPagePath("기아화성"), rep); err != nil {
		t.Fatal(err)
	}
	// ListPages walks lexically; "화성-계약.md" sorts after "대표.md" so this
	// entity is iterated after the rep — the old `|| Type=="entity"` clause let
	// it clobber the rep's code.
	ent := NewPage("기아화성 하도급 계약", "프로젝트", nil)
	ent.Meta.Code = "pl3-kia-epc-001"
	ent.Meta.Type = "entity"
	if err := s.WritePage("프로젝트/기아화성/화성-계약.md", ent); err != nil {
		t.Fatal(err)
	}

	wd := &WikiDreamer{store: s, logger: slog.Default()}
	ci := wd.buildCodeIndex()
	if got := ci.folderCode["프로젝트/기아화성"]; got != "pl3-kia-mod-001" {
		t.Errorf("folderCode = %q, want the rep's code pl3-kia-mod-001", got)
	}
}

// TestFindExistingPage_ChildCreateKeepsOwnPath (regression, M21): a CHILD page
// create carrying its project's inherited code must stay a create at its own
// path — the code signal resolves to the 대표페이지, and matching on it turned
// child creates into rep updates (child content absorbed into the rep). A rep
// proposal with the same code must still dedup onto the existing rep.
func TestFindExistingPage_ChildCreateKeepsOwnPath(t *testing.T) {
	s, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rep := NewPage("기아화성", "프로젝트", nil)
	rep.Meta.Code = "pl3-kia-mod-001"
	rep.Body = "# 기아화성"
	if err := s.WritePage(RepPagePath("기아화성"), rep); err != nil {
		t.Fatal(err)
	}
	wd := &WikiDreamer{store: s, logger: slog.Default()}

	child := wikiUpdate{
		Path:     "프로젝트/기아화성/기자재/모듈-상세.md",
		Title:    "모듈 상세",
		Code:     "pl3-kia-mod-001", // inherited project code
		Category: "프로젝트",
	}
	if got := wd.findExistingPage(child); got != "" {
		t.Fatalf("child create redirected onto %q — must stay a create in its own path", got)
	}

	repDup := wikiUpdate{
		Path:     "프로젝트/기아-오토랜드-화성/대표.md",
		Title:    "기아 오토랜드 화성",
		Code:     "pl3-kia-mod-001",
		Category: "프로젝트",
	}
	if got := wd.findExistingPage(repDup); got != RepPagePath("기아화성") {
		t.Fatalf("rep-page code dedup broken: got %q, want %q", got, RepPagePath("기아화성"))
	}
}

// TestGraphContext_CodeRefPreservedAcrossMove is the headline guarantee: a
// reference by code resolves to its target page, and keeps resolving after the
// target moves to a different path/folder (the whole point of a frozen identity).
func TestGraphContext_CodeRefPreservedAcrossMove(t *testing.T) {
	s, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := NewPage("트리나솔라 2026 모듈 공급계약", "프로젝트", nil)
	target.Meta.Code = "pl3-tri-mod-001"
	target.Meta.Summary = "트리나 모듈 공급 계약"
	if err := s.WritePage("프로젝트/트리나솔라-모듈계약/계약.md", target); err != nil {
		t.Fatal(err)
	}
	// The referrer points at the CODE, not any path.
	ref := NewPage("기아 광주 2공장 모듈 입찰", "프로젝트", nil)
	ref.Meta.Related = []string{"pl3-tri-mod-001"}
	ref.Meta.Summary = "기아 광주 입찰"
	if err := s.WritePage("프로젝트/기아-광주/입찰.md", ref); err != nil {
		t.Fatal(err)
	}

	before, err := s.GraphContext(context.Background(), "기아 광주 2공장 모듈 입찰", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before, "트리나") {
		t.Fatalf("code ref did not resolve to target: %q", before)
	}

	// Move the target: path/folder changes, code stays. MovePage does not repoint
	// the code ref (it only rewrites exact paths), so resolution must hold.
	if err := s.MovePage("프로젝트/트리나솔라-모듈계약/계약.md", "프로젝트/pl3-구매/트리나-계약.md"); err != nil {
		t.Fatal(err)
	}
	after, err := s.GraphContext(context.Background(), "기아 광주 2공장 모듈 입찰", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after, "트리나") {
		t.Fatalf("code ref broke after move (should be move-stable): %q", after)
	}
}
