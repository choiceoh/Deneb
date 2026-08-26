// skill_hint_scope.go — what part of a user message counts as the user's ASK.
//
// Trigger matching used to scan the whole user message, but a native-client
// message is frequently a thin wrapper around a large machine-inserted payload:
// an attachment listing, OCR text, a recording transcript, a work-area dump, a
// pasted work item. A trigger word inside that payload says nothing about what
// the operator wants — and firing on it injects a multi-thousand-token
// procedure that then rides the rest of the session (tail_register.go).
//
// Measured on 30 days of real messages (2026-08-26): 86 trigger fires, 49 of
// them (57%) matched inside such a payload — contract-review 25 of 28, and its
// body is 2,051 tokens. Skills whose triggers only ever matched the operator's
// own typing (deneb-ui-authoring, fact-check, kb-interview) had ZERO payload
// fires, which is what makes the boundary the right cut.
//
// The rule is deliberately one-directional: keep everything BEFORE the first
// payload marker. The share flow writes the operator's own note (📲 공유 맥락)
// ahead of the payload, so their words survive; nothing after the marker is
// theirs to begin with.
package chat

import (
	"regexp"
	"strings"
)

// skillHintPayloadMarkers begin a machine-inserted block inside a user message.
// Sources: miniapp_bridge.go (share/capture/OCR/recording composition) and the
// native client's work-item wrappers.
var skillHintPayloadMarkers = []string{
	"📎 첨부", // attachment listing (miniapp_bridge.go)
	"📄 공유 문서에서 추출한 텍스트",  // shared document text
	"📷 공유 이미지에서 추출한 텍스트", // OCR
	"🎙️ 공유 녹음을 받아썼습니다",   // recording transcript
	"🎙 공유 녹음을 받아썼습니다",    // same, without the variation selector
	"[작업 영역",         // work-area dump
	"[thinking]",     // pasted reasoning block
	"[피드 ",           // feed dump
	"[메일 ",           // mail-list dump
	"[작업피드",          // work-feed dump
	"이 업무 리포트를 바탕으로", // client work-item wrapper
	"이 업무 항목을 열었어",   // client work-item wrapper
}

// speakerTranscriptRe matches a diarized transcript line ("00:00:05 Speaker 1").
var speakerTranscriptRe = regexp.MustCompile(`(?m)^\s*\d{2}:\d{2}:\d{2}\s+Speaker\b`)

// skillTriggerScope returns the part of message that is the operator's own ask:
// everything before the first machine-inserted payload. A message that is
// nothing but payload yields "", which matches no trigger.
func skillTriggerScope(message string) string {
	cut := len(message)
	for _, marker := range skillHintPayloadMarkers {
		if i := strings.Index(message, marker); i >= 0 && i < cut {
			cut = i
		}
	}
	if loc := speakerTranscriptRe.FindStringIndex(message); loc != nil && loc[0] < cut {
		cut = loc[0]
	}
	return strings.TrimSpace(message[:cut])
}
