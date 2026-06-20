// org.go — miniapp.org.* RPC handlers (group org chart editor).
//
//   miniapp.org.get  — the operator's full org chart (조직도) as a flat node tree
//   miniapp.org.save — validate + persist an edited chart
//
// The chart is the MASTER source for the dashboard's part classification: a node
// tagged with a lane becomes a "파트별 업무 현황" column, and its members /
// keywords / companies seed that part's classification rules (see
// internal/domain/org.DeriveRules). Editing the chart here re-derives the
// dashboard grouping — there is no separate rules file to maintain.
//
// Storage: the chart is a plain JSON file at {stateDir}/org.json (operator data,
// never in the repo — holds real names). Reads/writes go straight to that file;
// writes are atomic (atomicfile) and rejected unless the tree validates
// (org.OrgTree.Validate: unique ids, existing parents, no cycles, unique lane
// keys). We borrow the handler *shape* from topicdocs (lazy I/O, requireAuth,
// RespondOK) but own the org-specific validation in the domain package.
//
// Privacy: this handler holds no names — it only marshals the tree the operator
// edits. The repo ships org.example.json with fake data as the copy template.

package handlerminiapp

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// maxOrgNodes caps a saved chart. A hand-maintained group org chart is small
// (tens of boxes); this bound rejects a malformed/pathological payload before
// it touches disk, without constraining any realistic chart.
const maxOrgNodes = 2000

// OrgDeps wires the org chart editor. Load resolves the current tree (env
// override → state dir, missing file → empty tree); Save validates and persists
// the marshaled bytes atomically to the resolved path. Both default to the
// org-package functions in production; tests inject fakes (and a temp path).
//
// Save is split as (resolve path) + (write bytes) so the handler owns the atomic
// write while the domain owns validation + the on-disk shape (OrgTree.Marshal).
type OrgDeps struct {
	// Load returns the current chart. A nil Load makes OrgMethods return nil
	// (domain unregistered) — but production always wires it, so the editor is
	// always available.
	Load func() (org.OrgTree, error)
	// SavePath resolves the file to write (org.ResolvePath in production).
	SavePath func() string
}

// MemberOut is the wire shape for one person in a node: their name plus the
// optional 직급 (rank) and 직책 (position). Mirrors org.Member field-for-field.
// There is no affiliation field — a person's affiliation (계열사/실/팀) is the
// tree node they sit under, so it is structural, not a member attribute.
//
//deneb:wire
type MemberOut struct {
	Name     string `json:"name"`
	Rank     string `json:"rank,omitempty"`
	Position string `json:"position,omitempty"`
}

// OrgNodeOut is the wire shape for one chart node. It mirrors org.OrgNode field-
// for-field (same JSON tags) so the native client shares one source of truth and
// the same shape round-trips through get → edit → save. The node's leader (부서장)
// is derived client-side as the member whose position is a leader role
// (본부장/실장/팀장) — there is no standalone head field.
//
//deneb:wire
type OrgNodeOut struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Type      string      `json:"type"`
	ParentID  string      `json:"parentId,omitempty"`
	Lane      string      `json:"lane,omitempty"`
	Members   []MemberOut `json:"members,omitempty"`
	Keywords  []string    `json:"keywords,omitempty"`
	Companies []string    `json:"companies,omitempty"`
}

// OrgTreeOut is the miniapp.org.get response and the miniapp.org.save request
// body: the whole chart as a flat node list joined by parentId.
//
//deneb:wire
type OrgTreeOut struct {
	Nodes []OrgNodeOut `json:"nodes"`
}

// OrgSaveOut is the miniapp.org.save result: a small ack with the persisted node
// count and whether the saved chart defines any dashboard parts (lane nodes), so
// the client can confirm the chart will drive the dashboard.
//
//deneb:wire
type OrgSaveOut struct {
	Saved     bool `json:"saved"`
	NodeCount int  `json:"nodeCount"`
	HasLanes  bool `json:"hasLanes"`
}

