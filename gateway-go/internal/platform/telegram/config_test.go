package telegram

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestAllowList_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantIDs   []int64
		wantUsers []string
		wantWild  bool
	}{
		{
			name:  "empty array",
			input: `[]`,
		},
		{
			name:    "numeric IDs only",
			input:   `[123, 456]`,
			wantIDs: []int64{123, 456},
		},
		{
			name:     "wildcard",
			input:    `["*"]`,
			wantWild: true,
		},
		{
			name:      "mixed types",
			input:     `[123, "*", "@peter", "bob"]`,
			wantIDs:   []int64{123},
			wantUsers: []string{"peter", "bob"},
			wantWild:  true,
		},
		{
			name:      "username without @",
			input:     `["alice"]`,
			wantUsers: []string{"alice"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var al AllowList
			if err := json.Unmarshal([]byte(tt.input), &al); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if len(al.IDs) != len(tt.wantIDs) {
				t.Errorf("IDs: got %v, want %v", al.IDs, tt.wantIDs)
			}
			for i, id := range tt.wantIDs {
				if al.IDs[i] != id {
					t.Errorf("IDs[%d]: got %d, want %d", i, al.IDs[i], id)
				}
			}
			if len(al.Usernames) != len(tt.wantUsers) {
				t.Errorf("Usernames: got %v, want %v", al.Usernames, tt.wantUsers)
			}
			for i, u := range tt.wantUsers {
				if al.Usernames[i] != u {
					t.Errorf("Usernames[%d]: got %q, want %q", i, al.Usernames[i], u)
				}
			}
			if al.Wildcard != tt.wantWild {
				t.Errorf("Wildcard: got %v, want %v", al.Wildcard, tt.wantWild)
			}
		})
	}
}

func TestAllowList_Methods(t *testing.T) {
	al := AllowList{IDs: []int64{42, 100}, Usernames: []string{"peter"}}

	if al.IsEmpty() {
		t.Error("expected non-empty")
	}
	if al.AllowsAll() {
		t.Error("expected not wildcard")
	}
	if !al.ContainsID(42) {
		t.Error("expected ContainsID(42)")
	}
	if al.ContainsID(99) {
		t.Error("expected !ContainsID(99)")
	}
	if !al.ContainsUsername("Peter") {
		t.Error("expected case-insensitive username match")
	}
	if al.ContainsUsername("bob") {
		t.Error("expected !ContainsUsername(bob)")
	}

	empty := AllowList{}
	if !empty.IsEmpty() {
		t.Error("expected empty")
	}

	wild := AllowList{Wildcard: true}
	if !wild.AllowsAll() {
		t.Error("expected wildcard")
	}
}

func TestAllowList_MarshalJSON(t *testing.T) {
	al := AllowList{IDs: []int64{42}, Usernames: []string{"peter"}, Wildcard: true}
	data := testutil.Must(json.Marshal(al))

	var al2 AllowList
	if err := json.Unmarshal(data, &al2); err != nil {
		t.Fatalf("roundtrip unmarshal error: %v", err)
	}
	if !al2.ContainsID(42) || !al2.ContainsUsername("peter") || !al2.AllowsAll() {
		t.Errorf("roundtrip failed: got %+v", al2)
	}
}

func TestConfig_UnmarshalJSON(t *testing.T) {
	input := `{
		"botToken": "123:ABC",
		"allowFrom": [42, "*", "@peter"],
		"groupAllowFrom": [100],
		"timeoutSeconds": 60
	}`

	var c Config
	if err := json.Unmarshal([]byte(input), &c); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if c.BotToken != "123:ABC" {
		t.Errorf("botToken: got %q", c.BotToken)
	}
	if !c.AllowFrom.ContainsID(42) {
		t.Error("expected allowFrom to contain 42")
	}
	if !c.AllowFrom.AllowsAll() {
		t.Error("expected allowFrom wildcard")
	}
	if !c.AllowFrom.ContainsUsername("peter") {
		t.Error("expected allowFrom to contain peter")
	}
	if !c.GroupAllowFrom.ContainsID(100) {
		t.Error("expected groupAllowFrom to contain 100")
	}
	if c.EffectiveTimeout() != 60 {
		t.Errorf("timeout: got %d", c.EffectiveTimeout())
	}
}
