// Package solarflow reads the topsolar SolarFlow ERP analytics engine
// (choiceoh/solarflow, a separate Rust/axum service) over its read-only
// /api/calc/* REST surface. SolarFlow ingests the same Amaranth ledger the
// groupware tool reads, but exposes *derived* intelligence Amaranth does not:
// landed cost, margin, LC maturity/limit timelines, supply forecast, receipt
// matching, inventory turnover, and a Korean natural-language search.
//
// On srv4 the engine runs locally (docker, 127.0.0.1:8081) with no auth — the
// default config just works. Everything here is read-only; there is no write
// path to SolarFlow from Deneb.
package solarflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const defaultBaseURL = "http://127.0.0.1:8081"

// Config points at one SolarFlow engine and the company to scope queries to.
type Config struct {
	BaseURL   string        // engine root, e.g. http://127.0.0.1:8081
	CompanyID string        // default company uuid (탑솔라 on this tenant); agent may override
	Token     string        // optional bearer, sent only when non-empty
	Timeout   time.Duration // per-request timeout
}

// FromEnv loads DENEB_SOLARFLOW_* settings. The URL always has a working
// default (local engine), so the second return is true whenever the engine is
// meant to be reachable; the company id is what actually gates real queries and
// is reported by StatusLine. Company id is env-driven (not hardcoded) because it
// is tenant configuration, not source.
func FromEnv() (Config, bool) {
	base := strings.TrimSpace(os.Getenv("DENEB_SOLARFLOW_URL"))
	if base == "" {
		base = defaultBaseURL
	}
	return Config{
		BaseURL:   strings.TrimRight(base, "/"),
		CompanyID: strings.TrimSpace(os.Getenv("DENEB_SOLARFLOW_COMPANY_ID")),
		Token:     strings.TrimSpace(os.Getenv("DENEB_SOLARFLOW_TOKEN")),
		Timeout:   25 * time.Second,
	}, true
}

// StatusLine reports config + whether the local engine answers /health. Never
// prints the token.
func StatusLine(ctx context.Context, cfg Config) string {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	health := probeHealth(ctx, cfg)
	comp := strings.TrimSpace(cfg.CompanyID)
	if comp == "" {
		comp = "미설정 (DENEB_SOLARFLOW_COMPANY_ID) — company 파라미터로 uuid를 넘기거나 env를 설정하라"
	}
	return fmt.Sprintf("SolarFlow ERP 분석: %s · 엔진=%s · 기본회사=%s · 읽기 전용(재고·마진·거래처·미수금·LC·수급전망·회전율·자연어검색)",
		health, base, comp)
}

func probeHealth(ctx context.Context, cfg Config) string {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	hctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(hctx, http.MethodGet, base+"/health", nil)
	if err != nil {
		return "연결 확인 불가"
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "꺼짐/도달 불가"
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return "연결됨"
	}
	return fmt.Sprintf("응답 %d", resp.StatusCode)
}

// endpoints maps a stable action name to the engine path. price-forecast-strategy
// is intentionally omitted: it is a market-observation calculator, not an ERP
// query, and takes inputs the agent does not have on hand.
var endpoints = map[string]string{
	"search":           "/api/calc/search",
	"inventory":        "/api/calc/inventory",
	"margin":           "/api/calc/margin-analysis",
	"customer":         "/api/calc/customer-analysis",
	"outstanding":      "/api/calc/outstanding-list",
	"receipt_match":    "/api/calc/receipt-match-suggest",
	"turnover":         "/api/calc/inventory-turnover",
	"price_trend":      "/api/calc/price-trend",
	"supply_forecast":  "/api/calc/supply-forecast",
	"order_risk":       "/api/calc/order-fulfillment-risk",
	"lc_maturity":      "/api/calc/lc-maturity-alert",
	"lc_limit":         "/api/calc/lc-limit-timeline",
	"lc_fee":           "/api/calc/lc-fee",
	"landed_cost":      "/api/calc/landed-cost",
	"exchange_compare": "/api/calc/exchange-compare",
}

