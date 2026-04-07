// tags.go provides structured XML tag wrapping for skill invocations.
//
// When a skill is invoked (via slash command or tool), the execution context
// is wrapped in XML tags so the LLM can track which skill was invoked and
// correlate results across multi-turn conversations.
package skills

import (
	"strings"
)

// WrapSkillInvocation wraps a skill execution result in structured XML tags.
func WrapSkillInvocation(skillName, skillType, args, contents string) string {
	var b strings.Builder
	b.WriteString("<skill-invocation>\n")

	b.WriteString("  <command-name>")
	b.WriteString(escapeXMLTag(skillName))
	b.WriteString("</command-name>\n")

	if skillType != "" {
		b.WriteString("  <command-type>")
		b.WriteString(escapeXMLTag(skillType))
		b.WriteString("</command-type>\n")
	}

	if args != "" {
		b.WriteString("  <command-args>")
		b.WriteString(escapeXMLTag(args))
		b.WriteString("</command-args>\n")
	}

	if contents != "" {
		b.WriteString("  <command-contents>\n")
		b.WriteString(contents)
		if !strings.HasSuffix(contents, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("  </command-contents>\n")
	}

	b.WriteString("</skill-invocation>")
	return b.String()
}

// WrapSkillError wraps a skill execution error in structured XML tags.
func WrapSkillError(skillName, skillType, args, errMsg string) string {
	var b strings.Builder
	b.WriteString("<skill-invocation>\n")

	b.WriteString("  <command-name>")
	b.WriteString(escapeXMLTag(skillName))
	b.WriteString("</command-name>\n")

	if skillType != "" {
		b.WriteString("  <command-type>")
		b.WriteString(escapeXMLTag(skillType))
		b.WriteString("</command-type>\n")
	}

	if args != "" {
		b.WriteString("  <command-args>")
		b.WriteString(escapeXMLTag(args))
		b.WriteString("</command-args>\n")
	}

	b.WriteString("  <command-error>")
	b.WriteString(escapeXMLTag(errMsg))
	b.WriteString("</command-error>\n")

	b.WriteString("</skill-invocation>")
	return b.String()
}

func escapeXMLTag(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
