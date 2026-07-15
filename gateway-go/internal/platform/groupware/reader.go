package groupware

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Config is the srv4 credentialed Amaranth reader.
type Config struct {
	URL      string // https://tsgw.topsolar.kr
	User     string
	Password string
	Company  string // default topsolar
	// ReaderJS is the absolute path to read.mjs. Empty → discover next to this module's repo scripts/.
	ReaderJS string
	NodeBin  string // default "node"
	Timeout  time.Duration
}

// Area is which Amaranth surface to read.
type Area string

const (
	AreaApproval Area = "approval" // 전자결재
	AreaBoard    Area = "board"    // 게시판
)

// Action is a read-only operation against an Area.
type Action string

const (
	ActionList       Action = "list"
	ActionRead       Action = "read"
	ActionAttachment Action = "attachment" // explicitly selected attachment only
	// ActionAct is mutate (approve/reject). Not exposed on the chat tool —
	// work-feed chips call ActApproval directly.
	ActionAct Action = "act"
)

// Request drives scripts/dev/groupware-reader/read.mjs.
type Request struct {
	Area       Area
	Action     Action
	Folder     string // approval only: pending|done|cc|total|all (미결|기결|수신참조|전체결재문서|순회)
	Query      string // title / keyword for list filter or read target
	DocID      string // attachment only: approval document id from read output
	Attachment string // attachment only: 1-based number, filename, fileKey, or fileId
	Source     string // optional provenance label (e.g. phone notification source)
	MatchText  string // notification body on stdin for approval read matching
	Limit      int    // list max lines (default 20, capped in JS)
}

// FromEnv loads DENEB_GROUPWARE_* settings. Enabled only when both user and password are set.
func FromEnv() (Config, bool) {
	user := strings.TrimSpace(os.Getenv("DENEB_GROUPWARE_USER"))
	pass := os.Getenv("DENEB_GROUPWARE_PASSWORD")
	if user == "" || pass == "" {
		return Config{}, false
	}
	url := strings.TrimSpace(os.Getenv("DENEB_GROUPWARE_URL"))
	if url == "" {
		url = "https://tsgw.topsolar.kr"
	}
	company := strings.TrimSpace(os.Getenv("DENEB_GROUPWARE_COMPANY"))
	if company == "" {
		company = "topsolar"
	}
	return Config{
		URL:      url,
		User:     user,
		Password: pass,
		Company:  company,
		ReaderJS: strings.TrimSpace(os.Getenv("DENEB_GROUPWARE_READER")),
		NodeBin:  strings.TrimSpace(os.Getenv("DENEB_GROUPWARE_NODE")),
		Timeout:  90 * time.Second,
	}, true
}

// StatusLine reports whether the reader is configured (never includes the password).
func StatusLine(cfg Config, ok bool) string {
	if !ok || strings.TrimSpace(cfg.User) == "" || cfg.Password == "" {
		return "그룹웨어 리더: 꺼짐 (DENEB_GROUPWARE_USER / DENEB_GROUPWARE_PASSWORD 미설정). " +
			"srv4에 아마란스 계정을 넣으면 전자결재·게시판을 읽을 수 있다."
	}
	u := strings.TrimSpace(cfg.URL)
	if u == "" {
		u = "https://tsgw.topsolar.kr"
	}
	co := strings.TrimSpace(cfg.Company)
	if co == "" {
		co = "topsolar"
	}
	return fmt.Sprintf("그룹웨어 리더: 설정됨 · %s · 회사=%s · 사용자=%s · 읽기 전용(전자결재·게시판)",
		u, co, cfg.User)
}

