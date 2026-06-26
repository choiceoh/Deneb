// exec_safety.go provides safety checks for shell command execution.
//
// While Deneb is single-user (no multi-tenant security concerns), these
// checks protect against accidental destructive operations and help the
// LLM understand when it's about to do something irreversible.
//
// Inspired by Claude Code's BashTool security module (18 files), but
// scoped to the subset relevant for single-user deployment.
package tools

import (
	"regexp"
	"strings"
)

// DestructiveCheck describes a potentially destructive command pattern.
type DestructiveCheck struct {
	Pattern     *regexp.Regexp
	Description string
	Severity    string // "warning" or "danger"
}

// sensitiveWriteTargets matches paths that should never be written to casually.
// Shell expansions ($HOME, ~) covered explicitly. Matches NousResearch/hermes-agent
// tools/approval.py:_SENSITIVE_WRITE_TARGET pattern, adapted for Deneb paths.
var sensitiveWriteTargets = regexp.MustCompile(
	`(?i)(?:` +
		`/etc/|` +
		`/dev/(sd[a-z]|nvme|vd[a-z])|` +
		`(?:~|\$home|\$\{home\})/\.ssh(?:/|$)|` +
		`(?:~|\$home|\$\{home\})/\.deneb/\.env\b` +
		`)`,
)

// sensitiveReadTargets matches commands that touch a credential / secret store.
// Unlike the fs tools (which hard-deny these paths in path_guard.go), exec keeps
// the warn-only model — the operator may have a legitimate one-off need — but the
// prepended warning makes a prompt-injection-driven `cat ~/.deneb/credentials |
// send` attempt visible to both the model and the operator log.
var sensitiveReadTargets = regexp.MustCompile(
	`(?i)(?:` +
		`(?:~|\$home|\$\{home\})?/?\.deneb/credentials|` +
		`(?:~|\$home|\$\{home\})?/?\.aws/credentials\b|` +
		`(?:~|\$home|\$\{home\})?/?\.ssh/id_(?:rsa|ed25519|ecdsa|dsa)|` +
		`(?:~|\$home|\$\{home\})?/?\.netrc\b|` +
		`(?:^|[\s/])\.env(?:\.[a-z]+)?\b` +
		`)`,
)

// severityBlock marks a check whose match exec REFUSES to run (vs the warn-only
// "warning"/"danger" levels, where the command still executes with the warning
// prepended). Reserved for the small, high-precision catastrophicPatterns set.
const severityBlock = "block"

// rmForceRecursivePattern matches an `rm` carrying both the recursive and force
// flags, in either order and combined or separate (-rf, -fr, -r -f, -f -r). It is
// shared by the warn list (ANY rm -rf is worth a warning) and the catastrophic
// block check (rm -rf whose TARGET is the root / home / a system directory).
var rmForceRecursivePattern = regexp.MustCompile(`\brm\s+(-[a-zA-Z]*r[a-zA-Z]*f[a-zA-Z]*|(-[a-zA-Z]*f[a-zA-Z]*r[a-zA-Z]*)|-[a-zA-Z]*r\s+-[a-zA-Z]*f|-[a-zA-Z]*f\s+-[a-zA-Z]*r)\b`)

// destructivePatterns detects commands that could cause data loss.
var destructivePatterns = []DestructiveCheck{
	{
		Pattern:     sensitiveReadTargets,
		Description: "references a credential/secret store (~/.deneb/credentials, ~/.aws/credentials, SSH keys, .env) — do not echo its contents",
		Severity:    "warning",
	},
	{
		Pattern:     rmForceRecursivePattern,
		Description: "recursive force delete (rm -rf)",
		Severity:    "danger",
	},
	{
		Pattern:     sensitiveWriteTargets,
		Description: "touches sensitive path (~/.ssh, ~/.deneb/.env, /etc, /dev block devices)",
		Severity:    "danger",
	},
	{
		Pattern:     regexp.MustCompile(`\bgit\s+(reset\s+--hard|clean\s+(-[a-zA-Z]*f|-[a-zA-Z]+\s+-[a-zA-Z]*f)|checkout\s+--\s+\.)`),
		Description: "destructive git operation",
		Severity:    "danger",
	},
	{
		Pattern:     regexp.MustCompile(`\bgit\s+push\s+.*--force\b`),
		Description: "force push",
		Severity:    "danger",
	},
	{
		Pattern:     regexp.MustCompile(`\b(mkfs|dd\s+if=|fdisk|parted)\b`),
		Description: "disk/partition operation",
		Severity:    "danger",
	},
	{
		Pattern:     regexp.MustCompile(`>\s*/dev/(sd[a-z]|nvme|vd[a-z])`),
		Description: "writing to block device",
		Severity:    "danger",
	},
	{
		Pattern:     regexp.MustCompile(`\bchmod\s+-R\s+777\b`),
		Description: "recursive world-writable permissions",
		Severity:    "warning",
	},
	{
		Pattern:     regexp.MustCompile(`\b(kill|killall|pkill)\s+-9\b`),
		Description: "force kill (SIGKILL)",
		Severity:    "warning",
	},
	{
		Pattern:     regexp.MustCompile(`\bsudo\s+rm\b`),
		Description: "sudo rm",
		Severity:    "warning",
	},
}

