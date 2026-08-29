package peopleops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// stub records the input it was called with and replays a canned answer.
func stub(out string, err error, seen *json.RawMessage) toolport.ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		if seen != nil {
			*seen = append(json.RawMessage(nil), input...)
		}
		return out, err
	}
}

func run(t *testing.T, fn toolport.ToolFunc, input string) string {
	t.Helper()
	out, err := fn(context.Background(), json.RawMessage(input))
	if err != nil {
		t.Fatalf("people(%s): %v", input, err)
	}
	return out
}

// TestPeopleFindFansOutToEverySource is the whole point of the tool: one call
// must reach all three stores and label what each returned.
func TestPeopleFindFansOutToEverySource(t *testing.T) {
	var contactsIn, orgIn, gwIn json.RawMessage
	fn := ToolPeople(Sources{
		Contacts:  stub("김성훈 010-1111-2222 (탑솔라)", nil, &contactsIn),
		Org:       stub("남도에코 > 1팀 > 김성훈 (팀장)", nil, &orgIn),
		Groupware: stub("김성훈 · 기획조정실 · 010-1111-2222", nil, &gwIn),
	})

	out := run(t, fn, `{"action":"find","query":"김성훈"}`)
	for _, want := range []string{"## 주소록", "## 조직도", "## 그룹웨어(라이브)", "팀장", "기획조정실"} {
		if !strings.Contains(out, want) {
			t.Fatalf("find output missing %q:\n%s", want, out)
		}
	}

	// Each source must be asked in its own dialect, not handed the raw input.
	if !strings.Contains(string(contactsIn), `"action":"search"`) {
		t.Errorf("contacts input = %s, want action=search", contactsIn)
	}
	if !strings.Contains(string(orgIn), `"query":"김성훈"`) {
		t.Errorf("org input = %s, want the query", orgIn)
	}
	if !strings.Contains(string(gwIn), `"area":"people"`) {
		t.Errorf("groupware input = %s, want area=people", gwIn)
	}
}

// TestPeopleFindSurvivesBrokenAndUnsetSources covers the degrade path: a nil
// source and a failing source must not take the working one down with them.
func TestPeopleFindSurvivesBrokenAndUnsetSources(t *testing.T) {
	fn := ToolPeople(Sources{
		Contacts:  stub("", errors.New("address book exploded"), nil),
		Org:       stub("남도에코 > 1팀 > 김성훈 (팀장)", nil, nil),
		Groupware: nil,
	})
	out := run(t, fn, `{"action":"find","query":"김성훈"}`)
	if !strings.Contains(out, "## 조직도") || strings.Contains(out, "## 주소록") {
		t.Fatalf("want only the org section, got:\n%s", out)
	}
}

// TestPeopleFindReportsNothingFound distinguishes "no source had an answer"
// from "sources are unavailable" — both end up empty, and the caller needs a
// sentence rather than a blank result.
func TestPeopleFindReportsNothingFound(t *testing.T) {
	fn := ToolPeople(Sources{
		Contacts: stub("주소록이 비어 있습니다. 네이티브 클라이언트에서…", nil, nil),
		Org:      stub("조직도가 아직 설정되지 않았습니다", nil, nil),
	})
	out := run(t, fn, `{"action":"find","query":"김성훈"}`)
	if !strings.Contains(out, "일치하는 사람이 없습니다") {
		t.Fatalf("want a not-found sentence, got: %s", out)
	}
}

// TestPeopleSingleSourceActionsDoNotFanOut: phone/company/tree are
// single-source by nature — only the address book reverses a number, only
// org.json holds the tree — so they must reach exactly that store.
func TestPeopleSingleSourceActionsDoNotFanOut(t *testing.T) {
	var contactsIn, orgIn json.RawMessage
	sources := Sources{
		Contacts:  stub("김성훈", nil, &contactsIn),
		Org:       stub("조직도:\n- 남도에코", nil, &orgIn),
		Groupware: stub("should not be reached", nil, nil),
	}

	for _, tc := range []struct {
		name, input, wantAction string
	}{
		{"phone", `{"action":"phone","query":"010-1111-2222"}`, `"action":"lookup"`},
		{"company", `{"action":"company","query":"탑솔라"}`, `"action":"by_company"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contactsIn = nil
			out := run(t, ToolPeople(sources), tc.input)
			if strings.Contains(out, "##") {
				t.Fatalf("%s must not fan out, got:\n%s", tc.name, out)
			}
			if !strings.Contains(string(contactsIn), tc.wantAction) {
				t.Fatalf("%s input = %s, want %s", tc.name, contactsIn, tc.wantAction)
			}
		})
	}

	t.Run("tree", func(t *testing.T) {
		orgIn = nil
		if out := run(t, ToolPeople(sources), `{"action":"tree"}`); !strings.Contains(out, "남도에코") {
			t.Fatalf("tree output = %s", out)
		}
		if orgIn == nil {
			t.Fatal("tree did not reach the org source")
		}
	})
}

// TestPeopleRequiresAQueryWhereItCannotGuess: every action but tree needs a
// subject, and the caller must get told rather than an empty fan-out.
func TestPeopleRequiresAQueryWhereItCannotGuess(t *testing.T) {
	fn := ToolPeople(Sources{Contacts: stub("김성훈", nil, nil), Org: stub("조직도:\n- 남도에코", nil, nil)})
	for _, input := range []string{`{"action":"find"}`, `{"action":"phone"}`, `{"action":"company"}`} {
		if out := run(t, fn, input); !strings.Contains(out, "필요합니다") && !strings.Contains(out, "필수입니다") {
			t.Errorf("%s should ask for a query, got: %s", input, out)
		}
	}
}

// TestPeopleDefaultsToFind: action is optional, and omitting it must not fall
// through to the usage line.
func TestPeopleDefaultsToFind(t *testing.T) {
	fn := ToolPeople(Sources{Contacts: stub("김성훈 010-1111-2222", nil, nil)})
	if out := run(t, fn, `{"query":"김성훈"}`); !strings.Contains(out, "## 주소록") {
		t.Fatalf("bare query should fan out, got: %s", out)
	}
}

// TestPeopleRejectsUnknownAction keeps the surface honest rather than silently
// treating a stale action name (the pre-merge contacts vocabulary) as find.
func TestPeopleRejectsUnknownAction(t *testing.T) {
	fn := ToolPeople(Sources{Contacts: stub("김성훈", nil, nil)})
	if out := run(t, fn, `{"action":"lookup","query":"김성훈"}`); !strings.Contains(out, "find, phone, company, tree") {
		t.Fatalf("want the usage line for an unknown action, got: %s", out)
	}
}

// TestPeopleRejectsMalformedInput: the tool must surface a parse error rather
// than silently treating garbage as a bare find.
func TestPeopleRejectsMalformedInput(t *testing.T) {
	fn := ToolPeople(Sources{Contacts: stub("김성훈", nil, nil)})
	if _, err := fn(context.Background(), json.RawMessage(`{"action":`)); err == nil {
		t.Fatal("malformed input should error")
	}
}
