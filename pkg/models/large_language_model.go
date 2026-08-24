package models

// TransactionTextRecognitionRequest represents the request of recognizing transaction from text
type TransactionTextRecognitionRequest struct {
	Text string `json:"text"`
}

// RecognizedTransactionResponse represents a view-object of recognized transaction response
type RecognizedTransactionResponse struct {
	Type                 TransactionType `json:"type"`
	Time                 int64           `json:"time,omitempty"`
	CategoryId           int64           `json:"categoryId,string,omitempty"`
	SourceAccountId      int64           `json:"sourceAccountId,string,omitempty"`
	DestinationAccountId int64           `json:"destinationAccountId,string,omitempty"`
	SourceAmount         int64           `json:"sourceAmount,omitempty"`
	DestinationAmount    int64           `json:"destinationAmount,omitempty"`
	TagIds               []string        `json:"tagIds,omitempty"`
	Comment              string          `json:"comment,omitempty"`
}

// RecognizedTransactionResult represents the result of recognized transaction
type RecognizedTransactionResult struct {
	Type                   string   `json:"type,omitempty" jsonschema:"enum=income,enum=expense,enum=transfer" jsonschema_description:"Transaction type (income, expense, transfer)"`
	Time                   string   `json:"time" jsonschema:"format=date-time" jsonschema_description:"Transaction time in long date time format (YYYY-MM-DD HH:mm:ss, e.g. 2023-01-01 12:00:00)"`
	Amount                 string   `json:"amount,omitempty" jsonschema_description:"Transaction amount"`
	AccountName            string   `json:"account,omitempty" jsonschema_description:"Account name for the transaction"`
	CategoryName           string   `json:"category,omitempty" jsonschema_description:"Category name for the transaction"`
	TagNames               []string `json:"tags,omitempty" jsonschema_description:"List of tags associated with the transaction (maximum 10 tags allowed)"`
	Description            string   `json:"description,omitempty" jsonschema_description:"Transaction description"`
	DestinationAmount      string   `json:"destination_amount,omitempty" jsonschema_description:"Destination amount for transfer transactions"`
	DestinationAccountName string   `json:"destination_account,omitempty" jsonschema_description:"Destination account name for transfer transactions"`
}

// RecognizedReceiptImageResponse is retained as a compatibility alias for the online fork.
type RecognizedReceiptImageResponse = RecognizedTransactionResponse

// RecognizedReceiptImageResult is retained as a compatibility alias for the online fork.
type RecognizedReceiptImageResult = RecognizedTransactionResult

// RecognizedReceiptImageResultItem represents one item in a batch recognition response
type RecognizedReceiptImageResultItem struct {
	Index   int                             `json:"index"`
	Success bool                            `json:"success"`
	Result  *RecognizedReceiptImageResponse `json:"result,omitempty"`
	Error   string                          `json:"error,omitempty"`
}

// RecognizedReceiptImageBatchResponse represents a batch of recognized receipt image results
type RecognizedReceiptImageBatchResponse struct {
	Results []*RecognizedReceiptImageResultItem `json:"results"`
}

const (
	AIAssistantModeChat    = "chat"
	AIAssistantModeSummary = "summary"
)

// AIAssistantHistoryItem represents one history message for ai assistant
type AIAssistantHistoryItem struct {
	Role    string `json:"role" binding:"required,oneof=user assistant"`
	Content string `json:"content" binding:"required,max=2048"`
}

// AIAssistantChatRequest represents all parameters for ai assistant chat request
type AIAssistantChatRequest struct {
	Mode    string                    `json:"mode" binding:"omitempty,oneof=chat summary"`
	Message string                    `json:"message" binding:"max=2048"`
	History []*AIAssistantHistoryItem `json:"history" binding:"max=20"`
}

// AIAssistantReferencedTransaction represents one referenced transaction in ai assistant response
type AIAssistantReferencedTransaction struct {
	Id                     int64           `json:"id,string"`
	Time                   int64           `json:"time"`
	TimeText               string          `json:"timeText,omitempty"`
	Type                   TransactionType `json:"type"`
	CategoryName           string          `json:"categoryName,omitempty"`
	SourceAccountName      string          `json:"sourceAccountName,omitempty"`
	DestinationAccountName string          `json:"destinationAccountName,omitempty"`
	SourceAmount           int64           `json:"sourceAmount"`
	DestinationAmount      int64           `json:"destinationAmount,omitempty"`
	Currency               string          `json:"currency,omitempty"`
	DestinationCurrency    string          `json:"destinationCurrency,omitempty"`
	Comment                string          `json:"comment,omitempty"`
	SimilarityScore        float64         `json:"similarityScore,omitempty"`
}

// AIAssistantChatResponse represents ai assistant chat response
type AIAssistantChatResponse struct {
	Mode       string                              `json:"mode"`
	Reply      string                              `json:"reply"`
	References []*AIAssistantReferencedTransaction `json:"references,omitempty"`
}

const (
	AIAssistantChatStreamChunkTypeThinkingDelta = "thinking_delta"
	AIAssistantChatStreamChunkTypeReplyDelta    = "reply_delta"
	AIAssistantChatStreamChunkTypeReferences    = "references"
	AIAssistantChatStreamChunkTypeDone          = "done"
)

// AIAssistantChatStreamChunk represents one ai assistant stream response chunk
type AIAssistantChatStreamChunk struct {
	Type       string                              `json:"type"`
	Mode       string                              `json:"mode,omitempty"`
	Delta      string                              `json:"delta,omitempty"`
	Reply      string                              `json:"reply,omitempty"`
	Thinking   string                              `json:"thinking,omitempty"`
	References []*AIAssistantReferencedTransaction `json:"references,omitempty"`
}

// AIAssistantResult represents the result schema of ai assistant response from llm
type AIAssistantResult struct {
	Reply string `json:"reply,omitempty" jsonschema_description:"Response text for user with bill summary and bookkeeping suggestions"`
}
