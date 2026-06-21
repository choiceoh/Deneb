package wiki

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

// Code assignment for dreamer filings. The frozen project code lives in page
// frontmatter (Frontmatter.Code) and is the move-stable identity. Two ways a
// filed page acquires one:
//
//  1. Folder inheritance (the common case): a new page filed under an existing
//     project folder inherits that project's code. No judgment needed — once the
//     migration stamps each project's entity page, every child mail/log under the
//     same folder is coded automatically.
//
//  2. LLM mint (new project): the dreamer proposes a dept-client-dtype stem; Go
//     validates the dept/dtype against the fixed vocabularies and assigns the next
//     free sequence in that namespace so two new projects never collide.

// codeIndex is the per-apply snapshot of which codes already exist: folder→code
// for inheritance, and the highest sequence seen per (dept-client-dtype)
// namespace for collision-free minting.
type codeIndex struct {
	folderCode map[string]string // project folder relPath -> code
	maxSeq     map[string]int    // "dept-client-dtype" -> highest 순번 seen
}

// buildCodeIndex scans existing pages once so filings can inherit or mint codes.
func (wd *WikiDreamer) buildCodeIndex() codeIndex {
	ci := codeIndex{folderCode: map[string]string{}, maxSeq: map[string]int{}}
	rels, err := wd.store.ListPages("프로젝트")
	if err != nil {
		return ci
	}
	for _, rp := range rels {
		page, perr := wd.store.ReadPage(rp)
		if perr != nil || page == nil || page.Meta.Code == "" {
			continue
		}
		code := page.Meta.Code
		dir := path.Dir(rp)
		// Prefer an entity page's code when a folder carries several; otherwise
		// first-seen wins.
		if _, ok := ci.folderCode[dir]; !ok || page.Meta.Type == "entity" {
			ci.folderCode[dir] = code
		}
		if stem, seq, ok := splitCode(code); ok {
			if seq > ci.maxSeq[stem] {
				ci.maxSeq[stem] = seq
			}
		}
	}
	return ci
}

// resolveCode returns the code a filing should carry, and records any mint in the
// index so later filings in the same batch stay collision-free. Only project
// pages (filed under 프로젝트/) are coded; everything else returns "".
func (ci *codeIndex) resolveCode(u wikiUpdate) string {
	if !strings.HasPrefix(u.Path, "프로젝트/") {
		return ""
	}
	dir := path.Dir(u.Path)
	// 1. Inherit from the project folder.
	if code, ok := ci.folderCode[dir]; ok {
		return code
	}
	// 2. Mint from an LLM-proposed dept-client-dtype stem (seq assigned by Go).
	stem := codeStem(u.Code)
	if stem == "" {
		return ""
	}
	seq := ci.maxSeq[stem] + 1
	ci.maxSeq[stem] = seq
	code := fmt.Sprintf("%s-%03d", stem, seq)
	ci.folderCode[dir] = code // a sibling filed later inherits the fresh code
	return code
}

// splitCode splits "dept-client-dtype-NNN" into its stem ("dept-client-dtype")
// and integer sequence. Reports false when s is not a valid project code.
func splitCode(s string) (stem string, seq int, ok bool) {
	if !ValidProjectCode(s) {
		return "", 0, false
	}
	i := strings.LastIndex(s, "-")
	n, err := strconv.Atoi(s[i+1:])
	if err != nil {
		return "", 0, false
	}
	return s[:i], n, true
}

// codeStem normalizes an LLM-proposed code to its "dept-client-dtype" stem,
// validating the dept and dtype against the fixed vocabularies. Accepts a full
// code (sequence dropped — Go owns the sequence) or a bare stem. Returns "" when
// malformed or using an unknown dept/dtype.
func codeStem(raw string) string {
	s := normalizeProjectCode(raw)
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "-")
	if len(parts) == 4 {
		parts = parts[:3] // drop any LLM-proposed sequence; Go assigns it
	}
	if len(parts) != 3 {
		return ""
	}
	dept, client, dtype := parts[0], parts[1], parts[2]
	if _, ok := DeptCodes[dept]; !ok {
		return ""
	}
	if _, ok := DealTypeCodes[dtype]; !ok {
		return ""
	}
	if !is3charSeg(dept) || !is3charSeg(client) || !is3charSeg(dtype) {
		return ""
	}
	return dept + "-" + client + "-" + dtype
}

// is3charSeg reports whether seg is exactly three [a-z0-9] characters.
func is3charSeg(seg string) bool {
	if len(seg) != 3 {
		return false
	}
	for _, c := range seg {
		if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
