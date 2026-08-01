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
	receiptImageRecognitionCurrentProvider  provider.LargeLanguageModelProvider
	receiptImageRecognitionFallbackProvider provider.LargeLanguageModelProvider
	aiAssistantCurrentProvider              provider.LargeLanguageModelProvider
	aiAssistantFallbackProvider             provider.LargeLanguageModelProvider
}

// Initialize a large language model provider container singleton instance
var (
	Container = &LargeLanguageModelProviderContainer{}
)

// InitializeLargeLanguageModelProvider initializes the current large language model provider according to the config
func InitializeLargeLanguageModelProvider(config *settings.Config) error {
	var err error = nil
	Container.receiptImageRecognitionCurrentProvider = nil
	Container.receiptImageRecognitionFallbackProvider = nil
	Container.aiAssistantCurrentProvider = nil
	Container.aiAssistantFallbackProvider = nil

	if config.ReceiptImageRecognitionLLMConfig != nil {
		Container.receiptImageRecognitionCurrentProvider, err = initializeLargeLanguageModelProvider(config.ReceiptImageRecognitionLLMConfig, config.EnableDebugLog)

		if err != nil {
			return err
		}
	}

	if config.ReceiptImageRecognitionFallbackLLMConfig != nil {
		Container.receiptImageRecognitionFallbackProvider, err = initializeLargeLanguageModelProvider(config.ReceiptImageRecognitionFallbackLLMConfig, config.EnableDebugLog)

		if err != nil {
			return err
		}
	}

	if config.EnableAIAssistant && config.AIAssistantLLMConfig != nil {
		Container.aiAssistantCurrentProvider, err = initializeLargeLanguageModelProvider(config.AIAssistantLLMConfig, config.EnableDebugLog)

		if err != nil {
			return err
		}
	}

	if config.EnableAIAssistant && config.AIAssistantFallbackLLMConfig != nil {
		Container.aiAssistantFallbackProvider, err = initializeLargeLanguageModelProvider(config.AIAssistantFallbackLLMConfig, config.EnableDebugLog)

		if err != nil {
			return err
		}
	}

	return nil
}

func initializeLargeLanguageModelProvider(llmConfig *settings.LLMConfig, enableResponseLog bool) (provider.LargeLanguageModelProvider, error) {
	if llmConfig.LLMProvider == settings.OpenAILLMProvider {
		return openai.NewOpenAILargeLanguageModelProvider(llmConfig, enableResponseLog), nil
	} else if llmConfig.LLMProvider == settings.OpenAICompatibleLLMProvider {
		return openai.NewOpenAICompatibleLargeLanguageModelProvider(llmConfig, enableResponseLog), nil
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
	} else if llmConfig.LLMProvider == "" {
		return nil, nil
	}

	return nil, errs.ErrInvalidLLMProvider
}

// GetJsonResponseByReceiptImageRecognitionModel returns the json response from the current large language model provider by receipt image recognition model
func (l *LargeLanguageModelProviderContainer) GetJsonResponseByReceiptImageRecognitionModel(c core.Context, uid int64, currentConfig *settings.Config, request *data.LargeLanguageModelRequest) (*data.LargeLanguageModelTextualResponse, error) {
	if currentConfig.ReceiptImageRecognitionLLMConfig == nil || l.receiptImageRecognitionCurrentProvider == nil {
		return nil, errs.ErrInvalidLLMProvider
	}

	return l.getJsonResponseWithFallback(c, uid, "receipt image recognition", l.receiptImageRecognitionCurrentProvider, currentConfig.ReceiptImageRecognitionLLMConfig, l.receiptImageRecognitionFallbackProvider, currentConfig.ReceiptImageRecognitionFallbackLLMConfig, request)
}

// GetJsonResponseByAIAssistantModel returns the json response from the current large language model provider by ai assistant model
func (l *LargeLanguageModelProviderContainer) GetJsonResponseByAIAssistantModel(c core.Context, uid int64, currentConfig *settings.Config, request *data.LargeLanguageModelRequest) (*data.LargeLanguageModelTextualResponse, error) {
	if currentConfig.AIAssistantLLMConfig == nil || l.aiAssistantCurrentProvider == nil {
		return nil, errs.ErrInvalidLLMProvider
	}

	return l.getJsonResponseWithFallback(c, uid, "ai assistant", l.aiAssistantCurrentProvider, currentConfig.AIAssistantLLMConfig, l.aiAssistantFallbackProvider, currentConfig.AIAssistantFallbackLLMConfig, request)
}

func (l *LargeLanguageModelProviderContainer) getJsonResponseWithFallback(c core.Context, uid int64, usage string, currentProvider provider.LargeLanguageModelProvider, currentLLMConfig *settings.LLMConfig, fallbackProvider provider.LargeLanguageModelProvider, fallbackLLMConfig *settings.LLMConfig, request *data.LargeLanguageModelRequest) (*data.LargeLanguageModelTextualResponse, error) {
	response, err := currentProvider.GetJsonResponse(c, uid, currentLLMConfig, request)

	if err == nil || fallbackProvider == nil || fallbackLLMConfig == nil || fallbackLLMConfig.LLMProvider == "" {
		return response, err
	}

	log.Warnf(c, "[large_language_model_provider_container.getJsonResponseWithFallback] primary %s provider failed for user \"uid:%d\", retrying with fallback provider, because %s", usage, uid, err.Error())
	return fallbackProvider.GetJsonResponse(c, uid, fallbackLLMConfig, request)
}
