package data

import "reflect"

type LargeLanguageModelRequestPromptType byte

// Large Language Model Request Prompt Type
const (
	LARGE_LANGUAGE_MODEL_REQUEST_PROMPT_TYPE_TEXT      LargeLanguageModelRequestPromptType = 0
	LARGE_LANGUAGE_MODEL_REQUEST_PROMPT_TYPE_IMAGE_URL LargeLanguageModelRequestPromptType = 1
)

type LargeLanguageModelResponseFormat byte

// Large Language Model Response Format
const (
	LARGE_LANGUAGE_MODEL_RESPONSE_FORMAT_TEXT LargeLanguageModelResponseFormat = 0
	LARGE_LANGUAGE_MODEL_RESPONSE_FORMAT_JSON LargeLanguageModelResponseFormat = 1
)

// LargeLanguageModelRequest represents a request to a large language model
type LargeLanguageModelRequest struct {
	Stream                 bool
	SystemPrompt           string
	UserPrompt             []byte
	UserPromptType         LargeLanguageModelRequestPromptType
	UserPromptContentType  string
	ResponseJsonObjectType reflect.Type
}

// LargeLanguageModelTextualResponse represents a textual response from a large language model
type LargeLanguageModelTextualResponse struct {
	Content string
}

type LargeLanguageModelStreamDeltaType byte

const (
	LARGE_LANGUAGE_MODEL_STREAM_DELTA_TYPE_REPLY LargeLanguageModelStreamDeltaType = iota
	LARGE_LANGUAGE_MODEL_STREAM_DELTA_TYPE_THINKING
)

// LargeLanguageModelStreamResponse represents the complete content collected
// while a provider streams deltas to the caller.
type LargeLanguageModelStreamResponse struct {
	Content  string
	Thinking string
}

// LargeLanguageModelStreamCallback receives each non-empty streamed delta.
type LargeLanguageModelStreamCallback func(deltaType LargeLanguageModelStreamDeltaType, delta string)
