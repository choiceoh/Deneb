package solarflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLooksLikeUUID(t *testing.T) {
	ok := []string{
		"99f0fc15-0555-4a41-a025-8bf3630a7947",
		"F7E6749A-0E47-4CDD-A656-6B4245D47AC0",
	}
	for _, s := range ok {
		if !looksLikeUUID(s) {
			t.Errorf("expected %q to be a uuid", s)
		}
	}
	bad := []string{"", "탑솔라", "99f0fc15", "99f0fc15_0555_4a41_a025_8bf3630a7947", "zzz0fc15-0555-4a41-a025-8bf3630a7947"}
	for _, s := range bad {
		if looksLikeUUID(s) {
			t.Errorf("expected %q NOT to be a uuid", s)
		}
	}
}

func TestBuildBodyPerAction(t *testing.T) {
	const comp = "99f0fc15-0555-4a41-a025-8bf3630a7947"

	// horizon maps to the right key per action.
	if b := buildBody(Request{Action: "lc_maturity", Horizon: 30}, comp, ""); b["days_ahead"] != 30 {
		t.Errorf("lc_maturity horizon → days_ahead, got %v", b["days_ahead"])
	}
	if b := buildBody(Request{Action: "supply_forecast", Horizon: 3}, comp, ""); b["months_ahead"] != 3 {
		t.Errorf("supply_forecast horizon → months_ahead, got %v", b["months_ahead"])
	}
	if b := buildBody(Request{Action: "turnover", Horizon: 60}, comp, ""); b["days"] != 60 {
		t.Errorf("turnover horizon → days, got %v", b["days"])
	}

	// receipt_match carries amount + resolved customer.
	b := buildBody(Request{Action: "receipt_match", ReceiptAmount: 1000}, comp, "cust-uuid")
	if b["customer_id"] != "cust-uuid" || b["receipt_amount"] != float64(1000) {
		t.Errorf("receipt_match body wrong: %#v", b)
	}

	// empty optional filters are omitted; company_id is always present.
	inv := buildBody(Request{Action: "inventory"}, comp, "")
	if _, ok := inv["product_id"]; ok {
		t.Errorf("empty product_id should be omitted: %#v", inv)
	}
	if inv["company_id"] != comp {
		t.Errorf("company_id must always be present")
	}
}

func TestRunRejectsEmptyCompany(t *testing.T) {
	out, err := Run(context.Background(), Config{}, Request{Action: "inventory"})
	if err == nil {
		t.Fatal("expected error for empty company")
	}
	if !strings.Contains(out, "company_id") {
		t.Errorf("expected company guidance, got %q", out)
	}
}

func TestRunRejectsUnknownAction(t *testing.T) {
	_, err := Run(context.Background(), Config{CompanyID: "x"}, Request{Action: "nope"})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestFormatSearch(t *testing.T) {
	raw := json.RawMessage(`{
	  "query":"모듈 재고","intent":"inventory",
	  "parsed":{"manufacturer":"JA","spec_wp":635,"keywords":["모듈","재고"]},
	  "results":[
	    {"result_type":"inventory","title":"JAM72D42-635LB",
	     "data":{"manufacturer":"JA","spec_wp":635,"physical_kw":61.6},
	     "link":{"module":"inventory","params":{"product_id":"6b65101f-12d5-437e-b25c-cdff42036033"}}}
	  ],
	  "warnings":[]
	}`)
	out := formatSearch(raw, "탑솔라", 20)
	for _, want := range []string{"SolarFlow 검색", "inventory", "JAM72D42-635LB", "product_id=6b65101f"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatSearch missing %q in:\n%s", want, out)
		}
	}
}

func TestFormatGenericCapsArrays(t *testing.T) {
	// A 30-element array must be capped to limit=5 and flagged, while scalar
	// summary fields survive intact.
	items := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		items = append(items, `{"n":`+string(rune('0'+i%10))+`}`)
	}
	raw := json.RawMessage(`{"calculated_at":"2026-07-22T00:00:00Z","summary":{"total":9},"items":[` + strings.Join(items, ",") + `]}`)
	out := formatGeneric("margin", "탑솔라", raw, 5)
	if !strings.Contains(out, "잘림") {
		t.Errorf("expected truncation note, got:\n%s", out)
	}
	if !strings.Contains(out, "\"total\": 9") {
		t.Errorf("summary must survive capping, got:\n%s", out)
	}
	body := out[strings.Index(out, "{"):]
	var parsed struct {
		Items []any `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("re-parse capped json: %v", err)
	}
	if len(parsed.Items) != 5 {
		t.Errorf("expected 5 capped items, got %d", len(parsed.Items))
	}
}

func TestActionsStable(t *testing.T) {
	got := Actions()
	if len(got) != len(endpoints) {
		t.Fatalf("Actions len %d != endpoints %d", len(got), len(endpoints))
	}
	for _, a := range got {
		if _, ok := endpoints[a]; !ok {
			t.Errorf("Actions returned unknown %q", a)
		}
	}
}
