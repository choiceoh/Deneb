// direct_grammar.go — the loader for direct_grammar.json, the canonical catalog
// of fact axes the agent may write from an explicit user command.
//
// Why data and not code: this catalog is the narrow inlet to canonical memory.
// Every phrasing it does not cover is a fact the agent silently fails to
// remember, so it has to grow — and a table that grows by editing a JSON file
// (and is answerable to a coverage test and a miss ledger) can be evolved by the
// improvement loop itself, which a switch statement buried in a 600-line
// classifier cannot. Structural guards deliberately stay in Go: reported
// speech, temporary scope, quoted payloads and third-party subjects are not
// per-axis rules and must not be weakened by a data edit.
package memory

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

//go:embed direct_grammar.json
var directGrammarJSON []byte

// factAxisCue is a substring test over a lowercased message. All of All must be
// present and at least one of Any. A bare JSON string is sugar for one Any term.
type factAxisCue struct {
	All []string `json:"all,omitempty"`
	Any []string `json:"any,omitempty"`
}

func (c *factAxisCue) UnmarshalJSON(raw []byte) error {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		c.All, c.Any = nil, []string{single}
		return nil
	}
	type plain factAxisCue
	var decoded plain
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*c = factAxisCue(decoded)
	return nil
}

func (c factAxisCue) matches(lower string) bool {
	for _, term := range c.All {
		if !strings.Contains(lower, term) {
			return false
		}
	}
	if len(c.Any) == 0 {
		return len(c.All) > 0
	}
	for _, term := range c.Any {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

// factAxis is one canonical fact key plus the phrasings that bind to it.
type factAxis struct {
	Key   string `json:"key"`
	Kind  string `json:"kind"`
	Class string `json:"classify"`
	// QueryAliases are the words a user actually searches this axis by. The
	// canonical key is English while queries are Korean, so without them a
	// search for "호칭" or "장기 목표" never reaches the axis it names.
	QueryAliases []string `json:"queryAliases,omitempty"`
	// EnglishAssert / EnglishForward are anchored whole-message commands.
	EnglishAssert  string `json:"englishAssert,omitempty"`
	EnglishForward string `json:"englishForward,omitempty"`
	// ForgetObjects are exact normalized noun phrases from "내 <object> 선호를
	// 지워줘"; ForgetCues and KnownAxisForgetCues are substring tests for the two
	// Korean deletion grammars.
	ForgetObjects       []string      `json:"forgetObjects,omitempty"`
	ForgetCues          []factAxisCue `json:"forgetCues,omitempty"`
	KnownAxisForgetCues []string      `json:"knownAxisForgetCues,omitempty"`

	classifyRE       *regexp.Regexp
	englishAssertRE  *regexp.Regexp
	englishForwardRE *regexp.Regexp
}

type directGrammar struct {
	SchemaVersion int         `json:"schemaVersion"`
	Comment       string      `json:"comment"`
	Axes          []*factAxis `json:"axes"`
}

const directGrammarSchemaVersion = 1

var factAxes = mustLoadDirectGrammar(directGrammarJSON)

func mustLoadDirectGrammar(raw []byte) []*factAxis {
	axes, err := loadDirectGrammar(raw)
	if err != nil {
		// The catalog is compiled in, so a failure here is a build-time defect
		// that would otherwise silently disable canonical memory writes.
		panic("memory: direct grammar catalog: " + err.Error())
	}
	return axes
}

func loadDirectGrammar(raw []byte) ([]*factAxis, error) {
	var doc directGrammar
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if doc.SchemaVersion != directGrammarSchemaVersion {
		return nil, fmt.Errorf("unsupported schema version %d", doc.SchemaVersion)
	}
	if len(doc.Axes) == 0 {
		return nil, fmt.Errorf("catalog is empty")
	}
	seen := make(map[string]struct{}, len(doc.Axes))
	for i, axis := range doc.Axes {
		if strings.TrimSpace(axis.Key) == "" {
			return nil, fmt.Errorf("axis %d has no key", i)
		}
		if _, duplicate := seen[axis.Key]; duplicate {
			return nil, fmt.Errorf("duplicate axis key %q", axis.Key)
		}
		seen[axis.Key] = struct{}{}
		switch axis.Kind {
		case "preference", "identity":
		default:
			return nil, fmt.Errorf("axis %q has unsupported kind %q", axis.Key, axis.Kind)
		}
		var err error
		if axis.classifyRE, err = compileAxisPattern(axis.Key, "classify", axis.Class); err != nil {
			return nil, err
		}
		if axis.classifyRE == nil {
			return nil, fmt.Errorf("axis %q has no classify pattern", axis.Key)
		}
		if axis.englishAssertRE, err = compileAxisPattern(axis.Key, "englishAssert", axis.EnglishAssert); err != nil {
			return nil, err
		}
		if axis.englishForwardRE, err = compileAxisPattern(axis.Key, "englishForward", axis.EnglishForward); err != nil {
			return nil, err
		}
		if len(axis.QueryAliases) == 0 {
			return nil, fmt.Errorf("axis %q has no query aliases", axis.Key)
		}
		for _, alias := range axis.QueryAliases {
			if strings.TrimSpace(alias) != alias || alias == "" || strings.Contains(alias, " ") {
				return nil, fmt.Errorf("axis %q query alias %q must be one bare token", axis.Key, alias)
			}
		}
	}
	return doc.Axes, nil
}

func compileAxisPattern(key, field, pattern string) (*regexp.Regexp, error) {
	if strings.TrimSpace(pattern) == "" {
		return nil, nil
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("axis %q %s pattern: %w", key, field, err)
	}
	return compiled, nil
}

// FactKeyQueryAliases returns the search vocabulary for a canonical fact key,
// or nil when the key is not a published axis. It is the single source for both
// the wiki fact search and chat recall: a key that gains an alias here becomes
// findable everywhere at once, and no surface keeps its own private table.
func FactKeyQueryAliases(key string) []string {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, axis := range factAxes {
		if axis.Key == key {
			return append([]string(nil), axis.QueryAliases...)
		}
	}
	return nil
}

// axisForEnglishAssertion returns the axis whose explicit English self-assertion
// matches the whole body.
func axisForEnglishAssertion(body string) (*factAxis, bool) {
	for _, axis := range factAxes {
		if axis.englishAssertRE != nil && axis.englishAssertRE.MatchString(body) {
			return axis, true
		}
	}
	return nil, false
}

// axisForEnglishForward returns the axis for an explicit "from now on …" command.
func axisForEnglishForward(body string) (*factAxis, bool) {
	for _, axis := range factAxes {
		if axis.englishForwardRE != nil && axis.englishForwardRE.MatchString(body) {
			return axis, true
		}
	}
	return nil, false
}

// axisForClassifiedText returns the axis a free-form Korean profile statement
// belongs to, used for both the fact key and the fact kind.
func axisForClassifiedText(lower string) (*factAxis, bool) {
	for _, axis := range factAxes {
		if axis.classifyRE.MatchString(lower) {
			return axis, true
		}
	}
	return nil, false
}

// axisForForgetObject binds an exact noun phrase from a Korean deletion command.
func axisForForgetObject(normalizedObject string) (*factAxis, bool) {
	for _, axis := range factAxes {
		for _, object := range axis.ForgetObjects {
			if object == normalizedObject {
				return axis, true
			}
		}
	}
	return nil, false
}

// axisForForgetCue binds a general Korean deletion command by substring cue.
func axisForForgetCue(lower string) (*factAxis, bool) {
	for _, axis := range factAxes {
		for _, cue := range axis.ForgetCues {
			if cue.matches(lower) {
				return axis, true
			}
		}
	}
	return nil, false
}

// axisForKnownAxisForget binds the "내 <축> 선호를 지워줘" grammar, whose noun
// phrase names a known axis outright.
func axisForKnownAxisForget(lower string) (*factAxis, bool) {
	for _, axis := range factAxes {
		for _, cue := range axis.KnownAxisForgetCues {
			if strings.Contains(lower, cue) {
				return axis, true
			}
		}
	}
	return nil, false
}
