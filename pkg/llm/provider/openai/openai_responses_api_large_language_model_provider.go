package openai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/errs"
	"github.com/mayswind/ezbookkeeping/pkg/httpclient"
	"github.com/mayswind/ezbookkeeping/pkg/llm/data"
	"github.com/mayswind/ezbookkeeping/pkg/llm/provider"
	"github.com/mayswind/ezbookkeeping/pkg/log"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
)

const (
	openAIResponsesPath                  = "responses"
	openAIResponsesReasoningSummaryLevel = "auto"
)

type openAILargeLanguageModelProvider struct {
	chatCompletionsProvider provider.LargeLanguageModelProvider
	responsesURL            string
	apiKey                  string
	modelID                 string
	httpClient              *http.Client
}

type openAIResponsesRequest struct {
	Model        string                         `json:"model"`
	Instructions string                         `json:"instructions,omitempty"`
	Input        []*openAIResponsesInputMessage `json:"input"`
	Stream       bool                           `json:"stream"`
	Store        bool                           `json:"store"`
	Reasoning    *openAIResponsesReasoningItem  `json:"reasoning,omitempty"`
}

type openAIResponsesReasoningItem struct {
	Summary string `json:"summary,omitempty"`
}

type openAIResponsesInputMessage struct {
	Role    string                             `json:"role"`
	Content []*openAIResponsesInputTextContent `json:"content"`
}

type openAIResponsesInputTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func newOpenAILargeLanguageModelProvider(llmConfig *settings.LLMConfig, enableResponseLog bool, chatCompletionsProvider provider.LargeLanguageModelProvider) provider.LargeLanguageModelProvider {
	return &openAILargeLanguageModelProvider{
		chatCompletionsProvider: chatCompletionsProvider,
		responsesURL:            llmConfig.GetOpenAIEndpointURL(openAIResponsesPath),
		apiKey:                  strings.TrimSpace(llmConfig.OpenAIAPIKey),
		modelID:                 strings.TrimSpace(llmConfig.OpenAIModelID),
		httpClient:              httpclient.NewHttpClient(llmConfig.LargeLanguageModelAPIRequestTimeout, llmConfig.LargeLanguageModelAPIProxy, llmConfig.LargeLanguageModelAPISkipTLSVerify, core.GetOutgoingUserAgent(), enableResponseLog),
	}
}

func (p *openAILargeLanguageModelProvider) GetJsonResponse(c core.Context, uid int64, request *data.LargeLanguageModelRequest) (*data.LargeLanguageModelTextualResponse, error) {
	return p.chatCompletionsProvider.GetJsonResponse(c, uid, request)
}

