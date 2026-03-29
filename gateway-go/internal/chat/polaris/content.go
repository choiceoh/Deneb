package polaris

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// --- topics action ---

func polarisTopics(docsDir, filter string) (string, error) {
	if _, err := os.Stat(docsDir); err != nil {
		return "No docs/ directory found in workspace.", nil
	}

	entries := getDocTree(docsDir)
	if len(entries) == 0 {
		return "No documentation files found in docs/.", nil
	}

	// Group by top-level directory.
	type categoryGroup struct {
		name    string
		entries []docEntry
	}
	groups := make(map[string]*categoryGroup)
	var groupOrder []string

	for _, e := range entries {
		parts := strings.SplitN(e.Path, "/", 2)
		cat := parts[0]
		if len(parts) == 1 {
			cat = "." // root-level docs
		}

		// Apply category filter if specified.
		if filter != "" && cat != filter {
			continue
		}

		if _, ok := groups[cat]; !ok {
			groups[cat] = &categoryGroup{name: cat}
			groupOrder = append(groupOrder, cat)
		}
		groups[cat].entries = append(groups[cat].entries, e)
	}

	sort.Strings(groupOrder)

	var sb strings.Builder

	// Compact mode: no filter → show category summary only.
	if filter == "" {
		fmt.Fprintf(&sb, "Deneb System Manual (%d docs, %d categories)\n\n", len(entries), len(groupOrder))
		for _, cat := range groupOrder {
			g := groups[cat]
			desc := categoryDescription(g.entries)
			if cat == "." {
				fmt.Fprintf(&sb, "  %-16s (%2d docs)  — %s\n", "(root)", len(g.entries), desc)
			} else {
				fmt.Fprintf(&sb, "  %-16s (%2d docs)  — %s\n", cat+"/", len(g.entries), desc)
			}
		}
		sb.WriteString("\nUse polaris(action:'topics', topic:'<category>') to browse a category.\n")
		sb.WriteString("Use polaris(action:'search', query:'<keyword>') to search.\n")
		sb.WriteString("Use polaris(action:'guides') for AI-curated system guides.\n")
		return sb.String(), nil
	}

	// Detail mode: filter specified → show docs in category with summaries.
	total := 0
	for _, g := range groups {
		total += len(g.entries)
	}
	fmt.Fprintf(&sb, "Deneb System Manual — %s/ (%d docs)\n\n", filter, total)

	for _, cat := range groupOrder {
		g := groups[cat]
		for i, e := range g.entries {
			prefix := "  |-- "
			if i == len(g.entries)-1 {
				prefix = "  `-- "
			}
			label := e.Title
			if label == "" {
				parts := strings.SplitN(e.Path, "/", 2)
				if len(parts) > 1 {
					label = parts[1]
				} else {
					label = parts[0]
				}
			}
			if e.Summary != "" {
				fmt.Fprintf(&sb, "%s%s — %s: %s\n", prefix, e.Path, label, e.Summary)
			} else {
				fmt.Fprintf(&sb, "%s%s — %s\n", prefix, e.Path, label)
			}
		}
	}

	sb.WriteString("\nUse polaris(action:'read', topic:'<path>') to read a doc.\n")
	sb.WriteString("Use polaris(action:'search', query:'<keyword>') to search.\n")
	return sb.String(), nil
}

// categoryDescription generates a brief description from the first few doc titles.
func categoryDescription(entries []docEntry) string {
	const maxTitles = 3
	var titles []string
	for i, e := range entries {
		if i >= maxTitles {
			break
		}
		t := e.Title
		if t == "" {
			parts := strings.SplitN(e.Path, "/", 2)
			if len(parts) > 1 {
				t = parts[1]
			} else {
				t = parts[0]
			}
		}
		titles = append(titles, t)
	}
	desc := strings.Join(titles, ", ")
	if len(entries) > maxTitles {
		desc += ", ..."
	}
	return desc
}

