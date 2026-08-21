// org_tool.go — read-only 조직도 queries. The operator maintains the real
// group→company→division→team tree (with 직급/직책 per member) in the native
// org editor; the dashboard derives its part lanes from it — but the agent
// could not answer "1팀 팀장 누구야" / "남도에코 조직 어떻게 돼". Read-only on
// purpose: the tree is operator-curated personal data, edits stay in the app.
package orgops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolOrg returns the org tool. The org package resolves its own file path
// ({stateDir}/org.json or DENEB_ORG_FILE) and a missing file loads as an
// empty tree, so no deps are injected.
func ToolOrg() toolport.ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Query string `json:"query"`
		}
		if err := jsonutil.UnmarshalInto("org params", input, &p); err != nil {
			return "", err
		}
		tree, err := org.Load()
		if err != nil {
			return "", fmt.Errorf("조직도 로드 실패: %w", err)
		}
		if len(tree.Nodes) == 0 {
			return "조직도가 아직 설정되지 않았습니다 (네이티브 앱 설정 > 조직도에서 편집).", nil
		}
		if q := strings.TrimSpace(p.Query); q != "" {
			return orgSearch(tree, q), nil
		}
		var b strings.Builder
		b.WriteString("조직도:\n")
		for _, root := range tree.Roots() {
			renderOrgNode(&b, tree, root, 0)
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
}

// renderOrgNode prints one node line (with members inline) then recurses.
func renderOrgNode(b *strings.Builder, tree org.OrgTree, n org.OrgNode, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(b, "%s- %s (%s)", indent, n.Name, n.Type)
	if n.Lane != "" {
		fmt.Fprintf(b, " · lane=%s", n.Lane)
	}
	b.WriteString("\n")
	for _, m := range n.Members {
		fmt.Fprintf(b, "%s    · %s%s\n", indent, m.Name, memberTitle(m))
	}
	for _, child := range tree.Children(n.ID) {
		renderOrgNode(b, tree, child, depth+1)
	}
}

// orgSearch returns nodes/members whose name matches the query substring
// (case-insensitive), each with its position in the tree.
func orgSearch(tree org.OrgTree, query string) string {
	q := strings.ToLower(query)
	names := map[string]string{} // id → name (for parent chains)
	for _, n := range tree.Nodes {
		names[n.ID] = n.Name
	}
	chain := func(n org.OrgNode) string {
		parts := []string{n.Name}
		for pid := n.ParentID; pid != ""; {
			var parent *org.OrgNode
			for i := range tree.Nodes {
				if tree.Nodes[i].ID == pid {
					parent = &tree.Nodes[i]
					break
				}
			}
			if parent == nil {
				break
			}
			parts = append([]string{parent.Name}, parts...)
			pid = parent.ParentID
		}
		return strings.Join(parts, " > ")
	}

	var b strings.Builder
	hits := 0
	for _, n := range tree.Nodes {
		nodeMatch := strings.Contains(strings.ToLower(n.Name), q)
		var members []org.Member
		for _, m := range n.Members {
			if strings.Contains(strings.ToLower(m.Name), q) {
				members = append(members, m)
			}
		}
		if !nodeMatch && len(members) == 0 {
			continue
		}
		hits++
		fmt.Fprintf(&b, "- %s (%s)\n", chain(n), n.Type)
		show := n.Members
		if !nodeMatch {
			show = members // only the matching people, not the whole team
		}
		for _, m := range show {
			fmt.Fprintf(&b, "    · %s%s\n", m.Name, memberTitle(m))
		}
	}
	if hits == 0 {
		return fmt.Sprintf("조직도에서 %q 를 찾지 못했습니다.", query)
	}
	return strings.TrimRight(b.String(), "\n")
}

// memberTitle renders " (직급/직책)" for a member, omitting empty parts.
func memberTitle(m org.Member) string {
	switch {
	case m.Rank != "" && m.Position != "":
		return fmt.Sprintf(" (%s/%s)", m.Rank, m.Position)
	case m.Rank != "":
		return " (" + m.Rank + ")"
	case m.Position != "":
		return " (" + m.Position + ")"
	default:
		return ""
	}
}
