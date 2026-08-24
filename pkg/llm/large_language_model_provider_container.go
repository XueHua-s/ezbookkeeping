package llm

import (
	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/errs"
	"github.com/mayswind/ezbookkeeping/pkg/llm/data"
	"github.com/mayswind/ezbookkeeping/pkg/llm/provider"
	"github.com/mayswind/ezbookkeeping/pkg/llm/provider/anthropic"
	"github.com/mayswind/ezbookkeeping/pkg/llm/provider/googleai"
	"github.com/mayswind/ezbookkeeping/pkg/llm/provider/lmstudio"
	"github.com/mayswind/ezbookkeeping/pkg/llm/provider/ollama"
	"github.com/mayswind/ezbookkeeping/pkg/llm/provider/openai"
	"github.com/mayswind/ezbookkeeping/pkg/log"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
)

// LargeLanguageModelProviderContainer contains the current large language model provider
type LargeLanguageModelProviderContainer struct {
	textRecognitionProvider         provider.LargeLanguageModelProvider
	receiptImageRecognitionProvider provider.LargeLanguageModelProvider
	aiAssistantProvider             provider.LargeLanguageModelProvider
}

type fallbackLargeLanguageModelProvider struct {
	usage            string
	primaryProvider  provider.LargeLanguageModelProvider
	fallbackProvider provider.LargeLanguageModelProvider
}

// Container is the singleton large language model provider container.
var Container = &LargeLanguageModelProviderContainer{}

