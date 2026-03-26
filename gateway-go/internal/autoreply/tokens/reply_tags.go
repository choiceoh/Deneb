package tokens

import (
	"regexp"
	"strings"
)

// ReplyTag represents a structured tag extracted from reply text.
// Tags use the format [[tag_name]] or [[tag_name:value]].
type ReplyTag struct {
	Name  string
	Value string
}

var replyTagRe = regexp.MustCompile(`\[\[([a-z_]+)(?::([^\]]*))?\]\]`)

// ExtractReplyTags extracts all [[tag]] directives from reply text.
func ExtractReplyTags(text string) []ReplyTag {
	matches := replyTagRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	tags := make([]ReplyTag, 0, len(matches))
	for _, m := range matches {
		tag := ReplyTag{Name: m[1]}
		if len(m) > 2 {
			tag.Value = m[2]
		}
		tags = append(tags, tag)
	}
	return tags
}

// StripReplyTags removes all [[tag]] directives from text.
func StripReplyTags(text string) string {
	return strings.TrimSpace(replyTagRe.ReplaceAllString(text, ""))
}

// HasReplyTag returns true if the text contains the specified tag.
func HasReplyTag(text, tagName string) bool {
	tags := ExtractReplyTags(text)
	for _, t := range tags {
		if t.Name == tagName {
			return true
		}
	}
	return false
}

// GetReplyTagValue returns the value of a specific tag, or empty string.
func GetReplyTagValue(text, tagName string) string {
	tags := ExtractReplyTags(text)
	for _, t := range tags {
		if t.Name == tagName {
			return t.Value
		}
	}
	return ""
}

// ApplyReplyThreading resolves reply threading from tags.
// Returns (replyToID, replyToCurrent).
func ApplyReplyThreading(text, defaultReplyTo string) (replyToID string, replyToCurrent bool) {
	if HasReplyTag(text, "reply_to_current") {
		return "", true
	}
	tagValue := GetReplyTagValue(text, "reply_to")
	if tagValue != "" {
		return tagValue, false
	}
	return defaultReplyTo, false
}
