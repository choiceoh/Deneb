package contacts

// Address books synced from a phone that triple-syncs (local + Samsung + Google)
// balloon far past the real headcount: the same person recurs under name variants
// ("박한주", "#박한주 부장(솔라테크)"), and colleagues share a switchboard number.
// Dedup collapses the safe duplicates with deterministic rules and surfaces the
// genuinely ambiguous pairs (same identifier, different name) for a later LLM /
// operator pass. It is pure and non-mutating — the store stays a replace-on-sync
// mirror; callers apply the result elsewhere (a cleaned vCard, a review UI).

import (
	"sort"
	"strings"
)

// roleLocalParts are non-personal email mailbox names shared across a company, so
// a match on one is NOT evidence of the same person.
var roleLocalParts = map[string]bool{
	"info": true, "sales": true, "contact": true, "admin": true, "cs": true,
	"help": true, "support": true, "master": true, "office": true, "mail": true,
	"webmaster": true, "ceo": true, "hr": true, "marketing": true, "service": true,
	"biz": true, "company": true, "korea": true, "team": true,
}

const (
	// sharedPhoneMinNames: a number carried by this many DISTINCT people is a
	// switchboard/representative line, never a personal-identity join key.
	sharedPhoneMinNames = 4
	// sharedEmailMinNames: same idea for a shared inbox below the role list.
	sharedEmailMinNames = 3
)

// MergeGroup is a set of address-book entries the deterministic rules judged to be
// one person (shared personal identifier + compatible name). Members index into
// the slice passed to Dedup.
type MergeGroup struct {
	Members   []int  `json:"members"`
	Canonical string `json:"canonical"` // cleanest display name for the merged card
}

// AmbiguousPair links two entries that share a personal identifier but whose names
// are not compatible — either the same person under a very different label or two
// different people sharing a number. The deterministic pass cannot tell; it hands
// these to adjudication. A and B index into the Dedup input.
type AmbiguousPair struct {
	A      int    `json:"a"`
	B      int    `json:"b"`
	Shared string `json:"shared"` // the phone/email that links them
}

// DedupResult is the deterministic dedup pass over an address book. It mutates
// nothing; Distinct is the entry count after applying Merges only.
type DedupResult struct {
	Merges    []MergeGroup    `json:"merges"`
	Ambiguous []AmbiguousPair `json:"ambiguous"`
	Total     int             `json:"total"`
	Distinct  int             `json:"distinct"`
}

// dedupKey reduces a contact display name to a match key. It reuses the shared
// NormalizePersonName (Korean titles, affiliation parens, separators) and first
// strips the "#" junk prefix that phone exports stamp on some entries.
func dedupKey(name string) string {
	return NormalizePersonName(strings.TrimLeft(strings.TrimSpace(name), "#＃"))
}

// dedouble undoes the first-syllable doubling a sync sometimes injects
// ("홍홍정우" -> "홍정우"). Applied only in comparison, never stored.
func dedouble(k string) string {
	r := []rune(k)
	if len(r) >= 3 && r[0] == r[1] {
		return string(r[1:])
	}
	return k
}

// namesCompatible reports whether two names can be the same person by the
// conservative deterministic bridges: equal / prefix (company or title tail) /
// first-syllable-doubling. A title-only entry (empty key) is NOT compatible with
// anything — letting it bridge would transitively fuse two different people who
// each merely share a number with the same "부장"/"기업지원과장" card. Cross name
// matches (nicknames, romanization, English↔Korean) are deliberately NOT bridged
// here — those are the ambiguous cases adjudication handles.
func namesCompatible(a, b string) bool {
	ka, kb := dedupKey(a), dedupKey(b)
	if ka == "" || kb == "" {
		return false
	}
	for _, x := range []string{ka, dedouble(ka)} {
		for _, y := range []string{kb, dedouble(kb)} {
			if x == y || strings.HasPrefix(x, y) || strings.HasPrefix(y, x) {
				return true
			}
		}
	}
	return false
}

// normalizeEmail lowercases and trims; returns "" for a non-address.
func normalizeEmail(e string) string {
	e = strings.ToLower(strings.TrimSpace(e))
	if !strings.Contains(e, "@") {
		return ""
	}
	return e
}

func emailLocalPart(e string) string {
	if at := strings.IndexByte(e, '@'); at > 0 {
		return e[:at]
	}
	return ""
}

