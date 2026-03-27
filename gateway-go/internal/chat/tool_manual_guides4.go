package chat

const webGuide = `The web tool provides search, fetch, and combined search+fetch capabilities for retrieving web content.

## Three Modes
1. **Fetch**: {"url": "https://..."} — extract content from a URL
2. **Search**: {"query": "..."} — web search, returns ranked results
3. **Search+Fetch**: {"query": "...", "fetch": N} — search then auto-fetch top N (1-3) results

## Schema
- url (string): URL to fetch (triggers fetch mode)
- query (string): search query (triggers search mode)
- fetch (number): auto-fetch top N results from search (1-3, default 0)
- maxChars (number): total output limit (default 50000)
- count (number): search results count (default 5)

## Content Processing Pipeline
- **HTML**: metadata extraction → signal detection → noise stripping (nav/ads/comments) → SGLang extraction or FFI conversion
- **JSON**: pretty-print via json.MarshalIndent
- **Binary/Documents**: parsed via LiteParse CLI (PDF, Office docs, etc.)
- **YouTube URLs**: auto-detected, extracts transcript via media.ExtractYouTubeTranscript

## SGLang Extraction (local AI content cleaning)
- Base URL: SGLANG_BASE_URL (default: http://127.0.0.1:30000/v1)
- Model: SGLANG_MODEL (default: Qwen/Qwen3.5-35B-A3B)
- Timeout: 45s per extraction
- Input cap: 100K chars
- Probe: GET /v1/models (3s timeout), re-probe every 5min if unavailable
- Fallback: FFI-based extraction if SGLang unavailable

## Output Format
<metadata>
Title, URL, FinalURL, ContentType, StatusCode, FetchMs
OrigChars, ExtractedChars, Retention%, WordCount
</metadata>
<content>
Extracted text content (truncated at maxChars)
</content>

## Error Classification (machine-readable)
- http_{STATUS}: HTTP errors (5xx retryable, 4xx not)
- content_too_large: exceeds 5MB (not retryable)
- ssrf_blocked: SSRF protection triggered (not retryable)
- dns_failure, redirect_loop, tls_error: not retryable
- connection_refused, connection_reset, timeout: retryable
- Each error includes a "retryable" boolean flag

## Limits
- Max download: 5 MB
- Default maxChars: 50,000
- Truncation: preserves metadata + section boundaries, appends "[...truncated: N chars remaining]"

## Key Files
- gateway-go/internal/chat/tool_web.go (tool implementation)
- docs/tools/web.md (17KB, comprehensive user docs)`

const execGuide = `The exec tool runs shell commands, and the process tool manages long-running background sessions.

## Exec Tool
### Schema
- command (string, required): shell command to execute
- workdir (string): working directory (defaults to agent workspace)
- timeout (number): seconds, default 30, min 1, max 300 (5 min)
- background (boolean): run in background, return sessionId immediately

### Execution
- Uses process.Manager if available (managed sessions with log/poll/kill)
- Fallback: direct exec via bash -c (no background support)
- Timeout enforced: timeout * 1000 milliseconds
- Output: stdout + stderr combined, exit code emphasized on failure

### Background Mode
- Returns sessionId immediately (no waiting for completion)
- Use process tool to poll/log/kill background sessions
- Useful for long-running builds, servers, watchers

## Process Tool (background session management)
### Actions
- list: all active process sessions with status
- poll: check status + new output (sessionId required; optional timeout in ms)
- log: full output log (sessionId required)
- write: send input to stdin (sessionId required; content field)
- kill: terminate process (sessionId required)

### Session Lifecycle
- Created by exec with background=true
- Persists until killed or process exits naturally
- Each session has unique sessionId for tracking

## Safety Notes
- Commands run in the agent's workspace directory by default
- No sandbox escape — respects gateway sandboxing settings
- Timeout prevents runaway commands (max 5 minutes)

## Key Files
- gateway-go/internal/chat/tools_core.go (exec tool registration)
- gateway-go/internal/process/ (Manager, session lifecycle)
- docs/tools/exec.md (9KB, user docs)`

const gatewayToolGuide = `The gateway tool provides self-management capabilities: config CRUD, restart, and self-update.

## Actions (6)

### restart
- Sends SIGUSR1 to the gateway process itself
- Gateway performs graceful restart (drain connections, reload config)
- No downtime if connection tracking is healthy

### config.get
- Returns current deneb.json config
- Output: {path, exists, valid, hash, config}

### config.schema.lookup
- Takes a dotted path (e.g., "agents.defaults.model")
- Returns the JSON Schema node for that config key
- Useful for validating values before patching

### config.patch
- Merges a patch object into existing deneb.json
- Deep merge: only specified keys are updated
- Preserves unmodified config values

### config.apply
- Replaces entire deneb.json with provided config
- Use with caution — overwrites all existing settings

### update.run
- Executes: git pull --rebase + make all
- Timeout: 2 minutes
- Writes sentinel file on completion
- Use for self-update from git repository

## Key Files
- gateway-go/internal/chat/tool_gateway.go
- docs/gateway/configuration.md (22KB)`

