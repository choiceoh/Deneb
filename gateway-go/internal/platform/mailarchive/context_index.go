package mailarchive

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/textsearch"
)

// ContextIndex is the archive's pure-Go full-text surface for agent lookups.
// It deliberately mirrors the wiki search stack: small, local, dependency-free,
// and rebuilt from the bounded archive window when a project history is needed.
type ContextIndex struct {
	idx      *textsearch.Index
	messages map[string]ContextMessage
}

func buildContextIndex(ctx context.Context, c *imapConn, cfg Config, opts ContextOptions) (*ContextIndex, error) {
	limit := clampContextIndexLimit(opts.IndexLimit)
	criteria := "ALL"
	if !opts.Since.IsZero() {
		criteria = "SINCE " + imapSinceDate(opts.Since)
	}
	msgs, err := searchContextMessagesLimited(ctx, c, cfg, criteria, opts, true, limit)
	if err != nil {
		return nil, err
	}
	return newContextIndex(msgs), nil
}

func newContextIndex(msgs []ContextMessage) *ContextIndex {
	ci := &ContextIndex{
		idx:      textsearch.New(),
		messages: make(map[string]ContextMessage, len(msgs)),
	}
	for _, msg := range msgs {
		key := contextMessageDedupeKey(msg)
		if key == "" {
			key = msg.Locator
		}
		if key == "" {
			continue
		}
		ci.messages[key] = msg
		ci.idx.Upsert(key, contextIndexFields(msg)...)
	}
	return ci
}

func (ci *ContextIndex) Search(query string, limit int) []ContextMessage {
	if ci == nil || ci.idx == nil {
		return nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	if limit <= 0 {
		limit = defaultContextLimit
	}
	hits := ci.idx.Search(query, limit)
	out := make([]ContextMessage, 0, len(hits))
	for _, hit := range hits {
		msg, ok := ci.messages[hit.ID]
		if !ok {
			continue
		}
		msg.Score += hit.Score
		msg.RankReasons = appendRankReason(msg.RankReasons, "local_fts")
		out = append(out, msg)
	}
	return out
}

func contextIndexFields(msg ContextMessage) []string {
	fields := []string{
		msg.Subject,
		msg.From,
		msg.To,
		msg.CC,
		msg.Snippet,
		msg.Body,
		msg.MessageID,
		strings.Join(msg.References, " "),
	}
	if len(msg.Attachments) > 0 {
		names := make([]string, 0, len(msg.Attachments))
		for _, att := range msg.Attachments {
			names = append(names, att.Filename, att.MimeType)
		}
		fields = append(fields, strings.Join(names, " "))
	}
	return fields
}
