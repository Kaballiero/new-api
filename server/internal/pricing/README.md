# pricing — цены для клиентов и конфигурация цен

## Ответственность

Модуль владеет тем, как платформа показывает и настраивает цены моделей:
- каталог цен (`Pricing`) — снимок, собранный из моделей, каналов, коэффициентов, billing-выражений и нативных цен upstream; его ревизия (`catalog_revision`) и публикация;
- проекция цены для потребителя (`Projection`): тариф группы, FX-пересчёт в рубли, страницы, обзор, детальная карточка модели, публичный каталог, минимальные цены предложений upstream;
- конфигурация цен моделей в опциях (`Configuration`): версии цены модели, проверка значений и выражений, встроенные цены;
- сравнение коэффициентов с upstream (ratio sync);
- резерв запроса шлюза к OpenRouter по тарифам endpoint'ов upstream (`estimate_upstream_reserve`).

Инварианты:
- значения цен, коэффициенты, состав каталога, ревизии, публикация и кэш потребительского каталога — источник истины для страниц цен; рефакторинг их не меняет;
- чтение с `expected_revision` отдаётся только для той же ревизии, иначе 409 `pricing_catalog_revision_mismatch`;
- изменение цены модели применяется только к версии, которую видел клиент (`expected_version`), иначе конфликт `ErrModelPricingConflict`;
- публикация выбранного upstream-каталога и синхронизация идут только для неизменённого источника (`ErrCatalogSourceChanged`);
- ревизия upstream-каталога, потерявшая больше `MaxCatalogModelLossPercent` (30%) опубликованных моделей (`CatalogModelLoss`), не применяется. Опубликованные модели — модели head, которые запрашивают каналы (`CatalogRequestedModels`: upstream-модель каждой ability включённого канала OpenRouter каталога); цели alias и модели, которые больше никто не запрашивает, не сравниваются. Одно правило для цен и каналов: head не двигается (`CatalogModelLossError`), а модели каналов из неё не применяются автоматически (`UpstreamModels.AutoApplyBlocked`);
- цена модели upstream-каталога — минимальные цены синхронных предложений её документа для операции её типа (`Projection.CatalogModelMinimumPrices`). По этому правилу `list_upstream_catalog_models` подтверждает цену добавляемой модели, а `sync_upstream_catalog` сравнивает цены двух ревизий (`CatalogPriceChanges`): компонент попадает в отчёт, если цена выросла или упала больше чем в `PriceChangeFactor` (2) раза, в том числе бесплатный компонент, получивший цену. Отчёт только информирует и head не задерживает.

