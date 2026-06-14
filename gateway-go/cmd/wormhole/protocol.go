// protocol.go — multi-protocol pass-through. wormhole speaks both wire APIs on
// the front (OpenAI /v1/chat/completions, Anthropic /v1/messages) and forwards
// each to a backend of the matching protocol. There is NO cross-translation: a
// client that already speaks Anthropic just rides straight through to an
// Anthropic backend — the key insight that makes "native Anthropic" a thin
// pass-through instead of a brittle OpenAI↔Anthropic converter.
package main

import "net/http"

const (
	protocolOpenAI    = "openai"
	protocolAnthropic = "anthropic"

	// defaultAnthropicVersion is sent upstream when an Anthropic client didn't pin
	// one itself — the Anthropic API requires the header.
	defaultAnthropicVersion = "2023-06-01"
)

// protocol returns the model's wire protocol, defaulting to OpenAI.
func (e modelEntry) protocol() string {
	if e.Protocol == protocolAnthropic {
		return protocolAnthropic
	}
	return protocolOpenAI
}

// applyUpstreamAuth injects the upstream credential in the shape the backend's
// protocol expects — Bearer for OpenAI, x-api-key (+ anthropic-version) for
// Anthropic — and passes the client's anthropic-version / anthropic-beta pins
// through so the upstream sees the versions the client asked for.
func applyUpstreamAuth(upReq *http.Request, entry modelEntry, clientReq *http.Request) {
	if entry.protocol() == protocolAnthropic {
		if entry.Key != "" {
			upReq.Header.Set("x-api-key", entry.Key)
		}
		ver := clientReq.Header.Get("anthropic-version")
		if ver == "" {
			ver = defaultAnthropicVersion
		}
		upReq.Header.Set("anthropic-version", ver)
		if beta := clientReq.Header.Get("anthropic-beta"); beta != "" {
			upReq.Header.Set("anthropic-beta", beta)
		}
		return
	}
	if entry.Key != "" {
		upReq.Header.Set("Authorization", "Bearer "+entry.Key)
	}
}
