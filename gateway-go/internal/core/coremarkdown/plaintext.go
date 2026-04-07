package coremarkdown

// ToPlainText parses markdown and returns only the plain text content,
// stripping all formatting. Convenience wrapper around MarkdownToIR.
func ToPlainText(markdown string) string {
	ir := MarkdownToIR(markdown, nil)
	return ir.Text
}