Понятия и бизнес-ошибки — в `domain/` (проекция, конфигурация, каталог, ratio sync, нормализация нативных цен, чтение цены endpoint'а `endpoint_price.go`, `errors.go`). Правила billing-выражений — `pkg/billingexpr/expr.md`.

## Возможности

| Сценарий | Что делает | Маршрут и доступ |
|---|---|---|
| `get_pricing` | классический список цен `/api/pricing` с группами, эндпоинтами и `pricing_version` | `GET /api/pricing`, `HeaderNavModuleAuth("pricing")` |
| `get_personal_pricing` | тариф текущего пользователя: полный каталог или страница (`limit`, `offset`, фильтры); страницы с вкладкой единиц или сортировкой по цене строятся из кэшированного списка (см. «Кэш проекций») | `GET /api/user/effective-pricing`, `UserAuth`, `DisableCache` |
| `get_model_pricing` | карточка одной модели для текущего пользователя (`model_name`) | `GET /api/user/effective-pricing/model`, `UserAuth`, `DisableCache` |
| `get_group_pricing` | тариф заданной группы (`group`, `using_group`), полный или постранично; анонимный полный каталог кэшируется по группе, страницы строятся из кэшированного списка тарифа (см. «Кэш проекций») | `GET /api/pricing/effective`, `RootAuth`, `DisableCache` |
| `get_pricing_overview` | обзор по модальностям (по 4 модели в секции по умолчанию) | `GET /api/pricing/effective/overview`, `RootAuth`, `DisableCache` |
| `list_unbillable_models` | диагностика моделей группы default без billing-конфигурации или подтверждённой публичной цены; рекомендации reserve для OpenRouter | `GET /api/models/unbillable`, AdminAuth |
| `get_public_models` | публичный каталог тарифа `default` по тегам (`tag`, `p`, `page_size`) из кэшированного списка (см. «Кэш проекций») | `GET /api/pricing/effective/models`, без авторизации, `DisableCache` |
| `preview_pricing` | предпросмотр тарифа выбранной группы (`user_group`) без смены группы | `GET /api/option/effective_pricing`, `RootAuth`, `DisableCache` |
| `get_pricing_config` | конфигурация цен моделей с версиями и схемами usage плагинов | `GET /api/option/model_pricing`, `RootAuth` |
| `update_pricing_config` | изменение цен моделей по версиям; аудит `model.pricing.update` | `PATCH /api/option/model_pricing`, `RootAuth` |
| `reset_model_ratios` | сброс `ModelRatio` к значениям по умолчанию | `POST /api/option/rest_model_ratio`, `RootAuth` |
| `get_ratio_config` | открытые коэффициенты для синхронизации другими инстансами | `GET /api/ratio_config`, `CriticalRateLimit` |
| `list_syncable_channels` | каналы с `base_url` и два пресета (официальный, models.dev) | `GET /api/ratio_sync/channels`, `RootAuth` |
| `fetch_upstream_ratios` | загрузка коэффициентов upstream и расхождения с локальными | `POST /api/ratio_sync/fetch`, `RootAuth` |
| `refresh_catalogue` | прогон фоновой задачи обновления нативных цен upstream и публикации каталога. Отклонённая ревизия upstream-каталога (`CatalogModelLossError`) завершает задачу ошибкой, пропавшие модели попадают в `lost_models` summary, а алерт уходит в Telegram-чат один раз на ревизию: ревизия последнего доставленного алерта — строка `pricing_catalog_rejected_revision` таблицы `options` (`SentAlerts`, `CatalogAlerts`). Цены, которые применённая ревизия изменила больше чем в 2 раза, попадают в `price_changes` summary (в USD) и отдельным сообщением в Telegram-чат (`CatalogAlerts.PriceChanges`): ревизии сокращены до 12 символов, суммы — в ₽ по текущему курсу, а без курса — в USD. Отправка пишется в лог; недоставленный отчёт тоже пишется в лог и не повторяется: head уже сдвинулся, следующий прогон сравнивает с ним | системная задача `pricing_catalog` (`app/systemruntime`) |
| `enqueue_catalogue_refresh` | постановка задачи обновления (`PRICING_CATALOG_TASK_ENABLED`) | без HTTP, `app.NewEnqueuePricingCatalogueRefresh` |
| `sync_upstream_catalog` | синхронизация upstream-каталога в неизменяемые записи; опубликованные новые документы передаются раскодированными в общий кэш документов. Manifest сравнивается с опубликованными моделями (`CatalogModelLoss`): при потере больше 30% из них возвращается `CatalogModelLossError`, head не меняется; пока ревизия отклонена, не применяются и изменения цен оставшихся моделей, а модель, добавленная в канал за это время, не получает цену каталога до движения head. Пропавшие модели, убранные из каналов, в сравнение не входят, поэтому следующая ревизия проходит. Перед публикацией цены моделей, которые head и новая выборка публикуют разными документами, сравниваются по `CatalogPriceChanges` при ratio 1, как при подтверждении `Addable`: кратность изменения не зависит от курса. Изменения записываются в USD вместе с remote-ревизиями head и новой ревизии (`CatalogPriceChangeReport`); документы head читает `LoadStoredCatalogDocuments`. Документ меняет ревизию, поэтому сравнение идёт только при смене head; отклонённая ревизия не сравнивается. Отчёт только информирует: если сохранённый документ head не читается, сравнение пропускается, ошибка попадает в `price_changes_error` summary и в лог, а ревизия публикуется | без HTTP, `refresh_catalogue` и outer `app/pricing.go` |
| `ensure_catalog_source`, `publish_catalog_selection` | источник upstream-каталога и публикация выбранных записей | без HTTP, native characterization; persistence API у `adapter/gorm/pricing` |
| `read_upstream_catalogue` | проекция upstream-каталога: документы опубликованной ревизии читает `LoadStoredCatalogDocuments`, документы, на которые ревизия больше не ссылается, освобождаются из кэша | без HTTP, `app/pricingruntime` |
| `list_upstream_catalog_models` | модели опубликованного upstream-каталога как `channels/domain.UpstreamModels` канала OpenRouter: `Listed` — модели manifest, `Addable` — те из них, что не alias, без суффикса `:free` и `:batch`, с операцией текста, эмбеддингов или генерации изображений по типу (`CatalogConsumerProfile`) и пути запроса из index, и с подтверждённой ценой синхронного предложения (`Projection.CatalogModelMinimumPrices` → `ConfirmedConsumerPrices`); `Shortcuts` — shortcuts маршрутизации OpenRouter (`OpenRouterShortcuts`: `:nitro`, `:floor`). Manifest и index читаются одной ревизии (при расхождении — одна повторная попытка, затем ошибка). Документы кандидатов загружает `LoadCatalogDocuments` (общий с `sync_upstream_catalog`: сохранённые — через `LoadStoredCatalogDocuments`, недостающие — из каталога, без сохранения); результат ревизии хранится в процессе (`adapter/pricing.UpstreamCatalogModelsCache`), поэтому документы ревизии читаются один раз. `AutoApplyBlocked` — manifest потерял больше 30% опубликованных моделей по тому же правилу `CatalogModelLoss`, что и `sync_upstream_catalog` (abilities и каналы читает `domain.CatalogChannels`, реализация `adapter/pricing/upstream.NativeStore`); флаг считается на каждый вызов, в том числе для результата из кэша; нечитаемый head — ошибка | без HTTP, `channelModelFetcher` в `app/channels.go` |
| `estimate_upstream_reserve` | резерв запроса шлюза к OpenRouter в USD — максимум стоимости по подходящим endpoint'ам всех маршрутизируемых моделей запроса. Endpoint отбирают фильтры провайдера (`only`, `ignore`, `order` при `allow_fallbacks: false`, `quantizations`, `require_parameters`, `max_price`), service tier (`priority`/`fast`, `flex`, `ultrafast` — только по запросу, явному slug или shortcut `:nitro`/`:floor`; `ultrafast` допускает и priority-endpoint'ы), лимиты контекста и вывода (без лимита клиента вывод ограничен `DefaultOutputTokens`); default-endpoint, на который откатывается запрос `flex`, исключается, если по цене ввода или вывода он дороже каждого подходящего flex-тарифа. Тарифы по длине промпта и окнам UTC (`overrides`) дают несколько вариантов, в резерв идёт наибольший. Неизмеримое количество (rerank, decisions, аудио, неизвестный тариф) — `domain.EstimateUnavailableError`: его покрывает только ручная цена или приблизительный резерв канала; при резерве-надбавке ошибка возвращается с первого такого endpoint'а, и шлюз повторяет оценку целым резервом. Для текстового резерва по тарифам возвращает границы подсказки `max_tokens` (`InputUSD`, `PromptPriceUSD`, `OutputPriceUSD`, `OutputTokens`) | без HTTP, `relay/channel/openrouter.RequestEstimator` через `app/pricingruntime.EstimateUpstreamReserve` |
| `rebuild_catalogue` | сборка снимка каталога цен | без HTTP, `app/pricingruntime` + `adapter/pricing/state` |
| `read_catalogue` | ревизия опубликованного каталога | без HTTP, `app/pricingruntime` + `adapter/pricing/state` |
| `publish_native_catalogue` | публикация нативных цен в опции | без HTTP, `app/pricingruntime` + `adapter/settings/publication` |
| `observe_catalogue_inputs` | отпечаток входных данных каталога | без HTTP, `app/pricingruntime` + `adapter/pricing/state` |
| `update_pricing_options`, `mutate_pricing_options` | запись опций цен вне HTTP-конфигурации | без HTTP, `app/pricingruntime` |

