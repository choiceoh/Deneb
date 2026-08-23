package groupware

import (
	"bytes"
	"context"
	"encoding/json"
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
	AreaSales    Area = "sales"    // 영업 매출마감
	AreaStock    Area = "stock"    // 현재고
	AreaPO       Area = "po"       // 발주현황
	AreaReceive  Area = "receive"  // 입고현황
	AreaShip     Area = "ship"     // 출고현황
	AreaPrice    Area = "price"    // 품목단가
	AreaPeople   Area = "people"   // 사원(이름·부서·직급/호칭·휴대폰·생년월일)
)

// Action is a read-only operation against an Area.
type Action string

const (
	ActionList               Action = "list"
	ActionRead               Action = "read"
	ActionAttachment         Action = "attachment"          // OCR / extracted text
	ActionAttachmentDownload Action = "attachment-download" // raw bytes as JSON envelope
	ActionSummary            Action = "summary"             // sales closing totals
	// ActionAct is mutate (approve/reject). Not exposed on the chat tool —
	// work-feed chips call ActApproval directly.
	ActionAct Action = "act"
)

// Request drives scripts/dev/groupware-reader/read.mjs.
type Request struct {
	Area       Area
	Action     Action
	Folder     string // approval: pending|done|cc|total|all; sales: ytd|month|today|year|last_year
	Query      string // title / keyword for list filter or read target
	DocID      string // attachment only: approval document id from read output
	Attachment string // attachment only: 1-based number, filename, fileKey, or fileId
	Source     string // optional provenance label (e.g. phone notification source)
	MatchText  string // notification body on stdin for approval read matching
	Limit      int    // list max lines (default 20, capped in JS)
	JSON       bool   // internal machine-readable approval-list output
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
			"srv4에 아마란스 계정을 넣으면 전자결재·게시판·매출·재고·발주·입고·출고·단가·사원을 읽을 수 있다."
	}
	u := strings.TrimSpace(cfg.URL)
	if u == "" {
		u = "https://tsgw.topsolar.kr"
	}
	co := strings.TrimSpace(cfg.Company)
	if co == "" {
		co = "topsolar"
	}
	return fmt.Sprintf("그룹웨어 리더: 설정됨 · %s · 회사=%s · 사용자=%s · 읽기 전용(전자결재·게시판·매출·재고·발주·입고·출고·단가·사원)",
		u, co, cfg.User)
}

// Run executes a read-only groupware scrape. On failure returns a calm Korean
// error string suitable for tool output (err is still set for callers that care).
func Run(ctx context.Context, cfg Config, req Request) (string, error) {
	return runWithOutputLimit(ctx, cfg, req, 0)
}

func runWithOutputLimit(ctx context.Context, cfg Config, req Request, maxOutputBytes int) (string, error) {
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
	if req.JSON {
		args = append(args, "--json")
	}

	cmd := exec.CommandContext(ctx, node, args...)
	cmd.Stdin = strings.NewReader(req.MatchText)
	// os.Environ() already carries DENEB_OCR_VL_URL / DENEB_GROUPWARE_OCR when
	// set on the gateway, so attachment OCR (PaddleOCR-VL → tesseract) works on
	// the tool and phone-enrich paths without extra plumbing.
	cmd.Env = append(
		os.Environ(),
		"DENEB_GROUPWARE_URL="+cfg.URL,
		"DENEB_GROUPWARE_USER="+cfg.User,
		"DENEB_GROUPWARE_PASSWORD="+cfg.Password,
		"DENEB_GROUPWARE_COMPANY="+cfg.Company,
	)
	stdout := boundedOutputBuffer{max: maxOutputBytes}
	var stderr bytes.Buffer
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
	if stdout.exceeded {
		err := fmt.Errorf("groupware output exceeds %d byte safety limit", maxOutputBytes)
		return fmt.Sprintf("그룹웨어 읽기 실패 (%s/%s): 출력이 안전 한도(%d bytes)를 초과했습니다", area, action, maxOutputBytes), err
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return fmt.Sprintf("그룹웨어 읽기 결과가 비었습니다 (%s/%s).", area, action),
			fmt.Errorf("empty groupware output")
	}
	return out, nil
}

type boundedOutputBuffer struct {
	buf      bytes.Buffer
	max      int
	exceeded bool
}

func (b *boundedOutputBuffer) Write(p []byte) (int, error) {
	written := len(p)
	if b.max <= 0 {
		_, err := b.buf.Write(p)
		return written, err
	}
	remaining := b.max - b.buf.Len()
	if remaining > 0 {
		keep := min(remaining, len(p))
		if _, err := b.buf.Write(p[:keep]); err != nil {
			return 0, err
		}
	}
	if len(p) > remaining {
		b.exceeded = true
	}
	return written, nil
}

