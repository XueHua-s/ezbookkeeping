export interface RecognizedTransactionResponse {
    readonly type: number;
    readonly time?: number;
    readonly categoryId?: string;
    readonly sourceAccountId?: string;
    readonly destinationAccountId?: string;
    readonly sourceAmount?: number;
    readonly destinationAmount?: number;
    readonly tagIds?: string[];
    readonly comment?: string;
}

export type RecognizedReceiptImageResponse = RecognizedTransactionResponse;

export interface RecognizedReceiptImageResultItem {
    readonly index: number;
    readonly success: boolean;
    readonly result?: RecognizedReceiptImageResponse;
    readonly error?: string;
}

export interface RecognizedReceiptImageBatchResponse {
    readonly results: RecognizedReceiptImageResultItem[];
}

export type AIAssistantMode = 'chat' | 'summary';
export type AIAssistantMessageRole = 'user' | 'assistant';

export interface AIAssistantHistoryItem {
    readonly role: AIAssistantMessageRole;
    readonly content: string;
}

export interface AIAssistantChatRequest {
    readonly mode: AIAssistantMode;
    readonly message?: string;
    readonly history?: AIAssistantHistoryItem[];
}

export interface AIAssistantReferencedTransaction {
    readonly id: string;
    readonly time: number;
    readonly timeText?: string;
    readonly type: number;
    readonly categoryName?: string;
    readonly sourceAccountName?: string;
    readonly destinationAccountName?: string;
    readonly sourceAmount: number;
    readonly destinationAmount?: number;
    readonly currency?: string;
    readonly destinationCurrency?: string;
    readonly comment?: string;
    readonly similarityScore?: number;
}

export interface AIAssistantChatResponse {
    readonly mode: AIAssistantMode;
    readonly reply: string;
    readonly references?: AIAssistantReferencedTransaction[];
}

export type AIAssistantChatStreamChunkType = 'thinking_delta' | 'reply_delta' | 'references' | 'done';

export interface AIAssistantChatStreamChunk {
    readonly type: AIAssistantChatStreamChunkType;
    readonly mode?: AIAssistantMode;
    readonly delta?: string;
    readonly reply?: string;
    readonly thinking?: string;
    readonly references?: AIAssistantReferencedTransaction[];
}
