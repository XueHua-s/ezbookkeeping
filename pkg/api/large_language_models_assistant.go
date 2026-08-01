package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/mayswind/ezbookkeeping/pkg/core"
	"github.com/mayswind/ezbookkeeping/pkg/errs"
	"github.com/mayswind/ezbookkeeping/pkg/httpclient"
	"github.com/mayswind/ezbookkeeping/pkg/llm"
	"github.com/mayswind/ezbookkeeping/pkg/llm/data"
	"github.com/mayswind/ezbookkeeping/pkg/log"
	"github.com/mayswind/ezbookkeeping/pkg/models"
	"github.com/mayswind/ezbookkeeping/pkg/settings"
	"github.com/mayswind/ezbookkeeping/pkg/templates"
	"github.com/mayswind/ezbookkeeping/pkg/utils"
)

const (
	aiAssistantOpenAIEmbeddingsPath              = "embeddings"
	aiAssistantKnowledgeBaseTransactionPageSize  = int32(180)
	aiAssistantKnowledgeBaseMaxTransactionCount  = 1800
	aiAssistantKnowledgeBaseHistoryCoverageYears = 2
	aiAssistantKnowledgeBaseTopK                 = 18
	aiAssistantEmbeddingRequestBatchSize         = 64
	aiAssistantMaxHistoryMessages                = 12
	aiAssistantMaxReferencedTransactionsCount    = 8
)

type openAIEmbeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openAIEmbeddingsResponse struct {
	Data []*openAIEmbeddingsResponseItem `json:"data"`
}

