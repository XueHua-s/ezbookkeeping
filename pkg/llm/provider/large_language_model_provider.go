package provider

import (
	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/llm/data"
)

// LargeLanguageModelProvider defines the structure of large language model provider
type LargeLanguageModelProvider interface {
	// GetJsonResponse returns the json response from the large language model provider
	GetJsonResponse(c core.Context, uid int64, request *data.LargeLanguageModelRequest) (*data.LargeLanguageModelTextualResponse, error)
}

// LargeLanguageModelStreamingProvider streams a textual response while also
// returning the complete collected response.
type LargeLanguageModelStreamingProvider interface {
	StreamTextResponse(c core.Context, uid int64, request *data.LargeLanguageModelRequest, callback data.LargeLanguageModelStreamCallback) (*data.LargeLanguageModelStreamResponse, error)
}
