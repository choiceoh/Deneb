// executor_messages.go — append/persist bookkeeping for RunAgent history.
package agent

import "github.com/choiceoh/deneb/gateway-go/internal/ai/llm"

// runMessageJournal keeps the in-memory conversation and its per-message
// persistence callback on one path. Initial messages are already part of the
// caller's history; only messages added through append or a staged message are
// persisted and counted.
type runMessageJournal struct {
	messages  []llm.Message
	onPersist func(llm.Message)
	persisted int
}

func newRunMessageJournal(messages []llm.Message, onPersist func(llm.Message)) *runMessageJournal {
	return &runMessageJournal{messages: messages, onPersist: onPersist}
}

func (j *runMessageJournal) append(message llm.Message) {
	j.messages = append(j.messages, message)
	j.persist(message)
}

// appendEphemeral adds an executor-owned instruction to the provider history
// without writing it into the user-visible conversation transcript.
func (j *runMessageJournal) appendEphemeral(message llm.Message) {
	j.messages = append(j.messages, message)
}

func (j *runMessageJournal) stage(message llm.Message) *stagedRunMessage {
	return &stagedRunMessage{journal: j, message: message}
}

func (j *runMessageJournal) persist(message llm.Message) {
	if j.onPersist == nil {
		return
	}
	j.onPersist(message)
	j.persisted++
}

// stagedRunMessage supports the finalize gate's two-phase lifecycle: the gate
// must observe a finishing assistant message before deciding whether to hold
// the turn, but a held message is appended to history only after that decision.
// persist and append are idempotent so the same assistant turn is never written
// twice when it moves from staged to appended.
type stagedRunMessage struct {
	journal   *runMessageJournal
	message   llm.Message
	persisted bool
	appended  bool
}

func (m *stagedRunMessage) persist() {
	if m.persisted {
		return
	}
	m.journal.persist(m.message)
	m.persisted = true
}

func (m *stagedRunMessage) append() {
	if m.appended {
		return
	}
	m.journal.messages = append(m.journal.messages, m.message)
	m.appended = true
	m.persist()
}
