# AI Feature Map

## Configuration

- Global AI flags: `conf/ezbookkeeping.ini` section `[llm]`.
- Receipt recognition provider config: `[llm_image_recognition]`; optional LLM-only fallback: `[llm_image_recognition_fallback]`.
- Assistant provider and embedding config: `[llm_assistant]`; optional chat-only fallback: `[llm_assistant_fallback]` (embedding never falls back).
- Config parser and defaults: `pkg/settings/setting.go`.

## Backend Runtime

- LLM provider container: `pkg/llm/large_language_model_provider_container.go`.
- OpenAI provider adapter: `pkg/llm/provider/openai/*`.
- Receipt image recognition API: `pkg/api/large_language_models.go`.
- Assistant orchestration and retrieval: `pkg/api/large_language_models_assistant.go`.
- Assistant embedding cache service: `pkg/services/ai_assistant_embeddings.go`.
- Assistant embedding model/table: `pkg/models/ai_assistant_embedding.go`.

## Prompt Layer

- Personal finance assistant system prompt: `templates/prompt/personal_finance_assistant.tmpl`.
- Receipt image recognition prompt: `templates/prompt/receipt_image_recognition.tmpl`.

## Frontend Runtime

- API calls: `src/lib/services.ts` (`v1/llm/*`).
- Server settings accessors: `src/lib/server_settings.ts`.
- Shared assistant interaction logic: `src/views/base/assistant/AssistantPageBase.ts`.
- Desktop assistant page: `src/views/desktop/assistant/AssistantPage.vue`.
- Mobile assistant page: `src/views/mobile/assistant/AssistantPage.vue`.

## Migration-Sensitive Data

- Assistant vector cache table: `ai_assistant_embedding`.
- Transaction attachment metadata table: `transaction_picture_info`.
- Object storage path prefixes: `transaction/`, `avatar/`.
- Migration runbook: `docs/ai-storage-migration.md`.
