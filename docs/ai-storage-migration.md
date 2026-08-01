# ezBookkeeping 全量迁移与数据安全手册（生产版）

> 这是一份“整项目迁移”文档，不仅限 AI。  
> 目标：在不丢失任何业务数据的前提下完成迁移。  
> 原则：先演练、后切换；先备份、后操作；可回滚。

## 1. 必须先明确的结论

- 必迁数据是三类：`数据库` + `对象存储文件` + `运行配置（含密钥）`。
- 仅导出 CSV/TSV 不是完整备份，不能替代数据库备份。
- 交易图片附件是独立对象存储数据，必须和数据库一起迁移。
- AI 助手向量缓存在数据库表 `ai_assistant_embedding`，可迁移也可重建。
- `recognize_receipt_image.json` 识图上传默认是临时识别输入，不会自动保存为交易图片附件。

## 2. 数据资产地图（整项目）

## 2.1 数据库（核心）

迁移时应按“整库”备份与恢复。业务相关表包括但不限于：

- `user`
- `two_factor`
- `two_factor_recovery_code`
- `token_record`
- `account`
- `transaction`
- `transaction_category`
- `transaction_tag_group`
- `transaction_tag`
- `transaction_tag_index`
- `transaction_template`
- `transaction_picture_info`
- `ai_assistant_embedding`
- `user_custom_exchange_rate`
- `user_application_cloud_setting`
- `user_external_auth`
- `insights_explorer`

说明：

- 表结构由 `./ezbookkeeping database update` / 启动时 `auto_update_database=true` 自动维护。
- `ai_assistant_embedding` 是 AI 向量缓存表，存储 JSON 向量和内容哈希。

## 2.2 对象存储（文件）

由 `[storage]` 决定后端类型（`local_filesystem` / `minio` / `webdav`）。

- 用户头像前缀：`avatar/`
- 交易图片前缀：`transaction/`
- 交易图片路径格式：`transaction/{uid}/{pictureId}.{ext}`

说明：

- 交易图片的“可见性”以数据库 `transaction_picture_info` 为准，文件迁移建议做前缀全量迁移。
- 即使某些图片在业务上已逻辑删除，也建议保留完整对象存储快照，避免误删引发历史数据不可恢复。

## 2.3 配置与密钥（必须同步）

至少需要完整迁移 `conf/ezbookkeeping.ini`（或你实际使用的 conf 文件），重点字段：

- `[database]`：数据库连接/路径
- `[storage]`：对象存储配置
- `[security] secret_key`：签名密钥（不一致会导致会话/令牌行为变化）
- `[uuid] server_id`：多实例场景必须保证唯一性
- `[auth]`：OAuth2/OIDC 相关配置
- `[llm]`、`[llm_image_recognition]`、`[llm_image_recognition_fallback]`、`[llm_assistant]`、`[llm_assistant_fallback]`：AI 功能相关配置

AI 配置建议重点核对（防止“能启动但行为偏差”）：

- `llm_image_recognition.llm_provider`
- `llm_image_recognition.openai_api_key`
- `llm_image_recognition.openai_base_url`
- `llm_image_recognition.openai_model_id`
- `llm_image_recognition.chat_completions_stream`
- `llm_image_recognition_fallback.llm_provider`
- `llm_image_recognition_fallback.openai_api_key`
- `llm_image_recognition_fallback.openai_base_url`
- `llm_image_recognition_fallback.openai_model_id`
- `llm_image_recognition_fallback.chat_completions_stream`
- `llm_assistant.llm_provider`
- `llm_assistant.openai_api_key`
- `llm_assistant.openai_base_url`
- `llm_assistant.openai_model_id`
- `llm_assistant.openai_embedding_model_id`
- `llm_assistant.chat_completions_stream`
- `llm_assistant_fallback.llm_provider`
- `llm_assistant_fallback.openai_api_key`
- `llm_assistant_fallback.openai_base_url`
- `llm_assistant_fallback.openai_model_id`
- `llm_assistant_fallback.chat_completions_stream`

`llm_assistant_fallback` 只接管对话模型请求，不接管 embedding；向量检索始终使用 `llm_assistant` 中的 embedding 配置。若 OpenAI 兼容上游只接受流式 Chat Completions，请显式设置对应 section 的 `chat_completions_stream = true`。

同时检查环境变量覆盖：

- `EBK_WORK_DIR`（影响相对路径解析）
- `EBK_<SECTION>_<ITEM>`（直接覆盖配置项）
- `EBKCFP_<SECTION>_<ITEM>`（从文件读取覆盖值）

如果迁移后环境变量缺失或不同，会导致“同一份 ini 但行为不同”。

## 3. 迁移前准备（强制）

1. 先做一次“演练迁移”（同版本、同流程、非生产数据）。  
2. 约定维护窗口，迁移期间停止写入。  
3. 记录源端版本号、提交号、配置来源（文件+环境变量）。  
4. 目标端预先准备：
   - 应用二进制/镜像版本
   - 数据库实例
   - 对象存储实例
   - conf 文件和密钥
5. 确保有可回滚备份，且备份可读可恢复（不是只“备了个文件”）。

## 4. 标准迁移流程（推荐）

## 4.1 冻结写入

- 停止应用实例（或摘流量并确认无写请求）。
- 禁止旧实例与新实例同时写同一套数据。

## 4.2 备份配置

- 备份 conf 文件：
  - `conf/ezbookkeeping.ini` 或自定义 `--conf-path` 文件
- 备份部署环境变量（尤其 `EBK_*` / `EBKCFP_*` / `EBK_WORK_DIR`）。

