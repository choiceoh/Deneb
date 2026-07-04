// project_match.go — server-side project↔item matching for miniapp.project.linked.
//
// This is the Go port of the andromeda client's ProjectHomePane heuristic
// (collectProjectRefs / isLinkedToProject / addProjectKeys / normalizeProjectKey),
// moved server-side so the matching has one home (the gateway, which owns the
// wiki graph) instead of a fragile generic object-traversal in each client.
//
// A project's identity is its name + 대표페이지 path + frozen code + graph-resolved
// owned-page refs (all already computed for the digest, §③). An item is linked
// when any of its project-ref strings matches an identity key — by full normalized
// value or by path leaf — exactly as the client did.
package handlerminiapp

import (
	"path"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// projectMatchKeys builds a project's identity key set from the same inputs the
// digest ships. Each value contributes its normalized form plus its path leaf,
// mirroring the client's addProjectKeys.
func projectMatchKeys(name, path, code string, refs []string) map[string]struct{} {
	keys := make(map[string]struct{})
	addMatchKey(keys, name)
	addMatchKey(keys, path)
	addMatchKey(keys, code)
	for _, r := range refs {
		addMatchKey(keys, r)
	}
	return keys
}

func addMatchKey(keys map[string]struct{}, v string) {
	k := normalizeMatchKey(v)
	if k == "" {
		return
	}
	keys[k] = struct{}{}
	if leaf := matchKeyLeaf(k); leaf != k {
		keys[leaf] = struct{}{}
	}
}

// normalizeMatchKey mirrors the client's normalizeProjectKey: lowercase, unify
// separators to '/', strip surrounding slashes and a trailing .md, collapse
// internal whitespace.
func normalizeMatchKey(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "\\", "/")
	v = strings.Trim(v, "/")
	v = strings.TrimSuffix(v, ".md")
	return strings.Join(strings.Fields(v), " ")
}

// projectSlotLeafKeys are the fixed per-project slot filenames in normalized
// key form (lowercased, ".md" stripped) — sourced from the layout's single
// truth (wiki/project_layout.go, wiki/project_log.go). They back the
// category-less fallback in matchKeyLeaf: a ref like "탑솔라/대표" has no
// 프로젝트/ prefix for wiki.ProjectNameOf, but its slot leaf still says the
// parent segment is the project.
var projectSlotLeafKeys = func() map[string]struct{} {
	m := make(map[string]struct{}, 3)
	for _, f := range []string{
		wiki.RepPageFile,                   // 대표.md
		wiki.LogPageFile,                   // 로그.md
		path.Base(wiki.LogArchivePath("")), // 로그-보관.md
	} {
		m[normalizeMatchKey(f)] = struct{}{}
	}
	return m
}()

// matchKeyLeaf returns the DISCRIMINATING leaf of a normalized key: the last
// '/'-separated segment (the whole key when there's no slash) — except for
// paths OWNED BY A PROJECT (프로젝트/<name>/... — wiki.ProjectNameOf owns the
// rule), where the project FOLDER segment identifies the project and is
// returned instead. That covers both the fixed slot filenames (대표.md/로그.md —
// after the folder migration every project carries them, so the leaf
// identifies a slot, not a project) and variable-named children (기자재/모듈.md
// — two projects both holding a 모듈.md would otherwise cross-link through the
// shared "모듈" key, showing other projects' items in the project corner).
// Reserved raw-data buckets (프로젝트/메일분석/<msgID>, 프로젝트/거래/…) are not
// project-owned and keep their unique file leaf (mail msgIDs must stay keys).
func matchKeyLeaf(k string) string {
	// Normalized keys have ".md" stripped; ProjectNameOf's legacy-flat branch
	// requires the extension, so probe with it re-attached. Korean segments
	// survive normalizeMatchKey's lowercasing unchanged.
	if name, ok := wiki.ProjectNameOf(k + ".md"); ok {
		return name
	}
	i := strings.LastIndex(k, "/")
	if i < 0 {
		return k
	}
	leaf := k[i+1:]
	if _, slot := projectSlotLeafKeys[leaf]; !slot {
		return leaf
	}
	// Category-less slot path ("탑솔라/대표") — the parent segment names the
	// project even though ProjectNameOf can't see the 프로젝트/ prefix.
	rest := k[:i]
	if j := strings.LastIndex(rest, "/"); j >= 0 {
		return rest[j+1:]
	}
	return rest
}

// itemLinkedToProject reports whether any of an item's project-ref strings match
// the project's identity keys (full or leaf) — the server equivalent of the
// client's isLinkedToProject.
func itemLinkedToProject(keys map[string]struct{}, refs ...string) bool {
	for _, r := range refs {
		k := normalizeMatchKey(r)
		if k == "" {
			continue
		}
		if _, ok := keys[k]; ok {
			return true
		}
		if leaf := matchKeyLeaf(k); leaf != k {
			if _, ok := keys[leaf]; ok {
				return true
			}
		}
	}
	return false
}

// mailMsgIDFromSource pulls the message ID out of a Deneb event Source provenance
// string ("mail:<msgID>", or a legacy "mail:<msgID>|<title>" form). Returns "" when
// the source isn't a mail link, so a hand-added calendar event never matches.
func mailMsgIDFromSource(source string) string {
	const prefix = "mail:"
	source = strings.TrimSpace(source)
	if !strings.HasPrefix(source, prefix) {
		return ""
	}
	id := source[len(prefix):]
	if i := strings.IndexByte(id, '|'); i >= 0 {
		id = id[:i]
	}
	return strings.TrimSpace(id)
}

// mailIDsFromRefs extracts linked mail message IDs from a project's owned-page
// refs. A mail analysis page lives at 프로젝트/<project>/메일분석/<msgID>.md (legacy:
// 프로젝트/mail-analyses/<project>/<msgID>.md) and lands in the project's refs
// through its Related[] edge (§③ projectOwnedRefs), so the page basename is the
// linked msgID. This reuses the graph resolution already in the digest — no mail
// store read — and a mail Related to several projects is in each project's refs,
// so it resolves for every project it cites (no regression versus the client's
// relatedProjects match).
func mailIDsFromRefs(refs []string) []string {
	out := []string{}
	seen := make(map[string]struct{})
	for _, r := range refs {
		// Normalize backslashes universally (not filepath.ToSlash, which is a no-op
		// off Windows) so a ref keyed with either separator resolves the same.
		slash := strings.ReplaceAll(r, "\\", "/")
		if !strings.Contains(slash, "/메일분석/") && !strings.Contains(slash, "/mail-analyses/") {
			continue
		}
		id := strings.TrimSuffix(slash, ".md")
		if i := strings.LastIndex(id, "/"); i >= 0 {
			id = id[i+1:]
		}
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
