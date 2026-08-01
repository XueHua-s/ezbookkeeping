package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/llm/data"
	"github.com/mayswind/ezbookkeeping/pkg/llm/provider"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
	"github.com/stretchr/testify/assert"
)

func TestOpenAILargeLanguageModelProvider_StreamTextResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "/v1/responses", request.URL.Path)
		assert.Equal(t, "Bearer test-key", request.Header.Get("Authorization"))

		var body openAIResponsesRequest
		assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		assert.Equal(t, "test-model", body.Model)
		assert.Equal(t, "system prompt", body.Instructions)
		assert.Len(t, body.Input, 1)
		assert.Equal(t, "user", body.Input[0].Role)
		assert.Len(t, body.Input[0].Content, 1)
		assert.Equal(t, "input_text", body.Input[0].Content[0].Type)
		assert.Equal(t, "user prompt", body.Input[0].Content[0].Text)
		assert.True(t, body.Stream)
		assert.False(t, body.Store)
		assert.Equal(t, openAIResponsesReasoningSummaryLevel, body.Reasoning.Summary)

		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"Thinking\"}\n\n"))
		_, _ = writer.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n\n"))
		_, _ = writer.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{}}\n\n"))
	}))
	defer server.Close()

	llmProvider := newOpenAILargeLanguageModelProvider(&settings.LLMConfig{
		OpenAIBaseURL:                       server.URL + "/v1/",
		OpenAIAPIKey:                        "test-key",
		OpenAIModelID:                       "test-model",
		LargeLanguageModelAPIRequestTimeout: 5000,
		LargeLanguageModelAPIProxy:          "none",
	}, false, nil)
	streamingProvider := llmProvider.(provider.LargeLanguageModelStreamingProvider)

	deltas := make([]string, 0, 2)
	response, err := streamingProvider.StreamTextResponse(core.NewNullContext(), 1, &data.LargeLanguageModelRequest{
		SystemPrompt: "system prompt",
		UserPrompt:   []byte("user prompt"),
	}, func(deltaType data.LargeLanguageModelStreamDeltaType, delta string) {
		deltas = append(deltas, delta)
	})

	assert.NoError(t, err)
	assert.Equal(t, "Hello", response.Content)
	assert.Equal(t, "Thinking", response.Thinking)
	assert.Equal(t, []string{"Thinking", "Hello"}, deltas)
}

func TestExtractTextFromOpenAIResponseCompletedEvent_NestedOutput(t *testing.T) {
	responseObject := map[string]any{
		"output": []any{
			map[string]any{
				"content": []any{
					map[string]any{"type": "reasoning", "text": "ignored"},
					map[string]any{"type": "output_text", "text": "Hello"},
					map[string]any{"type": "text", "text": " world"},
				},
			},
		},
	}

	assert.Equal(t, "Hello world", extractTextFromOpenAIResponseCompletedEvent(responseObject))
}
