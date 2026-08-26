package chat

import "testing"

// A card-authoring skill proves its procedure by emitting the card, not by
// calling a tool — the class exercise_tools cannot reach (ADR 0006).
func TestOutputEvidenceMatchesFence(t *testing.T) {
	answer := "요청하신 현황입니다.\n\n```deneb-ui\n<column><stat …/></column>\n```\n"
	if !matchesSkillOutputEvidence(answer, []string{"fence:deneb-ui"}) {
		t.Fatal("emitted card not recognized as evidence")
	}
}

// Prose ABOUT the artifact is instruction, not output: matching it would call
// an ignored procedure exercised, which is worse than leaving it unmeasured.
func TestOutputEvidenceIgnoresFenceMentionedInProse(t *testing.T) {
	answer := "카드로 보여드리려면 ```deneb-ui 로 감싸야 합니다. 이번엔 글로 정리했습니다."
	if matchesSkillOutputEvidence(answer, []string{"fence:deneb-ui"}) {
		t.Fatal("inline mention counted as an emitted card")
	}
}

func TestOutputEvidenceFenceAcceptsInfoStringAttributes(t *testing.T) {
	answer := "```deneb-ui theme=dark\n<column/>\n```"
	if !matchesSkillOutputEvidence(answer, []string{"fence:deneb-ui"}) {
		t.Fatal("info-string attributes broke the match")
	}
}

func TestOutputEvidenceMatchesHeadingIgnoringLevelAndSpacing(t *testing.T) {
	answer := "###   사전부검   결과\n\n내용"
	if !matchesSkillOutputEvidence(answer, []string{"heading:사전부검 결과"}) {
		t.Fatal("heading normalization failed")
	}
	if matchesSkillOutputEvidence(answer, []string{"heading:권고"}) {
		t.Fatal("unrelated heading matched")
	}
}

// A heading named only in a body line is not a heading.
func TestOutputEvidenceHeadingRequiresAHeadingLine(t *testing.T) {
	answer := "사전부검 결과를 아래에 적습니다."
	if matchesSkillOutputEvidence(answer, []string{"heading:사전부검 결과"}) {
		t.Fatal("plain sentence matched a heading pattern")
	}
}

func TestOutputEvidenceEmptyAnswerOrPatternIsNoEvidence(t *testing.T) {
	if matchesSkillOutputEvidence("", []string{"fence:deneb-ui"}) {
		t.Fatal("empty answer produced evidence")
	}
	if matchesSkillOutputEvidence("```deneb-ui\n```", []string{"", "fence:"}) {
		t.Fatal("empty pattern produced evidence")
	}
	if matchesSkillOutputEvidence("아무 내용", nil) {
		t.Fatal("no declared patterns produced evidence")
	}
}
