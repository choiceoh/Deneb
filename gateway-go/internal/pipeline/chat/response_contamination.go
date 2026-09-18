// response_contamination.go is the product-side guard for the 2026-09-17
// response-contamination class (Deneb stream_0043): a Korean answer that
// collapses mid-way into document continuation — transcript rows, our own
// prompt envelopes, chat-template special tokens, tool-call markup. The engine
// was cleared bit-for-bit; the model had drifted into "continue the document"
// mode. The operator caught it by eye. Nothing in the pipeline measured it,
// and the raw text went into the transcript, where the next turn would have
// read it as the assistant's own prior words (self-conditioning).
//
// The guard runs ONCE, right after the agent loop returns and before delivery,
// persistence and the complete event (run_exec.go), so all three agree:
//
//   - detection: HARD markers a user-facing answer must never contain —
//     chat-template special tokens, the envelopes and tail headers this
//     gateway itself injects, and transcript-row shapes at line start
//     (`[ctx] [assistant]`, `**[user]**`, `### Session:`, `source=session
//     ref=`). Matches inside fenced code blocks are ignored: an operator can
//     legitimately ask the model to show these formats verbatim.
//   - salvage: cut the answer at the first marker's line start when a real
//     Korean prefix survives, and say so in one line; otherwise replace the
//     whole answer with the same-tone fallback the empty-turn path uses.
//   - telemetry: `run.contamination` agentlog event + a warn log line with
//     the marker, offset, Hangul ratio and the soft collapse position, so the
//     class is counted instead of noticed.
//
// A SOFT signal — the Hangul share of the prose dropping from ≥50% to <20%
// across the answer — is logged only, never acted on: English-heavy answers
// (code review, a quoted email) are legitimate.
package chat

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"unicode"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
)

// contaminationMarkers may appear anywhere in the answer. Special tokens and
// the gateway's own injected structure are never legitimate answer text.
var contaminationMarkers = []string{
	"<|assistant|>", "<|user|>", "<|system|>", "<|observation|>", "<|im_start|>", "<|im_end|>", "<|end|>", "[gMASK]<sop>",
	"<transcript-excerpt", "</transcript-excerpt>", "<recall-context", "</recall-context>",
	"[전달 정책 — 이번 턴]", "[응답 언어·모드 — 이번 턴]", "## 회상 근거 (자동 검색)",
}

// contaminationRowMarkers are transcript-row shapes; they count only at the
// start of a line (after list/index decoration), which is where every renderer
// in this gateway puts them and where a continued transcript puts them too.
var contaminationRowMarkers = regexp.MustCompile(`(?m)^[ \t]*(?:[-*]|\d+\.)?[ \t]*(\[ctx\] \[(?:assistant|user)\]|\*\*\[(?:assistant|user)\]\*\*|### Session: |- source=session ref=")`)

var fencedCodeBlock = regexp.MustCompile("(?s)```.*?```")

// contaminationTrailer is appended to a salvaged (cut) answer.
const contaminationTrailer = "(응답 뒷부분이 손상돼 잘라냈어요. 이어서 물어보시면 계속할게요.)"

// contaminationFallback replaces an answer with no salvageable Korean prefix.
const contaminationFallback = "모델 응답이 손상돼 전달하지 못했어요. 다시 한 번 요청해 주세요."

// contaminationMinHangul is the Korean content a prefix needs to be worth
// delivering on its own instead of the fallback.
const contaminationMinHangul = 10

type contaminationReport struct {
	Marker      string  // the hard marker found ("" = none)
	Offset      int     // byte offset of the marker
	HangulRatio float64 // Hangul share of letters outside code fences (whole answer)
	CollapseAt  int     // soft signal: byte offset where Korean prose collapsed, -1 = none
}

func (r contaminationReport) hard() bool { return r.Marker != "" }

// detectContamination scans answer text for the hard markers (outside fenced
// code) and computes the soft signals.
func detectContamination(text string) contaminationReport {
	rep := contaminationReport{Offset: -1, CollapseAt: -1}
	if text == "" {
		return rep
	}
	fences := fencedCodeBlock.FindAllStringIndex(text, -1)
	inFence := func(off int) bool {
		for _, f := range fences {
			if off >= f[0] && off < f[1] {
				return true
			}
		}
		return false
	}
	best := -1
	for _, m := range contaminationMarkers {
		from := 0
		for {
			i := strings.Index(text[from:], m)
			if i < 0 {
				break
			}
			off := from + i
			if !inFence(off) {
				if best < 0 || off < best {
					best, rep.Marker = off, m
				}
				break
			}
			from = off + len(m)
		}
	}
	for _, loc := range contaminationRowMarkers.FindAllStringSubmatchIndex(text, -1) {
		off := loc[2] // start of the captured row marker
		if inFence(off) {
			continue
		}
		if best < 0 || off < best {
			best, rep.Marker = off, text[loc[2]:loc[3]]
		}
	}
	rep.Offset = best
	prose := fencedCodeBlock.ReplaceAllString(text, " ")
	rep.HangulRatio = hangulShare(prose)
	rep.CollapseAt = hangulCollapseAt(prose)
	return rep
}

