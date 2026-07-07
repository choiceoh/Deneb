// person_resolve.go — resolve external display names (org chart members, agent
// mentions) to their 인물 wiki page paths. Any caller that holds only a person's
// name — the org chart editor linking a member to their knowledge page, the org
// recall source surfacing who a query is about — uses this to bridge into the
// wiki. The bridge deliberately lives in the wiki package (not org/): it is the
// wiki that owns the person pages and the name normalization, and both stay
// unexported.

package wiki

// ResolvePersonPaths maps each display name to its 인물 page relPath, for the
// names that have a page. Matching uses NormalizePersonName — the same
// normalization contact enrichment uses — so "오선택 전무", "오선택(탑솔라)" and
// "오선택" all resolve to the one 인물/오선택… page. A name with no page is simply
// absent from the result (callers treat a missing key as "no wiki page yet").
//
// One disk scan (listPeopleByName) resolves the whole batch, so callers with
// many names — the org GET handler resolving every member — pass them all at
// once rather than calling per name. Within the current wiki one normalized name
// maps to exactly one page, so 동명이인 that were merged onto a shared page
// resolve to that shared page; distinguishing them needs more than a name.
func (s *Store) ResolvePersonPaths(names []string) map[string]string {
	if len(names) == 0 {
		return nil
	}
	people, err := s.listPeopleByName()
	if err != nil || len(people) == 0 {
		return nil
	}
	out := make(map[string]string, len(names))
	for _, name := range names {
		key := NormalizePersonName(name)
		if len([]rune(key)) < 2 {
			continue
		}
		if p, ok := people[key]; ok {
			out[name] = p.path
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
