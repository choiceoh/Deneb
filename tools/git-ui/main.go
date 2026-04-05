// Git UI — Deneb 프로젝트용 시각적 Git 대시보드.
// 개발자가 아닌 사용자를 위한 한국어 시각화 도구.
//
// Usage:
//
//	cd <repo-root>
//	go run tools/git-ui/main.go [-port 8090] [-repo .]
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed static
var staticFS embed.FS

var repoDir string

// ---------------------------------------------------------------------------
// JSON response types
// ---------------------------------------------------------------------------

type Overview struct {
	CurrentBranch string `json:"currentBranch"`
	TotalCommits  int    `json:"totalCommits"`
	BranchCount   int    `json:"branchCount"`
	RemoteURL     string `json:"remoteUrl"`
	LastCommit    string `json:"lastCommit"`
	StatusSummary Status `json:"statusSummary"`
}

type Status struct {
	Modified  []FileEntry `json:"modified"`
	Staged    []FileEntry `json:"staged"`
	Untracked []FileEntry `json:"untracked"`
}

type FileEntry struct {
	Path   string `json:"path"`
	Status string `json:"status"` // M, A, D, R, ?
}

type Commit struct {
	Hash       string   `json:"hash"`
	ShortHash  string   `json:"shortHash"`
	Parents    []string `json:"parents"`
	Author     string   `json:"author"`
	Date       string   `json:"date"`
	RelDate    string   `json:"relDate"`
	Subject    string   `json:"subject"`
	Refs       []string `json:"refs"`
	IsMerge    bool     `json:"isMerge"`
	FilesCount int      `json:"filesCount"`
	Additions  int      `json:"additions"`
	Deletions  int      `json:"deletions"`
}

type Branch struct {
	Name      string `json:"name"`
	ShortHash string `json:"shortHash"`
	Subject   string `json:"subject"`
	IsCurrent bool   `json:"isCurrent"`
	IsRemote  bool   `json:"isRemote"`
	Upstream  string `json:"upstream"`
}

type CommitDetail struct {
	Commit
	Body  string       `json:"body"`
	Files []DiffFile   `json:"files"`
	Diff  string       `json:"diff"`
}

type DiffFile struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// ---------------------------------------------------------------------------
// Git command helpers
// ---------------------------------------------------------------------------

func git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), string(exitErr.Stderr))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitLines(args ...string) ([]string, error) {
	out, err := git(args...)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// ---------------------------------------------------------------------------
// API handlers
// ---------------------------------------------------------------------------

func handleOverview(w http.ResponseWriter, r *http.Request) {
	branch, _ := git("rev-parse", "--abbrev-ref", "HEAD")

	countStr, _ := git("rev-list", "--count", "HEAD")
	totalCommits, _ := strconv.Atoi(countStr)

	branches, _ := gitLines("branch", "--format=%(refname:short)")
	branchCount := len(branches)

	remoteURL, _ := git("remote", "get-url", "origin")

	lastCommit, _ := git("log", "-1", "--format=%s")

	status := getStatus()

	writeJSON(w, Overview{
		CurrentBranch: branch,
		TotalCommits:  totalCommits,
		BranchCount:   branchCount,
		RemoteURL:     remoteURL,
		LastCommit:    lastCommit,
		StatusSummary: status,
	})
}

func getStatus() Status {
	lines, _ := gitLines("status", "--porcelain")
	var s Status
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		xy := line[:2]
		path := line[3:]

		entry := FileEntry{Path: path}

		// Index (staged) status
		switch xy[0] {
		case 'M':
			entry.Status = "수정"
			s.Staged = append(s.Staged, entry)
		case 'A':
			entry.Status = "추가"
			s.Staged = append(s.Staged, entry)
		case 'D':
			entry.Status = "삭제"
			s.Staged = append(s.Staged, entry)
		case 'R':
			entry.Status = "이름변경"
			s.Staged = append(s.Staged, entry)
		}

		// Worktree status
		switch xy[1] {
		case 'M':
			entry.Status = "수정"
			s.Modified = append(s.Modified, entry)
		case 'D':
			entry.Status = "삭제"
			s.Modified = append(s.Modified, entry)
		case '?':
			entry.Status = "새 파일"
			s.Untracked = append(s.Untracked, entry)
		}
	}
	return s
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, getStatus())
}

