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

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
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
	// LookupContact enriches a member at GET time with the address book: given a
	// member's display name it returns the matching contact's phones and emails
	// (nil/empty when no contact matches). It is read-only and best-effort —
	// the org chart (names/ranks/positions) stays the source of truth for the
	// chart, while the contacts store stays the source of truth for numbers, so
	// the two are never written back into org.json. A nil LookupContact (no
	// contacts store) simply disables enrichment; the editor still works.
	LookupContact func(name string) (phones, emails []string)
	// NotifyChanged, when set, fires once after a successful save — the
	// native-sync mirror (org.changed), so other clients drop their org/dashboard
	// snapshots instead of waiting out their TTL. Nil disables (tests).
	NotifyChanged func()
	// ResolvePeople maps member display names to their 인물 wiki page relPaths in
	// one call (one disk scan for the whole chart), so the GET response can link
	// each member to their knowledge page. Read-only, GET-only, never persisted.
	// A nil ResolvePeople (no wiki store) disables the person link; the editor
	// still works. This is the NAME join — the fallback when the identity email is
	// unknown.
	ResolvePeople func(names []string) map[string]string
	// ResolvePersonByEmail maps an email address to its 인물 page relPath — the
	// robust identity join that disambiguates 동명이인 the name cannot. GET prefers
	// it (via the member's enriched email) and falls back to ResolvePeople's name
	// match. nil disables the email join. Read-only, GET-only.
	ResolvePersonByEmail func(email string) string
}

// MemberOut is the wire shape for one person in a node: their name plus the
// optional 직급 (rank) and 직책 (position), and — only on the GET response — the
// phones/emails enriched from the contacts store so the native org chart can
// call or email a node's people directly. There is no affiliation field — a
// person's affiliation (계열사/실/팀) is the tree node they sit under, so it is
// structural, not a member attribute.
//
// Phones/Emails are read-only enrichment: they are filled at GET time by name-
// matching against the address book (the source of truth for numbers) and are
// NOT persisted — membersFromWire drops them, so org.save only ever stores
// name/rank/position. They round-trip through the wire as a convenience for the
// client, never back into org.json.
//
//deneb:wire
type MemberOut struct {
	Name     string   `json:"name"`
	Rank     string   `json:"rank,omitempty"`
	Position string   `json:"position,omitempty"`
	Phones   []string `json:"phones,omitempty"`
	Emails   []string `json:"emails,omitempty"`
	// PersonPath is the member's 인물 wiki page relPath (e.g.
	// "인물/오선택-전무-(기획조정실장).md"), resolved at GET time by name-matching
	// against the wiki so the native chart can open the person's knowledge page.
	// Empty when the member has no wiki page. Like Phones/Emails it is GET-only
	// enrichment — membersFromWire drops it, so org.save never persists it.
	PersonPath string `json:"personPath,omitempty"`
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
	return authenticated(func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		tree, err := deps.Load()
		if err != nil {
			return rpcerr.WrapUnavailable("org chart unavailable", err).Response(req.ID)
		}
		// Resolve every member's 인물 page in one batch (one disk scan) so
		// membersToWire can attach the person link without a per-member lookup.
		var personPaths map[string]string
		if deps.ResolvePeople != nil {
			personPaths = deps.ResolvePeople(allMemberNames(tree))
		}
		return rpcutil.RespondOK(req.ID, projectOrgTree(tree, deps.LookupContact, personPaths, deps.ResolvePersonByEmail))
	})
}

// orgSave validates and persists an edited chart. The whole tree is replaced
// (the editor sends the full node list, like a document save). Validation runs
// in the domain (OrgTree.Marshal → Validate) before any disk write, so an
// invalid edit (missing parent, cycle, duplicate lane) is rejected with a clear
// message and the existing file is left intact.
func orgSave(deps OrgDeps) rpcutil.HandlerFunc {
	return bindAuthenticated[OrgTreeOut](func(ctx context.Context, req *protocol.RequestFrame, p OrgTreeOut) *protocol.ResponseFrame {
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
		if deps.NotifyChanged != nil {
			deps.NotifyChanged()
		}
		return rpcutil.RespondOK(req.ID, OrgSaveOut{
			Saved:     true,
			NodeCount: len(tree.Nodes),
			HasLanes:  tree.HasLanes(),
		})
	})
}

// --- projection ------------------------------------------------------------

// projectOrgTree maps the domain tree to its wire shape. The structural fields
// are a 1:1 copy (kept explicit rather than aliasing the types so the domain
// stays free of //deneb:wire and the handler owns the wire contract); members
// are additionally enriched with contact phones/emails via lookup (nil lookup =
// no enrichment).
func projectOrgTree(t org.OrgTree, lookup func(name string) (phones, emails []string), personPaths map[string]string, resolveByEmail func(email string) string) OrgTreeOut {
	out := OrgTreeOut{Nodes: make([]OrgNodeOut, 0, len(t.Nodes))}
	for _, n := range t.Nodes {
		out.Nodes = append(out.Nodes, OrgNodeOut{
			ID:        n.ID,
			Name:      n.Name,
			Type:      n.Type,
			ParentID:  n.ParentID,
			Lane:      n.Lane,
			Members:   membersToWire(n.Members, lookup, personPaths, resolveByEmail),
			Keywords:  n.Keywords,
			Companies: n.Companies,
		})
	}
	return out
}

// allMemberNames flattens every member name in the tree for a single batch
// person-path resolution.
func allMemberNames(t org.OrgTree) []string {
	var names []string
	for _, n := range t.Nodes {
		for _, m := range n.Members {
			names = append(names, m.Name)
		}
	}
	return names
}

// membersToWire maps domain members to their wire shape (nil stays nil so an
// empty member list omits the JSON field). When lookup is non-nil, each member's
// name is matched against the contacts store and the resulting phones/emails are
// attached (read-only enrichment — never persisted; see MemberOut). A nil lookup
// (no contacts store wired) leaves phones/emails empty.
func membersToWire(ms []org.Member, lookup func(name string) (phones, emails []string), personPaths map[string]string, resolveByEmail func(email string) string) []MemberOut {
	if ms == nil {
		return nil
	}
	out := make([]MemberOut, 0, len(ms))
	for _, m := range ms {
		mo := MemberOut{Name: m.Name, Rank: m.Rank, Position: m.Position}
		if lookup != nil {
			mo.Phones, mo.Emails = lookup(m.Name)
		}
		// Prefer the EMAIL join (robust identity — disambiguates 동명이인) using the
		// member's enriched address; fall back to the NAME match when no address
		// resolves (e.g. a homonym page left without an identity email).
		if resolveByEmail != nil {
			for _, e := range mo.Emails {
				if p := resolveByEmail(e); p != "" {
					mo.PersonPath = p
					break
				}
			}
		}
		if mo.PersonPath == "" && personPaths != nil {
			mo.PersonPath = personPaths[m.Name]
		}
		out = append(out, mo)
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
// nil). It deliberately copies only name/rank/position and DROPS any inbound
// phones/emails: those are GET-time enrichment from the contacts store, not chart
// data, so a save must never write them into org.json (the contacts store stays
// the source of truth for numbers).
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
