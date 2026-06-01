package tools

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestWeeklyReport_Live runs the collector against the real wiki on the host.
// CI skips it (no wiki present). On the gateway host:
//
//	DENEB_WEEKLY_LIVE=1 go test -run TestWeeklyReport_Live ./internal/pipeline/chat/tools/ -v
func TestWeeklyReport_Live(t *testing.T) {
	if os.Getenv("DENEB_WEEKLY_LIVE") == "" {
		t.Skip("set DENEB_WEEKLY_LIVE=1 to run against the live wiki")
	}
	wikiDir := os.Getenv("DENEB_WIKI_DIR")
	if wikiDir == "" {
		wikiDir = os.ExpandEnv("$HOME/.deneb/wiki")
	}
	opts := WeeklyReportOpts{WikiDir: wikiDir}
	out, err := CollectWeeklyReportData(context.Background(), opts, time.Now())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if !strings.Contains(out, "groups") {
		t.Fatalf("unexpected output (no groups): %s", out)
	}
	t.Logf("weekly report data (%d bytes):\n%s", len(out), out)

	// Full pipeline: compose HTML + render PDF (or text fallback).
	pdfPath, textFallback, rendered := BuildWeeklyReportPDF(context.Background(), opts, time.Now())
	t.Logf("rendered=%v pdf=%q", rendered, pdfPath)
	if rendered {
		fi, err := os.Stat(pdfPath)
		if err != nil || fi.Size() == 0 {
			t.Fatalf("rendered=true but no pdf at %s: %v", pdfPath, err)
		}
		t.Logf("PDF ok: %s (%d bytes)", pdfPath, fi.Size())
	} else {
		t.Logf("PDF render fell back to text (%d bytes):\n%s", len(textFallback), textFallback)
	}
}