type openAIEmbeddingsResponseItem struct {
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

type aiAssistantPreparedPromptContext struct {
	Mode         string
	SystemPrompt string
	UserPrompt   string
	NoDataReply  string
	References   []*models.AIAssistantReferencedTransaction
}

type aiAssistantKnowledgeItem struct {
	Reference *models.AIAssistantReferencedTransaction
	Text      string
	TextHash  string
	Embedding []float64
}

type aiAssistantRetrievedKnowledgeItem struct {
	Item  *aiAssistantKnowledgeItem
	Score float64
}

type aiAssistantCurrencyOverview struct {
	Income      int64
	Expense     int64
	TransferOut int64
	TransferIn  int64
}

type aiAssistantExpenseCategoryOverview struct {
	CategoryName string
	Currency     string
	Amount       int64
}

// AssistantChatHandler returns ai assistant response for chat or summary request
func (a *LargeLanguageModelsApi) AssistantChatHandler(c *core.WebContext) (any, *errs.Error) {
	currentConfig := a.CurrentConfig()

	assistantContext, err := a.prepareAIAssistantPromptContext(c, currentConfig)

	if err != nil {
		return nil, err
	}

	if assistantContext.NoDataReply != "" {
		return &models.AIAssistantChatResponse{
			Mode:  assistantContext.Mode,
			Reply: assistantContext.NoDataReply,
		}, nil
	}

	llmRequest := &data.LargeLanguageModelRequest{
		Stream:                 false,
		SystemPrompt:           assistantContext.SystemPrompt,
		UserPrompt:             []byte(assistantContext.UserPrompt),
		UserPromptType:         data.LARGE_LANGUAGE_MODEL_REQUEST_PROMPT_TYPE_TEXT,
		ResponseJsonObjectType: reflect.TypeOf(models.AIAssistantResult{}),
	}

	uid := c.GetCurrentUid()
	llmResponse, llmErr := llm.Container.GetJsonResponseByAIAssistantModel(c, uid, llmRequest)

	if llmErr != nil {
		log.Errorf(c, "[large_language_models.AssistantChatHandler] failed to get llm response for user \"uid:%d\", because %s", uid, llmErr.Error())
		return nil, errs.Or(llmErr, errs.ErrOperationFailed)
	}

	if llmResponse == nil || strings.TrimSpace(llmResponse.Content) == "" {
		return nil, errs.ErrOperationFailed
	}

	reply := strings.TrimSpace(llmResponse.Content)
	llmResult := &models.AIAssistantResult{}

	if unmarshalErr := json.Unmarshal([]byte(llmResponse.Content), llmResult); unmarshalErr == nil &&
		llmResult != nil &&
		strings.TrimSpace(llmResult.Reply) != "" {
		reply = strings.TrimSpace(llmResult.Reply)
	}

	return &models.AIAssistantChatResponse{
		Mode:       assistantContext.Mode,
		Reply:      reply,
		References: assistantContext.References,
	}, nil
}

// AssistantChatStreamHandler returns ai assistant response stream for chat or summary request
func (a *LargeLanguageModelsApi) AssistantChatStreamHandler(c *core.WebContext) *errs.Error {
	currentConfig := a.CurrentConfig()
	assistantContext, err := a.prepareAIAssistantPromptContext(c, currentConfig)

	if err != nil {
		return err
	}

	if assistantContext.NoDataReply != "" {
		utils.WriteEventStreamJsonSuccessResult(c, &models.AIAssistantChatStreamChunk{
			Type:  models.AIAssistantChatStreamChunkTypeReplyDelta,
			Delta: assistantContext.NoDataReply,
		})

		utils.WriteEventStreamJsonSuccessResult(c, &models.AIAssistantChatStreamChunk{
			Type:  models.AIAssistantChatStreamChunkTypeDone,
			Mode:  assistantContext.Mode,
			Reply: assistantContext.NoDataReply,
		})
		return nil
	}

	uid := c.GetCurrentUid()
	streamResponse, streamErr := llm.Container.StreamTextResponseByAIAssistantModel(c, uid, &data.LargeLanguageModelRequest{
		SystemPrompt: assistantContext.SystemPrompt,
		UserPrompt:   []byte(assistantContext.UserPrompt),
	}, func(deltaType data.LargeLanguageModelStreamDeltaType, delta string) {
		chunkType := ""
		if deltaType == data.LARGE_LANGUAGE_MODEL_STREAM_DELTA_TYPE_REPLY {
			chunkType = models.AIAssistantChatStreamChunkTypeReplyDelta
		} else if deltaType == data.LARGE_LANGUAGE_MODEL_STREAM_DELTA_TYPE_THINKING {
			chunkType = models.AIAssistantChatStreamChunkTypeThinkingDelta
		}
		if chunkType != "" {
			utils.WriteEventStreamJsonSuccessResult(c, &models.AIAssistantChatStreamChunk{
				Type:  chunkType,
				Delta: delta,
			})
		}
	})
	if streamErr != nil {
		log.Errorf(c, "[large_language_models.AssistantChatStreamHandler] failed to stream llm response for user \"uid:%d\", because %s", uid, streamErr.Error())
		return errs.Or(streamErr, errs.ErrOperationFailed)
	}
	if streamResponse == nil {
		return errs.ErrOperationFailed
	}

	if len(assistantContext.References) > 0 {
		utils.WriteEventStreamJsonSuccessResult(c, &models.AIAssistantChatStreamChunk{
			Type:       models.AIAssistantChatStreamChunkTypeReferences,
			References: assistantContext.References,
		})
	}

	utils.WriteEventStreamJsonSuccessResult(c, &models.AIAssistantChatStreamChunk{
		Type:     models.AIAssistantChatStreamChunkTypeDone,
		Mode:     assistantContext.Mode,
		Reply:    streamResponse.Content,
		Thinking: streamResponse.Thinking,
	})
	return nil
}

func (a *LargeLanguageModelsApi) prepareAIAssistantPromptContext(c *core.WebContext, currentConfig *settings.Config) (*aiAssistantPreparedPromptContext, *errs.Error) {
	if !currentConfig.EnableAIAssistant ||
		currentConfig.AIAssistantLLMConfig == nil ||
		currentConfig.AIAssistantLLMConfig.LLMProvider == "" {
		return nil, errs.ErrAIAssistantNotEnabled
	}

	if currentConfig.AIAssistantLLMConfig.LLMProvider != settings.OpenAILLMProvider {
		return nil, errs.ErrAIAssistantOnlySupportsOpenAI
	}

	var request models.AIAssistantChatRequest
	err := c.ShouldBindJSON(&request)

	if err != nil {
		log.Warnf(c, "[large_language_models.prepareAIAssistantPromptContext] parse request failed, because %s", err.Error())
		return nil, errs.NewIncompleteOrIncorrectSubmissionError(err)
	}

	mode, normalizeErr := normalizeAIAssistantMode(request.Mode)

	if normalizeErr != nil {
		return nil, errs.ErrAIAssistantInvalidMode
	}

	request.Message = strings.TrimSpace(request.Message)

	if mode == models.AIAssistantModeChat && request.Message == "" {
		return nil, errs.ErrAIAssistantMessageIsEmpty
	}

	clientTimezone, timezoneErr := c.GetClientTimezone()

	if timezoneErr != nil {
		log.Warnf(c, "[large_language_models.prepareAIAssistantPromptContext] cannot get client timezone, because %s", timezoneErr.Error())
		return nil, errs.ErrClientTimezoneOffsetInvalid
	}

	uid := c.GetCurrentUid()
	_, userErr := a.users.GetUserById(c, uid)

	if userErr != nil {
		if !errs.IsCustomError(userErr) {
			log.Warnf(c, "[large_language_models.prepareAIAssistantPromptContext] failed to get user for user \"uid:%d\", because %s", uid, userErr.Error())
		}

		return nil, errs.ErrUserNotFound
	}

	transactions, transactionErr := a.getAIAssistantTransactionsForKnowledge(c, uid, clientTimezone)

	if transactionErr != nil {
		log.Errorf(c, "[large_language_models.prepareAIAssistantPromptContext] failed to get transactions for user \"uid:%d\", because %s", uid, transactionErr.Error())
		return nil, errs.Or(transactionErr, errs.ErrOperationFailed)
	}

	if len(transactions) < 1 {
		return &aiAssistantPreparedPromptContext{
			Mode:        mode,
			NoDataReply: a.getAIAssistantNoDataReply(c, mode),
		}, nil
	}

	accounts, accountErr := a.accounts.GetAllAccountsByUid(c, uid)

	if accountErr != nil {
		log.Errorf(c, "[large_language_models.prepareAIAssistantPromptContext] failed to get all accounts for user \"uid:%d\", because %s", uid, accountErr.Error())
		return nil, errs.Or(accountErr, errs.ErrOperationFailed)
	}

	accountMap := a.accounts.GetAccountMapByList(accounts)
	transactionIds := make([]int64, len(transactions))
	categoryIds := make([]int64, len(transactions))

	for i := 0; i < len(transactions); i++ {
		transaction := transactions[i]
		transactionId := transaction.TransactionId

		if transaction.Type == models.TRANSACTION_DB_TYPE_TRANSFER_IN {
			transactionId = transaction.RelatedId
		}

		transactionIds[i] = transactionId
		categoryIds[i] = transaction.CategoryId
	}

	categories, categoryErr := a.transactionCategories.GetCategoriesByCategoryIds(c, uid, utils.ToUniqueInt64Slice(categoryIds))

	if categoryErr != nil {
		log.Errorf(c, "[large_language_models.prepareAIAssistantPromptContext] failed to get categories for user \"uid:%d\", because %s", uid, categoryErr.Error())
		return nil, errs.Or(categoryErr, errs.ErrOperationFailed)
	}

	allTransactionTagIds, tagErr := a.transactionTags.GetAllTagIdsOfTransactions(c, uid, utils.ToUniqueInt64Slice(transactionIds))

	if tagErr != nil {
		log.Errorf(c, "[large_language_models.prepareAIAssistantPromptContext] failed to get transaction tags for user \"uid:%d\", because %s", uid, tagErr.Error())
		return nil, errs.Or(tagErr, errs.ErrOperationFailed)
	}

	tagMap := make(map[int64]*models.TransactionTag)
	allTagIds := utils.ToUniqueInt64Slice(a.transactionTags.GetTransactionTagIds(allTransactionTagIds))

	if len(allTagIds) > 0 {
		tagMap, tagErr = a.transactionTags.GetTagsByTagIds(c, uid, allTagIds)

		if tagErr != nil {
			log.Errorf(c, "[large_language_models.prepareAIAssistantPromptContext] failed to get tags for user \"uid:%d\", because %s", uid, tagErr.Error())
			return nil, errs.Or(tagErr, errs.ErrOperationFailed)
		}
	}

	knowledgeItems := a.buildAIAssistantKnowledgeItems(transactions, accountMap, categories, allTransactionTagIds, tagMap, clientTimezone)

	if len(knowledgeItems) < 1 {
		return &aiAssistantPreparedPromptContext{
			Mode:        mode,
			NoDataReply: a.getAIAssistantNoDataReply(c, mode),
		}, nil
	}

	embeddingQuery := a.buildAIAssistantEmbeddingQueryText(&request, mode, c.GetClientLocale())
	queryEmbedding, embeddingErr := a.getAIAssistantKnowledgeAndQueryEmbeddings(c, uid, currentConfig.AIAssistantLLMConfig, embeddingQuery, knowledgeItems)

	if embeddingErr != nil {
		log.Errorf(c, "[large_language_models.prepareAIAssistantPromptContext] failed to prepare embeddings for user \"uid:%d\", because %s", uid, embeddingErr.Error())
		return nil, errs.Or(embeddingErr, errs.ErrOperationFailed)
	}

	retrievedKnowledgeItems := selectTopAIAssistantKnowledgeItems(queryEmbedding, knowledgeItems, aiAssistantKnowledgeBaseTopK)
	retrievedKnowledgeText := buildRetrievedKnowledgePromptContent(retrievedKnowledgeItems)
	financialSnapshot := buildAIAssistantFinancialSnapshot(knowledgeItems, clientTimezone)
	systemPromptTemplate, templateErr := templates.GetTemplate(templates.SYSTEM_PROMPT_PERSONAL_FINANCE_ASSISTANT)

	if templateErr != nil {
		log.Errorf(c, "[large_language_models.prepareAIAssistantPromptContext] failed to get system prompt template for user \"uid:%d\", because %s", uid, templateErr.Error())
		return nil, errs.Or(templateErr, errs.ErrOperationFailed)
	}

	systemPromptParams := map[string]any{
		"CurrentDateTime":        utils.FormatUnixTimeToLongDateTime(time.Now().Unix(), clientTimezone),
		"ConversationMode":       mode,
		"PreferredReplyLanguage": getAIAssistantPreferredReplyLanguage(c.GetClientLocale()),
		"FinancialSnapshot":      financialSnapshot,
		"RetrievedKnowledge":     retrievedKnowledgeText,
	}

	var promptBuffer bytes.Buffer
	renderErr := systemPromptTemplate.Execute(&promptBuffer, systemPromptParams)

	if renderErr != nil {
		log.Errorf(c, "[large_language_models.prepareAIAssistantPromptContext] failed to generate system prompt for user \"uid:%d\", because %s", uid, renderErr.Error())
		return nil, errs.Or(renderErr, errs.ErrOperationFailed)
	}

	return &aiAssistantPreparedPromptContext{
		Mode:         mode,
		SystemPrompt: strings.ReplaceAll(promptBuffer.String(), "\r\n", "\n"),
		UserPrompt:   a.buildAIAssistantUserPrompt(&request, mode, c.GetClientLocale()),
		References:   buildAIAssistantResponseReferences(retrievedKnowledgeItems, aiAssistantMaxReferencedTransactionsCount),
	}, nil
}

func (a *LargeLanguageModelsApi) getAIAssistantTransactionsForKnowledge(c *core.WebContext, uid int64, clientTimezone *time.Location) ([]*models.Transaction, error) {
	maxTransactionTime := utils.GetMaxTransactionTimeFromUnixTime(time.Now().Unix())
	coverageStartUnixTime := getAIAssistantKnowledgeCoverageStartUnixTime(clientTimezone)
	page := int32(1)
	transactions := make([]*models.Transaction, 0, aiAssistantKnowledgeBaseTransactionPageSize)

	for len(transactions) < aiAssistantKnowledgeBaseMaxTransactionCount {
		remainingCount := aiAssistantKnowledgeBaseMaxTransactionCount - len(transactions)
		pageCount := aiAssistantKnowledgeBaseTransactionPageSize

		if remainingCount < int(pageCount) {
			pageCount = int32(remainingCount)
		}

		pageTransactions, err := a.transactions.GetTransactionsByMaxTime(c, uid, maxTransactionTime, 0, 0, nil, nil, nil, false, "", "", page, pageCount, false, true)

		if err != nil {
			return nil, err
		}

		if len(pageTransactions) < 1 {
			break
		}

		transactions = append(transactions, pageTransactions...)

		oldestTransactionUnixTime := utils.GetUnixTimeFromTransactionTime(pageTransactions[len(pageTransactions)-1].TransactionTime)

		if oldestTransactionUnixTime <= coverageStartUnixTime {
			break
		}

		if len(pageTransactions) < int(pageCount) {
			break
		}

		page++
	}

	return transactions, nil
}

func getAIAssistantKnowledgeCoverageStartUnixTime(clientTimezone *time.Location) int64 {
	if clientTimezone == nil {
		clientTimezone = time.Local
	}

	nowInClientTimezone := time.Now().In(clientTimezone)
	coverageStartYear := nowInClientTimezone.Year() - aiAssistantKnowledgeBaseHistoryCoverageYears
	coverageStartTime := time.Date(coverageStartYear, time.January, 1, 0, 0, 0, 0, clientTimezone)

	return coverageStartTime.Unix()
}

func normalizeAIAssistantMode(mode string) (string, error) {
	if mode == "" {
		return models.AIAssistantModeChat, nil
	}

	if mode == models.AIAssistantModeChat || mode == models.AIAssistantModeSummary {
		return mode, nil
	}

	return "", errs.ErrAIAssistantInvalidMode
}

func (a *LargeLanguageModelsApi) buildAIAssistantKnowledgeItems(transactions []*models.Transaction, accountMap map[int64]*models.Account, categoryMap map[int64]*models.TransactionCategory, allTransactionTagIds map[int64][]int64, tagMap map[int64]*models.TransactionTag, clientTimezone *time.Location) []*aiAssistantKnowledgeItem {
	knowledgeItems := make([]*aiAssistantKnowledgeItem, 0, len(transactions))

	for i := 0; i < len(transactions); i++ {
		transaction := transactions[i]

		if transaction.Type == models.TRANSACTION_DB_TYPE_TRANSFER_IN {
			transaction = a.transactions.GetRelatedTransferTransaction(transaction)
		}

		transactionType, err := transaction.Type.ToTransactionType()

		if err != nil {
			continue
		}

		transactionUnixTime := utils.GetUnixTimeFromTransactionTime(transaction.TransactionTime)
		transactionTimeText := utils.FormatUnixTimeToLongDateTime(transactionUnixTime, clientTimezone)
		sourceAccount := accountMap[transaction.AccountId]
		destinationAccount := accountMap[transaction.RelatedAccountId]
		categoryName := ""

		if category := categoryMap[transaction.CategoryId]; category != nil {
			categoryName = category.Name
		}

		transactionTagIds := allTransactionTagIds[transaction.TransactionId]
		tagNames := make([]string, 0, len(transactionTagIds))

		for j := 0; j < len(transactionTagIds); j++ {
			tag := tagMap[transactionTagIds[j]]

			if tag == nil {
				continue
			}

			tagNames = append(tagNames, tag.Name)
		}

		sort.Strings(tagNames)

		sourceCurrency := ""
		sourceAccountName := ""

		if sourceAccount != nil {
			sourceCurrency = sourceAccount.Currency
			sourceAccountName = sourceAccount.Name
		}

		destinationCurrency := ""
		destinationAccountName := ""
		destinationAmount := int64(0)

		if transaction.Type == models.TRANSACTION_DB_TYPE_TRANSFER_OUT {
			destinationAmount = transaction.RelatedAccountAmount
		}

		if destinationAccount != nil {
			destinationCurrency = destinationAccount.Currency
			destinationAccountName = destinationAccount.Name
		}

		reference := &models.AIAssistantReferencedTransaction{
			Id:                     transaction.TransactionId,
			Time:                   transactionUnixTime,
			TimeText:               transactionTimeText,
			Type:                   transactionType,
			CategoryName:           categoryName,
			SourceAccountName:      sourceAccountName,
			DestinationAccountName: destinationAccountName,
			SourceAmount:           transaction.Amount,
			DestinationAmount:      destinationAmount,
			Currency:               sourceCurrency,
			DestinationCurrency:    destinationCurrency,
			Comment:                transaction.Comment,
		}

		var itemTextBuilder strings.Builder
		itemTextBuilder.WriteString("transaction_id: ")
		itemTextBuilder.WriteString(utils.Int64ToString(reference.Id))
		itemTextBuilder.WriteString("\ntime: ")
		itemTextBuilder.WriteString(reference.TimeText)
		itemTextBuilder.WriteString("\ntype: ")
		itemTextBuilder.WriteString(getAIAssistantTransactionTypeText(reference.Type))
		itemTextBuilder.WriteString("\nsource_account: ")
		itemTextBuilder.WriteString(reference.SourceAccountName)
		itemTextBuilder.WriteString("\nsource_amount: ")
		itemTextBuilder.WriteString(utils.FormatAmount(reference.SourceAmount))
		itemTextBuilder.WriteString("\nsource_currency: ")
		itemTextBuilder.WriteString(reference.Currency)
		itemTextBuilder.WriteString("\ncategory: ")
		itemTextBuilder.WriteString(reference.CategoryName)

		if reference.Type == models.TRANSACTION_TYPE_TRANSFER {
			itemTextBuilder.WriteString("\ndestination_account: ")
			itemTextBuilder.WriteString(reference.DestinationAccountName)
			itemTextBuilder.WriteString("\ndestination_amount: ")
			itemTextBuilder.WriteString(utils.FormatAmount(reference.DestinationAmount))
			itemTextBuilder.WriteString("\ndestination_currency: ")
			itemTextBuilder.WriteString(reference.DestinationCurrency)
		}

		if len(tagNames) > 0 {
			itemTextBuilder.WriteString("\ntags: ")
			itemTextBuilder.WriteString(strings.Join(tagNames, ", "))
		}

		if reference.Comment != "" {
			itemTextBuilder.WriteString("\ncomment: ")
			itemTextBuilder.WriteString(reference.Comment)
		}

		itemText := itemTextBuilder.String()
		knowledgeItems = append(knowledgeItems, &aiAssistantKnowledgeItem{
			Reference: reference,
			Text:      itemText,
			TextHash:  calculateAIAssistantTextHash(itemText),
		})
	}

	return knowledgeItems
}

func (a *LargeLanguageModelsApi) buildAIAssistantEmbeddingQueryText(request *models.AIAssistantChatRequest, mode string, clientLocale string) string {
	normalizedLocale := normalizeAIAssistantClientLocale(clientLocale)

	if mode == models.AIAssistantModeSummary {
		if request.Message != "" {
			if strings.HasPrefix(normalizedLocale, "zh") {
				return "个人财务总结与记账建议，重点关注：" + request.Message
			}

			return "personal finance summary and bookkeeping suggestions focus: " + request.Message
		}

		if strings.HasPrefix(normalizedLocale, "zh") {
			return "总结近期个人财务趋势、支出结构、潜在风险和可执行的记账建议"
		}

		return "summarize recent personal finance trends, spending, risks, and bookkeeping suggestions"
	}

	queryTextBuilder := &strings.Builder{}
	queryTextBuilder.WriteString(request.Message)
	historyCount := len(request.History)

	if historyCount > aiAssistantMaxHistoryMessages {
		historyCount = aiAssistantMaxHistoryMessages
	}

	for i := len(request.History) - historyCount; i < len(request.History); i++ {
		if i < 0 || request.History[i] == nil || request.History[i].Role != "user" {
			continue
		}

		content := strings.TrimSpace(request.History[i].Content)

		if content == "" {
			continue
		}

		queryTextBuilder.WriteString("\n")
		queryTextBuilder.WriteString(content)
	}

	return strings.TrimSpace(queryTextBuilder.String())
}

func (a *LargeLanguageModelsApi) buildAIAssistantUserPrompt(request *models.AIAssistantChatRequest, mode string, clientLocale string) string {
	promptBuilder := &strings.Builder{}
	normalizedLocale := normalizeAIAssistantClientLocale(clientLocale)

	if mode == models.AIAssistantModeSummary {
		if strings.HasPrefix(normalizedLocale, "zh") {
			promptBuilder.WriteString("请基于我的账单数据，用中文给我一份个人财务总结，并提供可执行的记账建议。")
		} else {
			promptBuilder.WriteString("Please provide a personal finance summary and practical bookkeeping suggestions based on my bill data.")
		}

		if request.Message != "" {
			if strings.HasPrefix(normalizedLocale, "zh") {
				promptBuilder.WriteString("\n额外关注点：")
				promptBuilder.WriteString(request.Message)
			} else {
				promptBuilder.WriteString("\nAdditional focus: ")
				promptBuilder.WriteString(request.Message)
			}
		}
	} else {
		promptBuilder.WriteString("Latest user message:\n")
		promptBuilder.WriteString(request.Message)
	}

	historyItems := make([]string, 0, len(request.History))
	historyCount := len(request.History)

	if historyCount > aiAssistantMaxHistoryMessages {
		historyCount = aiAssistantMaxHistoryMessages
	}

	for i := len(request.History) - historyCount; i < len(request.History); i++ {
		if i < 0 || request.History[i] == nil {
			continue
		}

		role := strings.TrimSpace(request.History[i].Role)
		content := strings.TrimSpace(request.History[i].Content)

		if role == "" || content == "" {
			continue
		}

		historyItems = append(historyItems, strings.ToUpper(role)+": "+content)
	}

	if len(historyItems) > 0 {
		promptBuilder.WriteString("\n\nConversation history:\n")
		promptBuilder.WriteString(strings.Join(historyItems, "\n"))
	}

	return promptBuilder.String()
}

func normalizeAIAssistantClientLocale(clientLocale string) string {
	normalized := strings.ToLower(strings.TrimSpace(clientLocale))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	return normalized
}

func getAIAssistantPreferredReplyLanguage(clientLocale string) string {
	normalized := normalizeAIAssistantClientLocale(clientLocale)

	switch {
	case strings.HasPrefix(normalized, "zh-hant"), strings.HasPrefix(normalized, "zh-tw"), strings.HasPrefix(normalized, "zh-hk"), strings.HasPrefix(normalized, "zh-mo"):
		return "Traditional Chinese"
	case strings.HasPrefix(normalized, "zh"):
		return "Simplified Chinese"
	case strings.HasPrefix(normalized, "ja"):
		return "Japanese"
	case strings.HasPrefix(normalized, "ko"):
		return "Korean"
	case strings.HasPrefix(normalized, "fr"):
		return "French"
	case strings.HasPrefix(normalized, "de"):
		return "German"
	case strings.HasPrefix(normalized, "es"):
		return "Spanish"
	case strings.HasPrefix(normalized, "it"):
		return "Italian"
	case strings.HasPrefix(normalized, "ru"):
		return "Russian"
	case strings.HasPrefix(normalized, "uk"):
		return "Ukrainian"
	case strings.HasPrefix(normalized, "vi"):
		return "Vietnamese"
	case strings.HasPrefix(normalized, "th"):
		return "Thai"
	case strings.HasPrefix(normalized, "pt-br"):
		return "Brazilian Portuguese"
	case strings.HasPrefix(normalized, "pt"):
		return "Portuguese"
	case strings.HasPrefix(normalized, "tr"):
		return "Turkish"
	case strings.HasPrefix(normalized, "nl"):
		return "Dutch"
	case strings.HasPrefix(normalized, "sl"):
		return "Slovenian"
	case strings.HasPrefix(normalized, "ta"):
		return "Tamil"
	case strings.HasPrefix(normalized, "kn"):
		return "Kannada"
	default:
		return "English"
	}
}

func (a *LargeLanguageModelsApi) getAIAssistantNoDataReply(c *core.WebContext, mode string) string {
	clientLocale := strings.ToLower(c.GetClientLocale())

	if strings.Contains(clientLocale, "zh") {
		if mode == models.AIAssistantModeSummary {
			return "当前没有可用于总结的账单数据。请先记几笔账，再让我为你生成财务总结和建议。"
		}

		return "我暂时还没有你的账单数据。请先记录一些收入、支出或转账，我再为你分析。"
	}

	if mode == models.AIAssistantModeSummary {
		return "There is no bill data available for summary yet. Please add transactions first."
	}

	return "I do not have enough bill data yet. Please add some transactions first."
}

func (a *LargeLanguageModelsApi) getOpenAIEmbeddings(c core.Context, uid int64, llmConfig *settings.LLMConfig, inputs []string) ([][]float64, error) {
	openAIAPIKey := strings.TrimSpace(llmConfig.OpenAIAPIKey)
	openAIEmbeddingModelID := strings.TrimSpace(llmConfig.OpenAIEmbeddingModelID)

	if openAIAPIKey == "" {
		return nil, errs.ErrFailedToRequestRemoteApi
	}

	if openAIEmbeddingModelID == "" {
		return nil, errs.ErrAIAssistantEmbeddingModelInvalid
	}

	httpClient := httpclient.NewHttpClient(llmConfig.LargeLanguageModelAPIRequestTimeout, llmConfig.LargeLanguageModelAPIProxy, llmConfig.LargeLanguageModelAPISkipTLSVerify, core.GetOutgoingUserAgent(), false)
	embeddings := make([][]float64, 0, len(inputs))

	for start := 0; start < len(inputs); start += aiAssistantEmbeddingRequestBatchSize {
		end := start + aiAssistantEmbeddingRequestBatchSize

		if end > len(inputs) {
			end = len(inputs)
		}

		requestBody := &openAIEmbeddingsRequest{
			Model: openAIEmbeddingModelID,
			Input: inputs[start:end],
		}

		requestBodyBytes, err := json.Marshal(requestBody)

		if err != nil {
			return nil, errs.ErrOperationFailed
		}

		httpRequest, err := http.NewRequest("POST", llmConfig.GetOpenAIEndpointURL(aiAssistantOpenAIEmbeddingsPath), bytes.NewReader(requestBodyBytes))

		if err != nil {
			return nil, errs.ErrFailedToRequestRemoteApi
		}

		httpRequest.Header.Set("Authorization", "Bearer "+openAIAPIKey)
		httpRequest.Header.Set("Content-Type", "application/json")
		httpRequest = httpRequest.WithContext(httpclient.CustomHttpResponseLog(c, func(data []byte) {
			log.Debugf(c, "[large_language_models.getOpenAIEmbeddings] response is %s", data)
		}))

		response, err := httpClient.Do(httpRequest)

		if err != nil {
			return nil, errs.ErrFailedToRequestRemoteApi
		}

		responseBody, err := io.ReadAll(response.Body)
		response.Body.Close()

		if err != nil {
			return nil, errs.ErrFailedToRequestRemoteApi
		}

		if response.StatusCode != http.StatusOK {
			log.Errorf(c, "[large_language_models.getOpenAIEmbeddings] failed to request embeddings for user \"uid:%d\", status code is %d", uid, response.StatusCode)
			return nil, errs.ErrFailedToRequestRemoteApi
		}

		embeddingsResponse := &openAIEmbeddingsResponse{}
		err = json.Unmarshal(responseBody, embeddingsResponse)

		if err != nil {
			log.Errorf(c, "[large_language_models.getOpenAIEmbeddings] failed to parse embeddings response for user \"uid:%d\", because %s", uid, err.Error())
			return nil, errs.ErrFailedToRequestRemoteApi
		}

		if embeddingsResponse == nil || len(embeddingsResponse.Data) != len(requestBody.Input) {
			log.Errorf(c, "[large_language_models.getOpenAIEmbeddings] embeddings response count is invalid for user \"uid:%d\"", uid)
			return nil, errs.ErrFailedToRequestRemoteApi
		}

		sort.Slice(embeddingsResponse.Data, func(i, j int) bool {
			return embeddingsResponse.Data[i].Index < embeddingsResponse.Data[j].Index
		})

		for i := 0; i < len(embeddingsResponse.Data); i++ {
			item := embeddingsResponse.Data[i]

			if item == nil || len(item.Embedding) < 1 {
				log.Errorf(c, "[large_language_models.getOpenAIEmbeddings] one embedding item is invalid for user \"uid:%d\"", uid)
				return nil, errs.ErrFailedToRequestRemoteApi
			}

			embeddings = append(embeddings, item.Embedding)
		}
	}

	return embeddings, nil
}

func (a *LargeLanguageModelsApi) getAIAssistantKnowledgeAndQueryEmbeddings(c core.Context, uid int64, llmConfig *settings.LLMConfig, queryText string, knowledgeItems []*aiAssistantKnowledgeItem) ([]float64, error) {
	if llmConfig == nil {
		return nil, errs.ErrOperationFailed
	}

	openAIEmbeddingModelID := strings.TrimSpace(llmConfig.OpenAIEmbeddingModelID)

	if openAIEmbeddingModelID == "" {
		return nil, errs.ErrAIAssistantEmbeddingModelInvalid
	}

	transactionIds := make([]int64, 0, len(knowledgeItems))

	for i := 0; i < len(knowledgeItems); i++ {
		item := knowledgeItems[i]

		if item == nil || item.Reference == nil || item.Reference.Id <= 0 {
			continue
		}

		transactionIds = append(transactionIds, item.Reference.Id)
	}

	transactionIds = utils.ToUniqueInt64Slice(transactionIds)

	if a.embeddings != nil {
		err := a.embeddings.DeleteEmbeddingsNotInTransactionIds(c, uid, openAIEmbeddingModelID, transactionIds)

		if err != nil {
			return nil, err
		}
	}

	cachedEmbeddingMap := map[int64]*models.AIAssistantEmbedding{}
	var err error

	if a.embeddings != nil {
		cachedEmbeddingMap, err = a.embeddings.GetEmbeddingsByTransactionIds(c, uid, openAIEmbeddingModelID, transactionIds)

		if err != nil {
			return nil, err
		}
	}

	missingKnowledgeItems := make([]*aiAssistantKnowledgeItem, 0, len(knowledgeItems))

	for i := 0; i < len(knowledgeItems); i++ {
		item := knowledgeItems[i]

		if item == nil || item.Reference == nil || item.Reference.Id <= 0 {
			continue
		}

		cachedEmbedding := cachedEmbeddingMap[item.Reference.Id]

		if cachedEmbedding != nil && cachedEmbedding.ContentHash == item.TextHash {
			cachedEmbeddingVector, parseErr := unmarshalAIAssistantEmbeddingVector(cachedEmbedding.VectorData)

			if parseErr == nil && len(cachedEmbeddingVector) > 0 {
				item.Embedding = cachedEmbeddingVector
				continue
			}
		}

		missingKnowledgeItems = append(missingKnowledgeItems, item)
	}

	embeddingInputs := make([]string, 0, 1+len(missingKnowledgeItems))
	embeddingInputs = append(embeddingInputs, queryText)

	for i := 0; i < len(missingKnowledgeItems); i++ {
		embeddingInputs = append(embeddingInputs, missingKnowledgeItems[i].Text)
	}

	allEmbeddings, err := a.getOpenAIEmbeddings(c, uid, llmConfig, embeddingInputs)

	if err != nil {
		return nil, err
	}

	if len(allEmbeddings) != len(embeddingInputs) {
		return nil, errs.ErrOperationFailed
	}

	queryEmbedding := allEmbeddings[0]

	if len(missingKnowledgeItems) < 1 {
		return queryEmbedding, nil
	}

	embeddingCacheItems := make([]*models.AIAssistantEmbedding, 0, len(missingKnowledgeItems))

	for i := 0; i < len(missingKnowledgeItems); i++ {
		item := missingKnowledgeItems[i]

		if item == nil || item.Reference == nil || item.Reference.Id <= 0 {
			continue
		}

		item.Embedding = allEmbeddings[i+1]
		vectorData, marshalErr := marshalAIAssistantEmbeddingVector(item.Embedding)

		if marshalErr != nil {
			return nil, errs.ErrOperationFailed
		}

		embeddingCacheItems = append(embeddingCacheItems, &models.AIAssistantEmbedding{
			Uid:            uid,
			TransactionId:  item.Reference.Id,
			EmbeddingModel: openAIEmbeddingModelID,
			ContentHash:    item.TextHash,
			VectorData:     vectorData,
		})
	}

	if a.embeddings != nil {
		err = a.embeddings.SaveEmbeddings(c, embeddingCacheItems)

		if err != nil {
			return nil, err
		}
	}

	return queryEmbedding, nil
}

func calculateAIAssistantTextHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func marshalAIAssistantEmbeddingVector(vector []float64) (string, error) {
	if len(vector) < 1 {
		return "", errs.ErrOperationFailed
	}

	vectorData, err := json.Marshal(vector)

	if err != nil {
		return "", err
	}

	return string(vectorData), nil
}

func unmarshalAIAssistantEmbeddingVector(vectorData string) ([]float64, error) {
	if strings.TrimSpace(vectorData) == "" {
		return nil, errs.ErrOperationFailed
	}

	vector := make([]float64, 0, 64)
	err := json.Unmarshal([]byte(vectorData), &vector)

	if err != nil {
		return nil, err
	}

	if len(vector) < 1 {
		return nil, errs.ErrOperationFailed
	}

	return vector, nil
}

func selectTopAIAssistantKnowledgeItems(queryEmbedding []float64, knowledgeItems []*aiAssistantKnowledgeItem, topK int) []*aiAssistantRetrievedKnowledgeItem {
	if topK < 1 {
		return nil
	}

	rankedItems := make([]*aiAssistantRetrievedKnowledgeItem, 0, len(knowledgeItems))

	for i := 0; i < len(knowledgeItems); i++ {
		item := knowledgeItems[i]

		if item == nil {
			continue
		}

		score := calculateCosineSimilarity(queryEmbedding, item.Embedding)
		rankedItems = append(rankedItems, &aiAssistantRetrievedKnowledgeItem{
			Item:  item,
			Score: score,
		})
	}

	sort.Slice(rankedItems, func(i, j int) bool {
		return rankedItems[i].Score > rankedItems[j].Score
	})

	if len(rankedItems) > topK {
		rankedItems = rankedItems[:topK]
	}

	return rankedItems
}

func calculateCosineSimilarity(vectorA []float64, vectorB []float64) float64 {
	if len(vectorA) < 1 || len(vectorA) != len(vectorB) {
		return 0
	}

	dotProduct := 0.0
	normA := 0.0
	normB := 0.0

	for i := 0; i < len(vectorA); i++ {
		dotProduct += vectorA[i] * vectorB[i]
		normA += vectorA[i] * vectorA[i]
		normB += vectorB[i] * vectorB[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

func buildRetrievedKnowledgePromptContent(retrievedItems []*aiAssistantRetrievedKnowledgeItem) string {
	if len(retrievedItems) < 1 {
		return "No matched transactions."
	}

	contentBuilder := &strings.Builder{}

	for i := 0; i < len(retrievedItems); i++ {
		item := retrievedItems[i]
		reference := item.Item.Reference
		contentBuilder.WriteString("[")
		contentBuilder.WriteString(utils.IntToString(i + 1))
		contentBuilder.WriteString("] similarity=")
		contentBuilder.WriteString(utils.Float64ToString(math.Round(item.Score*10000) / 10000))
		contentBuilder.WriteString("\n")
		contentBuilder.WriteString(item.Item.Text)

		if i < len(retrievedItems)-1 {
			contentBuilder.WriteString("\n\n")
		} else if reference != nil && reference.Comment != "" {
			contentBuilder.WriteString("\n")
		}
	}

	return contentBuilder.String()
}

func buildAIAssistantFinancialSnapshot(knowledgeItems []*aiAssistantKnowledgeItem, clientTimezone *time.Location) string {
	if len(knowledgeItems) < 1 {
		return "No available bill data."
	}

	currentYearMonth := utils.FormatUnixTimeToNumericYearMonth(time.Now().Unix(), clientTimezone)
	overallByCurrency := make(map[string]*aiAssistantCurrencyOverview)
	thisMonthByCurrency := make(map[string]*aiAssistantCurrencyOverview)
	expenseCategoryMap := make(map[string]*aiAssistantExpenseCategoryOverview)

	var oldestTime int64
	var latestTime int64

	for i := 0; i < len(knowledgeItems); i++ {
		reference := knowledgeItems[i].Reference

		if reference == nil {
			continue
		}

		if oldestTime == 0 || reference.Time < oldestTime {
			oldestTime = reference.Time
		}

		if latestTime == 0 || reference.Time > latestTime {
			latestTime = reference.Time
		}

		currency := reference.Currency

		if currency == "" {
			currency = "UNKNOWN"
		}

		overall := overallByCurrency[currency]

		if overall == nil {
			overall = &aiAssistantCurrencyOverview{}
			overallByCurrency[currency] = overall
		}

		var currentMonth *aiAssistantCurrencyOverview

		if utils.FormatUnixTimeToNumericYearMonth(reference.Time, clientTimezone) == currentYearMonth {
			currentMonth = thisMonthByCurrency[currency]

			if currentMonth == nil {
				currentMonth = &aiAssistantCurrencyOverview{}
				thisMonthByCurrency[currency] = currentMonth
			}
		}

		if reference.Type == models.TRANSACTION_TYPE_INCOME {
			overall.Income += reference.SourceAmount

			if currentMonth != nil {
				currentMonth.Income += reference.SourceAmount
			}
		} else if reference.Type == models.TRANSACTION_TYPE_EXPENSE {
			overall.Expense += reference.SourceAmount

			if currentMonth != nil {
				currentMonth.Expense += reference.SourceAmount
			}

			categoryName := reference.CategoryName

			if categoryName == "" {
				categoryName = "Uncategorized"
			}

			expenseCategoryKey := currency + "|" + categoryName
			categoryOverview := expenseCategoryMap[expenseCategoryKey]

			if categoryOverview == nil {
				categoryOverview = &aiAssistantExpenseCategoryOverview{
					CategoryName: categoryName,
					Currency:     currency,
				}
				expenseCategoryMap[expenseCategoryKey] = categoryOverview
			}

			categoryOverview.Amount += reference.SourceAmount
		} else if reference.Type == models.TRANSACTION_TYPE_TRANSFER {
			overall.TransferOut += reference.SourceAmount

			if currentMonth != nil {
				currentMonth.TransferOut += reference.SourceAmount
			}

			destinationCurrency := reference.DestinationCurrency

			if destinationCurrency != "" {
				overallDestination := overallByCurrency[destinationCurrency]

				if overallDestination == nil {
					overallDestination = &aiAssistantCurrencyOverview{}
					overallByCurrency[destinationCurrency] = overallDestination
				}

				overallDestination.TransferIn += reference.DestinationAmount

				if utils.FormatUnixTimeToNumericYearMonth(reference.Time, clientTimezone) == currentYearMonth {
					currentMonthDestination := thisMonthByCurrency[destinationCurrency]

					if currentMonthDestination == nil {
						currentMonthDestination = &aiAssistantCurrencyOverview{}
						thisMonthByCurrency[destinationCurrency] = currentMonthDestination
					}

					currentMonthDestination.TransferIn += reference.DestinationAmount
				}
			}
		}
	}

	snapshotBuilder := &strings.Builder{}
	snapshotBuilder.WriteString("Transaction count: ")
	snapshotBuilder.WriteString(utils.IntToString(len(knowledgeItems)))
	snapshotBuilder.WriteString("\nDate range: ")
	snapshotBuilder.WriteString(utils.FormatUnixTimeToLongDateTime(oldestTime, clientTimezone))
	snapshotBuilder.WriteString(" ~ ")
	snapshotBuilder.WriteString(utils.FormatUnixTimeToLongDateTime(latestTime, clientTimezone))
	snapshotBuilder.WriteString("\nOverall cash flow by currency:")
	appendCurrencyOverviewLines(snapshotBuilder, overallByCurrency)
	snapshotBuilder.WriteString("\nThis month cash flow by currency:")
	appendCurrencyOverviewLines(snapshotBuilder, thisMonthByCurrency)
	snapshotBuilder.WriteString("\nTop expense categories:")
	appendTopExpenseCategories(snapshotBuilder, expenseCategoryMap, 5)

	return snapshotBuilder.String()
}

func appendCurrencyOverviewLines(snapshotBuilder *strings.Builder, overviewMap map[string]*aiAssistantCurrencyOverview) {
	if len(overviewMap) < 1 {
		snapshotBuilder.WriteString("\n- No data")
		return
	}

	currencies := make([]string, 0, len(overviewMap))

	for currency := range overviewMap {
		currencies = append(currencies, currency)
	}

	sort.Strings(currencies)

	for i := 0; i < len(currencies); i++ {
		currency := currencies[i]
		overview := overviewMap[currency]

		if overview == nil {
			continue
		}

		net := overview.Income - overview.Expense
		snapshotBuilder.WriteString("\n- ")
		snapshotBuilder.WriteString(currency)
		snapshotBuilder.WriteString(": income ")
		snapshotBuilder.WriteString(utils.FormatAmount(overview.Income))
		snapshotBuilder.WriteString(", expense ")
		snapshotBuilder.WriteString(utils.FormatAmount(overview.Expense))
		snapshotBuilder.WriteString(", net ")
		snapshotBuilder.WriteString(utils.FormatAmount(net))
		snapshotBuilder.WriteString(", transfer_out ")
		snapshotBuilder.WriteString(utils.FormatAmount(overview.TransferOut))
		snapshotBuilder.WriteString(", transfer_in ")
		snapshotBuilder.WriteString(utils.FormatAmount(overview.TransferIn))
	}
}

func appendTopExpenseCategories(snapshotBuilder *strings.Builder, expenseCategoryMap map[string]*aiAssistantExpenseCategoryOverview, limit int) {
	if len(expenseCategoryMap) < 1 {
		snapshotBuilder.WriteString("\n- No expense data")
		return
	}

	categoryOverviews := make([]*aiAssistantExpenseCategoryOverview, 0, len(expenseCategoryMap))

	for _, item := range expenseCategoryMap {
		categoryOverviews = append(categoryOverviews, item)
	}

	sort.Slice(categoryOverviews, func(i, j int) bool {
		return categoryOverviews[i].Amount > categoryOverviews[j].Amount
	})

	if len(categoryOverviews) > limit {
		categoryOverviews = categoryOverviews[:limit]
	}

	for i := 0; i < len(categoryOverviews); i++ {
		snapshotBuilder.WriteString("\n- ")
		snapshotBuilder.WriteString(categoryOverviews[i].CategoryName)
		snapshotBuilder.WriteString(" (")
		snapshotBuilder.WriteString(categoryOverviews[i].Currency)
		snapshotBuilder.WriteString("): ")
		snapshotBuilder.WriteString(utils.FormatAmount(categoryOverviews[i].Amount))
	}
}

func getAIAssistantTransactionTypeText(transactionType models.TransactionType) string {
	if transactionType == models.TRANSACTION_TYPE_EXPENSE {
		return "expense"
	}

	if transactionType == models.TRANSACTION_TYPE_INCOME {
		return "income"
	}

	if transactionType == models.TRANSACTION_TYPE_TRANSFER {
		return "transfer"
	}

	return "unknown"
}

func buildAIAssistantResponseReferences(retrievedKnowledgeItems []*aiAssistantRetrievedKnowledgeItem, maxCount int) []*models.AIAssistantReferencedTransaction {
	if len(retrievedKnowledgeItems) < 1 || maxCount < 1 {
		return nil
	}

	if len(retrievedKnowledgeItems) > maxCount {
		retrievedKnowledgeItems = retrievedKnowledgeItems[:maxCount]
	}

	references := make([]*models.AIAssistantReferencedTransaction, 0, len(retrievedKnowledgeItems))

	for i := 0; i < len(retrievedKnowledgeItems); i++ {
		if retrievedKnowledgeItems[i] == nil || retrievedKnowledgeItems[i].Item == nil || retrievedKnowledgeItems[i].Item.Reference == nil {
			continue
		}

		reference := *retrievedKnowledgeItems[i].Item.Reference
		reference.SimilarityScore = math.Round(retrievedKnowledgeItems[i].Score*10000) / 10000
		references = append(references, &reference)
	}

	return references
}
