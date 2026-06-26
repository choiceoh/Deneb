// exec_snapshot.go extends rollback coverage to exec-driven file mutations.
//
// The write/edit fs tools snapshot a file's prior content (via
// snapshotBeforeWrite) so /rollback can undo them. The exec tool was a gap: an
// agent running `sed -i`, `mv`, `rm`, a `>` redirect, `tee`, etc. mutates files
// the checkpoint system never recorded, so those changes could not be rolled
// back.
//
// This file closes the gap conservatively (approach "b": parse the command for
// obvious file targets). The exec tool does not track which files a command
// touches — process.ExecRequest carries only a working directory, not a file
// list — so approach "a" (snapshot known targets) is not available; and the
// checkpoint store is file-by-file (content blobs keyed by path), so there is no
// notion of snapshotting a whole directory. Parsing the small set of
// unambiguous, positional file-mutating forms is therefore the lower-risk fit.
//
// Everything here is BEST-EFFORT and NON-FATAL: targets we cannot confidently
// identify are simply not snapshotted (the command still runs), and any snapshot
// failure is logged like fs.go's snapshotBeforeWrite without blocking exec. The
// guarantee is "more rollback coverage than before", never "complete coverage" —
// shell expansions, globs, indirection ($(...), xargs, find -exec), and tools we
// don't model (awk -i inplace, perl -i, python scripts) are not covered, and
// snapshots remain file-content only (no transcript / side-effect rollback; see
// pkg/checkpoint/types.go).
package tools

import (
	"context"
	"os"
	"regexp"
	"strings"
)

// mutatingBaseCmd is the set of base commands whose targets we snapshot
// positionally. Redirection (`>`) and `sed -i` are detected separately, so a
// command can be mutating without its base being in this set.
var mutatingBaseCmd = map[string]bool{
	"mv": true, "cp": true, "rm": true, "tee": true, "sed": true,
}

// looksFileMutating is a cheap pre-filter so exec doesn't run the full target
// parser on the common non-mutating command (ls, cat, grep, go test, …). It is
// intentionally permissive: a false positive just runs the parser (which then
// finds no targets); a false negative would silently skip snapshotting, so the
// checks here must stay a SUPERSET of what execMutationTargets can act on.
func looksFileMutating(command string) bool {
	if DetectFileModification(command) != "" { // sed_in_place / redirect / tee
		return true
	}
	// DetectFileModification's redirect pattern requires a char before `>`, so a
	// segment-leading redirect (`> file`, or after `&&`/`|`) can slip past it;
	// redirectSingle (which the parser uses) also matches the leading form, so
	// check it here to keep this a strict superset of execMutationTargets.
	if redirectSingle.MatchString(command) {
		return true
	}
	for _, seg := range segmentSplit.Split(command, -1) {
		fields := strings.Fields(seg)
		if len(fields) == 0 {
			continue
		}
		base := fields[0]
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			base = base[idx+1:]
		}
		if mutatingBaseCmd[base] {
			return true
		}
	}
	return false
}

// execTarget is one parsed file the command is likely to touch. mustExist
// distinguishes operands of commands that act on existing files (sed -i, mv, cp,
// rm) from targets a command may CREATE (`> file`, `tee file`). The caller
// snapshots a create-capable target unconditionally (so a freshly created file
// can be rolled back to "did not exist"), but a must-exist target only when it
// actually resolves to a regular file — guarding against snapshotting a
// mis-parsed operand (e.g. a sed script that isn't a real path) as a bogus
// tombstone that /rollback would later try to delete.
type execTarget struct {
	raw       string
	mustExist bool
}

// snapshotExecTargets best-effort snapshots the files an exec command is likely
// to mutate, BEFORE the command runs, so /rollback can undo them. It mirrors the
// write/edit tools' snapshotBeforeWrite: it is a no-op when no Checkpointer is
// attached to ctx, snapshot failures are non-fatal (logged at Error inside
// snapshotBeforeWrite), and it dedupes by SHA-256 in the Manager, so re-running
// the same command does not spam the index.
//
// command is the raw shell command; workDir is the resolved working directory
// exec will run it in (used to resolve relative file arguments, exactly as the
// fs tools resolve against defaultDir). Targets that escape the workspace root
// are dropped by ResolvePath's containment check.
func snapshotExecTargets(ctx context.Context, command, workDir string) {
	for _, t := range execMutationTargets(command) {
		// Resolve relative targets against the command's working directory and
		// clamp to the workspace root (ResolvePath). An empty workDir leaves a
		// relative path relative to the process CWD, which the Manager then makes
		// absolute — acceptable best-effort behaviour.
		path := ResolvePath(t.raw, workDir)
		if t.mustExist {
			// Only snapshot operands of existing-file commands when the path is a
			// real regular file; skip scripts/options/typos so we don't record a
			// spurious tombstone. Symlinks/dirs/specials are skipped too — the fs
			// tools only ever snapshot regular files.
			if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
				continue
			}
		}
		snapshotBeforeWrite(ctx, path, "exec")
	}
}

// segmentSplit splits a command line into segments on the shell control
// operators that separate distinct simple commands (`;`, `&&`, `||`, `|`, `&`)
// so each segment can be parsed positionally on its own. It is intentionally
// naive — it does not honour quoting around the operators — because a missed or
// over-eager split only ever changes WHICH files we snapshot, never whether the
// command runs. Operators inside quotes are rare in the mutating forms we model.
var segmentSplit = regexp.MustCompile(`\|\||&&|;|\||&`)