// OrgMethods returns the miniapp.org.* handler map, or nil when the chart loader
// is unwired (so method_registry.go can skip registration).
func OrgMethods(deps OrgDeps) map[string]rpcutil.HandlerFunc {
	if deps.Load == nil || deps.SavePath == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.org.get":  orgGet(deps),
		"miniapp.org.save": orgSave(deps),
	}
}

// orgGet returns the current chart. A missing file yields an empty tree (NOT an
// error) so the native editor opens to a blank chart the operator can build; a
// corrupt/invalid on-disk chart surfaces as UNAVAILABLE so a bad file is visible
// rather than silently shown as empty.
func orgGet(deps OrgDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		tree, err := deps.Load()
		if err != nil {
			return rpcerr.WrapUnavailable("org chart unavailable", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, projectOrgTree(tree))
	}
}

// orgSave validates and persists an edited chart. The whole tree is replaced
// (the editor sends the full node list, like a document save). Validation runs
// in the domain (OrgTree.Marshal → Validate) before any disk write, so an
// invalid edit (missing parent, cycle, duplicate lane) is rejected with a clear
// message and the existing file is left intact.
func orgSave(deps OrgDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[OrgTreeOut](req)
		if errResp != nil {
			return errResp
		}
		if len(p.Nodes) > maxOrgNodes {
			return rpcerr.ValidationFailed("org chart has too many nodes").Response(req.ID)
		}

		tree := orgTreeFromWire(p)
		// Marshal validates first; an invalid tree returns an error here and we
		// never touch the file.
		data, err := tree.Marshal()
		if err != nil {
			return rpcerr.ValidationFailed("invalid org chart: " + err.Error()).Response(req.ID)
		}
		if err := atomicfile.WriteFile(deps.SavePath(), data, nil); err != nil {
			return rpcerr.WrapUnavailable("org chart write failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, OrgSaveOut{
			Saved:     true,
			NodeCount: len(tree.Nodes),
			HasLanes:  tree.HasLanes(),
		})
	}
}

// --- projection ------------------------------------------------------------

// projectOrgTree maps the domain tree to its wire shape. The field sets are
// identical, so this is a 1:1 copy (kept explicit rather than aliasing the types
// so the domain stays free of //deneb:wire and the handler owns the wire
// contract).
func projectOrgTree(t org.OrgTree) OrgTreeOut {
	out := OrgTreeOut{Nodes: make([]OrgNodeOut, 0, len(t.Nodes))}
	for _, n := range t.Nodes {
		out.Nodes = append(out.Nodes, OrgNodeOut{
			ID:        n.ID,
			Name:      n.Name,
			Type:      n.Type,
			ParentID:  n.ParentID,
			Lane:      n.Lane,
			Members:   membersToWire(n.Members),
			Keywords:  n.Keywords,
			Companies: n.Companies,
		})
	}
	return out
}

// membersToWire maps domain members to their wire shape (nil stays nil so an
// empty member list omits the JSON field).
func membersToWire(ms []org.Member) []MemberOut {
	if ms == nil {
		return nil
	}
	out := make([]MemberOut, 0, len(ms))
	for _, m := range ms {
		out = append(out, MemberOut{Name: m.Name, Rank: m.Rank, Position: m.Position})
	}
	return out
}

// orgTreeFromWire maps an inbound wire tree back to the domain type for
// validation + persistence.
func orgTreeFromWire(w OrgTreeOut) org.OrgTree {
	tree := org.OrgTree{Nodes: make([]org.OrgNode, 0, len(w.Nodes))}
	for _, n := range w.Nodes {
		tree.Nodes = append(tree.Nodes, org.OrgNode{
			ID:        n.ID,
			Name:      n.Name,
			Type:      n.Type,
			ParentID:  n.ParentID,
			Lane:      n.Lane,
			Members:   membersFromWire(n.Members),
			Keywords:  n.Keywords,
			Companies: n.Companies,
		})
	}
	return tree
}

// membersFromWire maps inbound wire members back to the domain type (nil stays
// nil).
func membersFromWire(ms []MemberOut) []org.Member {
	if ms == nil {
		return nil
	}
	out := make([]org.Member, 0, len(ms))
	for _, m := range ms {
		out = append(out, org.Member{Name: m.Name, Rank: m.Rank, Position: m.Position})
	}
	return out
}
