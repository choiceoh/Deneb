package telegram

import "testing"

func TestValidateSenderIdentity(t *testing.T) {
	tests := []struct {
		name       string
		fields     SenderIdentityFields
		wantIssues int
	}{
		{
			name:       "valid direct message",
			fields:     SenderIdentityFields{ChatType: "direct", SenderID: "123"},
			wantIssues: 0,
		},
		{
			name:       "valid group message",
			fields:     SenderIdentityFields{ChatType: "group", SenderID: "123"},
			wantIssues: 0,
		},
		{
			name:       "group without any sender info",
			fields:     SenderIdentityFields{ChatType: "group"},
			wantIssues: 1,
		},
		{
			name:       "direct without sender is OK",
			fields:     SenderIdentityFields{ChatType: "direct"},
			wantIssues: 0,
		},
		{
			name:       "invalid E164",
			fields:     SenderIdentityFields{ChatType: "direct", SenderE164: "12345"},
			wantIssues: 1,
		},
		{
			name:       "valid E164",
			fields:     SenderIdentityFields{ChatType: "direct", SenderE164: "+1234567890"},
			wantIssues: 0,
		},
		{
			name:       "username with @",
			fields:     SenderIdentityFields{ChatType: "group", SenderUsername: "@user", SenderID: "123"},
			wantIssues: 1,
		},
		{
			name:       "username with whitespace",
			fields:     SenderIdentityFields{ChatType: "group", SenderUsername: "user name", SenderID: "123"},
			wantIssues: 1,
		},
		{
			name:       "SenderID set but empty after trim",
			fields:     SenderIdentityFields{ChatType: "direct", SenderID: "  "},
			wantIssues: 1,
		},
		{
			name:       "group with name only",
			fields:     SenderIdentityFields{ChatType: "group", SenderName: "Alice"},
			wantIssues: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := ValidateSenderIdentity(tt.fields)
			if len(issues) != tt.wantIssues {
				t.Errorf("ValidateSenderIdentity() returned %d issues, want %d: %v", len(issues), tt.wantIssues, issues)
			}
		})
	}
}
