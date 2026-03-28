package chat

const providerGuide = `The provider system manages LLM provider plugins, model discovery, and runtime model resolution.

## Plugin Interface (gateway-go/internal/provider/)
Every provider implements the base Plugin interface:
- ID(): canonical provider ID (e.g. "anthropic", "openai", "zai")
- Label(): human-readable name
- AuthMethods(): supported auth kinds (api_key, bearer, oauth, token, none)

Optional adapter interfaces enable provider-specific hooks:
- DynamicModelResolver: custom model ID lookup
- ModelNormalizer: rewrite model IDs before inference
- CapabilitiesProvider: feature flags (streaming, caching, tools)
- RuntimeAuthProvider: credential exchange at call time
- ThinkingPolicyProvider: binary thinking, extended thinking support
- ModelSuppressionProvider: hide built-in models from catalog
- CatalogAugmenter: inject supplemental catalog entries

## Model Resolution Flow
1. Parse provider/model from user input or config
2. NormalizeProviderID(): canonical ID conversion
3. GetByNormalizedID(): provider lookup with alias support
4. ResolveDynamicModel(): optional custom model ID resolution
5. NormalizeModel(): rewrite model ID for wire format
6. PrepareAuth(): exchange credentials (API key, OAuth, device code)
7. DetectCapabilities(): streaming, caching, tools support

## Provider ID Normalization
- z.ai → zai
- opencode-zen → opencode
- qwen → qwen-portal
- bedrock → amazon-bedrock
- bytedance, doubao → volcengine
- Aliases supported: AliasProvider interface

## ConnectorConfig
- BaseURL (string): provider API endpoint
- APIKey (string): credential for requests
- AuthMode (string): api_key, bearer, oauth, token, none
- Headers (map[string]string): custom request headers (supports ${VAR} expansion)
- TimeoutMs (int64): per-request timeout

## Key Types
- CatalogEntry: Provider, ModelID, Label, ContextWindow, Reasoning, APIType
- RuntimeModel: resolved model for inference (provider, model, baseURL, apiType)
- PreparedAuth: runtime auth result (apiKey, baseURL, expiresAt)
- Capabilities: SupportsStreaming, SupportsCaching, SupportsTools

## Advanced Hooks
- ThinkingPolicyProvider: IsBinaryThinking, SupportsXHighThinking
- CatalogAugmenter: AugmentModelCatalog() adds extra entries
- UsageAuthProvider: billing auth resolution
- ApiKeyFormatter: profile credential formatting
- AuthDoctorProvider: auth failure diagnostic hints

## Key Files
- gateway-go/internal/provider/registry.go (plugin registration)
- gateway-go/internal/provider/discovery.go (ID normalization, lookup)
- gateway-go/internal/provider/connector.go (HTTP connector, auth injection)
- gateway-go/internal/provider/runtime.go (runtime model preparation)
- gateway-go/internal/provider/catalog.go (model catalog building)`

const liteparseGuide = `LiteParse provides local document parsing for PDFs, Office documents, and other binary formats.

## What It Does
Wraps the LiteParse CLI (lit) to extract text content from binary documents.
Used by the web tool to process downloaded documents and by other tools needing text extraction.

## Supported Formats
- PDF: application/pdf
- Office Open XML: DOCX, XLSX, PPTX (application/vnd.openxmlformats-officedocument.*)
- Legacy Office: DOC, XLS, PPT (application/msword, vnd.ms-excel, vnd.ms-powerpoint)
- OpenDocument: ODT, ODS, ODP (application/vnd.oasis.opendocument.*)
- CSV: text/csv

## Configuration Constants
- maxOutputBytes: 200 KB (text output cap per document)
- maxDocumentSize: 50 MB (maximum input file size)
- parseTimeout: 60 seconds (per-parse execution timeout)

## Parsing Flow
1. Check if lit CLI is available (cached check, npm i -g @llamaindex/liteparse)
2. Validate file size (< 50 MB)
3. Create temp directory with original file extension preserved
4. Execute: lit parse <inputPath>
5. Read stdout, truncate if > 200 KB
6. Return trimmed text content

## Web Tool Integration
- web tool's processDocument() detects binary MIME types
- Routes to liteparse.Parse(ctx, data, filename)
- Falls back gracefully on parse failures
- Korean error messages: "문서 파싱 실패" / "문서에서 텍스트를 추출하지 못했습니다"

## Installation
npm i -g @llamaindex/liteparse
Availability auto-detected at runtime; if missing, document parsing is skipped.

## Key Files
- gateway-go/internal/liteparse/ (Parse function, MIME detection)
- gateway-go/internal/chat/web_fetch.go (processDocument integration)`

