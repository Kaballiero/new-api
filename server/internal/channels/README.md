# channels — каналы к провайдерам

## Ответственность

Модуль владеет каналами (`channels`): тип и ключ провайдера (один ключ или несколько с отдельными статусами), base URL, модели и их маппинг, группы, теги, приоритет и вес, статус и причина отключения, баланс, время ответа, прокси и прочие настройки. Он отвечает за администрирование каналов, их проверку и синхронизацию моделей с upstream.

Модуль отвечает за инварианты канала:
- новый канал имеет ключ, имена моделей не длиннее 255 байт;
- канал Vertex AI имеет JSON-карту регионов с полем `default`, канал Codex — JSON-ключ с `access_token` и `account_id`;
- канал task plugin привязан к зарегистрированному плагину (ключ до 30 символов), канал New API имеет base URL;
- статус меняют только операции статуса, а не общее редактирование;
- ключи, base URL, переопределения и настройки меняет только администратор с правом `channel.sensitive_write`, привязку task plugin — с правом `task_plugin.bind`;
- список моделей канала изменяется по найденным изменениям upstream, только пока конфигурация загрузки моделей канала не изменилась.

Понятия и бизнес-ошибки описаны в `domain/`:
- `Channel`, `ChannelInfo` (режим нескольких ключей), фильтры и сортировка списка;
- проверка канала (`ValidateChannel`) и ключей Vertex AI (`VertexArrayKeys`);
- состояние обновления моделей upstream (`ModelUpdateState`), правила его расчёта (`ModelUpdateRules`) и отпечаток конфигурации загрузки (`ModelFetchFingerprint`);
- политика отключения и включения по ошибкам (`FailurePolicy`, `ChannelFailure`), проверка канала через шлюз (`Probe`, `ProbeOutcome`), классификация живости модели (`ClassifyModelValidation`);
- ошибки (`errors.go`).

## Возможности

Маршруты `/api/channel/*` идут через `AdminAuth` и `RequirePermission` с указанным правом; исключения отмечены. Аудит пишется через `recordManageAudit` (middleware `TokenOperationAudit` не участвует).

