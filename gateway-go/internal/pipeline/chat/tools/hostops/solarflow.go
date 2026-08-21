package hostops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/solarflow"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolSolarflow queries the topsolar SolarFlow ERP analytics engine (read-only,
// /api/calc/*). It complements the groupware tool: groupware reads the raw
// Amaranth ledger, SolarFlow returns derived intelligence (마진·미수금·LC 만기·
// 수급 전망·재고 회전율) plus a Korean natural-language search. Never mutates.
func ToolSolarflow() toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action        string  `json:"action"`
			Company       string  `json:"company"`     // uuid override (default = env company)
			Query         string  `json:"query"`       // search
			Customer      string  `json:"customer"`    // 거래처명 or uuid (outstanding/receipt_match)
			CustomerID    string  `json:"customer_id"` // explicit uuid filter
			Product       string  `json:"product_id"`
			Manufacturer  string  `json:"manufacturer_id"`
			DateFrom      string  `json:"date_from"`
			DateTo        string  `json:"date_to"`
			Horizon       int     `json:"horizon"`
			ReceiptAmount float64 `json:"receipt_amount"`
			CostBasis     string  `json:"cost_basis"`
			Period        string  `json:"period"`
			Limit         int     `json:"limit"`
		}
		if err := jsonutil.UnmarshalInto("solarflow params", input, &p); err != nil {
			return "", err
		}

		cfg, _ := solarflow.FromEnv()
		action := normalizeSolarflowAction(p.Action)
		switch action {
		case "", "status":
			return solarflow.StatusLine(ctx, cfg), nil
		}
		if !solarflowKnownAction(action) {
			return "", fmt.Errorf("unknown solarflow action %q (%s|status)", p.Action, strings.Join(solarflow.Actions(), "|"))
		}

		if action == "search" && strings.TrimSpace(p.Query) == "" {
			return `search에는 query(자연어)가 필요합니다. 예: query="모듈 재고" 또는 "미수금 많은 거래처".`, nil
		}

		company := strings.TrimSpace(p.Company)
		if company == "" {
			company = strings.TrimSpace(cfg.CompanyID)
		}

		out, err := solarflow.Run(ctx, cfg, solarflow.Request{
			Action:         action,
			CompanyID:      company,
			Query:          strings.TrimSpace(p.Query),
			Customer:       strings.TrimSpace(p.Customer),
			CustomerID:     strings.TrimSpace(p.CustomerID),
			ProductID:      strings.TrimSpace(p.Product),
			ManufacturerID: strings.TrimSpace(p.Manufacturer),
			DateFrom:       strings.TrimSpace(p.DateFrom),
			DateTo:         strings.TrimSpace(p.DateTo),
			Horizon:        p.Horizon,
			ReceiptAmount:  p.ReceiptAmount,
			CostBasis:      strings.TrimSpace(p.CostBasis),
			Period:         strings.TrimSpace(p.Period),
			Limit:          p.Limit,
		})
		if err != nil && strings.TrimSpace(out) != "" {
			// out already carries a calm Korean message.
			return out, nil
		}
		if err != nil {
			return fmt.Sprintf("SolarFlow 조회 실패: %v", err), nil
		}
		return out, nil
	}
}

func solarflowKnownAction(a string) bool {
	for _, k := range solarflow.Actions() {
		if k == a {
			return true
		}
	}
	return false
}

// normalizeSolarflowAction lowercases and maps Korean/alias action names.
func normalizeSolarflowAction(raw string) string {
	a := strings.ToLower(strings.TrimSpace(raw))
	switch a {
	case "검색", "찾기", "질의":
		return "search"
	case "재고", "재고집계":
		return "inventory"
	case "마진", "마진분석", "수익":
		return "margin"
	case "거래처", "거래처분석", "고객":
		return "customer"
	case "미수금", "미수", "미수채권", "outstanding_list":
		return "outstanding"
	case "수금매칭", "입금매칭", "receipt", "receipt-match", "receipt_match_suggest":
		return "receipt_match"
	case "회전율", "재고회전", "inventory_turnover":
		return "turnover"
	case "단가", "단가추이", "가격추이", "price-trend":
		return "price_trend"
	case "수급", "수급전망", "공급전망", "supply-forecast":
		return "supply_forecast"
	case "수주위험", "충당위험", "order-risk", "order_fulfillment_risk":
		return "order_risk"
	case "lc만기", "엘씨만기", "lc-maturity", "lc_maturity_alert":
		return "lc_maturity"
	case "lc한도", "한도", "lc-limit", "lc_limit_timeline":
		return "lc_limit"
	case "lc수수료", "수수료", "lc-fee":
		return "lc_fee"
	case "수입원가", "랜디드코스트", "landed-cost":
		return "landed_cost"
	case "환율비교", "환율", "exchange-compare":
		return "exchange_compare"
	}
	return a
}
