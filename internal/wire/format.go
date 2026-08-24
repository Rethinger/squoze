// Package wire detects the wire format of an LLM request body.
//
// squoze speaks three dialects out of the box: OpenAI Chat Completions,
// Anthropic Messages and the OpenAI Responses API. Detection is a cheap
// structural probe — no full unmarshal, no reflection — because it runs on
// every request in front of every mutation.
package wire

import (
	"bytes"
	"encoding/json"
)

// Format identifies a supported request shape.
type Format int

const (
	FormatUnknown Format = iota
	FormatOpenAIChat
	FormatAnthropicMessages
	FormatOpenAIResponses
)

func (f Format) String() string {
	switch f {
	case FormatOpenAIChat:
		return "openai_chat"
	case FormatAnthropicMessages:
		return "anthropic_messages"
	case FormatOpenAIResponses:
		return "openai_responses"
	default:
		return "unknown"
	}
}

type probe struct {
	Instructions json.RawMessage `json:"instructions"` // Responses API
	Input        json.RawMessage `json:"input"`        // Responses API
	System       json.RawMessage `json:"system"`       // Anthropic Messages
	Messages     json.RawMessage `json:"messages"`     // chat + Anthropic
}

func isSet(raw json.RawMessage) bool {
	return len(raw) > 0 && !isNull(raw)
}

func isNull(raw json.RawMessage) bool {
	return string(bytesTrim(raw)) == "null"
}

func bytesTrim(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && isSpace(b[start]) {
		start++
	}
	for end > start && isSpace(b[end-1]) {
		end--
	}
	return b[start:end]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

// Detect classifies a request body. Invalid JSON is FormatUnknown: callers
// must treat unknown as pass-through (fail-open contract).
//
// Priority matters: `system` exists only in Anthropic Messages, while both
// chat and Anthropic carry `messages`; Responses is identified by its
// dedicated `instructions`/`input` fields.
func Detect(body []byte) Format {
	var p probe
	if err := json.Unmarshal(body, &p); err != nil {
		return FormatUnknown
	}
	switch {
	case isSet(p.Instructions), isSet(p.Input):
		return FormatOpenAIResponses
	case isSet(p.System) && isSet(p.Messages):
		return FormatAnthropicMessages
	// Anthropic without a system prompt: `tool_use_id` exists only in
	// Anthropic tool_result blocks; OpenAI identifies tool output by
	// role:"tool" messages instead.
	case isSet(p.Messages) && hasToolUseID(body):
		return FormatAnthropicMessages
	case isSet(p.Messages):
		return FormatOpenAIChat
	default:
		return FormatUnknown
	}
}

func hasToolUseID(body []byte) bool {
	return bytes.Contains(body, []byte(`"tool_use_id"`))
}
