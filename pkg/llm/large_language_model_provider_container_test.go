package llm

import (
	"errors"
	"testing"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/llm/data"
	"github.com/mayswind/ezbookkeeping/pkg/llm/provider"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
	"github.com/stretchr/testify/assert"
)

type fallbackTestProvider struct {
	response   *data.LargeLanguageModelTextualResponse
	err        error
	calls      int
	lastStream bool
}

func (p *fallbackTestProvider) GetJsonResponse(c core.Context, uid int64, currentLLMConfig *settings.LLMConfig, request *data.LargeLanguageModelRequest) (*data.LargeLanguageModelTextualResponse, error) {
	p.calls++
	if request != nil {
		p.lastStream = request.Stream
	}
	return p.response, p.err
}

func TestGetJsonResponseByReceiptImageRecognitionModel_UsesFallbackAfterPrimaryFailure(t *testing.T) {
	primary := &fallbackTestProvider{err: errors.New("primary unavailable")}
	fallback := &fallbackTestProvider{response: &data.LargeLanguageModelTextualResponse{Content: "fallback"}}
	container := &LargeLanguageModelProviderContainer{
		receiptImageRecognitionCurrentProvider:  primary,
		receiptImageRecognitionFallbackProvider: fallback,
	}
	config := &settings.Config{
		ReceiptImageRecognitionLLMConfig:         &settings.LLMConfig{LLMProvider: settings.OpenAILLMProvider},
		ReceiptImageRecognitionFallbackLLMConfig: &settings.LLMConfig{LLMProvider: settings.OpenAILLMProvider},
	}

	response, err := container.GetJsonResponseByReceiptImageRecognitionModel(nil, 1, config, &data.LargeLanguageModelRequest{})

	assert.NoError(t, err)
	assert.Equal(t, "fallback", response.Content)
	assert.Equal(t, 1, primary.calls)
	assert.Equal(t, 1, fallback.calls)
	assert.False(t, primary.lastStream)
	assert.True(t, fallback.lastStream)
}

func TestGetJsonResponseByAIAssistantModel_DoesNotUseFallbackAfterPrimarySuccess(t *testing.T) {
	primary := &fallbackTestProvider{response: &data.LargeLanguageModelTextualResponse{Content: "primary"}}
	fallback := &fallbackTestProvider{response: &data.LargeLanguageModelTextualResponse{Content: "fallback"}}
	container := &LargeLanguageModelProviderContainer{
		aiAssistantCurrentProvider:  primary,
		aiAssistantFallbackProvider: fallback,
	}
	config := &settings.Config{
		AIAssistantLLMConfig:         &settings.LLMConfig{LLMProvider: settings.OpenAILLMProvider},
		AIAssistantFallbackLLMConfig: &settings.LLMConfig{LLMProvider: settings.OpenAILLMProvider},
	}

	response, err := container.GetJsonResponseByAIAssistantModel(nil, 1, config, &data.LargeLanguageModelRequest{})

	assert.NoError(t, err)
	assert.Equal(t, "primary", response.Content)
	assert.Equal(t, 1, primary.calls)
	assert.Equal(t, 0, fallback.calls)
}

var _ provider.LargeLanguageModelProvider = (*fallbackTestProvider)(nil)
