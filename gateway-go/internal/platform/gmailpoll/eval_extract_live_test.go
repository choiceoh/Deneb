package gmailpoll

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// TestExtractForEval_Live runs the REAL deal extractor (real prompt + jsonutil
// parse + amount gate) against several models through the wormhole router, to prove
// the production-fidelity extraction-benchmark idea: each model is scored on the
// exact task Deneb runs in prod, and a model that fences its JSON still extracts
// correctly (jsonutil strips it) — the failure mode the raw sparkfleet probe
// penalized. Skipped unless DENEB_EVAL_EXTRACT_LIVE=1.
//
//	DENEB_EVAL_EXTRACT_LIVE=1 \
//	DENEB_EVAL_WORMHOLE_URL=http://100.111.114.20:18800/v1 \
//	DENEB_EVAL_WORMHOLE_TOKEN=... \
//	go test -run TestExtractForEval_Live -v ./internal/platform/gmailpoll/
func TestExtractForEval_Live(t *testing.T) {
	if os.Getenv("DENEB_EVAL_EXTRACT_LIVE") != "1" {
		t.Skip("set DENEB_EVAL_EXTRACT_LIVE=1 (+ wormhole url/token) to run")
	}
	url := os.Getenv("DENEB_EVAL_WORMHOLE_URL")
	tok := os.Getenv("DENEB_EVAL_WORMHOLE_TOKEN")
	if url == "" || tok == "" {
		t.Fatal("DENEB_EVAL_WORMHOLE_URL and DENEB_EVAL_WORMHOLE_TOKEN required")
	}
	// The exact analysis text the sparkfleet deneb-deal-extract case feeds — the
	// JOCA Cable quote, including the OCR'd attachment. Prod truth: counterparty
	// "JOCA Cable", 견적서, 5,000,000원, 2026-09-30.
	const input = "이메일: JOCA Cable (fred@jocacable.com) — 2026년 2분기 태양광 케이블 가격 협상.\n" +
		"첨부 문서 (OCR):\n견적서\n발행일: 2026-06-10\n거래처: JOCA Cable (대표이사 Fred Lee)\n" +
		"품목: 6mm² 600V 케이블, 10mm² 600V 케이블\n수량: 월 5톤 이상\n총액: 5,000,000원\n" +
		"납기: 2026-09-30\n결제조건: 선금 30%, 납기 시 70%"

	client := llm.NewClient(url, tok)
	for _, model := range []string{"qwen3.6-35b-a3b", "deepseek-v4-flash", "glm-5.2"} {
		t.Run(model, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			start := time.Now()
			res, err := ExtractForEval(ctx, client, model, "deal", input)
			took := time.Since(start)
			if err != nil {
				t.Fatalf("%s: %v (%s)", model, err, took)
			}
			deal, _ := res.(*DealInfo)
			if deal == nil {
				t.Errorf("%s: extracted NO deal (nil) in %s — real-pipeline extraction failed", model, took)
				return
			}
			b, _ := json.Marshal(deal)
			t.Logf("%s (%s): %s", model, took.Round(time.Millisecond), b)
			// Prod-truth field checks (jsonutil already stripped any code fences).
			if deal.Counterparty != "JOCA Cable" {
				t.Errorf("%s: counterparty=%q want JOCA Cable", model, deal.Counterparty)
			}
			if deal.Amount == "" {
				t.Errorf("%s: amount dropped (the amount gate or extraction lost 5,000,000원)", model)
			}
		})
	}
}