Ошибки чтения цен и ratio sync переводит в HTTP общий `transport.WriteError` (тело `{success:false, message}` без `Abort`):
- `ErrRevisionMismatch` → 409 с `code: pricing_catalog_revision_mismatch`;
- `domain.InvalidQuery(err)` (ошибки разбора запроса, `invalid using_group`) и `ErrInvalidUserGroup` → 400, текст причины;
- `ErrUserGroupUnavailable` → 500; `ErrChannelQueryFailed` → 500 с i18n `channel.query_failed` (раньше «查询渠道失败»);
- `ErrModelPricingUnavailable` → 404;
- `ErrNoUpstreams` → 200 с i18n `channel.no_valid_upstream` (раньше «无有效上游渠道»);
- остальное, включая `ErrPublicPricingUnavailable` и ошибки чтения снимка/FX, → 503 с текстом ошибки.

Остальные ответы:
- `update_pricing_config`: ошибка JSON → 400; `ErrModelPricingConflict` в цепочке → 409; прочие ошибки проверки → 400; всё с текстом ошибки;
- `get_pricing_config`: ошибка → 200 `{success:false}` (`common.ApiError`); `list_syncable_channels` — так же;
- `get_ratio_config`: выключено → 403 `ratio_config_disabled` с i18n `pricing.ratio_config_disabled` (раньше «倍率配置接口未启用»);
- `fetch_upstream_ratios`: неверное тело → 400 с i18n `pricing.request_invalid` (раньше «请求参数格式错误»); нераспознанный ответ upstream в `test_results[].error` — i18n `pricing.upstream_data_unrecognized` (раньше «无法解析上游返回数据»);
- `reset_model_ratios`: ответ `{success, message}` с HTTP 200; успех — i18n `model.reset_success` (раньше «重置模型倍率成功»).

