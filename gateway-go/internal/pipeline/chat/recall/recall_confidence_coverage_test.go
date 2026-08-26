package recall

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every Kind this package actually produces must have an explicit case in
// recallConfidence. A kind that lands in the default arm inherits a threshold
// derived for nobody: org rows carry a FIXED rank anchor of 0.79, and the
// default arm asks 0.90 for even "medium", so every curated org-chart hit was
// labelled "low" — while diary's bar sat at its own 0.70 source prior and
// labelled every matched row "high". A constant label carries no information
// in either direction.
func TestRecallConfidenceCoversEveryProducedKind(t *testing.T) {
	// superseded is exempt by construction: the marker is stripped before
	// ranking and never rendered, so it has no confidence to report.
	exempt := map[string]bool{"superseded": true}

	produced := map[string]bool{}
	kindLiteral := regexp.MustCompile(`Kind:\s*"([a-z]+)"`)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range kindLiteral.FindAllStringSubmatch(string(raw), -1) {
			produced[m[1]] = true
		}
	}
	if len(produced) == 0 {
		t.Fatal("found no Kind literals; the scan is broken, not the code")
	}

	handled := map[string]bool{}
	raw, err := os.ReadFile("recall_evidence.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	start := strings.Index(body, "func recallConfidence(")
	if start < 0 {
		t.Fatal("recallConfidence not found")
	}
	end := strings.Index(body[start:], "\n}\n")
	if end < 0 {
		t.Fatal("could not delimit recallConfidence")
	}
	caseLiteral := regexp.MustCompile(`"([a-z]+)"`)
	for _, m := range caseLiteral.FindAllStringSubmatch(body[start:start+end], -1) {
		handled[m[1]] = true
	}

	for kind := range produced {
		if exempt[kind] || handled[kind] {
			continue
		}
		t.Errorf("recall kind %q has no case in recallConfidence; it falls into the default arm, "+
			"whose thresholds were derived for other sources", kind)
	}
}

// org rows are a deterministic chart lookup, so they declare their own label
// instead of being read off a fixed score.
func TestOrgEvidenceDeclaresItsOwnConfidence(t *testing.T) {
	member := outputOrgMemberEvidence(
		orgRecallCandidate{kind: orgRecallMemberCandidate, score: recallOrgSourcePrior},
		nil,
	)
	if got := recallConfidence(member); got != "high" {
		t.Errorf("org member confidence = %q, want high", got)
	}
	node := outputOrgNodeEvidence(
		orgRecallCandidate{kind: orgRecallNodeCandidate, score: recallOrgSourcePrior - 0.01},
	)
	if got := recallConfidence(node); got != "medium" {
		t.Errorf("org department confidence = %q, want medium", got)
	}
	// The declared label must survive the score rescaling every row goes
	// through (broadening penalty), which is exactly what a fixed anchor
	// cannot do on its own.
	member.Score *= recallBroadeningPenalty
	if got := recallConfidence(member); got != "high" {
		t.Errorf("org member confidence after broadening penalty = %q, want high", got)
	}
}

// The diary bar must be able to fail. It previously sat at diary's own source
// prior, so no matched row could ever miss it.
func TestDiaryConfidenceBarIsReachableFromBothSides(t *testing.T) {
	weak := recallEvidence{Kind: "diary", Score: 0.70 + 0.10, At: 1}
	if got := recallConfidence(weak); got != "medium" {
		t.Errorf("weak diary match confidence = %q, want medium", got)
	}
	strong := recallEvidence{Kind: "diary", Score: 0.70 + 0.45, At: 1}
	if got := recallConfidence(strong); got != "high" {
		t.Errorf("strong diary match confidence = %q, want high", got)
	}
}