// --- search action ---

// searchMatchResult is a structured search result for internal use.
type searchMatchResult struct {
	Path     string
	Line     int
	Snippet  string
	HitCount int
	IsGuide  bool
}

// polarisSearchInternal returns structured search results for programmatic use.
func polarisSearchInternal(docsDir, query string) []searchMatchResult {
	keywords := strings.Fields(strings.ToLower(query))
	if len(keywords) == 0 {
		return nil
	}

	var matches []searchMatchResult

	// Search docs/ files.
	if _, err := os.Stat(docsDir); err == nil {
		entries := getDocTree(docsDir)
		for _, e := range entries {
			absPath := filepath.Join(docsDir, e.Path+".md")
			content, err := readDocFile(absPath)
			if err != nil {
				continue
			}
			_, _, body := parseFrontmatter(content)
			lower := strings.ToLower(body)

			// AND matching: all keywords must appear in the document.
			allPresent := true
			hitCount := 0
			for _, kw := range keywords {
				count := strings.Count(lower, kw)
				if count == 0 {
					allPresent = false
					break
				}
				hitCount += count
			}
			if !allPresent {
				continue
			}

			// Extract best matching snippet.
			lines := strings.Split(body, "\n")
			for i, line := range lines {
				lineLower := strings.ToLower(line)
				for _, kw := range keywords {
					if strings.Contains(lineLower, kw) {
						start := i - 2
						if start < 0 {
							start = 0
						}
						end := i + 3
						if end > len(lines) {
							end = len(lines)
						}
						matches = append(matches, searchMatchResult{
							Path:     e.Path,
							Line:     i + 1,
							Snippet:  strings.Join(lines[start:end], "\n"),
							HitCount: hitCount,
						})
						goto nextDoc
					}
				}
			}
		nextDoc:
		}
	}

	// Search builtin guides.
	for _, key := range builtinGuideOrder {
		g := builtinGuides[key]
		lower := strings.ToLower(g.Content)
		allPresent := true
		hitCount := 0
		for _, kw := range keywords {
			count := strings.Count(lower, kw)
			if count == 0 {
				allPresent = false
				break
			}
			hitCount += count
		}
		if !allPresent {
			continue
		}
		lines := strings.Split(g.Content, "\n")
		for i, line := range lines {
			lineLower := strings.ToLower(line)
			for _, kw := range keywords {
				if strings.Contains(lineLower, kw) {
					start := i - 2
					if start < 0 {
						start = 0
					}
					end := i + 3
					if end > len(lines) {
						end = len(lines)
					}
					matches = append(matches, searchMatchResult{
						Path:     key,
						Line:     i + 1,
						Snippet:  strings.Join(lines[start:end], "\n"),
						HitCount: hitCount,
						IsGuide:  true,
					})
					break
				}
			}
		}
	}

	// Sort by relevance (hit count descending).
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].HitCount > matches[j].HitCount
	})

	return matches
}