const metricsGuide = `The metrics package provides Prometheus-compatible instrumentation with lock-free atomic counters.

## Metric Types
- Counter: monotonically increasing (labeled). Methods: Inc(labels...)
- Gauge: up/down value. Methods: Inc(), Dec(), Set(v)
- Histogram: value distribution with configurable buckets. Methods: Observe(v), ObserveDuration(start, labels...)

All types use sync/atomic for lock-free, concurrent-safe recording.

## Registered Metrics

### RPC
- deneb_rpc_requests_total (Counter, labels: method, status)
- deneb_rpc_duration_seconds (Histogram, labels: method)
  Buckets: 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10

### LLM
- deneb_llm_request_duration_seconds (Histogram, labels: provider, model)
  Buckets: 0.1, 0.5, 1, 2, 5, 10, 30, 60, 120
- deneb_llm_tokens_total (Counter, labels: direction, model)

### Sessions
- deneb_active_sessions (Gauge)
- deneb_websocket_clients (Gauge)

## Middleware Integration
RPCInstrumentation() middleware wraps RPC handlers:
- Records RPCRequestsTotal.Inc(method, status) per request
- Records RPCDuration.ObserveDuration(start, method) for latency

## Endpoint
- Path: /metrics
- Format: Prometheus text exposition format
- Implementation: WriteMetrics(w io.Writer)
- Usage: curl http://localhost:18789/metrics or Prometheus scraper

## Key Files
- gateway-go/internal/metrics/ (Counter, Gauge, Histogram types)
- gateway-go/internal/middleware/ (RPCInstrumentation)
- gateway-go/internal/server/ (/metrics endpoint registration)`

const nodesGuide = `Nodes are companion devices (iOS/Android/macOS/headless) that connect to the Gateway WebSocket with role:"node" and expose command surfaces via node.invoke.

## What Nodes Are
- Peripherals, NOT gateways. They don't run the gateway service.
- Connect via WebSocket (same port as operators) with device pairing.
- Expose command families: canvas.*, camera.*, device.*, notifications.*, system.*, location.*, sms.*, screen.*

## Pairing + Status
- WS nodes use device pairing: node presents identity during connect, Gateway creates pairing request.
- CLI: deneb devices list, deneb devices approve <requestId>, deneb nodes status

## Command Families
- Canvas: snapshot, present, navigate, eval (JS), hide, A2UI push/reset
- Camera: list, snap (--facing), clip (--duration, --no-audio, max 60s)
- Screen: record (--duration, --fps, --no-audio, max 60s)
- Location: get (lat/lon, accuracy, timestamp; off by default)
- System: run (shell), notify, which; gated by exec approvals
- Android: device.*, notifications.*, photos.*, contacts.*, calendar.*, callLog.*, sms.*, motion.*

## Remote Node Host
- Config: tools.exec.host=node, tools.exec.node=<id>
- Start: deneb node run --host <gateway-host> --port 18789

## Key Files
- docs/nodes/ (index, audio, camera, images, location-command, media-understanding, talk, troubleshooting)
- gateway-go/internal/events/node_events.go (event relay)
- gateway-go/internal/rpc/handler/node/node.go (RPC handlers)

## Gotchas
- camera.snap/clip require foreground; background returns NODE_BACKGROUND_UNAVAILABLE, not timeout
- system.run strips dangerous env vars (DYLD_*, LD_*, NODE_OPTIONS) silently
- screen.record max 60s; exceeding silently truncates`

const transcriptGuide = `The transcript system persists session conversation history as JSONL (newline-delimited JSON) files.

## Storage Format
Each session has one .jsonl file at ~/.deneb/agents/<agentId>/sessions/<sessionId>.jsonl

### Session Header (first line)
{type: "session", version: int, id: string, timestamp: int64, cwd: string}

### Message Lines (subsequent lines)
TranscriptMessage: {Type, Role (user/assistant/system), Content, ID, Timestamp (unix ms), TokenCount}

### Summary Lines (after compaction)
{type: "summary", role: "system", content: "Summary of N earlier messages...", metadata: {compacted: true, originalCount: N, compactedAt: int64}}

## Writer Operations
- EnsureSession(): create transcript file with header
- AppendMessage(): append JSON line (atomic write)
- ReadMessages(): get all non-header messages
- ReadPreview(): last N messages (truncated for efficiency)
- DeleteSession(): remove transcript file
- OnAppend(): register listener callback for real-time updates

## Scanner Configuration
- Initial buffer: 512 KB
- Maximum buffer: 10 MB per line (handles large tool outputs)

## Compaction Integration (transcript/compressor.go)
CompactionConfig defaults:
- ContextThreshold: 0.75 (compact when token usage exceeds 75%)
- FreshTailCount: 8 (recent messages always preserved)
- MaxUncompactedMessages: 200 (trigger at message count)

Compaction flow:
1. Evaluate: check token ratio and message count
2. Split: head (to compact) + tail (preserve recent messages)
3. Build: generate summary message from head portion
4. Write: header + summary + tail to temp file
5. Atomic: rename temp file to original (crash-safe)

CompactionResult: {OK, Compacted, Reason, OriginalMessages, RetainedMessages, SummaryCount}

## Key Files
- gateway-go/internal/transcript/ (Writer, Reader, Compressor)
- gateway-go/internal/session/ (session manager uses transcript for persistence)
- gateway-go/internal/chat/compaction.go (agent-level compaction orchestration)`
