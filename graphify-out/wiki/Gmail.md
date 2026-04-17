# Gmail

> 78 nodes · cohesion 0.05

## Key Concepts

- **Client** (17 connections) — `gateway-go/internal/platform/gmail/operations.go`
- **.poll()** (15 connections) — `gateway-go/internal/platform/gmailpoll/service.go`
- **gmail.go** (14 connections) — `gateway-go/internal/pipeline/chat/tools/gmail.go`
- **.readJSON()** (11 connections) — `gateway-go/internal/platform/gmail/client.go`
- **ToolGmail()** (11 connections) — `gateway-go/internal/pipeline/chat/tools/gmail.go`
- **client.go** (10 connections) — `gateway-go/internal/platform/gmail/client.go`
- **Service** (10 connections) — `gateway-go/internal/platform/gmailpoll/service.go`
- **gmailInbox()** (9 connections) — `gateway-go/internal/pipeline/chat/tools/gmail.go`
- **NewClient()** (8 connections) — `gateway-go/internal/ai/llm/client.go`
- **.postJSON()** (8 connections) — `gateway-go/internal/platform/gmail/client.go`
- **.refresh()** (8 connections) — `gateway-go/internal/platform/gmail/client.go`
- **gmailAnalyze()** (8 connections) — `gateway-go/internal/pipeline/chat/tools/gmail.go`
- **TestStateStore_SaveAndLoad()** (8 connections) — `gateway-go/internal/platform/gmailpoll/state_test.go`
- **TestStateStore_TrimSeenIDs()** (8 connections) — `gateway-go/internal/platform/gmailpoll/state_test.go`
- **DefaultClient()** (7 connections) — `gateway-go/internal/platform/gmail/client.go`
- **.GetMessage()** (7 connections) — `gateway-go/internal/platform/gmail/operations.go`
- **.Search()** (7 connections) — `gateway-go/internal/platform/gmail/operations.go`
- **gmailSearch()** (7 connections) — `gateway-go/internal/pipeline/chat/tools/gmail.go`
- **resolveRecipient()** (7 connections) — `gateway-go/internal/pipeline/chat/tools/gmail.go`
- **state_test.go** (6 connections) — `gateway-go/internal/platform/gmailpoll/state_test.go`
- **.doAPI()** (6 connections) — `gateway-go/internal/platform/gmail/client.go`
- **.ListLabels()** (6 connections) — `gateway-go/internal/platform/gmail/operations.go`
- **.validToken()** (6 connections) — `gateway-go/internal/platform/gmail/client.go`
- **gmailLabel()** (6 connections) — `gateway-go/internal/pipeline/chat/tools/gmail.go`
- **gmailSend()** (6 connections) — `gateway-go/internal/pipeline/chat/tools/gmail.go`
- *... and 53 more nodes in this community*

## Relationships

- No strong cross-community connections detected

## Source Files

- `gateway-go/internal/ai/llm/client.go`
- `gateway-go/internal/pipeline/chat/tools/gmail.go`
- `gateway-go/internal/pipeline/chat/tools/kv.go`
- `gateway-go/internal/pipeline/chat/tools/morning_letter.go`
- `gateway-go/internal/platform/gmail/client.go`
- `gateway-go/internal/platform/gmail/client_test.go`
- `gateway-go/internal/platform/gmail/operations.go`
- `gateway-go/internal/platform/gmail/types.go`
- `gateway-go/internal/platform/gmailpoll/service.go`
- `gateway-go/internal/platform/gmailpoll/state.go`
- `gateway-go/internal/platform/gmailpoll/state_test.go`

## Audit Trail

- EXTRACTED: 226 (58%)
- INFERRED: 163 (42%)
- AMBIGUOUS: 0 (0%)

---

*Part of the graphify knowledge wiki. See [[index]] to navigate.*