const mediaGuide = `Media tools: image analysis (vision), YouTube transcripts, and file delivery.

## image Tool (vision analysis)
### Schema
- image (string): single image path or URL
- images (array): multiple images (up to 20)
- prompt (string): analysis prompt (default: "Describe this image in detail")
- model (string): vision model (default: "claude-sonnet-4-20250514")

### Processing
- Local files: read + base64-encode, MIME type auto-detected (21 formats via Rust FFI)
- URLs: passed as image_url blocks (OpenAI format)
- Timeout: 60s per analysis call
- MaxTokens: 4096 per response
- Supports: PNG, JPEG, GIF, WebP, BMP, SVG, TIFF, HEIC, AVIF, etc.

## youtube_transcript Tool
- Input: YouTube URL (validated)
- Calls media.ExtractYouTubeTranscript (90s timeout)
- Returns formatted transcript via media.FormatYouTubeResult
- Useful for summarizing video content

## send_file Tool
- Sends files to the user via their current channel
- File size limit: 50 MB (Telegram constraint)
- MIME type auto-detected from file content (magic bytes, 21 formats)
- Supports: documents, images, audio, video
- Delivery via MediaSendFn callback (channel-specific formatting)

## MIME Detection (Rust FFI: core-rs/core/src/media/)
- 21 formats detected by magic bytes (not extension)
- 35+ MIME-to-extension mappings
- MediaCategory: image, video, audio, document, archive, other

## Key Files
- gateway-go/internal/chat/tool_media.go (image, youtube_transcript, send_file)
- gateway-go/internal/media/ (extraction, formatting)
- core-rs/core/src/media/ (MIME detection, magic bytes)
- docs/tools/index.md`

const gmailGuide = `Gmail tool provides native OAuth2 access to Gmail for inbox management, search, read, send, reply, and labeling.

## Actions (6)

### inbox
- Parallel fetch: unread messages + important messages
- Returns formatted summary with sender, subject, date, snippet
- Default max: 10 messages per category

### search
- query (string, required): Gmail search syntax
- Supports: from:, to:, subject:, is:unread, has:attachment, after:, before:, label:, in:
- max (number): results limit (default 10, max 50)
- Returns formatted search results

### read
- message_id (string, required): email or thread ID
- Returns full email content via FormatMessage
- Includes: from, to, cc, date, subject, body

### send
- to (string, required): recipient email
- subject (string, required): email subject
- body (string, required): email content
- cc, bcc (strings): comma-separated additional recipients
- html (boolean): send body as HTML (default: plain text)
- Auto-learns contact alias after successful send

### reply
- message_id (string, required): original email ID
- body (string, required): reply content
- Optional to override (otherwise replies to original sender)

### label
- label_action (enum): list, add, remove
- label_name (string): label name for add/remove
- message_id (string): target email for add/remove

## Contact Alias Resolution
- KV store key format: "gmail.contacts.{localpart}" → full email
- If input contains '@', used as-is
- Auto-learned after send (stores alias → email mapping)

## Configuration
- OAuth2 credentials required (Gmail API access)
- Timeout: 30s default, 60s max
- Output language: Korean (한국어)

## Key Files
- gateway-go/internal/chat/tool_gmail.go
- gateway-go/internal/gmail/ (OAuth2, API client)`

