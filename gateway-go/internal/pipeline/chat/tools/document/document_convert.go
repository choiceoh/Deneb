package document

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// document_convert.go — conversion fallback for formats the native Go parsers
// don't read (HWP, legacy binary Office, ODF, HWPX). The file-pointer capture
// model makes this natural: an attachment is materialized once, so converting it
// to extractable text up front is all the agent needs to `read` it later.
//
// SECURITY: the input is UNTRUSTED attachment bytes, and LibreOffice in particular
// is a large parser attack surface. Every converter therefore runs like the
// PDF-OCR fallback (rasterizePDF): the bytes go to a throwaway temp dir, the
// command takes only LITERAL arguments (no tainted string reaches a shell), it is
// bounded by a timeout, and LibreOffice gets an isolated per-call user profile so
// a malicious document can't touch shared state and concurrent conversions can't
// fight over one profile lock.

const convertTimeout = 90 * time.Second

// convertUnsupported turns an unsupported document into extractable text via an
// installed converter, or returns docUnsupported when none matches / the tool is
// absent (the caller then reports the format as unsupported, unchanged). Wired
// into extractDocument's default case, so every capture path gains the formats.
func convertUnsupported(ctx context.Context, data []byte, filename string) docResult {
	ext := strings.ToLower(filepath.Ext(filename))
	if isConverterExtension(ext) {
		// LibreOffice content-sniffs its input, so an attacker can rename an ODF/
		// OOXML ZIP bomb to a legacy extension. Detect and preflight ZIP structure
		// before choosing any converter; only genuinely non-ZIP legacy payloads
		// may bypass the archive budget.
		if err := preflightConverterInput(data, ext); err != nil {
			return docResult{kind: docUnsupported, err: err}
		}
	}
	switch ext {
	case ".hwp":
		// hwp5txt is a focused, lightweight HWP→text parser — smaller surface than
		// LibreOffice, so prefer it; fall back to soffice (which also reads .hwp).
		if text, ok, err := hwp5txtToText(ctx, data); err != nil {
			return docResult{kind: docUnsupported, err: err}
		} else if ok {
			return docResult{kind: docText, text: text}
		}
		return sofficeConvertAndExtract(ctx, data, ".hwp")
	case ".doc", ".rtf", ".odt", ".hwpx", ".wps",
		".xls", ".ods",
		".ppt", ".odp":
		return sofficeConvertAndExtract(ctx, data, strings.ToLower(filepath.Ext(filename)))
	default:
		return docResult{kind: docUnsupported}
	}
}

// hwp5txtToText runs hwp5txt on HWP bytes and returns its plain-text output.
func hwp5txtToText(ctx context.Context, data []byte) (string, bool, error) {
	if _, err := exec.LookPath("hwp5txt"); err != nil {
		return "", false, nil
	}
	dir, err := os.MkdirTemp("", "deneb-hwp-")
	if err != nil {
		return "", false, nil
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "in.hwp"), data, 0o600); err != nil {
		return "", false, nil
	}
	text, err := runBoundedTextCommand(ctx, convertTimeout, nil, func(runCtx context.Context, _ io.Reader, stdout, stderr io.Writer) error {
		// Literal args, run inside the temp dir — no tainted input reaches the process.
		cmd := exec.CommandContext(runCtx, "hwp5txt", "in.hwp") //nolint:gosec // G204 — literal args, temp dir
		cmd.Dir = dir
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		return cmd.Run()
	})
	if err != nil {
		if errors.Is(err, errDocumentExtractionLimit) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", false, err
		}
		return "", false, nil
	}
	if text != "" {
		return text, true, nil
	}
	return "", false, nil
}

// sofficeConvertAndExtract converts a LibreOffice-readable format to its OOXML
// counterpart, then re-runs the native extractor on the result (which preserves
// tables). The converted format is always natively handled, so this never
// re-enters extractDocument's default case.
func sofficeConvertAndExtract(ctx context.Context, data []byte, ext string) docResult {
	if _, err := exec.LookPath("soffice"); err != nil {
		return docResult{kind: docUnsupported}
	}
	target := sofficeTarget(ext)
	dir, err := os.MkdirTemp("", "deneb-soffice-")
	if err != nil {
		return docResult{kind: docUnsupported, err: err}
	}
	defer os.RemoveAll(dir)
	inName := "in" + ext
	if err := os.WriteFile(filepath.Join(dir, inName), data, 0o600); err != nil {
		return docResult{kind: docUnsupported, err: err}
	}
	runCtx, cancel := context.WithTimeout(ctx, convertTimeout)
	defer cancel()
	// Isolated per-call profile: a malicious doc can't touch shared LibreOffice
	// state, and parallel conversions don't collide on one profile lock.
	profile := "-env:UserInstallation=file://" + filepath.Join(dir, "loprofile")
	args := []string{"--headless", profile, "--convert-to", target, "--outdir", ".", inName}
	cmd := exec.CommandContext(runCtx, "soffice", args...) //nolint:gosec // G204 — literal args, temp dir, isolated profile
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return docResult{kind: docUnsupported, err: fmt.Errorf("soffice 변환 실패: %w", err)}
	}
	outPath := filepath.Join(dir, "in."+target)
	outBytes, err := readBoundedRegularFile(outPath, maxDocumentOutputBytes)
	_ = os.Remove(outPath) // release converter output disk space before native parsing
	if err != nil {
		return docResult{kind: docUnsupported, err: fmt.Errorf("soffice 변환 출력 없음: %w", err)}
	}
	return extractDocument(ctx, outBytes, "converted."+target, "")
}

func isConverterExtension(ext string) bool {
	switch ext {
	case ".hwp", ".doc", ".rtf", ".odt", ".hwpx", ".wps",
		".xls", ".ods", ".ppt", ".odp":
		return true
	default:
		return false
	}
}

func isZipConverterInput(ext string) bool {
	switch ext {
	case ".hwpx", ".odt", ".ods", ".odp":
		return true
	default:
		return false
	}
}

// sofficeTarget maps an input extension to the OOXML target that best preserves
// its content for the native re-extractor.
func sofficeTarget(ext string) string {
	switch ext {
	case ".xls", ".ods":
		return "xlsx"
	case ".ppt", ".odp":
		return "pptx"
	default: // .doc .rtf .odt .hwp .hwpx .wps
		return "docx"
	}
}
