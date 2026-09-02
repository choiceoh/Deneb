package translateops

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The browser client declares the same two envelope prefixes this package parses
// and emits. Nothing binds them: the Go tests below build their fixtures from the
// Go constants, and the Kotlin asset-contract test only greps the JS — so the two
// sides could drift apart and every suite would stay green while inline-split
// blocks silently stopped translating.
//
// This test is the binding. It reads the client asset, evaluates its JS string
// literals, and compares the bytes.
//
// It exists because of a false alarm, not a real one: on 2026-08-30 an
// investigation concluded the sentinel had been dropped from the Go side seven
// weeks earlier. It had not — U+E000 is a private-use rune that renders as
// nothing in sed, grep and git diff, so a literal one looks like an absent one.
// The drift was imaginary; the *absence of a check* was not.
var jsPrefixDecl = regexp.MustCompile(`(?m)^\s*var\s+(SEGMENT_PAYLOAD_PREFIX|PARTS_RESULT_PREFIX)\s*=\s*'([^']*)'`)

func clientTranslateAsset(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "..", "..",
		"client-android", "app", "composeApp", "src", "androidMain",
		"assets", "deneb-translate.js")
	data, err := os.ReadFile(path)
	if err != nil {
		// A moved asset must fail loudly rather than silently stop checking.
		t.Fatalf("read client translate asset %s: %v", path, err)
	}
	return string(data)
}

// evalJSString resolves the \uXXXX escapes a JS single-quoted literal may carry.
// strconv.Unquote on a Go-quoted copy is enough: both languages spell \uXXXX the
// same way, and these literals contain no other escapes.
func evalJSString(t *testing.T, raw string) string {
	t.Helper()
	value, err := strconv.Unquote(`"` + raw + `"`)
	if err != nil {
		t.Fatalf("evaluate JS literal %q: %v", raw, err)
	}
	return value
}

func TestClientAndGatewayAgreeOnEnvelopePrefixes(t *testing.T) {
	matches := jsPrefixDecl.FindAllStringSubmatch(clientTranslateAsset(t), -1)
	found := make(map[string]string, 2)
	for _, m := range matches {
		found[m[1]] = evalJSString(t, m[2])
	}
	if len(found) != 2 {
		t.Fatalf("expected both prefix declarations in the client asset, got %d: %v", len(found), found)
	}

	for _, tc := range []struct {
		jsName  string
		gateway string
	}{
		{"SEGMENT_PAYLOAD_PREFIX", translateSegmentEnvelopePrefix},
		{"PARTS_RESULT_PREFIX", translatePartsEnvelopePrefix},
	} {
		if got := found[tc.jsName]; got != tc.gateway {
			// %q so an invisible rune difference is readable in the failure.
			t.Errorf("%s: client sends %q, gateway uses %q", tc.jsName, got, tc.gateway)
		}
	}
}

// The parser must accept what the client actually ships, and the parts encoder
// must emit what the client actually matches on. Both are checked against the
// asset's own literals rather than this package's constants, so a one-sided edit
// fails here instead of in the field.
func TestParserAcceptsClientEnvelopeAndEncoderEmitsClientPrefix(t *testing.T) {
	matches := jsPrefixDecl.FindAllStringSubmatch(clientTranslateAsset(t), -1)
	found := make(map[string]string, 2)
	for _, m := range matches {
		found[m[1]] = evalJSString(t, m[2])
	}

	segment := found["SEGMENT_PAYLOAD_PREFIX"] +
		`{"text":"Hello there","context":"Hello there world","role":"body"}`
	in := parseTranslateInput(segment)
	if in.Text != "Hello there" {
		t.Errorf("client envelope not parsed: Text = %q (whole envelope would go to DeepL as source)", in.Text)
	}
	if in.Context != "Hello there world" || in.Role != "body" {
		t.Errorf("client envelope context/role lost: %q / %q", in.Context, in.Role)
	}

	parts := parseTranslateInput(found["SEGMENT_PAYLOAD_PREFIX"] + `{"parts":["Hello ","there"],"role":"body"}`)
	if len(parts.Parts) != 2 {
		t.Fatalf("client parts envelope not parsed: %+v", parts)
	}

	out := make([]string, 1)
	if !encodeDeepLParts(out, [][]string{{"안녕 ", "하세요"}}) {
		t.Fatal("encodeDeepLParts failed")
	}
	if !strings.HasPrefix(out[0], found["PARTS_RESULT_PREFIX"]) {
		// The client scores an unmatched parts reply as a permanent, non-retryable
		// failure for the whole unit — the block stays in the source language for
		// the life of the document.
		t.Errorf("parts reply %q does not carry the prefix the client matches (%q)", out[0], found["PARTS_RESULT_PREFIX"])
	}
}

// jsBatchCharsDecl reads the client's per-request payload budget.
var jsBatchCharsDecl = regexp.MustCompile(`(?m)^\s*var\s+MAX_BATCH_PAYLOAD_CHARS\s*=\s*(\d+)\s*;`)

// TestContractClientBatchFitsOneServerWave binds two constants that live in
// different languages and repositories-worth of distance, and whose drift is
// SILENT — nothing fails, the page just gets slower.
//
// The client ships one request; this package splits it into
// translateMaxCharsPerBatch chunks and runs translateMaxConcurrentBatches of them
// at once. So a request larger than chars×concurrency costs a second DeepL wave
// — a full round trip (~1s measured) on the tail request that finishes the page.
//
// It was already off by 2,000 chars when this test was written (client 20,000 vs
// a 18,000 wave), which is exactly the failure mode: someone retunes one side for
// a good reason and the other side quietly stops fitting.
func TestContractClientBatchFitsOneServerWave(t *testing.T) {
	m := jsBatchCharsDecl.FindStringSubmatch(clientTranslateAsset(t))
	if m == nil {
		t.Fatal("MAX_BATCH_PAYLOAD_CHARS not found in deneb-translate.js — did the declaration change shape?")
	}
	clientBudget, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("MAX_BATCH_PAYLOAD_CHARS = %q: %v", m[1], err)
	}
	oneWave := translateMaxCharsPerBatch * translateMaxConcurrentBatches
	if clientBudget > oneWave {
		t.Fatalf("client ships up to %d chars per request but one server wave covers %d "+
			"(translateMaxCharsPerBatch %d × translateMaxConcurrentBatches %d) — the tail request "+
			"pays an extra DeepL round trip. Lower the client budget or raise the server bounds.",
			clientBudget, oneWave, translateMaxCharsPerBatch, translateMaxConcurrentBatches)
	}
	// A budget far under one wave is also a bug: it splits work the server could
	// have done in a single wave into extra client round trips.
	if clientBudget*2 < oneWave {
		t.Fatalf("client budget %d is less than half of one server wave (%d) — the page pays "+
			"extra gateway round trips for batches the server would have run together",
			clientBudget, oneWave)
	}
}