func handleLog(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	if limitStr == "" {
		limitStr = "80"
	}
	branchFilter := r.URL.Query().Get("branch")

	// Format: hash|shorthash|parents|author|ISO date|relative date|subject|refs
	format := "%H|%h|%P|%an|%aI|%ar|%s|%D"
	args := []string{"log", "--format=" + format, "--max-count=" + limitStr}
	if branchFilter != "" && branchFilter != "all" {
		args = append(args, branchFilter)
	} else {
		args = append(args, "--all")
	}

	lines, err := gitLines(args...)
	if err != nil {
		writeError(w, err)
		return
	}

	// Also get numstat for additions/deletions per commit
	statArgs := []string{"log", "--format=%H", "--numstat", "--max-count=" + limitStr}
	if branchFilter != "" && branchFilter != "all" {
		statArgs = append(statArgs, branchFilter)
	} else {
		statArgs = append(statArgs, "--all")
	}
	statOut, _ := git(statArgs...)
	statMap := parseNumstat(statOut)

	var commits []Commit
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 8)
		if len(parts) < 8 {
			continue
		}

		parents := strings.Fields(parts[2])
		refs := parseRefs(parts[7])

		stat := statMap[parts[0]]

		commits = append(commits, Commit{
			Hash:       parts[0],
			ShortHash:  parts[1],
			Parents:    parents,
			Author:     parts[3],
			Date:       parts[4],
			RelDate:    koreanRelDate(parts[4]),
			Subject:    parts[6],
			Refs:       refs,
			IsMerge:    len(parents) > 1,
			FilesCount: stat.files,
			Additions:  stat.add,
			Deletions:  stat.del,
		})
	}

	writeJSON(w, commits)
}

type commitStat struct {
	files, add, del int
}

func parseNumstat(raw string) map[string]commitStat {
	result := make(map[string]commitStat)
	lines := strings.Split(raw, "\n")
	var currentHash string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// If line is 40-char hex, it's a commit hash
		if len(line) == 40 && isHex(line) {
			currentHash = line
			continue
		}
		if currentHash == "" {
			continue
		}
		// numstat line: additions \t deletions \t path
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		s := result[currentHash]
		s.files++
		if a, err := strconv.Atoi(parts[0]); err == nil {
			s.add += a
		}
		if d, err := strconv.Atoi(parts[1]); err == nil {
			s.del += d
		}
		result[currentHash] = s
	}
	return result
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func parseRefs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ", ")
	var refs []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		// Clean up "HEAD -> main" format
		p = strings.TrimPrefix(p, "HEAD -> ")
		if p == "HEAD" {
			continue
		}
		// Remove "origin/" prefix for display
		refs = append(refs, p)
	}
	return refs
}

func koreanRelDate(isoDate string) string {
	t, err := time.Parse(time.RFC3339, isoDate)
	if err != nil {
		return isoDate
	}
	diff := time.Since(t)
	switch {
	case diff < time.Minute:
		return "방금 전"
	case diff < time.Hour:
		return fmt.Sprintf("%d분 전", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%d시간 전", int(diff.Hours()))
	case diff < 7*24*time.Hour:
		return fmt.Sprintf("%d일 전", int(diff.Hours()/24))
	case diff < 30*24*time.Hour:
		return fmt.Sprintf("%d주 전", int(diff.Hours()/(24*7)))
	case diff < 365*24*time.Hour:
		return fmt.Sprintf("%d개월 전", int(diff.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%d년 전", int(diff.Hours()/(24*365)))
	}
}

func handleBranches(w http.ResponseWriter, r *http.Request) {
	format := "%(refname:short)|%(objectname:short)|%(subject)|%(upstream:short)|%(HEAD)"
	lines, err := gitLines("branch", "--format="+format, "-a")
	if err != nil {
		writeError(w, err)
		return
	}

	var branches []Branch
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 5)
		if len(parts) < 5 {
			continue
		}
		b := Branch{
			Name:      parts[0],
			ShortHash: parts[1],
			Subject:   parts[2],
			Upstream:  parts[3],
			IsCurrent: strings.TrimSpace(parts[4]) == "*",
			IsRemote:  strings.HasPrefix(parts[0], "remotes/"),
		}
		branches = append(branches, b)
	}

	writeJSON(w, branches)
}

func handleCommitDetail(w http.ResponseWriter, r *http.Request) {
	hash := r.URL.Query().Get("hash")
	if hash == "" {
		http.Error(w, "hash required", http.StatusBadRequest)
		return
	}
	// Sanitize: only allow hex chars
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			http.Error(w, "invalid hash", http.StatusBadRequest)
			return
		}
	}

	format := "%H|%h|%P|%an|%aI|%ar|%s|%D"
	line, err := git("log", "-1", "--format="+format, hash)
	if err != nil {
		writeError(w, err)
		return
	}

	parts := strings.SplitN(line, "|", 8)
	if len(parts) < 8 {
		writeError(w, fmt.Errorf("unexpected format"))
		return
	}

	parents := strings.Fields(parts[2])

	body, _ := git("log", "-1", "--format=%b", hash)

	// Get file stats
	statLines, _ := gitLines("diff-tree", "--no-commit-id", "-r", "--numstat", hash)
	var files []DiffFile
	for _, sl := range statLines {
		fields := strings.SplitN(sl, "\t", 3)
		if len(fields) < 3 {
			continue
		}
		add, _ := strconv.Atoi(fields[0])
		del, _ := strconv.Atoi(fields[1])
		status := "수정"
		if add > 0 && del == 0 {
			status = "추가"
		} else if add == 0 && del > 0 {
			status = "삭제"
		}
		files = append(files, DiffFile{
			Path:      fields[2],
			Status:    status,
			Additions: add,
			Deletions: del,
		})
	}

	// Get the actual diff (limited to avoid huge responses)
	diff, _ := git("diff-tree", "-p", "--no-commit-id", "-r", hash, "--stat-width=120")
	// Truncate very long diffs
	if len(diff) > 200000 {
		diff = diff[:200000] + "\n\n... (diff가 너무 길어 잘렸습니다)"
	}

	detail := CommitDetail{
		Commit: Commit{
			Hash:       parts[0],
			ShortHash:  parts[1],
			Parents:    parents,
			Author:     parts[3],
			Date:       parts[4],
			RelDate:    koreanRelDate(parts[4]),
			Subject:    parts[6],
			Refs:       parseRefs(parts[7]),
			IsMerge:    len(parents) > 1,
			FilesCount: len(files),
		},
		Body:  body,
		Files: files,
		Diff:  diff,
	}

	writeJSON(w, detail)
}