| Сценарий | Что делает | Маршрут и право |
|---|---|---|
| `list_channels` | список каналов или групп по тегам, постранично, со счётчиками типов | `GET /api/channel/` — `channel.read` |
| `search_channels` | поиск по ключевому слову, группе и модели | `GET /api/channel/search` — `channel.read` |
| `list_channel_models` | встроенный список моделей OpenAI-формата | `GET /api/channel/models` — `channel.read` |
| `list_enabled_channel_models` | модели включённых каналов | `GET /api/channel/models_enabled` — `channel.read` |
| `get_channel_operations` | число повторов (`retry_times`) | `GET /api/channel/ops` — `channel.read` |
| `get_channel` | канал без ключа и причин отключения ключей | `GET /api/channel/:id` — `channel.read` |
| `reveal_channel_key` | ключ канала, аудит `channel.key_view` | `POST /api/channel/:id/key` — `RootAuth`, `CriticalRateLimit`, `DisableCache`, `SecureVerificationRequired` |
| `create_channel` | создание одного канала, пакета или канала с несколькими ключами, ответ 201, аудит `channel.create` | `POST /api/channel/` — `channel.sensitive_write` |
| `update_channel` | изменение канала под блокировкой строки, аудит `channel.update` со списком изменённых полей | `PUT /api/channel/` — `channel.write` |
| `copy_channel` | копия канала с суффиксом имени (по умолчанию `_复制`), аудит `channel.copy` | `POST /api/channel/copy/:id` — `channel.sensitive_write` |
| `delete_channel` | удаление канала и сброс прокси-клиента, аудит `channel.delete` | `DELETE /api/channel/:id` — `channel.sensitive_write` |
| `delete_channels_batch` | пакетное удаление, аудит `channel.delete_batch` | `POST /api/channel/batch` — `channel.sensitive_write` |
| `delete_disabled_channels` | удаление отключённых каналов, аудит `channel.delete_disabled` | `DELETE /api/channel/disabled` — `channel.sensitive_write` |
| `update_channel_status` | включение или ручное отключение, аудит `channel.status_update` | `POST /api/channel/:id/status` — `channel.operate` |
| `update_channel_status_batch` | то же пачкой, аудит `channel.status_update_batch` | `POST /api/channel/status/batch` — `channel.operate` |
| `disable_tag_channels`, `enable_tag_channels` | отключение и включение каналов тега, аудит `channel.tag_disable` / `channel.tag_enable` | `POST /api/channel/tag/disabled`, `/tag/enabled` — `channel.operate` |
| `edit_tag_channels` | изменение каналов тега (переопределения — только с `channel.sensitive_write`), аудит `channel.tag_edit` | `PUT /api/channel/tag` — `channel.write` |
| `set_channel_tags_batch` | установка тега пачке каналов, аудит `channel.tag_batch_set` | `POST /api/channel/batch/tag` — `channel.write` |
| `get_tag_models` | самый длинный список моделей среди каналов тега | `GET /api/channel/tag/models` — `channel.read` |
| `repair_channel_abilities` | пересборка таблицы abilities | `POST /api/channel/fix` — `channel.operate` |
| `manage_multi_keys` | статус, включение, отключение и удаление ключей канала с несколькими ключами, аудит `channel.multi_key_manage` | `POST /api/channel/multi_key/manage` — `channel.operate` (удаление — `channel.sensitive_write`) |
| `test_channel` | проверка канала запросом через шлюз | `GET /api/channel/test/:id` — `channel.operate` |
| `test_channels` | постановка системной задачи проверки всех каналов (409, если задача уже есть) | `GET /api/channel/test` — `channel.operate` |
| `validate_models` | проверка живости моделей канала (alive / dead / uncertain) | `POST /api/channel/validate_models` — `RootAuth` |
| `update_channel_balance` | баланс канала у провайдера | `GET /api/channel/update_balance/:id` — `channel.operate` |
| `update_channels_balances` | баланс всех включённых каналов; канал с нулевым балансом отключается | `GET /api/channel/update_balance` — `channel.operate`; фоновая задача `AutomaticallyUpdateChannels` (`CHANNEL_UPDATE_FREQUENCY`) |
| `fetch_upstream_models` | модели сохранённого канала у upstream | `GET /api/channel/fetch_models/:id` — `channel.operate` |
| `preview_channel_models` | модели несохранённого канала или канала advanced custom | `POST /api/channel/fetch_models` — `channel.sensitive_write` |
| `detect_upstream_model_updates` | поиск изменений моделей upstream одного канала | `POST /api/channel/upstream_updates/detect` — `channel.operate` |
| `detect_all_upstream_model_updates` | постановка системной задачи поиска изменений, аудит `channel.upstream_detect_all` | `POST /api/channel/upstream_updates/detect_all` — `channel.operate` |
| `apply_upstream_model_updates` | применение выбранных изменений, аудит `channel.upstream_apply` | `POST /api/channel/upstream_updates/apply` — `channel.write` |
| `apply_all_upstream_model_updates` | применение всех найденных изменений, аудит `channel.upstream_apply_all` | `POST /api/channel/upstream_updates/apply_all` — `channel.write` |
| `scan_upstream_model_updates` | один прогон проверки моделей upstream всех каналов с уведомлением администраторов | системная задача `model_update` |
| `run_channel_tests` | один прогон проверки каналов: отключение медленных и сбойных, включение восстановленных | системная задача `channel_test` |
| `refresh_codex_credential` | обновление OAuth-учётных данных Codex | `POST /api/channel/:id/codex/refresh` — `channel.sensitive_write`; также `service.RefreshCodexChannelCredential` |
| `refresh_codex_credentials` | обновление истекающих учётных данных Codex | фоновая задача `service.StartCodexCredentialAutoRefreshTask` |
| `discover_codex_models` | модели Codex у upstream | без HTTP, через `service` |
| `get_codex_usage`, `get_codex_reset_credits`, `reset_codex_usage` | использование Codex, кредиты сброса и сброс | `GET /api/channel/:id/codex/usage`, `GET .../usage/reset-credits` — `channel.read`; `POST .../usage/reset` — `channel.operate` |
| `pull_ollama_model`, `stream_ollama_model_pull`, `delete_ollama_model`, `get_ollama_version` | модели и версия Ollama | `POST /api/channel/ollama/pull`, `/ollama/pull/stream`, `DELETE /ollama/delete`, `GET /ollama/version/:id` — `channel.sensitive_write` |
| `get_affinity_cache_stats`, `clear_affinity_cache` | статистика и очистка кэша привязки каналов | `GET`, `DELETE /api/option/channel_affinity_cache` — `RootAuth` |
| `get_affinity_usage_stats` | использование кэша привязки по правилу и ключу | `GET /api/log/channel_affinity_usage_cache` — `AdminAuth` |
| `change_channel_state` | смена статуса канала или ключа с учётом кэша | без HTTP, через `model.UpdateChannelStatus` |
| `disable_channel`, `enable_channel` | отключение и включение по ошибкам шлюза с уведомлением root | без HTTP, через `app/channelruntime.Disable` / `app/channelruntime.Enable` |

Ошибки сценариев — значения из `domain/errors.go`. Их переводит в HTTP общий `transport.WriteError`; `transport.Message` даёт текст ошибки на языке запроса:
- `ErrChannelNotFound` → 404 `channel_not_found`;
- `domain.Internal(err)` → 500 `internal_error` с текстом причины;
- `domain.Invalid(err)` → 400 `channel_validation_failed`, `domain.Reject(err)` → 400 `invalid_params`, `domain.ModelFetchFailed(err)` → 502 `fetch_models_failed`, `ErrAddModeUnsupported` → 400 `mode_unsupported`;
- ошибки Ollama → 400 / 404 / 500 без кода (как в `origin/main`), ошибка версии Ollama → 200;
- остальные ошибки, в том числе ошибки хранения в сценариях, где их так отдавали всегда, → 200 `{success:false}` с текстом ошибки; тексты, которые раньше были китайскими, берутся из i18n (`zh-CN` прежний).