func (b *boundedOutputBuffer) String() string {
	return b.buf.String()
}

// ApprovalSummary is the stable machine-readable projection of one approval-list row.
type ApprovalSummary struct {
	DocID   string `json:"docId"`
	Title   string `json:"title"`
	DocNo   string `json:"docNo"`
	Drafter string `json:"drafter"`
	Date    string `json:"date"`
	Status  string `json:"status"`
	Folder  string `json:"folder"`
}

// BoardSummary is the stable machine-readable projection of one recent notice.
type BoardSummary struct {
	PostID     string `json:"postId"`
	Title      string `json:"title"`
	Author     string `json:"author"`
	Date       string `json:"date"`
	CategoryID string `json:"categoryId"`
}

// ListApprovals returns a structured approval-folder snapshot without parsing the
// human-facing list output.
func ListApprovals(ctx context.Context, cfg Config, folder string, limit int) ([]ApprovalSummary, error) {
	out, err := Run(ctx, cfg, Request{
		Area:   AreaApproval,
		Action: ActionList,
		Folder: folder,
		Limit:  limit,
		JSON:   true,
	})
	if err != nil {
		return nil, wrapGroupwareRunError(out, err)
	}
	return parseApprovalSummaries(out)
}

func parseApprovalSummaries(raw string) ([]ApprovalSummary, error) {
	var summaries []ApprovalSummary
	if err := json.Unmarshal([]byte(raw), &summaries); err != nil {
		return nil, fmt.Errorf("parse groupware approval list JSON: %w", err)
	}
	if summaries == nil {
		summaries = []ApprovalSummary{}
	}
	return summaries, nil
}

// ListBoardPosts returns a structured recent-notice snapshot without parsing the
// human-facing board list.
func ListBoardPosts(ctx context.Context, cfg Config, limit int) ([]BoardSummary, error) {
	out, err := Run(ctx, cfg, Request{
		Area:   AreaBoard,
		Action: ActionList,
		Limit:  limit,
		JSON:   true,
	})
	if err != nil {
		return nil, wrapGroupwareRunError(out, err)
	}
	return parseBoardSummaries(out)
}

// wrapGroupwareRunError keeps the reader's Korean diagnostic (stdout/stderr) on
// the returned error — bare "exit status 1" is useless in radar logs.
func wrapGroupwareRunError(out string, err error) error {
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(out)
	if msg == "" {
		return err
	}
	return fmt.Errorf("%s: %w", msg, err)
}

func parseBoardSummaries(raw string) ([]BoardSummary, error) {
	var summaries []BoardSummary
	if err := json.Unmarshal([]byte(raw), &summaries); err != nil {
		return nil, fmt.Errorf("parse groupware board list JSON: %w", err)
	}
	if summaries == nil {
		summaries = []BoardSummary{}
	}
	return summaries, nil
}

// ReadBoardPost fetches one 게시판 post body by id or title keyword (reader
// area=board action=read).
func ReadBoardPost(ctx context.Context, cfg Config, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("board query required")
	}
	return Run(ctx, cfg, Request{
		Area:   AreaBoard,
		Action: ActionRead,
		Query:  query,
	})
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
	cmd.Env = append(
		os.Environ(),
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
	cmd.Env = append(
		os.Environ(),
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
	// Prefer source-relative discovery (tests / untrimmed builds). Production
	// binaries are built with -trimpath, so runtime.Caller paths are not on
	// disk — fall back to WorkingDirectory (systemd sets ~/deneb) and the
	// executable's sibling repo layout (dist/deneb-gateway → ../scripts/...).
	var candidates []string
	if _, file, _, ok := runtime.Caller(0); ok {
		root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
		candidates = append(candidates, filepath.Join(root, "scripts", "dev", "groupware-reader", "read.mjs"))
	}
	if wd, err := os.Getwd(); err == nil && wd != "" {
		candidates = append(candidates, filepath.Join(wd, "scripts", "dev", "groupware-reader", "read.mjs"))
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		dir := filepath.Dir(exe)
		candidates = append(
			candidates,
			filepath.Join(dir, "..", "scripts", "dev", "groupware-reader", "read.mjs"),
			filepath.Join(dir, "scripts", "dev", "groupware-reader", "read.mjs"),
		)
	}
	for _, p := range candidates {
		p = filepath.Clean(p)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}