func handleDiffWorking(w http.ResponseWriter, r *http.Request) {
	diff, _ := git("diff")
	stagedDiff, _ := git("diff", "--staged")

	result := map[string]string{
		"working": diff,
		"staged":  stagedDiff,
	}
	writeJSON(w, result)
}

func handleGraph(w http.ResponseWriter, r *http.Request) {
	// Simple ASCII graph from git
	limitStr := r.URL.Query().Get("limit")
	if limitStr == "" {
		limitStr = "40"
	}
	out, err := git("log", "--graph", "--oneline", "--all", "--decorate", "--max-count="+limitStr)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]string{"graph": out})
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Printf("JSON encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	port := flag.Int("port", 8090, "서버 포트")
	bind := flag.String("bind", "127.0.0.1", "바인드 주소 (0.0.0.0 = 모든 인터페이스, Tailscale 원격 접속용)")
	repo := flag.String("repo", ".", "Git 저장소 경로")
	flag.Parse()

	absRepo, err := filepath.Abs(*repo)
	if err != nil {
		log.Fatalf("경로 오류: %v", err)
	}
	repoDir = absRepo

	// Verify it's a git repo
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		// Could be a worktree — check git rev-parse
		cmd := exec.Command("git", "rev-parse", "--git-dir")
		cmd.Dir = repoDir
		if err := cmd.Run(); err != nil {
			log.Fatalf("%s 은(는) Git 저장소가 아닙니다", repoDir)
		}
	}

	// API routes
	http.HandleFunc("/api/overview", handleOverview)
	http.HandleFunc("/api/status", handleStatus)
	http.HandleFunc("/api/log", handleLog)
	http.HandleFunc("/api/branches", handleBranches)
	http.HandleFunc("/api/commit", handleCommitDetail)
	http.HandleFunc("/api/diff/working", handleDiffWorking)
	http.HandleFunc("/api/graph", handleGraph)

	// Static files
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal(err)
	}
	http.Handle("/", http.FileServer(http.FS(staticSub)))

	addr := fmt.Sprintf("%s:%d", *bind, *port)
	fmt.Printf("🌿 Deneb Git 대시보드 시작\n")
	fmt.Printf("   저장소: %s\n", repoDir)
	fmt.Printf("   주소:   http://%s\n", addr)
	if *bind == "0.0.0.0" {
		// Show Tailscale IP if available
		if tsIP := getTailscaleIP(); tsIP != "" {
			fmt.Printf("   Tailscale: http://%s:%d\n", tsIP, *port)
		}
		fmt.Printf("   ⚠️  외부 접속 허용됨 (Tailscale 네트워크 등)\n")
	}
	fmt.Printf("   브라우저에서 열어주세요!\n")

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

// getTailscaleIP returns the first Tailscale (100.x.x.x) IP found, or "".
func getTailscaleIP() string {
	out, err := exec.Command("tailscale", "ip", "-4").Output()
	if err != nil {
		return ""
	}
	ip := strings.TrimSpace(string(out))
	if ip != "" {
		return ip
	}
	return ""
}
