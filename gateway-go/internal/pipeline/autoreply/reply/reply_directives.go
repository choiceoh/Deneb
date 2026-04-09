// reply_directives.go — Reply directive processing (media splitting, threading, silent).
package reply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/core/coreparsing/mediatokens"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/tokens"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// splitMediaFromOutput extracts MEDIA: tokens from output text via the
// mediatokens package (pure Go implementation).
func splitMediaFromOutput(raw string) (text string, mediaURLs []string, mediaURL string, audioAsVoice bool) {
	r := mediatokens.Parse(raw)
	var primary string
	if len(r.MediaURLs) > 0 {
		primary = r.MediaURLs[0]
	}
	return r.Text, r.MediaURLs, primary, r.AudioAsVoice
}

// ParseReplyDirectives parses reply directives from raw agent output text.
// Extracts MEDIA: tokens, threading tags, and silent tokens.
func ParseReplyDirectives(raw, currentMessageID, silentToken string) chatport.ReplyDirectives {
	text, mediaURLs, mediaURL, audioAsVoice := splitMediaFromOutput(raw)

	// Strip leaked tool-call markup that some models emit as text.
	text = StripLeakedToolCallMarkup(text)

	// Extract reply threading tags.
	replyToID, replyToCurrent := tokens.ApplyReplyThreading(text, "")
	hasReplyTag := replyToCurrent || replyToID != ""

	if hasReplyTag {
		text = tokens.StripReplyTags(text)
	}

	if replyToCurrent && currentMessageID != "" {
		replyToID = currentMessageID
	}

	if silentToken == "" {
		silentToken = tokens.SilentReplyToken
	}
	isSilent := tokens.IsSilentReplyText(text, silentToken)
	if isSilent {
		text = ""
	}

	return chatport.ReplyDirectives{
		Text:           text,
		MediaURLs:      mediaURLs,
		MediaURL:       mediaURL,
		ReplyToID:      replyToID,
		ReplyToCurrent: replyToCurrent,
		ReplyToTag:     hasReplyTag,
		AudioAsVoice:   audioAsVoice,
		IsSilent:       isSilent,
	}
}
