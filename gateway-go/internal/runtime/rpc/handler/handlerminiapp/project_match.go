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

import "strings"

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

// matchKeyLeaf returns the last '/'-separated segment of a normalized key (the
// whole key when there's no slash).
func matchKeyLeaf(k string) string {
	if i := strings.LastIndex(k, "/"); i >= 0 {
		return k[i+1:]
	}
	return k
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

// mailIDsFromRefs extracts linked mail message IDs from a project's owned-page
// refs. A mail analysis page lives at 프로젝트/mail-analyses/<project>/<msgID>.md and
// lands in the project's refs through its Related[] edge (§③ projectOwnedRefs), so
// the page basename is the linked msgID. This reuses the graph resolution already
// in the digest — no mail store read — and a mail Related to several projects is in
// each project's refs, so it resolves for every project it cites (no regression
// versus the client's relatedProjects match).
func mailIDsFromRefs(refs []string) []string {
	out := []string{}
	seen := make(map[string]struct{})
	for _, r := range refs {
		// Normalize backslashes universally (not filepath.ToSlash, which is a no-op
		// off Windows) so a ref keyed with either separator resolves the same.
		slash := strings.ReplaceAll(r, "\\", "/")
		if !strings.Contains(slash, "/mail-analyses/") {
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
