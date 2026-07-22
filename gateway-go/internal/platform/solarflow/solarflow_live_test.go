package solarflow

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveEngine exercises Run against a real SolarFlow engine. It is skipped
// unless DENEB_SOLARFLOW_LIVE=1 (CI stays hermetic; run manually on a host where
// the engine is reachable). Company id comes from DENEB_SOLARFLOW_COMPANY_ID or
// falls back to the topsolar tenant id.
func TestLiveEngine(t *testing.T) {
	if os.Getenv("DENEB_SOLARFLOW_LIVE") != "1" {
		t.Skip("set DENEB_SOLARFLOW_LIVE=1 to run the live SolarFlow integration smoke")
	}
	cfg, _ := FromEnv()
	if cfg.CompanyID == "" {
		cfg.CompanyID = "99f0fc15-0555-4a41-a025-8bf3630a7947" // 탑솔라(주)
	}
	cfg.Timeout = 20 * time.Second
	ctx := context.Background()

	cases := []struct {
		name    string
		req     Request
		mustHit []string // substrings that prove live data flowed
	}{
		{"search", Request{Action: "search", CompanyID: cfg.CompanyID, Query: "모듈 재고", Limit: 5}, []string{"SolarFlow 검색"}},
		{"inventory", Request{Action: "inventory", CompanyID: cfg.CompanyID, Limit: 5}, []string{"SolarFlow inventory", "summary"}},
		{"margin", Request{Action: "margin", CompanyID: cfg.CompanyID, Limit: 3}, []string{"SolarFlow margin", "summary"}},
		{"turnover", Request{Action: "turnover", CompanyID: cfg.CompanyID, Limit: 3}, []string{"SolarFlow turnover", "total"}},
		{"lc_maturity", Request{Action: "lc_maturity", CompanyID: cfg.CompanyID, Horizon: 30}, []string{"SolarFlow lc_maturity"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Run(ctx, cfg, tc.req)
			if err != nil {
				t.Fatalf("%s: %v (out=%q)", tc.name, err, out)
			}
			for _, want := range tc.mustHit {
				if !strings.Contains(out, want) {
					t.Errorf("%s: missing %q in output:\n%s", tc.name, want, truncate(out, 800))
				}
			}
			t.Logf("%s OK:\n%s", tc.name, truncate(out, 500))
		})
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
