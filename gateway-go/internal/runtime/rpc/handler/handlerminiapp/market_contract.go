package handlerminiapp

// MarketQuote is one instrument on the wire. changePct is the percent change
// versus the previous close.
//
//deneb:wire
type MarketQuote struct {
	Symbol    string  `json:"symbol"`
	Label     string  `json:"label"`
	Price     float64 `json:"price"`
	PrevClose float64 `json:"prevClose"`
	ChangePct float64 `json:"changePct"`
	Currency  string  `json:"currency"`
}

// MarketSummary is the miniapp.market.summary response.
//
//deneb:wire
type MarketSummary struct {
	Quotes []MarketQuote `json:"quotes"`
	AsOf   int64         `json:"asOf"`
	Stale  bool          `json:"stale"`
}
