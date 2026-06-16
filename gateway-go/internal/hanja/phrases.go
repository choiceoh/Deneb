package hanja

// chinesePhrases maps pure-Chinese function words/connectors — the ones a
// Chinese-lineage model code-switches into Korean prose — to their Korean
// equivalent. These are NOT Sino-Korean vocabulary: their per-character reading
// is gibberish (所以→소이, 但是→단시), so the reading table can't help; they need
// a (small, deterministic) translation.
//
// Scope is deliberately narrow and safe:
//   - Closed-class connectors/adverbs/demonstratives only — never single
//     particles (的/了/是), which are context-dependent.
//   - Each key is a multi-character sequence that does NOT occur in Korean-Hanja
//     vocabulary, so an exact whole-run match (see Streamer.flushRun) never
//     fires on real Korean text.
//   - Both Traditional and Simplified spellings are listed where they differ.
//
// Expand as real cases appear (the validation corpus only turned up 所以). This
// is a safety net for the rare residual; the breadth fix is prompting the model
// not to code-switch.
var chinesePhrases = map[string]string{
	"所以": "그래서",
	"因此": "따라서",
	"但是": "하지만",
	"可是": "하지만",
	"然而": "그러나",
	"因为": "왜냐하면", "因爲": "왜냐하면",
	"而且": "게다가",
	"虽然": "비록", "雖然": "비록",
	"如果": "만약",
	"那么": "그러면", "那麼": "그러면",
	"这样": "이렇게", "這樣": "이렇게",
	"那样": "그렇게", "那樣": "그렇게",
	"这个": "이것", "這個": "이것",
	"那个": "그것", "那個": "그것",
	"什么": "무엇", "什麼": "무엇",
	"怎么": "어떻게", "怎麼": "어떻게",
	"没有": "없음", "沒有": "없음",
	"比如": "예를 들어",
	// NOTE: do NOT add sequences that are valid Korean-Hanja words — 不過(불과),
	// 目前(목전), 然後(연후) read correctly via the table and must fall through.
}
