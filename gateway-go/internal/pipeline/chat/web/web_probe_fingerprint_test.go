package web

import (
	"strings"
	"testing"
)

const probeTestPage = `# Deneb changelog

## 3.1

Fixed the calendar rollover.

## 3.2

Added the browser sidecar and per-domain tier memory.
`

func envelopeOf(body string) string {
	return "<metadata>\nURL: https://example.com/changelog\n</metadata>\n<content>\n" + body + "\n</content>"
}

// A yes/no question about a page should cost a yes/no answer, not the page.
func TestProbeReturnsAVerdictAndItsEvidence(t *testing.T) {
	out := probeContent("https://example.com/changelog", "browser sidecar", envelopeOf(probeTestPage))

	if !strings.Contains(out, "Verdict: mentioned") {
		t.Fatalf("probe missed a term the page states:\n%s", out)
	}
	if !strings.Contains(out, "browser sidecar") {
		t.Fatalf("verdict carries no evidence:\n%s", out)
	}
	// The point of the mode, measured where it matters: on a page of real size
	// the answer stays bounded instead of scaling with the document.
	big := probeTestPage + strings.Repeat("\n## Filler\n\nUnrelated release notes paragraph.\n", 400)
	bigOut := probeContent("https://example.com/changelog", "browser sidecar", envelopeOf(big))
	if !strings.Contains(bigOut, "Verdict: mentioned") {
		t.Fatalf("probe lost the term in a large page:\n%s", bigOut)
	}
	if len(bigOut) > probeExcerptChars+400 {
		t.Fatalf("probe grew with the page: %d chars for a %d char page", len(bigOut), len(big))
	}
	if len(bigOut) >= len(big)/4 {
		t.Fatalf("probe was not materially cheaper than the page (%d vs %d)", len(bigOut), len(big))
	}
}

// "Not mentioned" is an answer. Reporting it as an error would push the model
// into a pointless retry.
func TestProbeReportsAbsenceAsAnAnswerNotAFailure(t *testing.T) {
	out := probeContent("https://example.com/changelog", "판매 실적 그래프", envelopeOf(probeTestPage))

	if !strings.Contains(out, "Verdict: no-mention") {
		t.Fatalf("absence was not reported as a verdict:\n%s", out)
	}
	if strings.Contains(out, "<error>") {
		t.Fatalf("absence was dressed up as an error:\n%s", out)
	}
	if !strings.Contains(out, "re-fetch without probe") {
		t.Fatalf("no way out offered to the model:\n%s", out)
	}
}

// A first visit has nothing to compare against and must stay silent rather than
// claiming the page is new-and-changed.
func TestFingerprintSaysNothingOnAFirstVisit(t *testing.T) {
	pageFingerprints = newFingerprintMemory("")

	if got := changeSummary("https://example.com/a", probeTestPage, 20000); got != "" {
		t.Fatalf("first visit reported a change: %q", got)
	}
}

func TestFingerprintNamesTheSectionsThatMoved(t *testing.T) {
	pageFingerprints = newFingerprintMemory("")
	url := "https://example.com/b"
	changeSummary(url, probeTestPage, 20000)

	// Same page, one section edited and one added.
	edited := strings.Replace(probeTestPage, "Fixed the calendar rollover.", "Fixed the calendar rollover and the midnight bug.", 1)
	edited += "\n## 3.3\n\nWayback fallback.\n"

	got := changeSummary(url, edited, 20000)
	if !strings.Contains(got, "3.1") {
		t.Fatalf("edited section not reported: %q", got)
	}
	if !strings.Contains(got, "3.3 (new)") {
		t.Fatalf("added section not reported: %q", got)
	}
	if strings.Contains(got, "3.2") {
		t.Fatalf("untouched section reported as changed: %q", got)
	}
}

func TestFingerprintIgnoresWhitespaceReflow(t *testing.T) {
	pageFingerprints = newFingerprintMemory("")
	url := "https://example.com/c"
	changeSummary(url, probeTestPage, 20000)

	// Same words, rewrapped — a rendering difference, not an edit.
	reflowed := strings.ReplaceAll(probeTestPage, "Added the browser sidecar and per-domain tier memory.",
		"Added the browser sidecar\nand per-domain tier   memory.")

	if got := changeSummary(url, reflowed, 20000); got != "Changed: none since last fetch" {
		t.Fatalf("reflow was reported as an edit: %q", got)
	}
}

// Fingerprints must survive a gateway restart: the comparison that matters
// happens hours or days later, across the restarts every deploy causes. An
// in-memory-only store would be empty at exactly the moment it should answer.
func TestFingerprintsSurviveARestart(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/fingerprints.json"

	first := newFingerprintMemory(path)
	if _, _, isFirst := first.compare("https://example.com/d", probeTestPage, 20000); !isFirst {
		t.Fatal("a fresh store should report a first visit")
	}

	// A new process reading the same file.
	restarted := newFingerprintMemory(path)
	edited := strings.Replace(probeTestPage, "Fixed the calendar rollover.", "Fixed the calendar rollover twice.", 1)
	changed, _, isFirst := restarted.compare("https://example.com/d", edited, 20000)
	if isFirst {
		t.Fatal("the restarted store lost what it had recorded")
	}
	if len(changed) != 1 || !strings.Contains(changed[0], "3.1") {
		t.Fatalf("edit not detected across restart: %v", changed)
	}
}

// The same URL fetched under a different maxChars saw a different slice of the
// page, so its sections are not comparable. Reporting them as changes is how a
// live run produced "(new)" for every section on an unchanged page.
func TestFingerprintDoesNotCompareAcrossBudgets(t *testing.T) {
	m := newFingerprintMemory("")
	if _, _, first := m.compare("https://example.com/e", probeTestPage, 20000); !first {
		t.Fatal("first visit should report nothing")
	}

	// Same page, smaller budget → a truncated extraction with fewer sections.
	shorter := strings.Split(probeTestPage, "## 3.2")[0]
	changed, _, first := m.compare("https://example.com/e", shorter, 2000)

	if !first {
		t.Fatalf("a different budget was compared anyway, reporting: %v", changed)
	}
	if len(changed) != 0 {
		t.Fatalf("spurious changes across budgets: %v", changed)
	}

	// And the new budget becomes the baseline, so the NEXT fetch at that budget
	// does compare.
	if _, _, first := m.compare("https://example.com/e", shorter, 2000); first {
		t.Fatal("the new budget was not recorded as the baseline")
	}
}
