package runtimeops

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// The phone-event ledger is owned by runtime/phoneevents, which writes one
// JSONL file per day under this directory. Reading it here rather than
// importing that package keeps the pipeline from depending upward on runtime;
// the entry shape ({ts,type,source,text}) is the stable part of the contract.
const (
	phoneLedgerDirname = "phone-events"
	// A week is the useful horizon for "what were we talking about" — long
	// enough to cover a slow thread, short enough that the digest stays
	// readable. The ledger itself retains 30 days.
	phoneMessagesDays = 7
	// Per-room and total caps. A chat room can carry hundreds of notifications;
	// the agent needs the shape of the conversation, not a transcript.
	phoneMessagesPerRoom = 8
	phoneMessagesRooms   = 12
)

type phoneLedgerEntry struct {
	TS     string `json:"ts"`
	Type   string `json:"type"`
	Source string `json:"source"`
	Text   string `json:"text"`
}

// phoneRoomEntries splits one notification into (room, messages).
//
// The Android listener resolves MessagingStyle into a stable shape:
//
//	대화방: 선향란 과장
//	메시지:
//	- 선향란 과장: 네. 진행하겠습니다.
//	- 선향란 과장: 자료는 내일 드리겠습니다.
//
// so a chat room is a parse, not a guess, and each `- ` line is a real separate
// message.
//
// Everything else collapses to ONE entry under its app name. Splitting those by
// newline was wrong: an SMS notification is cumulative and re-carries the whole
// thread, so a single card-approval text turned into six "messages" and crowded
// a real conversation out of the digest. A push title and its body are likewise
// one event, not two.
func phoneRoomEntries(e phoneLedgerEntry) (room string, messages []string) {
	lines := strings.Split(e.Text, "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "대화방: ") {
		room = strings.TrimSpace(strings.TrimPrefix(lines[0], "대화방: "))
		for _, line := range lines[1:] {
			line = strings.TrimSpace(line)
			if line == "메시지:" || line == "" {
				continue
			}
			if msg := strings.TrimSpace(strings.TrimPrefix(line, "- ")); msg != "" {
				messages = append(messages, msg)
			}
		}
		if room != "" && len(messages) > 0 {
			return room, messages
		}
	}
	room = strings.TrimSpace(e.Source)
	if room == "" {
		room = "(출처 미상)"
	}
	if one := collapseLines(e.Text); one != "" {
		messages = []string{one}
	}
	return room, messages
}

// collapseLines folds a multi-line notification into one readable line and
// bounds it — a cumulative SMS can carry a dozen repeats of the same alert.
func collapseLines(s string) string {
	parts := make([]string, 0, 4)
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			parts = append(parts, line)
		}
	}
	out := strings.Join(parts, " / ")
	if r := []rune(out); len(r) > 160 {
		out = string(r[:160]) + "…"
	}
	return out
}

type phoneRoomThread struct {
	room  string
	last  time.Time
	lines []string // "07-26 13:32 발신자: 내용", oldest first
}

// readPhoneMessageThreads groups the recent notification ledger by conversation.
//
// This is the read side the ledger never had: it has been recording every
// notification since it was wired, and until now nothing in the gateway ever
// opened it ("later wiki digestion" was never built). Grouping by room is what
// turns a flat, ephemeral feed into the context an assistant can actually reason
// over — "이 건 어디까지 이야기됐지" is a question about a thread, not an event.
func readPhoneMessageThreads(now time.Time, days int) []phoneRoomThread {
	dir := filepath.Join(config.ResolveStateDir(), phoneLedgerDirname)
	byRoom := map[string]*phoneRoomThread{}
	for d := days - 1; d >= 0; d-- {
		day := now.AddDate(0, 0, -d).Format("2006-01-02")
		f, err := os.Open(filepath.Join(dir, day+".jsonl"))
		if err != nil {
			continue // a quiet day simply has no file
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			var e phoneLedgerEntry
			if json.Unmarshal(sc.Bytes(), &e) != nil || e.Type != "notification" {
				continue
			}
			room, messages := phoneRoomEntries(e)
			if len(messages) == 0 {
				continue
			}
			at, err := time.Parse(time.RFC3339, e.TS)
			if err != nil {
				continue
			}
			t := byRoom[room]
			if t == nil {
				t = &phoneRoomThread{room: room}
				byRoom[room] = t
			}
			if at.After(t.last) {
				t.last = at
			}
			for _, msg := range messages {
				t.lines = append(t.lines, at.Format("01-02 15:04")+" "+msg)
			}
			// Trim as we go so a chatty room cannot grow without bound.
			if over := len(t.lines) - phoneMessagesPerRoom; over > 0 {
				t.lines = t.lines[over:]
			}
		}
		_ = f.Close()
	}

	threads := make([]phoneRoomThread, 0, len(byRoom))
	for _, t := range byRoom {
		threads = append(threads, *t)
	}
	// Most recently active first: that is the order the operator would ask in.
	sort.Slice(threads, func(i, j int) bool { return threads[i].last.After(threads[j].last) })
	if len(threads) > phoneMessagesRooms {
		threads = threads[:phoneMessagesRooms]
	}
	return threads
}

func formatPhoneMessageThreads(threads []phoneRoomThread) string {
	if len(threads) == 0 {
		return "최근 알림에서 모은 대화가 없습니다."
	}
	var b strings.Builder
	// The one-sidedness is stated up front: an app posts no notification for what
	// the user sends, so the only outgoing messages here are the ones Deneb sent
	// itself. Reading this feed as a full conversation would misjudge it.
	fmt.Fprintf(&b, "최근 %d일 알림 기준 대화 %d개 (수신 + 데네브가 보낸 답장만 — 사용자가 앱에서 직접 보낸 메시지는 알림이 없어 보이지 않습니다):\n",
		phoneMessagesDays, len(threads))
	for _, t := range threads {
		fmt.Fprintf(&b, "\n[%s] 마지막 %s\n", t.room, t.last.Format("01-02 15:04"))
		for _, line := range t.lines {
			b.WriteString("  " + line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
