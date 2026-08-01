package llm

import (
	"errors"
	"testing"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/llm/data"
	"github.com/mayswind/ezbookkeeping/pkg/llm/provider"
	"github.com/stretchr/testify/assert"
)

type fallbackTestProvider struct {
	response       *data.LargeLanguageModelTextualResponse
	err            error
	calls          int
	lastRequest    *data.LargeLanguageModelRequest
	streamResponse *data.LargeLanguageModelStreamResponse
	streamErr      error
	streamDeltas   []string
	streamCalls    int
}

func (p *fallbackTestProvider) GetJsonResponse(c core.Context, uid int64, request *data.LargeLanguageModelRequest) (*data.LargeLanguageModelTextualResponse, error) {
	p.calls++
	p.lastRequest = request
	return p.response, p.err
}

func (p *fallbackTestProvider) StreamTextResponse(c core.Context, uid int64, request *data.LargeLanguageModelRequest, callback data.LargeLanguageModelStreamCallback) (*data.LargeLanguageModelStreamResponse, error) {
	p.streamCalls++
	for _, delta := range p.streamDeltas {
		if callback != nil {
			callback(data.LARGE_LANGUAGE_MODEL_STREAM_DELTA_TYPE_REPLY, delta)
		}
	}
	return p.streamResponse, p.streamErr
}

func TestGetJsonResponseByReceiptImageRecognitionModel_UsesFallbackAfterPrimaryFailure(t *testing.T) {
	primary := &fallbackTestProvider{err: errors.New("primary unavailable")}
	fallback := &fallbackTestProvider{response: &data.LargeLanguageModelTextualResponse{Content: "fallback"}}
	container := &LargeLanguageModelProviderContainer{
		receiptImageRecognitionProvider: &fallbackLargeLanguageModelProvider{
			usage:            "receipt image recognition",
			primaryProvider:  primary,
			fallbackProvider: fallback,
		},
	}
	request := &data.LargeLanguageModelRequest{}

	response, err := container.GetJsonResponseByReceiptImageRecognitionModel(nil, 1, request)

	assert.NoError(t, err)
	assert.Equal(t, "fallback", response.Content)
	assert.Equal(t, 1, primary.calls)
	assert.Equal(t, 1, fallback.calls)
	assert.Same(t, request, primary.lastRequest)
	assert.Same(t, request, fallback.lastRequest)
}

func TestGetJsonResponseByAIAssistantModel_ReturnsFallbackFailure(t *testing.T) {
	primary := &fallbackTestProvider{err: errors.New("primary unavailable")}
	fallbackErr := errors.New("fallback unavailable")
	fallback := &fallbackTestProvider{err: fallbackErr}
	container := &LargeLanguageModelProviderContainer{
		aiAssistantProvider: &fallbackLargeLanguageModelProvider{
			usage:            "ai assistant",
			primaryProvider:  primary,
			fallbackProvider: fallback,
		},
	}

	response, err := container.GetJsonResponseByAIAssistantModel(nil, 1, &data.LargeLanguageModelRequest{})

	assert.Nil(t, response)
	assert.ErrorIs(t, err, fallbackErr)
	assert.Equal(t, 1, primary.calls)
	assert.Equal(t, 1, fallback.calls)
}

func TestGetJsonResponseByAIAssistantModel_DoesNotUseFallbackAfterPrimarySuccess(t *testing.T) {
	primary := &fallbackTestProvider{response: &data.LargeLanguageModelTextualResponse{Content: "primary"}}
	fallback := &fallbackTestProvider{response: &data.LargeLanguageModelTextualResponse{Content: "fallback"}}
	container := &LargeLanguageModelProviderContainer{
		aiAssistantProvider: &fallbackLargeLanguageModelProvider{
			usage:            "ai assistant",
			primaryProvider:  primary,
			fallbackProvider: fallback,
		},
	}

	response, err := container.GetJsonResponseByAIAssistantModel(nil, 1, &data.LargeLanguageModelRequest{})

	assert.NoError(t, err)
	assert.Equal(t, "primary", response.Content)
	assert.Equal(t, 1, primary.calls)
	assert.Equal(t, 0, fallback.calls)
}

func TestStreamTextResponseByAIAssistantModel_UsesFallbackBeforeFirstDelta(t *testing.T) {
	primary := &fallbackTestProvider{streamErr: errors.New("primary unavailable")}
	fallback := &fallbackTestProvider{
		streamResponse: &data.LargeLanguageModelStreamResponse{Content: "fallback"},
		streamDeltas:   []string{"fallback"},
	}
	container := &LargeLanguageModelProviderContainer{
		aiAssistantProvider: &fallbackLargeLanguageModelProvider{
			usage:            "ai assistant",
			primaryProvider:  primary,
			fallbackProvider: fallback,
		},
	}

	deltas := make([]string, 0, 1)
	response, err := container.StreamTextResponseByAIAssistantModel(nil, 1, &data.LargeLanguageModelRequest{}, func(deltaType data.LargeLanguageModelStreamDeltaType, delta string) {
		deltas = append(deltas, delta)
	})

	assert.NoError(t, err)
	assert.Equal(t, "fallback", response.Content)
	assert.Equal(t, []string{"fallback"}, deltas)
	assert.Equal(t, 1, primary.streamCalls)
	assert.Equal(t, 1, fallback.streamCalls)
}

func TestStreamTextResponseByAIAssistantModel_DoesNotFallbackAfterFirstDelta(t *testing.T) {
	primaryErr := errors.New("primary interrupted")
	primary := &fallbackTestProvider{
		streamErr:    primaryErr,
		streamDeltas: []string{"partial"},
	}
	fallback := &fallbackTestProvider{
		streamResponse: &data.LargeLanguageModelStreamResponse{Content: "fallback"},
	}
	container := &LargeLanguageModelProviderContainer{
		aiAssistantProvider: &fallbackLargeLanguageModelProvider{
			usage:            "ai assistant",
			primaryProvider:  primary,
			fallbackProvider: fallback,
		},
	}

	response, err := container.StreamTextResponseByAIAssistantModel(nil, 1, &data.LargeLanguageModelRequest{}, func(deltaType data.LargeLanguageModelStreamDeltaType, delta string) {})

	assert.Nil(t, response)
	assert.ErrorIs(t, err, primaryErr)
	assert.Equal(t, 1, primary.streamCalls)
	assert.Equal(t, 0, fallback.streamCalls)
}

var _ provider.LargeLanguageModelProvider = (*fallbackTestProvider)(nil)
var _ provider.LargeLanguageModelStreamingProvider = (*fallbackTestProvider)(nil)
var _ provider.LargeLanguageModelProvider = (*fallbackLargeLanguageModelProvider)(nil)
