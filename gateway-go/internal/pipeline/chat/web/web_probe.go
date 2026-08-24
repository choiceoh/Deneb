// web_probe.go — Check one claim against a page without reading the page.
//
// "Does this changelog mention the 3.2 release?" costs a full fetch today: a few
// thousand tokens of page to answer a yes/no. Probe fetches the same page but
// returns only a verdict and the sentence that supports it, so the check costs
// tens of tokens instead of thousands.
//
// A probe never guesses. It reports what the page says about the claim's terms,
// not whether the claim is true — the model draws that conclusion from the
// excerpt. A page that says nothing about the terms is reported as such, which
// is a real answer and not an error.
package web

import (
	"fmt"
	"strings"
)

// probeExcerptChars is the excerpt budget. Enough for the sentence that carries
// the answer plus its neighbours; far below the cost of the page.
const probeExcerptChars = 400

// formatProbeResult renders the compact probe envelope.
func formatProbeResult(url, claim, excerpt string, found bool) string {
	var b strings.Builder
	b.WriteString("<probe>\n")
	fmt.Fprintf(&b, "URL: %s\n", url)
	fmt.Fprintf(&b, "Claim: %s\n", claim)
	if !found {
		b.WriteString("Verdict: no-mention\n")
		b.WriteString("Note: the page does not discuss these terms. This is not a fetch failure — re-fetch without probe to read the page.\n")
		b.WriteString("</probe>")
		return b.String()
	}
	b.WriteString("Verdict: mentioned\n<excerpt>\n")
	b.WriteString(excerpt)
	b.WriteString("\n</excerpt>\n</probe>")
	return b.String()
}

// probeContent narrows already-fetched content to what bears on claim.
func probeContent(url, claim, content string) string {
	body := extractEnvelopeContent(content)
	if strings.TrimSpace(body) == "" {
		return formatProbeResult(url, claim, "", false)
	}
	if excerpt, ok := focusExcerpt(body, claim, probeExcerptChars); ok {
		return formatProbeResult(url, claim, strings.TrimSpace(excerpt.Text), true)
	}
	// The page may be short enough that focusExcerpt declined (already within
	// budget). Then the question is simply whether the terms appear at all.
	if countMatches(body, focusTokenSet(claim)) >= minFocusScore {
		return formatProbeResult(url, claim, strings.TrimSpace(truncateProbeBody(body)), true)
	}
	return formatProbeResult(url, claim, "", false)
}

// truncateProbeBody keeps a short head of a page that fit within the focus
// budget, so a "mentioned" verdict still carries evidence.
func truncateProbeBody(body string) string {
	if len(body) <= probeExcerptChars {
		return body
	}
	cut, _ := truncateAtSection(body, probeExcerptChars)
	return cut
}

// extractEnvelopeContent pulls the <content> body out of a formatted fetch
// result, or returns the input unchanged when it is not an envelope (errors and
// the adapter formats).
func extractEnvelopeContent(result string) string {
	start := strings.Index(result, "<content>\n")
	if start < 0 {
		return result
	}
	body := result[start+len("<content>\n"):]
	return strings.TrimSuffix(body, "\n</content>")
}