// InitializeLargeLanguageModelProvider initializes the current large language model provider according to the config
func InitializeLargeLanguageModelProvider(config *settings.Config) error {
	var err error
	Container.textRecognitionProvider, err = initializeLargeLanguageModelProvider(config.TextRecognitionLLMConfig, config.EnableDebugLog)
	if err != nil {
		return err
	}

	Container.receiptImageRecognitionProvider, err = initializeLargeLanguageModelProviderWithFallback(
		"receipt image recognition",
		config.ReceiptImageRecognitionLLMConfig,
		config.ReceiptImageRecognitionFallbackLLMConfig,
		config.EnableDebugLog,
	)
	if err != nil {
		return err
	}

	Container.aiAssistantProvider = nil
	if config.EnableAIAssistant {
		Container.aiAssistantProvider, err = initializeLargeLanguageModelProviderWithFallback(
			"ai assistant",
			config.AIAssistantLLMConfig,
			config.AIAssistantFallbackLLMConfig,
			config.EnableDebugLog,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func initializeLargeLanguageModelProviderWithFallback(usage string, primaryConfig *settings.LLMConfig, fallbackConfig *settings.LLMConfig, enableResponseLog bool) (provider.LargeLanguageModelProvider, error) {
	primaryProvider, err := initializeLargeLanguageModelProvider(primaryConfig, enableResponseLog)
	if err != nil {
		return nil, err
	}

	fallbackProvider, err := initializeLargeLanguageModelProvider(fallbackConfig, enableResponseLog)
	if err != nil {
		return nil, err
	}

	if primaryProvider == nil {
		return nil, nil
	}

	return &fallbackLargeLanguageModelProvider{
		usage:            usage,
		primaryProvider:  primaryProvider,
		fallbackProvider: fallbackProvider,
	}, nil
}

func initializeLargeLanguageModelProvider(llmConfig *settings.LLMConfig, enableResponseLog bool) (provider.LargeLanguageModelProvider, error) {
	if llmConfig == nil || llmConfig.LLMProvider == "" {
		return nil, nil
	} else if llmConfig.LLMProvider == settings.OpenAILLMProvider {
		return openai.NewOpenAILargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.OpenAICompatibleLLMProvider {
		return openai.NewOpenAICompatibleLargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.OpenAIResponsesCompatibleLLMProvider {
		return openai.NewOpenAIResponsesCompatibleLargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.AnthropicLLMProvider {
		return anthropic.NewAnthropicLargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.AnthropicCompatibleLLMProvider {
		return anthropic.NewAnthropicCompatibleLargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.OpenRouterLLMProvider {
		return openai.NewOpenRouterLargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.OllamaLLMProvider {
		return ollama.NewOllamaLargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.LMStudioLLMProvider {
		return lmstudio.NewLMStudioLargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.GoogleAILLMProvider {
		return googleai.NewGoogleAILargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	}

	return nil, errs.ErrInvalidLLMProvider
}

// GetJsonResponseByTextRecognitionModel returns the json response from the transaction text recognition model
func (l *LargeLanguageModelProviderContainer) GetJsonResponseByTextRecognitionModel(c core.Context, uid int64, request *data.LargeLanguageModelRequest) (*data.LargeLanguageModelTextualResponse, error) {
	if l.textRecognitionProvider == nil {
		return nil, errs.ErrInvalidLLMProvider
	}

	return l.textRecognitionProvider.GetJsonResponse(c, uid, request)
}

// GetJsonResponseByReceiptImageRecognitionModel returns the json response from the current large language model provider by receipt image recognition model
func (l *LargeLanguageModelProviderContainer) GetJsonResponseByReceiptImageRecognitionModel(c core.Context, uid int64, request *data.LargeLanguageModelRequest) (*data.LargeLanguageModelTextualResponse, error) {
	if l.receiptImageRecognitionProvider == nil {
		return nil, errs.ErrInvalidLLMProvider
	}

	return l.receiptImageRecognitionProvider.GetJsonResponse(c, uid, request)
}

// GetJsonResponseByAIAssistantModel returns the json response from the current large language model provider by ai assistant model
func (l *LargeLanguageModelProviderContainer) GetJsonResponseByAIAssistantModel(c core.Context, uid int64, request *data.LargeLanguageModelRequest) (*data.LargeLanguageModelTextualResponse, error) {
	if l.aiAssistantProvider == nil {
		return nil, errs.ErrInvalidLLMProvider
	}

	return l.aiAssistantProvider.GetJsonResponse(c, uid, request)
}

// GetJsonResponse retries the complete request with the fallback only when the
// primary provider fails before returning a response.
func (p *fallbackLargeLanguageModelProvider) GetJsonResponse(c core.Context, uid int64, request *data.LargeLanguageModelRequest) (*data.LargeLanguageModelTextualResponse, error) {
	response, err := p.primaryProvider.GetJsonResponse(c, uid, request)

	if err == nil || p.fallbackProvider == nil {
		return response, err
	}

	p.logFallback(c, uid, err)
	return p.fallbackProvider.GetJsonResponse(c, uid, request)
}

// StreamTextResponse retries with the fallback only if the primary fails
// before emitting any delta, which prevents duplicate or interleaved output.
func (p *fallbackLargeLanguageModelProvider) StreamTextResponse(c core.Context, uid int64, request *data.LargeLanguageModelRequest, callback data.LargeLanguageModelStreamCallback) (*data.LargeLanguageModelStreamResponse, error) {
	primaryProvider, ok := p.primaryProvider.(provider.LargeLanguageModelStreamingProvider)
	if !ok {
		return nil, errs.ErrInvalidLLMProvider
	}

	streamed := false
	trackedCallback := func(deltaType data.LargeLanguageModelStreamDeltaType, delta string) {
		streamed = true
		if callback != nil {
			callback(deltaType, delta)
		}
	}
	response, err := primaryProvider.StreamTextResponse(c, uid, request, trackedCallback)
	if err == nil || streamed || p.fallbackProvider == nil {
		return response, err
	}

	fallbackProvider, ok := p.fallbackProvider.(provider.LargeLanguageModelStreamingProvider)
	if !ok {
		return response, err
	}

	p.logFallback(c, uid, err)
	return fallbackProvider.StreamTextResponse(c, uid, request, callback)
}

func (p *fallbackLargeLanguageModelProvider) logFallback(c core.Context, uid int64, err error) {
	log.Warnf(c, "[large_language_model_provider_container] primary %s provider failed for user \"uid:%d\", retrying with fallback provider, because %s", p.usage, uid, err.Error())
}

// StreamTextResponseByAIAssistantModel streams a response from the configured
// AI assistant provider without exposing provider-specific protocols.
func (l *LargeLanguageModelProviderContainer) StreamTextResponseByAIAssistantModel(c core.Context, uid int64, request *data.LargeLanguageModelRequest, callback data.LargeLanguageModelStreamCallback) (*data.LargeLanguageModelStreamResponse, error) {
	streamingProvider, ok := l.aiAssistantProvider.(provider.LargeLanguageModelStreamingProvider)
	if !ok {
		return nil, errs.ErrInvalidLLMProvider
	}
	return streamingProvider.StreamTextResponse(c, uid, request, callback)
}

var _ provider.LargeLanguageModelStreamingProvider = (*fallbackLargeLanguageModelProvider)(nil)