Исключения: ошибка `reveal_channel_key`, кроме «не найден», уходит в `writeSecurityOperationError`; `clear_affinity_cache` и `get_affinity_usage_stats` отвечают 400 с текстом ошибки; поток `stream_ollama_model_pull` после начала пишет ошибку событием SSE.

Уведомления и сохраняемые тексты без запроса (отключение и включение канала, завершение проверки, сводка обновления моделей, причина «недостаточно средств») пишутся на языке root-пользователя, без него — на `i18n.DefaultLang`. Сводку обновления моделей получают все подписанные администраторы на языке root. Причина «время ответа превышено» и подпись лога проверки канала — на языке пользователя, от имени которого идёт проверка (для фоновой проверки — root).

OpenAPI-генератор (`cmd/gen-admin-openapi/channels.go`) читает DTO и `http.go` сценариев; коды ошибок каждого обработчика закреплены в его таблице, потому что их пишет `transport.WriteError`.

## Зависимости

- Чужие `domain` модуль не импортирует.
- Шлюз модулю не виден: тест канала, загрузка моделей, политика ошибок и настройки шлюза (`setting`, `settings`) приходят через узкие интерфейсы.
- Сборка — `internal/app/channels.go`: `NewChannelsHTTP` один раз через `sync.OnceValue` в `app/httpapi`; фоновые прогоны собираются на каждый запуск (`NewChannelTestTaskUsecase`, `NewScanChannelModelsUsecase`, `NewChannelBalancesUsecase`). Usecase, который читает каналы, создаётся на каждый запрос: `adapter/gorm/channels.Store` хранит записи, загруженные в рамках одного запроса, чтобы изменения попадали в объект кэша каналов.
- Хранение: `internal/adapter/gorm/channels` (`Store`) поверх `internal/adapter/gorm/channels/record` (GORM-запись `channels`, abilities, теги, настройки шлюза в DTO relaykit).
- Шлюз: `internal/adapter/channels` — проверка канала (`ProbeEngine`, `ProbeOutcome`, `TestFailures`), загрузка моделей, баланс, Ollama, Codex, кэш привязки, тексты уведомления об обновлении моделей; сама проверка — `internal/adapter/gateway/channels`, failure orchestration — `gateway/transport`.
- Права: `app/authorizationcore` (`ChannelSensitiveWrite`, `TaskPluginBind`); аудит — callbacks app/httpapi и audit/transport.MarkLogged.
- Потребители: `controller/channel*.go`, `controller/codex_usage.go`, `controller/model.go` (HTTP), `app/channelruntime`, `app/channelcore`, gateway и taskplugins через технические ports.

## Границы и долг

- Выбор канала при запросе остаётся в gateway/middleware. Единственный cache, polling/status mutex и ability SQL принадлежат `adapter/gorm/channels`; счётчик used_quota обслуживает adapter/gorm/counters вместе с user/token очередями. `model.Channel` и `model.Ability` — строгие aliases записей.
- Удаление модели из каталога переписывает abilities через channels adapter на переданном tx. Pricing получает технические channel/ability facts через свой store; бизнес-правила не копируются.
- `app/channelcore` собирает смену статуса, Codex discovery/refresh и прежние расписания. `app/channelruntime` собирает enable/disable/root notifications; `adapter/channels` отображает gateway errors в failure policy, а `adapter/channels/codex` обслуживает прежние HTTP clients/cache.
- Gateway probe engine перенесён в adapter/gateway/channels; controller хранит только HTTP delegates, а private worker/select conveniences — только в *_test.go. Новых model/service мостов нет.
- `domain` и usecase транзитивно не зависят от Gin/GORM/Redis. JSON полей mapping, ключей и `OtherInfo` обслуживает `adapter/gorm/channels/record.Codec`; бизнес получает типизированные maps/ключи и узкие codec capabilities. Диагностика `OtherInfo` приходит явно через `Codec.Log`, маскирование и log preview — из app через прежние common-функции.
- Статусы каналов объявлены в существующем leaf `constant`; прежние имена `common.ChannelStatus*` ссылаются на те же константы. Wire snapshot и patch остаются в `adapter/channels.ChannelPatchCodec`; domain сохраняет правила валидации, нормализации и смены статусов. Ошибки `clear_affinity_cache` и `get_affinity_usage_stats` остаются в их пакетах.
- Mapping sentinels объявлены только в `channels/domain`; прежняя injected Errors граница использует те же значения без aliases в model.
- `detect_upstream_model_updates` и `scan_upstream_model_updates` (метод `Check`), `apply_upstream_model_updates` и `apply_all_upstream_model_updates` (метод `Apply`) содержат одинаковый код: сценарии не вызывают друг друга, а общий шаг зависит от хранилища.
- Конструктор `run_channel_tests` и `scan_upstream_model_updates` принимает больше трёх функций без группировки.

Сборка channel effects, HTTP и scheduler находится в app/httpapi; один technical
adapter/channels.ModelUpdateNotifications хранит прежнюю suppression state. Balance loop
также запускается из app, сохраняя sleep-first/minutes. На каждом узле его включает
CHANNEL_UPDATE_FREQUENCY, без нового master/once gate.