OpenAPI-генератор (`cmd/gen-admin-openapi/pricing.go`) читает DTO и `http.go` сценариев, а контракт ответов и коды ошибок берёт из своей таблицы `pricingHandlerSources`.

## Зависимости

- `catalog/domain` (`Model`, `Vendor`, метаданные) — цена строится поверх модели; `channels/domain` — поверх канала (`domain/catalogue.go`).
- Сборка — `internal/app/pricing.go` (`NewPricingHTTP`), один раз через `sync.OnceValue` в `app/httpapi/pricing.go`; аудит — callback app/httpapi к app/audit.Recording.
- Документы upstream-каталога неизменяемы и адресуются hash. `LoadStoredCatalogDocuments` берёт сохранённый документ из кэша по источнику и hash (`domain.CatalogDocumentCache`, реализация `adapter/pricing.UpstreamCatalogDocumentsCache`, экземпляр `app/pricingruntime.UpstreamCatalogDocuments`), остальные читает из БД, раскодирует с проверкой и кэширует. Повторно документ раскодируется, только если кэш его освободил (ревизия на него больше не ссылалась) или если два сценария одновременно впервые читают его. Кэш общий для `read_upstream_catalogue`, `sync_upstream_catalog` и `list_upstream_catalog_models`; в нём только сохранённые документы: загруженные из каталога попадают туда после публикации.
- Адаптеры: `adapter/pricing` (`Reader`, `ConsumerCache`, `ProjectionCache`, `RatioSettings`, `RatioChannels`, `UpstreamCatalogModelsCache`, `UpstreamCatalogDocumentsCache`, плагины, тексты алерта отклонённой ревизии `CatalogModelLossNotification` и отчёта об изменении цен `CatalogPriceChangesNotification`; отправку в Telegram-чат на языке root собирает `app/pricing.go`), `adapter/gorm/settings` (строка `pricing_catalog_rejected_revision`), `adapter/pricing/upstream` (HTTP-клиенты ratio sync и нативных цен, override, нормализация), `adapter/gorm/pricing` (опции, каталог, неизменяемые записи, наблюдения).
- Настройки: `setting/ratio_setting`, `setting/billing_setting`, account group policy в settings/accounts. Leaf `app/pricingruntime` собирает статические сценарии once; `adapter/pricing/state` владеет единым опубликованным snapshot, revision/generation/observation state и lock order. DB, колонки, настройки, registry и clock читаются поздно.
- Модели upstream-каталога (`list_upstream_catalog_models`) возвращаются как `channels/domain.UpstreamModels`; сценарий собирает `app/pricing.go`, вызывает обновление моделей каналов через `app/channels.go`.
- Включённый канал OpenRouter каталога, чей маппинг моделей не разрешается (неверный JSON или цикл), делает опубликованные модели неизвестными: `CatalogRequestedModels` возвращает такие каналы отдельно от запрошенных моделей. `refresh_catalogue` тогда завершается ошибкой, и head не двигается; `list_upstream_catalog_models` отдаёт модели с `AutoApplyBlocked` и этими каналами в `UnresolvedMappingChannels` (также без опубликованного head), поэтому плановая проверка каналов ничего не применяет, а ручные проверка и применение работают.
- Потребители: `controller/pricing*.go`, `ratio_sync.go`, `ratio_config.go`, `model_pricing_config.go` (HTTP bindings и wire-декларации OpenAPI); `app/pricingruntime` и технические adapters (сценарии без HTTP).
- Шлюз (`relay/channel/openrouter.RequestEstimator`, в том числе admission Batch через `adapter/gateway/batch`) берёт `estimate_upstream_reserve` из `app/pricingruntime.EstimateUpstreamReserve`; сценарий создаётся один раз и получает только JSON-кодек (`Capabilities`). Тариф endpoint'а — `domain.NativePricedEndpoint`, шлюз преобразует в него обнаруженные endpoint'ы. `domain.ReadEndpointPrice` читает цену endpoint'а и для нормализации нативных цен (`adapter/pricing/upstream`).