// Run executes a read-only groupware scrape. On failure returns a calm Korean
// error string suitable for tool output (err is still set for callers that care).
func Run(ctx context.Context, cfg Config, req Request) (string, error) {
	if strings.TrimSpace(cfg.User) == "" || cfg.Password == "" {
		return StatusLine(cfg, false), fmt.Errorf("groupware credentials unset")
	}
	script := cfg.ReaderJS
	if script == "" {
		script = defaultReaderJS()
	}
	if script == "" {
		return "그룹웨어 리더 스크립트를 찾지 못했습니다 (scripts/dev/groupware-reader/read.mjs).",
			fmt.Errorf("groupware reader script not found")
	}
	node := cfg.NodeBin
	if node == "" {
		node = "node"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	area := strings.ToLower(strings.TrimSpace(string(req.Area)))
	if area == "" {
		area = string(AreaApproval)
	}
	action := strings.ToLower(strings.TrimSpace(string(req.Action)))
	if action == "" {
		action = string(ActionRead)
	}
	args := []string{script, "--area", area, "--action", action}
	if folder := strings.TrimSpace(req.Folder); folder != "" {
		args = append(args, "--folder", folder)
	}
	if q := strings.TrimSpace(req.Query); q != "" {
		args = append(args, "--query", q)
	}
	if docID := strings.TrimSpace(req.DocID); docID != "" {
		args = append(args, "--doc-id", docID)
	}
	if attachment := strings.TrimSpace(req.Attachment); attachment != "" {
		args = append(args, "--attachment", attachment)
	}
	if src := strings.TrimSpace(req.Source); src != "" {
		args = append(args, "--source", src)
	}
	if req.Limit > 0 {
		args = append(args, "--limit", strconv.Itoa(req.Limit))
	}

	cmd := exec.CommandContext(ctx, node, args...)
	cmd.Stdin = strings.NewReader(req.MatchText)
	// os.Environ() already carries DENEB_OCR_VL_URL / DENEB_GROUPWARE_OCR when
	// set on the gateway, so attachment OCR (PaddleOCR-VL → tesseract) works on
	// the tool and phone-enrich paths without extra plumbing.
	cmd.Env = append(os.Environ(),
		"DENEB_GROUPWARE_URL="+cfg.URL,
		"DENEB_GROUPWARE_USER="+cfg.User,
		"DENEB_GROUPWARE_PASSWORD="+cfg.Password,
		"DENEB_GROUPWARE_COMPANY="+cfg.Company,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		// Never echo credentials if somehow present.
		msg = strings.ReplaceAll(msg, cfg.Password, "****")
		return fmt.Sprintf("그룹웨어 읽기 실패 (%s/%s): %s", area, action, msg), err
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return fmt.Sprintf("그룹웨어 읽기 결과가 비었습니다 (%s/%s).", area, action),
			fmt.Errorf("empty groupware output")
	}
	return out, nil
}

// ReadApproval logs into Amaranth on srv4 and returns the document text matching notiText.
// Searches 미결 → 기결 → 수신참조 → 전체결재문서 (folder=all). Empty string = skip / failure.
func ReadApproval(ctx context.Context, cfg Config, source, notiText string) string {
	out, err := Run(ctx, cfg, Request{
		Area:      AreaApproval,
		Action:    ActionRead,
		Folder:    "all",
		Source:    source,
		MatchText: notiText,
	})
	if err != nil || out == "" {
		return ""
	}
	// Suppress calm credential-missing strings from the proactive path.
	if strings.HasPrefix(out, "그룹웨어 리더:") || strings.HasPrefix(out, "그룹웨어 읽기 실패") {
		return ""
	}
	return out
}

// ExtractDocID pulls `id: 99178` (or `id=99178`) from a groupware read body.
func ExtractDocID(body string) string {
	for _, line := range strings.Split(body, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "id:") {
			return strings.TrimSpace(strings.TrimPrefix(s, "id:"))
		}
		if strings.HasPrefix(s, "id=") {
			return strings.TrimSpace(strings.TrimPrefix(s, "id="))
		}
	}
	return ""
}

// ActApproval submits 승인 (approve) or 반려 (reject) for docID via eap110A21.
// Operator-facing only — never call from the chat tool path.
func ActApproval(ctx context.Context, cfg Config, docID, decision, comment string) (string, error) {
	docID = strings.TrimSpace(docID)
	decision = strings.TrimSpace(strings.ToLower(decision))
	if docID == "" {
		return "", fmt.Errorf("doc id required")
	}
	switch decision {
	case "approve", "reject", "승인", "반려":
	default:
		return "", fmt.Errorf("decision must be approve|reject")
	}
	if strings.TrimSpace(cfg.User) == "" || cfg.Password == "" {
		return "", fmt.Errorf("groupware credentials unset")
	}
	script := cfg.ReaderJS
	if script == "" {
		script = defaultReaderJS()
	}
	if script == "" {
		return "", fmt.Errorf("groupware reader script not found")
	}
	node := cfg.NodeBin
	if node == "" {
		node = "node"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{
		script,
		"--area", "approval",
		"--action", "act",
		"--doc-id", docID,
		"--decision", decision,
	}
	if c := strings.TrimSpace(comment); c != "" {
		args = append(args, "--comment", c)
	}
	cmd := exec.CommandContext(ctx, node, args...)
	cmd.Env = append(os.Environ(),
		"DENEB_GROUPWARE_URL="+cfg.URL,
		"DENEB_GROUPWARE_USER="+cfg.User,
		"DENEB_GROUPWARE_PASSWORD="+cfg.Password,
		"DENEB_GROUPWARE_COMPANY="+cfg.Company,
		// Explicit opt-in: the reader refuses eap110A21 without this, so an
		// accidental CLI run or a read path can never submit an approval.
		"DENEB_GROUPWARE_ACT=1",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		msg = strings.ReplaceAll(msg, cfg.Password, "****")
		return "", fmt.Errorf("groupware act: %s", msg)
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("empty groupware act output")
	}
	return out, nil
}

// LoginCheck verifies credentials against the live site (ops smoke).
func LoginCheck(ctx context.Context, cfg Config) error {
	script := cfg.ReaderJS
	if script == "" {
		script = defaultReaderJS()
	}
	if script == "" {
		return fmt.Errorf("groupware reader script not found")
	}
	node := cfg.NodeBin
	if node == "" {
		node = "node"
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, script, "--login-check")
	cmd.Env = append(os.Environ(),
		"DENEB_GROUPWARE_URL="+cfg.URL,
		"DENEB_GROUPWARE_USER="+cfg.User,
		"DENEB_GROUPWARE_PASSWORD="+cfg.Password,
		"DENEB_GROUPWARE_COMPANY="+cfg.Company,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		msg = strings.ReplaceAll(msg, cfg.Password, "****")
		return fmt.Errorf("groupware login-check: %s", msg)
	}
	if !strings.Contains(string(out), "login ok") {
		return fmt.Errorf("groupware login-check: unexpected output %q", strings.TrimSpace(string(out)))
	}
	return nil
}

func defaultReaderJS() string {
	// gateway-go/internal/platform/groupware → repo root scripts/dev/groupware-reader/read.mjs
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	// .../gateway-go/internal/platform/groupware/reader.go
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	p := filepath.Join(root, "scripts", "dev", "groupware-reader", "read.mjs")
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return ""
}
