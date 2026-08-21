package orgops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
)

func seedOrgFile(t *testing.T) {
	t.Helper()
	tree := org.OrgTree{Nodes: []org.OrgNode{
		{ID: "g1", Name: "그룹", Type: org.NodeTypeGroup},
		{ID: "c1", Name: "남도에코", Type: org.NodeTypeCompany, ParentID: "g1"},
		{ID: "t1", Name: "1팀", Type: org.NodeTypeTeam, ParentID: "c1", Members: []org.Member{
			{Name: "김민준", Rank: "부장", Position: "팀장"},
			{Name: "이서연", Rank: "대리"},
		}},
	}}
	data, err := tree.Marshal()
	if err != nil {
		t.Fatalf("marshal org: %v", err)
	}
	path := filepath.Join(t.TempDir(), "org.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write org: %v", err)
	}
	t.Setenv("DENEB_ORG_FILE", path)
}

func TestToolOrgReturnsTreeAndSearchResults(t *testing.T) {
	seedOrgFile(t)
	tool := ToolOrg()
	ctx := context.Background()

	out, err := tool(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	for _, want := range []string{"그룹", "남도에코", "1팀", "김민준 (부장/팀장)", "이서연 (대리)"} {
		if !strings.Contains(out, want) {
			t.Errorf("tree missing %q: %q", want, out)
		}
	}

	out, err = tool(ctx, json.RawMessage(`{"query":"김민준"}`))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "그룹 > 남도에코 > 1팀") || !strings.Contains(out, "김민준") {
		t.Errorf("search must show the person with their chain: %q", out)
	}
	if strings.Contains(out, "이서연") {
		t.Errorf("person query must not list unmatched teammates: %q", out)
	}

	out, _ = tool(ctx, json.RawMessage(`{"query":"없는사람"}`))
	if !strings.Contains(out, "찾지 못했습니다") {
		t.Errorf("miss must say not found: %q", out)
	}
}

func TestToolOrgReportsUnsetWhenFileMissing(t *testing.T) {
	t.Setenv("DENEB_ORG_FILE", filepath.Join(t.TempDir(), "missing.json"))
	out, err := ToolOrg()(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unset org: %v", err)
	}
	if !strings.Contains(out, "설정되지 않았습니다") {
		t.Errorf("missing org file must report unset: %q", out)
	}
}