// execMutationTargets parses command and returns the files it is likely to
// mutate, tagged by whether they must already exist. It recognises a
// deliberately small, high-precision set of positional forms; anything outside
// that set yields no targets (the command still runs, just without added
// rollback coverage):
//
//   - redirection `> file` (not `>>` append, not `>&`/`2>`, not `> /dev/…`)
//     → create-capable (snapshot even if absent: enables rollback-to-deleted)
//   - `tee [-a] file [file…]`                                  → create-capable
//   - `sed -i …/--in-place [script] file [file…]`              → must-exist
//   - `mv  src… dest` (sources vanish, dest is overwritten)    → must-exist
//   - `cp  src… dest` (dest is overwritten; sources snapshotted harmlessly so a
//     follow-up `rm` of a source still rolls back)             → must-exist
//   - `rm  file…` (each non-flag argument)                     → must-exist
//
// Returned paths may be relative; the caller resolves them and applies the
// must-exist gate. Duplicates (by raw token) are removed; the first occurrence's
// mustExist wins, which is intentional — a token first seen as a redirect target
// (create-capable) should not be downgraded to must-exist by a later mention.
func execMutationTargets(command string) []execTarget {
	var out []execTarget
	seen := map[string]bool{}
	add := func(tok string, mustExist bool) {
		tok = unquote(tok)
		if tok == "" || isDevNull(tok) {
			return
		}
		// Skip anything still containing a shell glob/metachar — we cannot know
		// statically which files it expands to, and snapshotting the literal
		// (e.g. "*.go") would be meaningless.
		if strings.ContainsAny(tok, "*?{}$`~[]()") {
			return
		}
		if seen[tok] {
			return
		}
		seen[tok] = true
		out = append(out, execTarget{raw: tok, mustExist: mustExist})
	}

	for _, seg := range segmentSplit.Split(command, -1) {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		// Redirection targets can appear in any segment regardless of the base
		// command, so scan for them first. A redirect may create the file.
		for _, t := range redirectTargets(seg) {
			add(t, false)
		}

		fields := strings.Fields(seg)
		// Strip leading env assignments (FOO=bar) and benign prefixes so the
		// base command lines up — same normalisation as extractBaseCommand.
		for len(fields) > 0 {
			f := fields[0]
			if strings.Contains(f, "=") && !strings.HasPrefix(f, "-") {
				fields = fields[1:]
				continue
			}
			if f == "sudo" || f == "env" || f == "nice" || f == "nohup" || f == "time" {
				fields = fields[1:]
				continue
			}
			break
		}
		if len(fields) == 0 {
			continue
		}
		base := fields[0]
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			base = base[idx+1:]
		}
		operands := nonFlagArgs(fields[1:])

		switch base {
		case "sed":
			if sedModifiesFile(seg) {
				// `sed -i SCRIPT FILE…`: the first operand is the script
				// (also true for `sed -i -e SCRIPT FILE` and `-f PROG FILE`,
				// where the script/prog is the first surviving operand). Drop it;
				// the remaining operands are the files edited in place. The
				// must-exist gate discards any stray script token that slips
				// through, so a worst case is under- not mis-coverage.
				if len(operands) > 1 {
					for _, t := range operands[1:] {
						add(t, true)
					}
				}
			}
		case "tee":
			for _, t := range operands {
				add(t, false)
			}
		case "mv", "cp":
			for _, t := range operands {
				add(t, true)
			}
		case "rm":
			for _, t := range operands {
				add(t, true)
			}
		}
	}
	return out
}

// redirectSingle matches a single `>` output redirect to a filename, capturing
// the target. It deliberately rejects: `>>` (append — file content is preserved,
// less urgent and the prior content is still partly recoverable), `>&`/`&>`/`2>`
// (fd dups / stderr redirects), and is followed up by a /dev/* drop in add().
// A leading char class `[^>&|]` ensures the `>` is not the second char of `>>`
// or part of `2>`/`&>`; (?:^|[^>&\d]) handles start-of-segment and the common
// `cmd >file` / `cmd > file` spacing.
var redirectSingle = regexp.MustCompile(`(?:^|[^>&\d])>\s*([^\s>&|;]+)`)

// redirectTargets extracts filenames that segment redirects stdout into.
func redirectTargets(seg string) []string {
	var out []string
	for _, m := range redirectSingle.FindAllStringSubmatch(seg, -1) {
		if len(m) == 2 {
			out = append(out, m[1])
		}
	}
	return out
}

// nonFlagArgs drops option flags (-i, --in-place, …) and returns the operands.
// A bare "--" terminates option parsing (everything after is an operand), and a
// lone "-" (stdin/stdout) is dropped. It does not attempt to associate values
// with flags that take separate arguments; the mutating commands we model
// (sed/tee/mv/cp/rm) take their file operands positionally, not via valued flags.
func nonFlagArgs(args []string) []string {
	var out []string
	optsDone := false
	for _, a := range args {
		if !optsDone {
			if a == "--" {
				optsDone = true
				continue
			}
			if a == "-" {
				continue
			}
			if strings.HasPrefix(a, "-") {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

// unquote strips a single matching pair of surrounding quotes so a quoted target
// ("my file.txt") resolves to its literal path. Inner quotes/escapes are left
// as-is — full shell unquoting is out of scope and unnecessary for the simple
// targets we model.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// isDevNull reports whether tok is a /dev/* sink we must never snapshot.
func isDevNull(tok string) bool {
	return tok == "/dev/null" || strings.HasPrefix(tok, "/dev/")
}
