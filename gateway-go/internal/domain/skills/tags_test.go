package skills

import (
	"strings"
	"testing"
)

func TestWrapSkillInvocationEscapesMetadataAndPreservesContents(t *testing.T) {
	got := WrapSkillInvocation(`review<&>`, `local>`, `--query "a&b"`, "result <kept-as-content>")
	for _, want := range []string{
		"<command-name>review&lt;&amp;&gt;</command-name>",
		"<command-type>local&gt;</command-type>",
		"<command-args>--query \"a&amp;b\"</command-args>",
		"  <command-contents>\nresult <kept-as-content>\n  </command-contents>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("wrapped invocation missing %q:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, "</skill-invocation>") {
		t.Fatalf("invocation has malformed closing tag: %q", got)
	}
}

func TestWrapSkillInvocationOmitsEmptyOptionalFields(t *testing.T) {
	got := WrapSkillInvocation("fact-check", "", "", "")
	if strings.Contains(got, "command-type") || strings.Contains(got, "command-args") || strings.Contains(got, "command-contents") {
		t.Fatalf("empty optional fields were emitted: %s", got)
	}
	if !strings.Contains(got, "<command-name>fact-check</command-name>") {
		t.Fatalf("command name missing: %s", got)
	}
}

func TestWrapSkillInvocationAvoidsDoubleNewlineWhenContentAlreadyTerminated(t *testing.T) {
	got := WrapSkillInvocation("x", "prompt", "", "already terminated\n")
	if strings.Contains(got, "already terminated\n\n  </command-contents>") {
		t.Fatalf("extra blank line inserted before closing tag: %q", got)
	}
}

func TestWrapSkillErrorEscapesEveryUserControlledField(t *testing.T) {
	got := WrapSkillError("bad</command-name>", "system&", "<arg>", `failure <fatal> & retry`)
	for _, forbidden := range []string{"bad</command-name>", "system&</", "<arg>", "failure <fatal>"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("unescaped value %q in %s", forbidden, got)
		}
	}
	for _, want := range []string{"bad&lt;/command-name&gt;", "system&amp;", "&lt;arg&gt;", "failure &lt;fatal&gt; &amp; retry"} {
		if !strings.Contains(got, want) {
			t.Errorf("escaped value %q missing in %s", want, got)
		}
	}
}

func TestEscapeXMLTagEncodesAmpersandBeforeOtherEntities(t *testing.T) {
	if got, want := escapeXMLTag("<&amp;>"), "&lt;&amp;amp;&gt;"; got != want {
		t.Fatalf("escapeXMLTag = %q, want %q", got, want)
	}
}
