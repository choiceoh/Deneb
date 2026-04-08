package directives

import "testing"

func TestExtractExecDirective_NoDirective(t *testing.T) {
	result := ExtractExecDirective("hello world")
	if result.HasDirective {
		t.Fatal("expected no directive")
	}
	if result.Cleaned != "hello world" {
		t.Fatalf("unexpected cleaned: %q", result.Cleaned)
	}
}

func TestExtractExecDirective_Empty(t *testing.T) {
	result := ExtractExecDirective("")
	if result.HasDirective {
		t.Fatal("expected no directive")
	}
	if result.Cleaned != "" {
		t.Fatalf("unexpected cleaned: %q", result.Cleaned)
	}
}

func TestExtractExecDirective_BasicExec(t *testing.T) {
	result := ExtractExecDirective("/exec")
	if !result.HasDirective {
		t.Fatal("expected directive")
	}
	if result.HasExecOptions {
		t.Fatal("expected no exec options")
	}
}

func TestExtractExecDirective_WithHost(t *testing.T) {
	result := ExtractExecDirective("/exec host=sandbox")
	if !result.HasDirective {
		t.Fatal("expected directive")
	}
	if result.ExecHost != ExecHostSandbox {
		t.Fatalf("got %q, want host=sandbox", result.ExecHost)
	}
	if !result.HasExecOptions {
		t.Fatal("expected HasExecOptions")
	}
}

func TestExtractExecDirective_MultipleOptions(t *testing.T) {
	result := ExtractExecDirective("do stuff /exec host=gateway security=full ask=always")
	if !result.HasDirective {
		t.Fatal("expected directive")
	}
	if result.ExecHost != ExecHostGateway {
		t.Fatalf("got %q, want host=gateway", result.ExecHost)
	}
	if result.ExecSecurity != ExecSecurityFull {
		t.Fatalf("got %q, want security=full", result.ExecSecurity)
	}
	if result.ExecAsk != ExecAskAlways {
		t.Fatalf("got %q, want ask=always", result.ExecAsk)
	}
	if result.Cleaned != "do stuff" {
		t.Fatalf("unexpected cleaned: %q", result.Cleaned)
	}
}

func TestExtractExecDirective_InvalidHost(t *testing.T) {
	result := ExtractExecDirective("/exec host=invalid")
	if !result.HasDirective {
		t.Fatal("expected directive")
	}
	if !result.InvalidHost {
		t.Fatal("expected InvalidHost")
	}
}

func TestExtractExecDirective_ColonSyntax(t *testing.T) {
	result := ExtractExecDirective("/exec host:sandbox")
	if !result.HasDirective {
		t.Fatal("expected directive")
	}
	if result.ExecHost != ExecHostSandbox {
		t.Fatalf("got %q, want host=sandbox with colon syntax", result.ExecHost)
	}
}

func TestExtractExecDirective_NodeOption(t *testing.T) {
	result := ExtractExecDirective("/exec node=my-worker")
	if !result.HasDirective {
		t.Fatal("expected directive")
	}
	if result.ExecNode != "my-worker" {
		t.Fatalf("got %q, want node=my-worker", result.ExecNode)
	}
}

func TestNormalizeExecHost(t *testing.T) {
	tests := []struct {
		input string
		want  ExecHost
		ok    bool
	}{
		{"sandbox", ExecHostSandbox, true},
		{"GATEWAY", ExecHostGateway, true},
		{"Node", ExecHostNode, true},
		{"invalid", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := NormalizeExecHost(tt.input)
		if got != tt.want || ok != tt.ok {
			t.Errorf("NormalizeExecHost(%q) = (%q, %v), want (%q, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}