// CheckDestructiveCommand returns warnings for potentially destructive
// commands. Returns nil if the command appears safe.
func CheckDestructiveCommand(command string) []DestructiveCheck {
	var matches []DestructiveCheck
	for _, check := range destructivePatterns {
		if check.Pattern.MatchString(command) {
			// --force-with-lease is a safer alternative to --force; exclude it.
			if check.Description == "force push" && strings.Contains(command, "--force-with-lease") {
				continue
			}
			matches = append(matches, check)
		}
	}
	return matches
}

// FormatDestructiveWarnings returns a human-readable warning string
// for destructive command detections.
func FormatDestructiveWarnings(checks []DestructiveCheck) string {
	if len(checks) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("⚠ Destructive command detected:\n")
	for _, c := range checks {
		sb.WriteString("  - ")
		sb.WriteString(c.Description)
		sb.WriteString(" [")
		sb.WriteString(c.Severity)
		sb.WriteString("]\n")
	}
	return sb.String()
}

// catastrophicRootTarget matches an argument that names the filesystem root, the
// home directory itself, or a whole top-level system directory — the targets that
// turn a recursive delete / chmod / chown into an unrecoverable system wipe. It
// is deliberately narrow: deeper paths (rm -rf /tmp/x, ./build, ~/project/x,
// /etc/nginx) do NOT match and stay warn-only, so legitimate cleanup is never
// blocked. Only "/", "/*", "~", "$HOME", and a bare system dir (or "<dir>/*")
// trip it.
var catastrophicRootTarget = regexp.MustCompile(
	`(?i)(?:` +
		`--no-preserve-root\b|` + // explicit "yes, wipe root" opt-in
		`(?:^|\s)/\*?(?:\s|$)|` + // "/" or "/*" as a whole target (root)
		`(?:^|\s)(?:~|\$\{?home\}?)(?:/)?(?:\s|$|\*)|` + // ~ or $HOME (home root, not ~/sub)
		`(?:^|\s)/(?:etc|usr|bin|sbin|lib|lib64|var|boot|sys|proc|dev|root|home)(?:/\*)?(?:\s|$)` + // /etc, /etc/* …
		`)`,
)

// chmodChownRecursivePattern matches a recursive chmod/chown (-R or --recursive).
var chmodChownRecursivePattern = regexp.MustCompile(`\b(?:chmod|chown)\s+(?:-[a-zA-Z]*R[a-zA-Z]*|--recursive)\b`)

// diskDestroyPattern matches an overwrite/format of a raw block device: a
// redirect onto /dev/sdX, `dd of=/dev/sdX`, or `mkfs … /dev/sdX`. These wipe an
// entire disk and have no plausible automated use.
var diskDestroyPattern = regexp.MustCompile(
	`(?i)(?:` +
		`>\s*/dev/(?:sd[a-z]|hd[a-z]|vd[a-z]|nvme\d+n\d+)|` +
		`\bdd\b[^|;&]*\bof=/dev/(?:sd[a-z]|hd[a-z]|vd[a-z]|nvme\d+n\d+)|` +
		`\bmkfs(?:\.[a-z0-9]+)?\b[^|;&]*/dev/(?:sd[a-z]|hd[a-z]|vd[a-z]|nvme\d+n\d+)` +
		`)`,
)

// forkBombPattern matches the classic `:(){ :|:& };:` shell fork bomb (with
// flexible whitespace). The shape is unique enough that a match is never a false
// positive.
var forkBombPattern = regexp.MustCompile(`:\s*\(\s*\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`)

// CheckCatastrophicCommand returns block-severity matches for commands that are
// unrecoverable AND have no plausible legitimate use in an automated agent
// context: wiping the filesystem root / home / a whole system directory,
// overwriting a raw block device, formatting a disk, or a fork bomb. exec refuses
// to run any command that matches (see exec.go). This is the BLOCK tier above the
// warn-only destructivePatterns — kept small and high-precision so legitimate
// operations (rm -rf ./build, git reset --hard, dd of=backup.img) are never
// blocked. Returns nil if the command is not catastrophic.
func CheckCatastrophicCommand(command string) []DestructiveCheck {
	var matches []DestructiveCheck
	if rmForceRecursivePattern.MatchString(command) && catastrophicRootTarget.MatchString(command) {
		matches = append(matches, DestructiveCheck{
			Description: "재귀 강제 삭제(rm -rf)가 루트(/)·홈(~/$HOME)·시스템 디렉터리를 대상으로 함 — 복구 불가",
			Severity:    severityBlock,
		})
	}
	if chmodChownRecursivePattern.MatchString(command) && catastrophicRootTarget.MatchString(command) {
		matches = append(matches, DestructiveCheck{
			Description: "재귀 chmod/chown이 루트·시스템 디렉터리를 대상으로 함 — 시스템 권한 파괴",
			Severity:    severityBlock,
		})
	}
	if diskDestroyPattern.MatchString(command) {
		matches = append(matches, DestructiveCheck{
			Description: "원시 블록 디바이스(/dev/sd*, nvme*) 덮어쓰기 또는 디스크 포맷(dd/mkfs) — 디스크 전체 파괴",
			Severity:    severityBlock,
		})
	}
	if forkBombPattern.MatchString(command) {
		matches = append(matches, DestructiveCheck{
			Description: "fork bomb — 시스템 자원 고갈로 호스트 마비",
			Severity:    severityBlock,
		})
	}
	return matches
}

