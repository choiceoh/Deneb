package genesis

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// bridge_surface.go — reject skills that instruct a `deneb.<fn>()` the
// code_action bridge does not expose.
//
// code_action preloads a `deneb` object with a FIXED set of attributes. A skill
// that tells the agent to call something else does not degrade — it raises
// AttributeError the moment the agent follows its own procedure.
//
// Measured 2026-08-26, project-status-briefing carried three such calls:
//
//	deneb.deal_ledger()      ×2 — the bridge exposes deneb.deals()
//	deneb.project_history()  ×1 — it is an ACTION: deneb.mail_archive(action="project_history")
//
// Both mistakes are the same one: a real NAME from a neighbouring vocabulary
// (deal_ledger is a real tool, project_history is a real mail_archive action)
// used as a bridge attribute. Prose cannot tell those apart, and nothing
// checked, so the skill shipped and its own procedure could not run.
//
// This check is deliberately narrow. It is not "does this tool exist" — the
// judge answers that from the tool registry (and rejecting a candidate merely
// for naming a tool absent from the incumbent is the false-reject class fixed
// separately). It is the much smaller question of whether an attribute exists
// on one specific object whose surface is a closed list.

// bridgeSurface is injected, never written down here.
//
// A checked-in copy is how this vocabulary drifts: the copy and the
// implementation get edited by different changes, nothing compares them, and a
// consumer holding the stale copy rejects a correct call or accepts a broken
// one. The first version of this file hardcoded eight names and had already
// missed `gmail` on the day it was written — the failure it was built to catch,
// committed by the guard itself.
//
// codeaction.BridgeSurface() derives the list from the EMBEDDED runtime that
// defines the methods. Wired in at construction; an empty set disables the check
// rather than asserting an empty surface (which would reject every call).
var bridgeSurfaceFn func() []string

// SetBridgeSurface wires the authoritative surface accessor.
func SetBridgeSurface(fn func() []string) { bridgeSurfaceFn = fn }

func bridgeSurfaceSet() map[string]struct{} {
	if bridgeSurfaceFn == nil {
		return nil
	}
	names := bridgeSurfaceFn()
	out := make(map[string]struct{}, len(names))
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			out[n] = struct{}{}
		}
	}
	return out
}

// bridgeCallPattern matches a `deneb.<name>` reference in skill prose.
var bridgeCallPattern = regexp.MustCompile(`deneb\.([a-z_][a-z0-9_]*)`)

// bridgeSurfaceHint points a wrong name at the right one where the confusion is
// known, so a rejection tells the author what to write instead of only what not
// to.
var bridgeSurfaceHint = map[string]string{
	"deal_ledger":     `deneb.deals()`,
	"project_history": `deneb.mail_archive(action="project_history")`,
	"deal":            `deneb.deals()`,
	"deals_ledger":    `deneb.deals()`,
	"contact":         `deneb.contacts()`,
	"calendars":       `deneb.calendar()`,
}

// unknownBridgeCalls returns the `deneb.<fn>` names in body that the bridge does
// not expose, sorted for a stable message.
func unknownBridgeCalls(body string) []string {
	surface := bridgeSurfaceSet()
	if len(surface) == 0 {
		return nil // no authority wired — say nothing rather than reject everything
	}
	seen := map[string]struct{}{}
	for _, m := range bridgeCallPattern.FindAllStringSubmatch(body, -1) {
		name := m[1]
		if _, ok := surface[name]; ok {
			continue
		}
		seen[name] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// bridgeSurfacePreflight rejects a candidate that introduces a `deneb.<fn>` the
// bridge does not expose.
//
// Only NEW names are rejected. A candidate that inherits an existing bad call
// from the incumbent is not made worse by it, and blocking those would freeze
// every skill that already carries one — the repair could never land.
func bridgeSurfacePreflight(originalContent, candidateBody string) (bool, string) {
	inherited := map[string]struct{}{}
	for _, n := range unknownBridgeCalls(originalContent) {
		inherited[n] = struct{}{}
	}
	var added []string
	for _, n := range unknownBridgeCalls(candidateBody) {
		if _, ok := inherited[n]; ok {
			continue
		}
		added = append(added, n)
	}
	if len(added) == 0 {
		return true, ""
	}
	parts := make([]string, 0, len(added))
	for _, n := range added {
		if hint, ok := bridgeSurfaceHint[n]; ok {
			parts = append(parts, fmt.Sprintf("deneb.%s → %s", n, hint))
		} else {
			parts = append(parts, "deneb."+n)
		}
	}
	return false, "code_action 브리지에 없는 함수를 호출한다 (따라 하면 AttributeError): " + strings.Join(parts, ", ")
}
