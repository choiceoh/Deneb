package tools

import (
	"encoding/json"
	"strconv"
	"strings"
)

// flexInt is an int that also accepts a JSON string ("10" → 10). LLMs routinely
// emit numeric tool params as quoted strings, and a plain `int` field then fails
// to unmarshal — failing the ENTIRE tool call over a benign type quirk (observed
// in prod: sessions_history/sessions_search erroring on `"limit":"10"`). Use this
// for optional numeric params where tolerance beats strictness. Empty string → 0.
type flexInt int

// Int returns the underlying int for use at call sites.
func (f flexInt) Int() int { return int(f) }

func (f *flexInt) UnmarshalJSON(b []byte) error {
	// Fast path: a real JSON number.
	var i int
	if err := json.Unmarshal(b, &i); err == nil {
		*f = flexInt(i)
		return nil
	}
	// Tolerate a quoted number ("10") or empty string.
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		*f = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*f = flexInt(n)
	return nil
}