// FormatCatastrophicRefusal renders the message exec returns IN PLACE OF running
// a blocked command — a clear Korean refusal naming each reason, so the model
// adjusts (narrow the target) rather than retries blindly. The operator can still
// run the command directly on the host.
func FormatCatastrophicRefusal(checks []DestructiveCheck) string {
	if len(checks) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("⛔ 실행 거부 — 복구 불가능한 시스템 파괴 위험으로 이 명령을 실행하지 않았습니다:\n")
	for _, c := range checks {
		sb.WriteString("  - ")
		sb.WriteString(c.Description)
		sb.WriteString("\n")
	}
	sb.WriteString("정말 필요하면 운영자가 호스트에서 직접 실행하세요. 의도와 다르면 대상 경로를 좁혀(루트·시스템 경로 대신 구체 경로) 다시 시도하세요.")
	return sb.String()
}

// sedModifiesFile detects sed commands that modify files in-place.
// Returns true if the sed command uses -i flag (in-place editing).
func sedModifiesFile(command string) bool {
	// Match sed -i or sed --in-place patterns.
	return sedInPlacePattern.MatchString(command)
}

var sedInPlacePattern = regexp.MustCompile(`\bsed\s+(-[a-zA-Z]*i|--in-place)\b`)

// DetectFileModification checks if a command is likely to modify files.
// Returns the type of modification detected, or empty string if none.
func DetectFileModification(command string) string {
	if sedModifiesFile(command) {
		return "sed_in_place"
	}
	// Output redirection to a file.
	if redirectPattern.MatchString(command) {
		return "redirect"
	}
	// tee command (writes to file and stdout).
	if teePattern.MatchString(command) {
		return "tee"
	}
	return ""
}

var (
	redirectPattern = regexp.MustCompile(`[^|>]>\s*[^/&>]`) // > file (not >> and not > /dev/null)
	teePattern      = regexp.MustCompile(`\btee\s+[^|]`)
)

// shellSegmentSplit splits a command on shell separators so an in-place edit in
// one segment doesn't pull tokens from another (sed -i a.go && cat b.go).
var shellSegmentSplit = regexp.MustCompile(`\|\||&&|[|;&\n]`)

// redirectTargetPattern captures the target of a single '>' redirect (not '>>').
var redirectTargetPattern = regexp.MustCompile(`(?:^|[^>])>\s*([^\s|&>;]+)`)

// InPlaceFileTargets returns candidate file paths a command modifies IN PLACE —
// sed -i targets and single-'>' redirect targets — so exec can checkpoint them
// before running, giving exec's destructive file edits the same /rollback net the
// fs Write/Edit tools already have.
//
// It is deliberately OVER-inclusive and pure (no filesystem touch): the caller
// resolves each candidate against the workdir and snapshots only those that exist
// as regular files. So a misparsed sed script fragment or flag simply fails the
// existence check and drops out — false candidates are harmless (snapshots dedupe
// by SHA), and the only failure mode worth avoiding is missing the REAL target,
// which over-inclusion prevents. Globs / here-docs / exotic quoting yield extra or
// no candidates and fall through unsnapshotted — no worse than before this guard.
func InPlaceFileTargets(command string) []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(tok string) {
		tok = strings.Trim(tok, "'\"")
		if tok == "" {
			return
		}
		if _, ok := seen[tok]; ok {
			return
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	for _, seg := range shellSegmentSplit.Split(command, -1) {
		// sed -i / --in-place: every trailing non-flag bareword is a candidate.
		if sedInPlacePattern.MatchString(seg) {
			for i, f := range strings.Fields(seg) {
				if i == 0 || strings.HasPrefix(f, "-") {
					continue // command word + flags (incl. -i.bak)
				}
				add(f)
			}
		}
		// single '>' redirect (not '>>'): the token after '>' is the target.
		if m := redirectTargetPattern.FindStringSubmatch(seg); m != nil {
			add(m[1])
		}
	}
	return out
}
