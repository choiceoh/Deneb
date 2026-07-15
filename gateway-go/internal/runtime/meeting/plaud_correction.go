// plaud_correction.go — Plaud ASR correction assets (glossary + correction prompt).
//
// Meeting synthesis used to bury correction rules inside the report prompt and
// lean only on the lean 업무 topic block (~2KB). That under-corrected proper
// nouns. These two workspace files are the durable surface:
//
//	topics/plaud-glossary.md        — canonical terms / 원문→교정 pairs
//	topics/plaud-correction.md      — how to apply the glossary + unit judgment
//	topics/plaud-do-not-correct.md  — forbidden false corrections (≠)
//
// Files are optional; missing correction prompt falls back to the embedded
// default. Glossary slicing / promotion live in plaud_glossary.go.
package meeting

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	// PlaudGlossaryFile is the workspace-relative filename under topics/.
	PlaudGlossaryFile = "plaud-glossary.md"
	// PlaudCorrectionPromptFile is the correction-instruction filename under topics/.
	PlaudCorrectionPromptFile = "plaud-correction.md"

	// Caps keep the synthesis prompt bounded (topic knowledge is ~2KB;
	// glossary can grow denser — still well under a single context slice).
	plaudGlossaryMaxRunes         = 12_000
	plaudCorrectionPromptMaxRunes = 8_000
)

// plaudMeetingReportPrompt is the stable output contract for wiki/feed parsing
// (관련프로젝트 line). Kept in Go — not operator-editable — so format drift
// cannot break landing.
const plaudMeetingReportPrompt = `당신은 업무 회의록 분석가다. 회의 전사를 읽고 한국어 회의록을 작성한다.
전사의 명백한 오인식은 위 교정 지침·용어집·배경지식으로 고쳐 본문에 반영한다.

출력 형식 (마크다운, 이 구조 그대로):
## 요약
- (3~6개 불릿 — 무슨 회의였고 무엇이 진행됐는지)
## 결정사항
- (확정된 결정만. 없으면 "- 없음")
## 액션 아이템
- (담당자/기한이 언급됐으면 함께. 없으면 "- 없음")
## 리스크·미해결
- (걸림돌, 다음 회의로 넘어간 쟁점. 없으면 "- 없음")
## 표기 교정
- (본문에 반영한 대표 교정 5~15개: "원문 → 교정" 꼴. 실제로 표기가 달라진 항목만 —
  원문과 교정문이 같은 항목·"유지/확인" 항목은 넣지 마라. 없으면 "- 없음")

규칙:
- 전사에 있는 내용만 쓴다. 추측은 "(추정)"을 붙인다.
- 발언자 이름이 식별되면 결정/액션에 이름을 남긴다.
- 마지막 줄에 정확히 한 줄: "관련프로젝트: <아래 후보 목록의 path를 쉼표로, 해당 없으면 없음>"
- 관련프로젝트는 후보 목록에 있는 path만 쓸 수 있다.`

// defaultPlaudCorrectionPrompt is used when topics/plaud-correction.md is
// missing or empty. Keep in sync with docs/reference/templates/topics/plaud-correction.md.
const defaultPlaudCorrectionPrompt = `당신은 태양광·EPC 업무 회의 전사의 ASR 교정기다.
전사는 음성인식 결과라 오인식이 섞여 있다. 아래 용어집과 배경지식을 우선 적용해
명백한 오인식만 고친다. 의미·사실·숫자는 추측으로 바꾸지 않는다.

## 교정 원칙
- 용어집에 "원문 → 교정"이 있으면 그대로 적용한다.
- 용어집의 정규 표기(프로젝트·인명·지명·제품·거래처)가 비슷한 오인식으로 들리면 정규 표기로 교정한다.
- 단위·동음이의: 발전용량은 MW(메가), 철골·자재 수량은 매(장), 중량은 톤, 소형 설비는 kW.
- "교정 금지"에 있는 쌍(≠ 또는 →)은 절대 적용하지 않는다 (예: 오형석 ≠ 오선택, 매≠MW).
- 확신 없으면 원문을 유지한다. 추정 교정은 "표기 교정"에만 "(추정)"을 붙이고 본문에는 넣지 않는다.
- 용어집·배경지식에 없는 일반어 윤문은 하지 않는다.`

// loadPlaudTopicFile reads topicsDir/name, trims, and rune-caps. Empty path or
// missing/empty file → "".
func loadPlaudTopicFile(topicsDir, name string, maxRunes int) string {
	topicsDir = strings.TrimSpace(topicsDir)
	if topicsDir == "" || strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(topicsDir, name))
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return ""
	}
	if maxRunes > 0 && utf8.RuneCountInString(content) > maxRunes {
		content = string([]rune(content)[:maxRunes])
		content = strings.TrimSpace(content)
	}
	return content
}

// LoadPlaudGlossary returns the operator glossary body (may be empty).
func LoadPlaudGlossary(topicsDir string) string {
	return loadPlaudTopicFile(topicsDir, PlaudGlossaryFile, plaudGlossaryMaxRunes)
}

// LoadPlaudCorrectionPrompt returns the correction prompt, or the embedded
// default when the file is absent.
func LoadPlaudCorrectionPrompt(topicsDir string) string {
	if body := loadPlaudTopicFile(topicsDir, PlaudCorrectionPromptFile, plaudCorrectionPromptMaxRunes); body != "" {
		return body
	}
	return defaultPlaudCorrectionPrompt
}