## 4.3 备份数据库（重点）

### SQLite

```bash
sqlite3 /path/to/ezbookkeeping.db "PRAGMA wal_checkpoint(FULL);"
sqlite3 /path/to/ezbookkeeping.db ".backup '/backup/ezbookkeeping.db.bak'"
sqlite3 /backup/ezbookkeeping.db.bak "PRAGMA integrity_check;"
```

预期：`integrity_check` 返回 `ok`。

### MySQL

```bash
mysqldump \
  --single-transaction \
  --quick \
  --routines \
  --triggers \
  --events \
  --hex-blob \
  --set-gtid-purged=OFF \
  -h <host> -P <port> -u <user> -p <db_name> \
  > /backup/ezbookkeeping.sql
```

### PostgreSQL

```bash
pg_dump \
  -h <host> -p <port> -U <user> -d <db_name> \
  -Fc \
  -f /backup/ezbookkeeping.dump
```

## 4.4 备份对象存储

### `local_filesystem`

- 完整备份 storage 根目录（默认 `storage/`）：
  - `storage/transaction/`
  - `storage/avatar/`

示例：

```bash
rsync -aHAX --numeric-ids --info=progress2 /src/storage/ /backup/storage/
```

### `minio` / `webdav`

- 按 bucket/root path 做全量同步（至少包含 `transaction/`、`avatar/` 前缀）。
- 建议保留迁移前快照或版本化备份。

## 4.5 传输到目标端并恢复

1. 恢复数据库备份。  
2. 恢复对象存储数据。  
3. 恢复 conf 与环境变量。  
4. 核对目标端路径：
   - 相对路径以 `EBK_WORK_DIR` 或进程工作目录为基准。

## 4.6 数据库结构对齐（必须执行）

在目标端执行：

```bash
./ezbookkeeping --conf-path=/path/to/ezbookkeeping.ini database update
```

说明：

- 该命令会同步表结构（含 `ai_assistant_embedding`）。
- 即使 `auto_update_database=true`，生产迁移仍建议先手动执行一次，再启动服务。

## 4.7 启动服务并验证

```bash
./ezbookkeeping --conf-path=/path/to/ezbookkeeping.ini server run
```

## 5. 迁移后校验清单（必须逐条勾）

## 5.1 功能校验

- 能正常登录（历史用户可登录）
- 交易列表、分类、账户、标签、模板数据正常
- 历史交易图片可打开
- 新上传交易图片可显示
- AI 助手可正常响应（启用时）

## 5.2 统计校验（建议）

使用数据统计接口/页面比对迁移前后：

- 账户数
- 交易数
- 分类数
- 标签数
- 交易图片数
- 模板数
- Insights Explorer 数

接口：`GET /api/v1/data/statistics.json`

## 5.3 数据库抽检（建议）

抽检核心表记录数（源端 vs 目标端）：

- `transaction`
- `account`
- `transaction_picture_info`
- `ai_assistant_embedding`（若迁移）

说明：

- `ai_assistant_embedding` 可选迁移，不一致不一定是错误（可能选择了重建策略）。

## 5.4 对象存储抽检（建议）

- 抽样核对多个用户的图片是否可访问。
- 对于本地文件存储，可比较目录结构和文件总量。
- 文件总数允许大于“有效图片记录数”（历史逻辑删除可能仍保留文件）。

## 6. 回滚方案（必须提前准备）

触发条件示例：

- 登录异常或大面积 5xx
- 核心表计数严重不一致
- 图片大面积 404

回滚步骤：

1. 停止目标服务。  
2. 恢复目标端到迁移前快照（数据库 + 对象存储 + 配置）。  
3. 恢复源端服务。  
4. 标记故障窗口并保留日志与备份用于复盘。

## 7. 高风险点与注意事项

- 不要把 CSV/TSV 导出当作全量备份。  
  CSV/TSV 主要是交易数据导出，不覆盖全部系统状态与文件对象。

- 识图接口不是图片持久化接口。  
  `recognize_receipt_image.json` 只识别图片并返回建议字段；交易附件要走 `transaction/pictures/upload.json`。

- 配置不一致比数据错误更隐蔽。  
  同一套数据库，若 `secret_key`、对象存储配置或环境变量覆盖不同，会表现为“功能正常但行为异常”。

- 多实例并行写入需谨慎。  
  若新旧实例同时写入，必须明确数据库与对象存储一致性策略，并确保 `uuid.server_id` 不冲突。

- 大版本跨越建议分段升级。  
  先在演练环境验证每一步，再执行生产迁移。

## 8. Docker 场景补充

官方镜像工作目录通常是 `/ezbookkeeping`，常见持久化目录：

- `/ezbookkeeping/data`
- `/ezbookkeeping/storage`
- `/ezbookkeeping/conf`
- `/ezbookkeeping/log`（可选）

迁移时请确保这些目录（或等价挂载卷）一致迁移。

## 9. AI 向量缓存迁移策略

- 表：`ai_assistant_embedding`
- 推荐：生产迁移时一并迁移（减少首次查询延迟）。
- 可选：不迁移，目标端按需重建（业务数据不受影响，只影响 AI 首次响应速度）。

## 10. 最小可执行清单（TL;DR）

1. 停服务，冻结写入。  
2. 备份 conf + 环境变量。  
3. 备份整库。  
4. 备份对象存储（`transaction/`、`avatar/`）。  
5. 恢复到目标端。  
6. 执行 `database update`。  
7. 启动服务。  
8. 做统计与抽样校验。  
9. 通过后切流量，失败就回滚。