// Actions returns the sorted action names (for schema/help/validation).
func Actions() []string {
	out := make([]string, 0, len(endpoints))
	for k := range endpoints {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Request is one read-only query. Only the fields relevant to Action are used.
type Request struct {
	Action         string
	CompanyID      string  // resolved company uuid (already defaulted by the caller)
	Query          string  // search
	Customer       string  // outstanding/receipt_match: customer name to resolve, or a uuid
	CustomerID     string  // outstanding/receipt_match/customer/margin filter: explicit uuid
	ProductID      string  // optional uuid filter
	ManufacturerID string  // optional uuid filter
	DateFrom       string  // margin/customer: YYYY-MM-DD
	DateTo         string  // margin/customer: YYYY-MM-DD
	Horizon        int     // lc_maturity=days_ahead, lc_limit/supply_forecast=months_ahead, turnover=days
	ReceiptAmount  float64 // receipt_match
	CostBasis      string  // margin/customer: cif|landed|fifo
	Period         string  // price_trend: quarterly|monthly
	Limit          int     // max rows shown (default 20, cap 50)
}

// Run executes one read-only query and returns a chat-ready Korean string. On a
// handled error it returns a calm message plus the error (callers may show the
// message and ignore err).
func Run(ctx context.Context, cfg Config, req Request) (string, error) {
	path, ok := endpoints[req.Action]
	if !ok {
		return fmt.Sprintf("알 수 없는 solarflow action %q — %s 중 하나.", req.Action, strings.Join(Actions(), "|")),
			fmt.Errorf("unknown action %q", req.Action)
	}
	company := strings.TrimSpace(req.CompanyID)
	if company == "" {
		return "회사(company_id)가 필요합니다 — DENEB_SOLARFLOW_COMPANY_ID env를 설정하거나 company 파라미터로 uuid를 넘겨라.",
			fmt.Errorf("company id unset")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	// outstanding/receipt_match need a customer uuid; resolve a name if given.
	customerID := strings.TrimSpace(req.CustomerID)
	if req.Action == "outstanding" || req.Action == "receipt_match" {
		if customerID == "" {
			resolved, msg := resolveCustomer(ctx, cfg, company, req.Customer, limit)
			if resolved == "" {
				return msg, fmt.Errorf("customer unresolved")
			}
			customerID = resolved
		}
		if req.Action == "receipt_match" && req.ReceiptAmount <= 0 {
			return "receipt_match에는 receipt_amount(>0)가 필요합니다 — 입금액을 넘겨라.", fmt.Errorf("receipt_amount required")
		}
	}

	body := buildBody(req, company, customerID)
	raw, err := post(ctx, cfg, path, body)
	if err != nil {
		return fmt.Sprintf("SolarFlow 조회 실패: %v", err), err
	}
	if apiErr := extractError(raw); apiErr != "" {
		return "SolarFlow: " + apiErr, fmt.Errorf("engine error: %s", apiErr)
	}

	if req.Action == "search" {
		return formatSearch(raw, company, limit), nil
	}
	return formatGeneric(req.Action, company, raw, limit), nil
}

// buildBody assembles the engine request map, including only fields that apply.
func buildBody(req Request, company, customerID string) map[string]any {
	b := map[string]any{"company_id": company}
	add := func(k, v string) {
		if s := strings.TrimSpace(v); s != "" {
			b[k] = s
		}
	}
	switch req.Action {
	case "search":
		add("query", req.Query)
	case "inventory", "exchange_compare":
		add("product_id", req.ProductID)
		add("manufacturer_id", req.ManufacturerID)
	case "margin":
		add("manufacturer_id", req.ManufacturerID)
		add("customer_id", customerID)
		add("product_id", req.ProductID)
		add("date_from", req.DateFrom)
		add("date_to", req.DateTo)
		add("cost_basis", req.CostBasis)
	case "customer":
		add("customer_id", customerID)
		add("date_from", req.DateFrom)
		add("date_to", req.DateTo)
		add("cost_basis", req.CostBasis)
	case "outstanding":
		b["customer_id"] = customerID
	case "receipt_match":
		b["customer_id"] = customerID
		b["receipt_amount"] = req.ReceiptAmount
	case "turnover":
		if req.Horizon > 0 {
			b["days"] = req.Horizon
		}
	case "lc_maturity":
		if req.Horizon > 0 {
			b["days_ahead"] = req.Horizon
		}
	case "lc_limit", "supply_forecast":
		if req.Horizon > 0 {
			b["months_ahead"] = req.Horizon
		}
		add("product_id", req.ProductID)
		add("manufacturer_id", req.ManufacturerID)
	case "price_trend":
		add("manufacturer_id", req.ManufacturerID)
		add("product_id", req.ProductID)
		add("period", req.Period)
	case "order_risk", "lc_fee", "landed_cost":
		// company_id alone is a valid query for these.
	}
	return b
}

// resolveCustomer maps a customer name to a uuid via customer-analysis (which
// lists every customer with ids). Exact match wins; else a single substring
// match; else a disambiguation message. If name itself looks like a uuid it is
// returned as-is.
func resolveCustomer(ctx context.Context, cfg Config, company, name string, limit int) (id, msg string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "outstanding/receipt_match에는 customer(거래처명) 또는 customer_id가 필요합니다."
	}
	if looksLikeUUID(name) {
		return name, ""
	}
	raw, err := post(ctx, cfg, endpoints["customer"], map[string]any{"company_id": company})
	if err != nil {
		return "", fmt.Sprintf("거래처 해석 실패(customer-analysis): %v", err)
	}
	var parsed struct {
		Items []struct {
			CustomerID   string `json:"customer_id"`
			CustomerName string `json:"customer_name"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &parsed) != nil || len(parsed.Items) == 0 {
		return "", "거래처 목록을 읽지 못했습니다."
	}
	lower := strings.ToLower(name)
	type cand struct{ id, nm string }
	var exact, subs []cand
	for _, it := range parsed.Items {
		n := strings.TrimSpace(it.CustomerName)
		if n == "" || strings.TrimSpace(it.CustomerID) == "" {
			continue
		}
		ln := strings.ToLower(n)
		switch {
		case ln == lower:
			exact = append(exact, cand{it.CustomerID, n})
		case strings.Contains(ln, lower):
			subs = append(subs, cand{it.CustomerID, n})
		}
	}
	if len(exact) == 1 {
		return exact[0].id, ""
	}
	cands := exact
	if len(cands) == 0 {
		cands = subs
	}
	if len(cands) == 1 {
		return cands[0].id, ""
	}
	if len(cands) == 0 {
		return "", fmt.Sprintf("거래처 %q를 찾지 못했습니다. customer=정확한 이름 또는 customer_id=uuid로 다시 시도하라.", name)
	}
	var names []string
	for i, c := range cands {
		if i >= limit {
			names = append(names, fmt.Sprintf("…외 %d", len(cands)-i))
			break
		}
		names = append(names, c.nm)
	}
	return "", fmt.Sprintf("거래처 %q가 여러 건입니다: %s. 더 정확한 이름이나 customer_id로 지정하라.", name, strings.Join(names, ", "))
}

func looksLikeUUID(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

// post issues one JSON POST and returns the raw response body.
func post(ctx context.Context, cfg Config, path string, body map[string]any) (json.RawMessage, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, base+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if t := strings.TrimSpace(cfg.Token); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 500 {
		// A 5xx with a Korean {"error":...} body is a handled validation/compute
		// failure — hand it back so the caller surfaces the message.
		if msg := extractError(data); msg != "" {
			return data, nil
		}
		return nil, fmt.Errorf("engine http %d", resp.StatusCode)
	}
	return data, nil
}

func extractError(raw json.RawMessage) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil {
		return strings.TrimSpace(e.Error)
	}
	return ""
}