func polarisSearch(docsDir, query string) (string, error) {
	if query == "" {
		return "", fmt.Errorf("query is required for search action")
	}

	matches := polarisSearchInternal(docsDir, query)
	if len(matches) == 0 {
		return fmt.Sprintf("No matches found for %q in documentation.", query), nil
	}

	// Cap results.
	truncated := false
	if len(matches) > polarisMaxSearchResults {
		matches = matches[:polarisMaxSearchResults]
		truncated = true
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d matches for %q:\n\n", len(matches), query)
	for _, m := range matches {
		tag := ""
		if m.IsGuide {
			tag = "[guide] "
		}
		fmt.Fprintf(&sb, "### %s%s (line %d)\n%s\n\n", tag, m.Path, m.Line, m.Snippet)
	}
	if truncated {
		sb.WriteString("... (showing first 15 results, refine your query for more specific results)\n")
	}
	return sb.String(), nil
}

// --- read action ---

func polarisRead(docsDir, topic string) (string, error) {
	if topic == "" {
		return "", fmt.Errorf("topic is required for read action (e.g. 'concepts/session')")
	}

	absPath := filepath.Join(docsDir, topic+".md")
	content, err := readDocFile(absPath)
	if err != nil {
		// Try with /index.md for directory topics.
		absPath = filepath.Join(docsDir, topic, "index.md")
		content, err = readDocFile(absPath)
		if err != nil {
			return fmt.Sprintf("Document not found: %q. Use polaris(action:'topics') to browse available docs.", topic), nil
		}
	}

	_, _, body := parseFrontmatter(content)
	body = strings.TrimSpace(body)

	// Truncate if too long: head 75% + tail 25% to minimize content loss.
	if len(body) > polarisMaxReadChars {
		headSize := polarisMaxReadChars * 3 / 4
		tailSize := polarisMaxReadChars - headSize
		body = body[:headSize] + "\n\n... [truncated — use search for specific sections] ...\n\n" + body[len(body)-tailSize:]
	}

	return body, nil
}

// --- guides action ---

// guideEntry represents a curated guide.
type guideEntry struct {
	Key     string
	Title   string
	Summary string
	Content string
}

// builtinGuideOrder defines the display order for guides.
var builtinGuideOrder = []string{
	// Core Engine
	"aurora", "vega", "agent-loop", "compaction", "system-prompt", "architecture",
	// Tools
	"tools", "web", "exec", "gateway-tool", "media", "gmail", "data-tools", "pilot", "liteparse",
	// Runtime
	"sessions", "sessions-tools", "memory", "channels", "telegram", "message", "skills", "cron",
	// Infrastructure
	"provider", "metrics", "transcript", "nodes",
}

// guideCategory groups related guides for structured browsing.
type guideCategory struct {
	Key    string
	Label  string
	Guides []string
}

// guideCategories defines the 4 top-level categories for guides.
var guideCategories = []guideCategory{
	{"core", "Core Engine", []string{"aurora", "vega", "agent-loop", "compaction", "system-prompt", "architecture"}},
	{"tools", "Tools", []string{"tools", "web", "exec", "gateway-tool", "media", "gmail", "data-tools", "pilot", "liteparse"}},
	{"runtime", "Runtime", []string{"sessions", "sessions-tools", "memory", "channels", "telegram", "message", "skills", "cron"}},
	{"infra", "Infrastructure", []string{"provider", "metrics", "transcript", "nodes"}},
}

// guideRelated maps a guide key to its most related guides.
var guideRelated = map[string][]string{
	"aurora":         {"compaction", "system-prompt", "agent-loop"},
	"vega":           {"memory", "pilot"},
	"agent-loop":     {"tools", "sessions", "system-prompt"},
	"compaction":     {"aurora", "memory", "transcript"},
	"system-prompt":  {"agent-loop", "skills", "aurora"},
	"architecture":   {"agent-loop", "provider"},
	"tools":          {"agent-loop", "pilot", "exec"},
	"web":            {"pilot", "liteparse"},
	"exec":           {"pilot", "tools"},
	"gateway-tool":   {"architecture"},
	"media":          {"telegram", "web"},
	"gmail":          {"data-tools", "message"},
	"data-tools":     {"gmail", "tools"},
	"pilot":          {"tools", "exec", "vega"},
	"liteparse":      {"web", "media"},
	"sessions":       {"sessions-tools", "transcript", "agent-loop"},
	"sessions-tools": {"sessions"},
	"memory":         {"compaction", "vega", "aurora"},
	"channels":       {"telegram", "message"},
	"telegram":       {"channels", "message", "media"},
	"message":        {"telegram", "channels", "sessions-tools"},
	"skills":         {"system-prompt", "tools"},
	"cron":           {"sessions"},
	"provider":       {"agent-loop", "architecture"},
	"metrics":        {"architecture"},
	"transcript":     {"sessions", "compaction"},
	"nodes":          {"exec", "media"},
}

// builtinGuides contains AI-curated system knowledge.
// Each guide is ~2-4K chars of dense, actionable information.
var builtinGuides = map[string]guideEntry{
	"aurora": {
		Key:     "aurora",
		Title:   "Aurora Context Engine",
		Summary: "Context assembly lifecycle, token budgeting, aurora tools",
		Content: auroraGuide,
	},
	"vega": {
		Key:     "vega",
		Title:   "Vega Search Engine",
		Summary: "BM25 + semantic hybrid search, FTS5, embedding backends",
		Content: vegaGuide,
	},
	"agent-loop": {
		Key:     "agent-loop",
		Title:   "Agent Execution Loop",
		Summary: "LLM->tool loop, event streams, hooks, timeouts",
		Content: agentLoopGuide,
	},
	"compaction": {
		Key:     "compaction",
		Title:   "Message Compaction",
		Summary: "Hierarchical summarization, fresh-tail protection, memory flush",
		Content: compactionGuide,
	},
	"tools": {
		Key:     "tools",
		Title:   "Tool System Deep Dive",
		Summary: "ToolDef/ToolRegistry, parallel execution, $ref chaining, post-processing",
		Content: toolsGuide,
	},
	"system-prompt": {
		Key:     "system-prompt",
		Title:   "System Prompt Assembly",
		Summary: "Fixed sections, bootstrap injection, prompt modes, cache breakpoints",
		Content: systemPromptGuide,
	},
	"memory": {
		Key:     "memory",
		Title:   "Memory System",
		Summary: "Daily + long-term memory, search/get, semantic recall, auto-flush",
		Content: memoryGuide,
	},
	"sessions": {
		Key:     "sessions",
		Title:   "Session Lifecycle",
		Summary: "State machine, session kinds, spawn/steer/kill",
		Content: sessionsGuide,
	},
	"architecture": {
		Key:     "architecture",
		Title:   "System Architecture",
		Summary: "Go + Rust FFI + Node.js, IPC boundaries, hardware profiles",
		Content: architectureGuide,
	},
	"channels": {
		Key:     "channels",
		Title:   "Channel System",
		Summary: "Plugin registry, Telegram optimization, routing, groups",
		Content: channelsGuide,
	},
	"telegram": {
		Key:     "telegram",
		Title:   "Telegram Integration",
		Summary: "Bot API, MarkdownV2, inline keyboards, forum topics, access control",
		Content: telegramGuide,
	},
	"skills": {
		Key:     "skills",
		Title:   "Skills System",
		Summary: "Skill discovery, eligibility, prompt injection, SKILL.md format",
		Content: skillsGuide,
	},
	"pilot": {
		Key:     "pilot",
		Title:   "Pilot Tool",
		Summary: "Local sglang AI orchestrator, shortcuts, chaining, conditional sources",
		Content: pilotGuide,
	},
	"cron": {
		Key:     "cron",
		Title:   "Cron Scheduler",
		Summary: "Job scheduling, delivery modes, session keys, failure alerts",
		Content: cronGuide,
	},
	"web": {
		Key:     "web",
		Title:   "Web Tool",
		Summary: "Search, fetch, search+fetch modes, SGLang extraction, error classification",
		Content: webGuide,
	},
	"exec": {
		Key:     "exec",
		Title:   "Exec & Process Tools",
		Summary: "Shell commands, background sessions, process management",
		Content: execGuide,
	},
	"gateway-tool": {
		Key:     "gateway-tool",
		Title:   "Gateway Self-Management",
		Summary: "Config CRUD, restart (SIGUSR1), self-update (git pull + make)",
		Content: gatewayToolGuide,
	},
	"media": {
		Key:     "media",
		Title:   "Media Tools",
		Summary: "Image vision analysis, YouTube transcripts, file delivery, MIME detection",
		Content: mediaGuide,
	},
	"gmail": {
		Key:     "gmail",
		Title:   "Gmail Integration",
		Summary: "OAuth2 inbox, search, read, send, reply, labels, contact aliases",
		Content: gmailGuide,
	},
	"data-tools": {
		Key:     "data-tools",
		Title:   "Data Tools (KV, Clipboard, HTTP)",
		Summary: "Persistent KV store and HTTP API client",
		Content: dataToolsGuide,
	},
	"sessions-tools": {
		Key:     "sessions-tools",
		Title:   "Session Management Tools",
		Summary: "List, history, search, restore, send, spawn, subagents, status",
		Content: sessionToolsGuide,
	},
	"message": {
		Key:     "message",
		Title:   "Message Tool",
		Summary: "Send, reply, thread-reply, react via channels",
		Content: messageGuide,
	},
	"provider": {
		Key:     "provider",
		Title:   "Provider & Model System",
		Summary: "LLM provider plugins, model discovery, catalog, auth, normalization",
		Content: providerGuide,
	},
	"liteparse": {
		Key:     "liteparse",
		Title:   "Document Parsing (LiteParse)",
		Summary: "PDF, Office, CSV text extraction via lit CLI",
		Content: liteparseGuide,
	},
	"metrics": {
		Key:     "metrics",
		Title:   "Metrics & Observability",
		Summary: "Prometheus-compatible counters, histograms, /metrics endpoint",
		Content: metricsGuide,
	},
	"transcript": {
		Key:     "transcript",
		Title:   "Transcript Storage",
		Summary: "JSONL session history, append-only persistence, compaction integration",
		Content: transcriptGuide,
	},
	"nodes": {
		Key:     "nodes",
		Title:   "Nodes (Edge Devices)",
		Summary: "Companion devices: canvas, camera, location, system commands, Android data",
		Content: nodesGuide,
	},
}

func polarisGuides(topic string) (string, error) {
	if topic == "" {
		// Categorized listing of all guides.
		var sb strings.Builder
		fmt.Fprintf(&sb, "Deneb System Guides (%d guides, %d categories)\n\n", len(builtinGuides), len(guideCategories))
		for _, cat := range guideCategories {
			fmt.Fprintf(&sb, "%s (%d guides):\n", cat.Label, len(cat.Guides))
			for _, key := range cat.Guides {
				g := builtinGuides[key]
				fmt.Fprintf(&sb, "  %-16s — %s\n", g.Key, g.Summary)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("Use polaris(action:'guides', topic:'<key>') to read a guide.\n")
		sb.WriteString("Use polaris(action:'guides', topic:'core|tools|runtime|infra') to browse a category.\n")
		return sb.String(), nil
	}

	// Check if topic is a category key.
	for _, cat := range guideCategories {
		if cat.Key == topic {
			var sb strings.Builder
			fmt.Fprintf(&sb, "%s (%d guides)\n\n", cat.Label, len(cat.Guides))
			for _, key := range cat.Guides {
				g := builtinGuides[key]
				fmt.Fprintf(&sb, "  %-16s — %s\n", g.Key, g.Summary)
			}
			sb.WriteString("\nUse polaris(action:'guides', topic:'<key>') to read a guide.\n")
			return sb.String(), nil
		}
	}

	// Read a specific guide.
	g, ok := builtinGuides[topic]
	if !ok {
		return fmt.Sprintf("Unknown guide %q. Use polaris(action:'guides') to list available guides.", topic), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n%s", g.Title, g.Content)

	// Append related guides.
	if related, ok := guideRelated[topic]; ok && len(related) > 0 {
		sb.WriteString("\n\n## Related Guides\n")
		for _, rkey := range related {
			if rg, ok := builtinGuides[rkey]; ok {
				fmt.Fprintf(&sb, "- %s — %s\n", rg.Key, rg.Summary)
			}
		}
	}

	return sb.String(), nil
}