// hangulShare is Hangul syllables / (Hangul + Latin letters); 0 when neither.
func hangulShare(s string) float64 {
	h, l := countHangulLatin(s)
	if h+l == 0 {
		return 0
	}
	return float64(h) / float64(h+l)
}

func countHangulLatin(s string) (hangul, latin int) {
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Hangul, r):
			hangul++
		case r < 0x80 && unicode.IsLetter(r):
			latin++
		}
	}
	return hangul, latin
}

// hangulCollapseAt walks 200-rune windows in 50-rune steps and reports the
// first window that falls below 20% Hangul after the prose had been ≥50%.
func hangulCollapseAt(s string) int {
	runes := []rune(s)
	seenKorean := false
	for i := 0; i+200 <= len(runes) || (i == 0 && len(runes) > 0); i += 50 {
		end := i + 200
		if end > len(runes) {
			end = len(runes)
		}
		h, l := countHangulLatin(string(runes[i:end]))
		if h+l < 40 {
			if end == len(runes) {
				break
			}
			continue
		}
		ratio := float64(h) / float64(h+l)
		if ratio >= 0.5 {
			seenKorean = true
		} else if seenKorean && ratio < 0.2 {
			return len(string(runes[:i]))
		}
		if end == len(runes) {
			break
		}
	}
	return -1
}

// salvageContaminatedText returns the deliverable text for a hard-marker
// report: the Korean prefix before the marker's line plus the trailer, or the
// fallback when nothing worth reading precedes the marker.
func salvageContaminatedText(text string, rep contaminationReport) string {
	if !rep.hard() {
		return text
	}
	cut := rep.Offset
	if nl := strings.LastIndex(text[:cut], "\n"); nl >= 0 {
		cut = nl
	} else {
		cut = 0
	}
	prefix := trimNonKoreanTail(strings.TrimSpace(text[:cut]))
	if h, _ := countHangulLatin(prefix); h < contaminationMinHangul {
		return contaminationFallback
	}
	return prefix + "\n\n" + contaminationTrailer
}

// trimNonKoreanTail drops trailing lines that carry Latin prose but no Hangul
// at all. In the incident the language collapsed BEFORE the first marker
// ("Following the user request I will now summarize." then the transcript
// rows), so the line the marker sits on is not where the damage began. This
// runs only once a hard marker has been found, so a legitimate English closing
// line in a clean answer is never touched; lines without letters (URLs, table
// rules, numbers) are kept as-is once a Korean line is reached.
func trimNonKoreanTail(prefix string) string {
	lines := strings.Split(prefix, "\n")
	end := len(lines)
	for end > 0 {
		line := strings.TrimSpace(lines[end-1])
		if line == "" {
			end--
			continue
		}
		h, l := countHangulLatin(line)
		if h == 0 && l >= 3 {
			end--
			continue
		}
		break
	}
	return strings.TrimSpace(strings.Join(lines[:end], "\n"))
}

// salvageContaminatedResult applies the guard to the agent result in place:
// Text (the channel reply), AllText (transcript persistence) and
// DeliverableText (proactive delivery) end up telling the same story. AllText
// and DeliverableText are accumulations whose tail is the final turn's Text,
// so the cut is applied to that tail; a marker in an EARLIER turn's narration
// is left alone (cutting there would drop a clean final answer).
func salvageContaminatedResult(res *agent.AgentResult, deps runDeps, params RunParams, logger *slog.Logger) {
	if res == nil || res.Text == "" {
		return
	}
	rep := detectContamination(res.Text)
	if !rep.hard() && rep.CollapseAt < 0 {
		return
	}
	salvaged := false
	if rep.hard() {
		clean := salvageContaminatedText(res.Text, rep)
		res.AllText = replaceFinalTurnText(res.AllText, res.Text, clean)
		res.DeliverableText = replaceFinalTurnText(res.DeliverableText, res.Text, clean)
		res.Text = clean
		salvaged = true
	}
	if logger != nil {
		logger.Warn("response contamination detected",
			"session", params.SessionKey, "marker", rep.Marker, "offset", rep.Offset,
			"hangulRatio", fmt.Sprintf("%.2f", rep.HangulRatio), "collapseAt", rep.CollapseAt, "salvaged", salvaged)
	}
	agentlog.LogTyped(deps.agentLog, params.SessionKey, "run.contamination", map[string]any{
		"marker":      rep.Marker,
		"offset":      rep.Offset,
		"hangulRatio": rep.HangulRatio,
		"collapseAt":  rep.CollapseAt,
		"salvaged":    salvaged,
		"stopReason":  res.StopReason,
		"turns":       res.Turns,
	})
}

// replaceFinalTurnText swaps the final turn's text inside an accumulation.
// When the accumulation does not end with the exact final text (the
// deliverable strips a narration head), the marker is located inside the
// final-turn region instead; a marker before that region is not touched.
func replaceFinalTurnText(acc, finalText, clean string) string {
	if acc == "" {
		return acc
	}
	if strings.HasSuffix(acc, finalText) {
		return acc[:len(acc)-len(finalText)] + clean
	}
	rep := detectContamination(acc)
	if rep.hard() && rep.Offset >= len(acc)-len(finalText) {
		return salvageContaminatedText(acc, rep)
	}
	return acc
}