## Границы и долг

- Модулю не принадлежат каналы (модуль `channels`) и метаданные моделей (модуль `catalog`).
- ⚠ Каналы для ratio sync читаются через `adapter/gorm/channels` в `app/pricing.go` (`GetChannelsByIds`, `GetAllChannels`, `GetChannelById` + `GetNextEnabledKey` для ключа OpenRouter), а не через сценарии `channels`.
- ⚠ `adapter/gorm/pricing` и `adapter/pricing/upstream` используют записи `adapter/gorm/catalog/record` и `adapter/gorm/channels/record` напрямую.
- `domain` и сценарии транзитивно не зависят от `common`, `billingexpr`, `jsplugin` и `catalogsync` и не импортируют gateway DTO. Единственный разрешённый gateway leaf — прежняя цепочка `constant → relaykit/types → relaykit/relayconvert/kitutil` для EndpointType/FinishReason. Правило `pricing-constant-alias-leaf` проверяет точные пакеты и рёбра; direct business imports gateway и любые другие gateway paths запрещены. Выделение constant leaf относится к необязательной волне 9. Offer/document/schema/tier/display facts принадлежат pricing; `Capabilities` явно передаёт codecs, expression/catalog readers и endpoint rules. Арифметика и expression engine остаются в прежних пакетах.
- `adapter/pricing` преобразует native facts по полям с сохранением wire-представления. Enum labels кодируются прежним `jsplugin.LocalizedText.MarshalJSON` в raw display facts, поэтому nil label остаётся `{}`; description сохраняет map и прежний `omitempty`. Typed persisted catalog decoder сохраняет прежние SDK wire-типы и исходные UnmarshalTypeError Type/Struct/Field/Offset facts для domain containers, включая точные тексты ошибок. Alias resolver собирается один раз на snapshot и вызывает прежний `catalogsync.ResolveAlias`.
- Ratio request/result DTO принадлежат своим сценариям; общий `RatioDifference` — domain. HTTP передаёт прежний Gin binder codec-адаптеру: тот использует исходный wire request и validation, затем отображает его в plain Request. Поэтому названия типов в диагностике binding остаются прежними. `AdvancedEndpoints` и `ChannelCodec` оставляют relay DTO и их parsing в адаптере.
- Резерв OpenRouter: шлюз сам обнаруживает endpoint'ы (`/endpoints`), разбирает запрос, проверяет формат и стрим, измеряет количества (токены промпта, `n`, картинки, правило Cohere Embed), нормализует slug провайдеров (нижний регистр, `/priority` → `/fast`) и применяет политику резерва (ручная цена, приблизительный резерв, надбавка, повтор целым резервом). Проверки политики перемешаны с проверками формата и стрима, их перенос изменил бы порядок ошибок. Константы шлюза (`DefaultOutputTokens`, `MaxTokensLimit`) приходят во входе: импорт `relay/helper` дал бы цикл.
- Правила `estimate_upstream_reserve` близки к `NativePricingVectors` и `CatalogServiceTierAllowed`, но не объединены: у них другие тексты ошибок, границы порога промпта и правило `ultrafast`. Тексты ошибок резерва уходят клиенту по-английски, без i18n.
- Сценарии создаёт app. `adapter/settings/publication` предоставляет один publisher/lock для settings, legacy pricing options и native publication; каталожные удаления получают same-tx техническую capability.
- Бизнес-ошибки принадлежат pricing/domain; model сохраняет только строгие table aliases для immutable catalog records.
- Имена пресетов «官方倍率预设» / «models.dev 价格预设» в `list_syncable_channels` — идентификаторы, с которыми сравнивает клиент (`client/src/features/system-settings/models/constants.ts`); не локализуются.
- `consumerContext` используется сценариями чтения и диагностикой `list_unbillable_models`. Публичные страницы скрывают модели без подтверждённой цены (явный ноль считается ценой) и без возможности биллинга; персональные страницы и overview сохраняют полный состав. Eligibility читает catalog owner через узкий порт, minute cache принадлежит `adapter/pricing`. После успешного refresh диагностические причины добавляются в summary и лог.
- Creator берётся из сохранённых metadata, затем catalog author, затем эвристики имени; display vendors не записываются в БД. Context window добавляется как display tag только при отсутствии сохранённого K/M tag.