// StreamTextResponse hides the OpenAI Responses API wire format and emits only
// provider-neutral reply and thinking deltas.
func (p *openAILargeLanguageModelProvider) StreamTextResponse(c core.Context, uid int64, request *data.LargeLanguageModelRequest, callback data.LargeLanguageModelStreamCallback) (*data.LargeLanguageModelStreamResponse, error) {
	if p.apiKey == "" || p.modelID == "" {
		return nil, errs.ErrFailedToRequestRemoteApi
	}

	requestBodyBytes, err := json.Marshal(&openAIResponsesRequest{
		Model:        p.modelID,
		Instructions: request.SystemPrompt,
		Input: []*openAIResponsesInputMessage{
			{
				Role: "user",
				Content: []*openAIResponsesInputTextContent{
					{
						Type: "input_text",
						Text: string(request.UserPrompt),
					},
				},
			},
		},
		Stream: true,
		Store:  false,
		Reasoning: &openAIResponsesReasoningItem{
			Summary: openAIResponsesReasoningSummaryLevel,
		},
	})
	if err != nil {
		log.Errorf(c, "[openai_responses_api_large_language_model_provider.StreamTextResponse] failed to marshal request for user \"uid:%d\", because %s", uid, err.Error())
		return nil, errs.ErrOperationFailed
	}

	httpRequest, err := http.NewRequest("POST", p.responsesURL, bytes.NewReader(requestBodyBytes))
	if err != nil {
		log.Errorf(c, "[openai_responses_api_large_language_model_provider.StreamTextResponse] failed to build request for user \"uid:%d\", because %s", uid, err.Error())
		return nil, errs.ErrFailedToRequestRemoteApi
	}

	httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	httpRequest = httpRequest.WithContext(httpclient.CustomHttpResponseLog(c, func(responseData []byte) {
		log.Debugf(c, "[openai_responses_api_large_language_model_provider.StreamTextResponse] response is %s", responseData)
	}))

	response, err := p.httpClient.Do(httpRequest)
	if err != nil {
		log.Errorf(c, "[openai_responses_api_large_language_model_provider.StreamTextResponse] failed to request response stream for user \"uid:%d\", because %s", uid, err.Error())
		return nil, errs.ErrFailedToRequestRemoteApi
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(response.Body)
		log.Errorf(c, "[openai_responses_api_large_language_model_provider.StreamTextResponse] failed to request response stream for user \"uid:%d\", because response code is %d, response is %s", uid, response.StatusCode, string(responseBody))
		return nil, errs.ErrFailedToRequestRemoteApi
	}

	streamResponse := &data.LargeLanguageModelStreamResponse{}
	replyBuilder := &strings.Builder{}
	thinkingBuilder := &strings.Builder{}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 4096), 4*1024*1024)
	eventDataLines := make([]string, 0, 4)
	streamDone := false

	processCurrentEvent := func() error {
		if len(eventDataLines) < 1 {
			return nil
		}

		eventData := strings.TrimSpace(strings.Join(eventDataLines, "\n"))
		eventDataLines = eventDataLines[:0]
		done, eventErr := processOpenAIResponsesStreamEvent(c, uid, eventData, replyBuilder, thinkingBuilder, callback)
		if done {
			streamDone = true
		}
		return eventErr
	}

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			if err := processCurrentEvent(); err != nil {
				return nil, err
			}
			if streamDone {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			eventDataLines = append(eventDataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	if err := scanner.Err(); err != nil {
		log.Errorf(c, "[openai_responses_api_large_language_model_provider.StreamTextResponse] failed to read response stream for user \"uid:%d\", because %s", uid, err.Error())
		return nil, errs.ErrFailedToRequestRemoteApi
	}
	if !streamDone {
		if err := processCurrentEvent(); err != nil {
			return nil, err
		}
	}

	streamResponse.Content = replyBuilder.String()
	streamResponse.Thinking = thinkingBuilder.String()
	return streamResponse, nil
}

func processOpenAIResponsesStreamEvent(c core.Context, uid int64, eventData string, replyBuilder *strings.Builder, thinkingBuilder *strings.Builder, callback data.LargeLanguageModelStreamCallback) (bool, error) {
	if eventData == "" {
		return false, nil
	}
	if eventData == "[DONE]" {
		return true, nil
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(eventData), &event); err != nil {
		log.Warnf(c, "[openai_responses_api_large_language_model_provider.processOpenAIResponsesStreamEvent] failed to parse event data for user \"uid:%d\", because %s", uid, err.Error())
		return false, nil
	}

	eventType, _ := event["type"].(string)
	if eventType == "response.reasoning_summary_text.delta" {
		delta, _ := event["delta"].(string)
		appendOpenAIResponsesStreamDelta(data.LARGE_LANGUAGE_MODEL_STREAM_DELTA_TYPE_THINKING, delta, thinkingBuilder, callback)
		return false, nil
	}
	if eventType == "response.output_text.delta" {
		delta, _ := event["delta"].(string)
		appendOpenAIResponsesStreamDelta(data.LARGE_LANGUAGE_MODEL_STREAM_DELTA_TYPE_REPLY, delta, replyBuilder, callback)
		return false, nil
	}
	if eventType == "response.completed" {
		if replyBuilder.Len() < 1 {
			if responseObject, ok := event["response"].(map[string]any); ok {
				appendOpenAIResponsesStreamDelta(data.LARGE_LANGUAGE_MODEL_STREAM_DELTA_TYPE_REPLY, extractTextFromOpenAIResponseCompletedEvent(responseObject), replyBuilder, callback)
			}
		}
		return true, nil
	}
	if eventType == "error" {
		log.Errorf(c, "[openai_responses_api_large_language_model_provider.processOpenAIResponsesStreamEvent] openai stream returns error for user \"uid:%d\", payload is %s", uid, eventData)
		return true, errs.ErrFailedToRequestRemoteApi
	}

	return false, nil
}

func appendOpenAIResponsesStreamDelta(deltaType data.LargeLanguageModelStreamDeltaType, delta string, builder *strings.Builder, callback data.LargeLanguageModelStreamCallback) {
	if delta == "" {
		return
	}
	builder.WriteString(delta)
	if callback != nil {
		callback(deltaType, delta)
	}
}

func extractTextFromOpenAIResponseCompletedEvent(responseObject map[string]any) string {
	outputText, _ := responseObject["output_text"].(string)
	if outputText != "" {
		return outputText
	}

	outputItems, ok := responseObject["output"].([]any)
	if !ok || len(outputItems) < 1 {
		return ""
	}

	outputBuilder := &strings.Builder{}
	for _, outputItem := range outputItems {
		outputItemMap, ok := outputItem.(map[string]any)
		if !ok {
			continue
		}
		contentItems, ok := outputItemMap["content"].([]any)
		if !ok {
			continue
		}
		for _, contentItem := range contentItems {
			contentItemMap, ok := contentItem.(map[string]any)
			if !ok {
				continue
			}
			contentType, _ := contentItemMap["type"].(string)
			if contentType != "output_text" && contentType != "text" {
				continue
			}
			text, _ := contentItemMap["text"].(string)
			outputBuilder.WriteString(text)
		}
	}

	return outputBuilder.String()
}

var _ provider.LargeLanguageModelProvider = (*openAILargeLanguageModelProvider)(nil)
var _ provider.LargeLanguageModelStreamingProvider = (*openAILargeLanguageModelProvider)(nil)