// Dedup runs the deterministic clustering pass over contacts. Two entries merge
// when they share a PERSONAL phone or email (not a switchboard/role address) and
// their names are compatible; a shared identifier with incompatible names becomes
// an AmbiguousPair. Union-find makes merges transitive within a name family.
func Dedup(contacts []Contact) DedupResult {
	n := len(contacts)
	res := DedupResult{Total: n, Distinct: n}
	if n == 0 {
		return res
	}

	// identifier -> distinct normalized names carrying it (to spot shared lines)
	phoneNames := map[string]map[string]struct{}{}
	emailNames := map[string]map[string]struct{}{}
	add := func(m map[string]map[string]struct{}, key, name string) {
		if m[key] == nil {
			m[key] = map[string]struct{}{}
		}
		m[key][name] = struct{}{}
	}
	contactPhones := make([][]string, n)
	contactEmails := make([][]string, n)
	for i := range contacts {
		key := dedupKey(contacts[i].Name)
		for _, p := range contacts[i].Phones {
			if np := normalizePhone(p); len(np) >= 9 {
				contactPhones[i] = append(contactPhones[i], np)
				add(phoneNames, np, key)
			}
		}
		for _, e := range contacts[i].Emails {
			if ne := normalizeEmail(e); ne != "" {
				contactEmails[i] = append(contactEmails[i], ne)
				add(emailNames, ne, key)
			}
		}
	}
	sharedPhone := func(p string) bool { return len(phoneNames[p]) >= sharedPhoneMinNames }
	sharedEmail := func(e string) bool {
		return len(emailNames[e]) >= sharedEmailMinNames || roleLocalParts[emailLocalPart(e)]
	}

	uf := newUnionFind(n)

	// bucket entries by each personal identifier, then link within the bucket
	idxByID := map[string][]int{}
	for i := 0; i < n; i++ {
		for _, p := range contactPhones[i] {
			if !sharedPhone(p) {
				idxByID["p:"+p] = append(idxByID["p:"+p], i)
			}
		}
		for _, e := range contactEmails[i] {
			if !sharedEmail(e) {
				idxByID["e:"+e] = append(idxByID["e:"+e], i)
			}
		}
	}
	ambSeen := map[[2]int]bool{}
	var ambiguous []AmbiguousPair
	ids := make([]string, 0, len(idxByID))
	for id := range idxByID {
		ids = append(ids, id)
	}
	sort.Strings(ids) // deterministic order
	for _, id := range ids {
		idxs := idxByID[id]
		base := idxs[0]
		for _, j := range idxs[1:] {
			switch {
			case namesCompatible(contacts[base].Name, contacts[j].Name):
				uf.union(base, j)
			case dedupKey(contacts[base].Name) == "" || dedupKey(contacts[j].Name) == "":
				// a title-only card ("부장") — leave it apart; never a bridge
			default:
				ra, rb := uf.find(base), uf.find(j)
				if ra == rb {
					continue
				}
				key := [2]int{min(ra, rb), max(ra, rb)}
				if !ambSeen[key] {
					ambSeen[key] = true
					ambiguous = append(ambiguous, AmbiguousPair{A: base, B: j, Shared: id[2:]})
				}
			}
		}
	}

	merges, distinct := mergeGroupsFromClusters(contacts, uf)
	res.Merges = merges
	res.Ambiguous = ambiguous
	res.Distinct = distinct
	return res
}

// unionFind is the disjoint-set forest shared by the deterministic Dedup pass and
// the adjudicated Resolve.
type unionFind struct{ parent []int }

func newUnionFind(n int) *unionFind {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	return &unionFind{parent: p}
}

func (u *unionFind) find(x int) int { // iterative (path-halving), not recursive
	for u.parent[x] != x {
		u.parent[x] = u.parent[u.parent[x]]
		x = u.parent[x]
	}
	return x
}

func (u *unionFind) union(a, b int) { u.parent[u.find(a)] = u.find(b) }

// mergeGroupsFromClusters turns the current union-find state into sorted
// MergeGroups (only clusters of ≥2) plus the distinct-person count over all n.
func mergeGroupsFromClusters(contacts []Contact, uf *unionFind) ([]MergeGroup, int) {
	groups := map[int][]int{}
	for i := range contacts {
		r := uf.find(i)
		groups[r] = append(groups[r], i)
	}
	var merges []MergeGroup
	for _, members := range groups {
		if len(members) < 2 {
			continue
		}
		sort.Ints(members)
		merges = append(merges, MergeGroup{Members: members, Canonical: canonicalName(contacts, members)})
	}
	sort.Slice(merges, func(i, j int) bool { return merges[i].Members[0] < merges[j].Members[0] })
	return merges, len(groups)
}

// canonicalName picks the cleanest label for a merged group: the shortest
// non-empty normalized key's own value, so "박한주" wins over "#박한주 부장(솔라테크)".
func canonicalName(contacts []Contact, members []int) string {
	best := ""
	bestLen := 1 << 30
	for _, i := range members {
		k := dedupKey(contacts[i].Name)
		if k == "" {
			continue
		}
		if l := len([]rune(k)); l < bestLen {
			best, bestLen = k, l
		}
	}
	if best == "" {
		return contacts[members[0]].Name
	}
	return best
}