const dataToolsGuide = `Data tools: KV store (persistent), clipboard (in-memory), and HTTP API client.

## KV Store (kv tool)
### Storage
- File: ~/.deneb/kv.json (JSON object, persisted to disk)
- Thread-safe singleton (sync.RWMutex)
- Auto-creates directory with 0o755, file with 0o644

### Actions
- get: requires key; returns value or "Key not found"
- set: requires key + value; returns "Stored" confirmation
- delete: requires key; returns success or "Key not found"
- list: optional prefix filter; returns sorted key list

### Use Cases
- Contact aliases (gmail.contacts.*)
- User preferences, cached lookups
- Cross-session persistent state

## Clipboard (clipboard tool)
### Storage
- In-memory ring buffer, max 32 items (lost on gateway restart)
- Thread-safe singleton (sync.RWMutex, sync.Once)

### clipEntry
- Content (string), Label (string, optional), CreatedAt (Unix timestamp)

### Actions
- set: content (required), optional label; returns label + char count
- get: returns most recent entry (newest)
- list: all entries newest-first, 80-char preview per item
- clear: removes all entries, returns count removed

### Use Cases
- Temporary data passing between tool calls
- Clipboard for copy/paste workflows
- Quick scratch storage (non-persistent)

## HTTP Tool (http tool)
### Schema
- url (string, required): HTTP/HTTPS URL
- method (string): GET/POST/PUT/PATCH/DELETE (default: GET)
- headers (object): custom request headers
- body (string): raw request body
- json (object): JSON body (auto-sets Content-Type: application/json)
- timeout (number): seconds, default 30, max 120
- max_response_chars (number): default 50000

### Response Format
- HTTP status code + phrase
- Selected headers: Content-Type, Content-Length, Location
- Response body (truncated with "[...truncated]" if exceeds limit)

### Details
- User-Agent: "Deneb-Gateway/1.0"
- Max response: capped at 5 MB download
- Follows redirects (default Go behavior)

## Key Files
- gateway-go/internal/chat/tool_kv.go (KV store)
- gateway-go/internal/chat/tool_clipboard.go (clipboard)
- gateway-go/internal/chat/tool_http.go (HTTP client)`

const sessionToolsGuide = `Session tools provide full session lifecycle management: list, browse history, search, restore, cross-session messaging, and sub-agent spawning.

## sessions_list (browse active sessions)
- limit (number): default 50, min 1
- kinds (array): filter by "main", "group", "cron", "hook"
- Returns: session key, kind, status, model, marker if current session

## sessions_history (read past messages)
- sessionKey (string, required): target session
- limit (number): default 20, min 1
- Returns: session key, total message count, formatted message list (role + timestamp + content)

## sessions_search (full-text search across transcripts)
- query (string, required): search terms
- maxResults (number): default 20, min 1, max 100
- Returns: matched messages with surrounding context [before, after]

## sessions_restore (import history from another session)
- sourceSessionKey (string, required): session to import from
- limit (number): default 0 (import all messages)
- Copies messages into current session's history

## sessions_send (cross-session messaging)
- sessionKey (string): target session (default: "main")
- message (string, required): message to inject
- Triggers an agent run in the target session with the given message

## sessions_spawn (create sub-agent)
- task (string, required): task description for the sub-agent
- label (string): human-readable label (used in session key)
- model (string): model override for sub-agent
- Session key format: {parentKey}:{label}:{unixMs}
- Sub-agent runs independently with its own transcript

## subagents (monitor/control sub-agents)
- action (enum): list, kill, steer
- target (string): index (1-based), label, session key, or "all" (for kill)
- message (string): for steer action (injects message into running sub-agent)
- List: sorted by running first, then by UpdatedAt descending

## session_status (current session info)
- sessionKey (string): optional (defaults to current session)
- Returns: session key, time, kind, status, model, token usage, runtime info

## Key Files
- gateway-go/internal/chat/tool_sessions.go (all 8 tools)
- gateway-go/internal/session/ (Manager, state machine)
- docs/concepts/session.md, docs/concepts/session-tool.md`

const messageGuide = `The message tool sends messages to users via channels, with support for replies, threads, and reactions.

## Actions (4)

### send
- message (string, required): text content to send
- to (string): recipient (chat ID or user ID)
- channel (string): target channel (e.g., "telegram")
- silent (boolean): send without notification
- Uses context delivery + replyFunc for routing
- Timeout: 30s

### reply
- message (string, required): reply text
- replyTo (string): message ID to reply to (required)
- Sends as a native reply (quoted message in Telegram)
- Timeout: 30s

### thread-reply
- message (string, required): reply text
- replyTo (string): message ID (required)
- Like reply but threaded (creates/continues a thread)
- Timeout: 30s

### react
- emoji (string, required): reaction emoji (e.g., "👍", "❤️")
- messageId (string, required): message to react to
- Internal payload format: "__react:{msgId}:{emoji}"
- Timeout: 10s (shorter than text sends)

## Routing
- Default: sends to current conversation (same channel + chat)
- Cross-channel: specify channel + to for routing to different destination
- Cross-session: use sessions_send tool instead (triggers agent run in target session)

## Telegram-Specific
- Messages auto-formatted as MarkdownV2
- Respects 4096-char message limit (auto-split if needed)
- Inline keyboards can be attached via channel-specific extensions
- Silent mode: sends without push notification

## Key Files
- gateway-go/internal/chat/tool_message.go
- docs/concepts/messages.md`
