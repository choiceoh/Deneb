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
	pageFingerprints.pages = map[string][]sectionFingerprint{}
	pageFingerprints.order = nil

	if got := changeSummary("https://example.com/a", probeTestPage); got != "" {
		t.Fatalf("first visit reported a change: %q", got)
	}
}

func TestFingerprintNamesTheSectionsThatMoved(t *testing.T) {
	pageFingerprints.pages = map[string][]sectionFingerprint{}
	pageFingerprints.order = nil
	url := "https://example.com/b"
	changeSummary(url, probeTestPage)

	// Same page, one section edited and one added.
	edited := strings.Replace(probeTestPage, "Fixed the calendar rollover.", "Fixed the calendar rollover and the midnight bug.", 1)
	edited += "\n## 3.3\n\nWayback fallback.\n"

	got := changeSummary(url, edited)
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
	pageFingerprints.pages = map[string][]sectionFingerprint{}
	pageFingerprints.order = nil
	url := "https://example.com/c"
	changeSummary(url, probeTestPage)

	// Same words, rewrapped — a rendering difference, not an edit.
	reflowed := strings.ReplaceAll(probeTestPage, "Added the browser sidecar and per-domain tier memory.",
		"Added the browser sidecar\nand per-domain tier   memory.")

	if got := changeSummary(url, reflowed); got != "Changed: none since last fetch" {
		t.Fatalf("reflow was reported as an edit: %q", got)
	}
}