ResetModelRatios пишет через settings owner из app, сохраняя дополнительную PublishRatios после Persist.
Pricing/catalog composition принадлежит leaf app; технические records/cache/publication state находятся
в adapters. Одноразовая request-specific SQL mutation capability передаётся shared mutation usecase
при Execute, без отдельного postcommit вызова и без общего изменяемого request state.

## Кэш проекций

Проекция всего каталога (цены провайдеров upstream, минимумы, дедупликация) — самая дорогая часть
чтения цен. Страницы `get_group_pricing`, `get_public_models` и `get_personal_pricing` строятся из
готового списка тарифа: `domain.EffectivePricingList` / `domain.PublicPricingModelsList` содержат
спроецированные модели в порядке имён, а страница только фильтрует, сортирует и режет их. JSON
страницы совпадает с прежним.

- Ключ — пара (группа пользователя, using group); на ключ хранится только последний список.
  Публичный каталог — один ключ `default`/`default`.
- Персональные списки — отдельный кэш: в них все модели тарифа, без фильтра подтверждённых цен,
  и каждая спроецирована (`EffectivePricingList.Projected`). Пользователь входит в проекцию только
  через условия `user_id` в param override каналов (`CatalogProviderAllowed`); группа пользователя
  уже в ключе. Есть ли такие условия, `Projection.CatalogOverridesReadUserID` проверяет один раз на
  отпечаток тарифа (`ProjectionCache.LoadUserDependence`). Без них пользователи тарифа делят один
  список, с ними у каждого пользователя свой. Личных списков не больше 8 на процесс
  (`UserListLimit`), лишний вытесняет давнее всего читавшийся.
- Персональная страница с сортировкой по имени без вкладки единиц берёт готовый список, а если его
  ещё нет, не ждёт: проецирует только свои модели, как раньше. Вкладка или сортировка по цене
  ждёт первого построения списка.
- Отпечаток входов `domain.PricingProjectionInputs`: ревизия потребительского каталога
  (`ConsumerPricingRevision`: снимок, группы, auto/usable groups, коэффициенты групп, FX) и
  набор billable-моделей (читается отдельно, кэш 1 минута, ревизию не меняет; `nil` в self-use
  отличается от пустого набора). Персональные списки billable-модели не фильтруют, их отпечаток —
  только ревизия.
- Изменился отпечаток — отдаётся прежний список (с его ревизией: `expected_revision` сверяется с
  отданным списком), а один фоновый пересчёт на ключ строит новый. Ждёт только запрос к ключу без
  списка; одновременные такие запросы делят одно построение (singleflight).
- Кэш — `adapter/pricing.ProjectionCache`; фоновые построения запускает `app/pricing.go`
  (`runPricingProjectionBuild`: recover и лог), прогрев тарифа `default` — `StartPricingProjectionWarmup`
  после первой загрузки FX (`pricingfx.StartModelCostFX`), не блокируя старт. Там же
  `get_personal_pricing.Usecase.WarmUp` строит общий персональный список группы `default` тем же
  путём, что и запрос (ключ `default`/пустой using group: персональные страницы using group не
  принимают). Если проекция зависит от `user_id`, общего списка нет и прогрев ничего не строит.
- Overview, карточка модели и полный каталог этим кэшем не пользуются.

## Курсы валют для стоимости

`refresh_cost_fx` загружает last-good baseline, получает provider facts, проверяет московскую
календарную дату и USD nominal, сохраняет новую публикацию через CAS либо возвращает прежнюю
версию при тех же дате/курсе. Version conflict не повторяется. Record `ModelCostFXRow` находится
в `adapter/gorm/pricing/record`, SQL и transaction — в `adapter/gorm/pricing/fx`, единственный RAM
cache и monotonic publication — в `adapter/pricing/fx`. Normalize/ошибки/публикация — pricing/domain.
`model.ModelCostFXRow` остаётся только table alias для bootstrap/migrations.

`app/pricingfx` собирает refresh один раз и запускает replica reload once на каждом узле:
немедленное чтение DB, затем minute ticker; scheduled refresh остаётся master-gated system task.
HTTP adapter `adapter/http/pricing/fx` сохраняет 10s timeout, запрет redirects, 1MiB limit и прежний
JSON wire type namespace `service`, чтобы диагностические error bytes не менялись. Package path
принадлежит adapter; прежних service FX bodies/facades нет. Billing читает только опубликованную RAM;
экономика, frozen basis, source/version и quota conversion остаются существующему billing owner.
