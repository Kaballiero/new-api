# pricing — цены для клиентов и конфигурация цен

## Ответственность

Модуль владеет тем, как платформа показывает и настраивает цены моделей:
- каталог цен (`Pricing`) — снимок, собранный из моделей, каналов, коэффициентов, billing-выражений и нативных цен upstream; его ревизия (`catalog_revision`) и публикация;
- проекция цены для потребителя (`Projection`): тариф группы, FX-пересчёт в рубли, страницы, обзор, детальная карточка модели, публичный каталог, минимальные цены предложений upstream;
- конфигурация цен моделей в опциях (`Configuration`): версии цены модели, проверка значений и выражений, встроенные цены;
- сравнение коэффициентов с upstream (ratio sync).

Инварианты:
- значения цен, коэффициенты, состав каталога, ревизии, публикация и кэш потребительского каталога — источник истины для страниц цен; рефакторинг их не меняет;
- чтение с `expected_revision` отдаётся только для той же ревизии, иначе 409 `pricing_catalog_revision_mismatch`;
- изменение цены модели применяется только к версии, которую видел клиент (`expected_version`), иначе конфликт `ErrModelPricingConflict`;
- публикация выбранного upstream-каталога и синхронизация идут только для неизменённого источника (`ErrCatalogSourceChanged`).

Понятия и бизнес-ошибки — в `domain/` (проекция, конфигурация, каталог, ratio sync, нормализация нативных цен, `errors.go`). Правила billing-выражений — `pkg/billingexpr/expr.md`.

## Возможности

| Сценарий | Что делает | Маршрут и доступ |
|---|---|---|
| `get_pricing` | классический список цен `/api/pricing` с группами, эндпоинтами и `pricing_version` | `GET /api/pricing`, `HeaderNavModuleAuth("pricing")` |
| `get_personal_pricing` | тариф текущего пользователя: полный каталог или страница (`limit`, `offset`, фильтры) | `GET /api/user/effective-pricing`, `UserAuth`, `DisableCache` |
| `get_model_pricing` | карточка одной модели для текущего пользователя (`model_name`) | `GET /api/user/effective-pricing/model`, `UserAuth`, `DisableCache` |
| `get_group_pricing` | тариф заданной группы (`group`, `using_group`), полный или постранично; анонимный полный каталог кэшируется по группе | `GET /api/pricing/effective`, `RootAuth`, `DisableCache` |
| `get_pricing_overview` | обзор по модальностям (по 4 модели в секции по умолчанию) | `GET /api/pricing/effective/overview`, `RootAuth`, `DisableCache` |
| `get_public_models` | публичный каталог тарифа `default` по тегам (`tag`, `p`, `page_size`) | `GET /api/pricing/effective/models`, без авторизации, `DisableCache` |
| `preview_pricing` | предпросмотр тарифа выбранной группы (`user_group`) без смены группы | `GET /api/option/effective_pricing`, `RootAuth`, `DisableCache` |
| `get_pricing_config` | конфигурация цен моделей с версиями и схемами usage плагинов | `GET /api/option/model_pricing`, `RootAuth` |
| `update_pricing_config` | изменение цен моделей по версиям; аудит `model.pricing.update` | `PATCH /api/option/model_pricing`, `RootAuth` |
| `reset_model_ratios` | сброс `ModelRatio` к значениям по умолчанию | `POST /api/option/rest_model_ratio`, `RootAuth` |
| `get_ratio_config` | открытые коэффициенты для синхронизации другими инстансами | `GET /api/ratio_config`, `CriticalRateLimit` |
| `list_syncable_channels` | каналы с `base_url` и два пресета (официальный, models.dev) | `GET /api/ratio_sync/channels`, `RootAuth` |
| `fetch_upstream_ratios` | загрузка коэффициентов upstream и расхождения с локальными | `POST /api/ratio_sync/fetch`, `RootAuth` |
| `refresh_catalogue` | прогон фоновой задачи обновления нативных цен upstream и публикации каталога | системная задача `pricing_catalog` (`controller/pricing_catalog_refresh.go`) |
| `enqueue_catalogue_refresh` | постановка задачи обновления (`PRICING_CATALOG_TASK_ENABLED`) | без HTTP, `app.NewEnqueuePricingCatalogueRefresh` |
| `sync_upstream_catalog` | синхронизация upstream-каталога в неизменяемые записи | без HTTP, `refresh_catalogue` и outer `app/pricing.go` |
| `ensure_catalog_source`, `publish_catalog_selection` | источник upstream-каталога и публикация выбранных записей | без HTTP, native characterization; persistence API у `adapter/gorm/pricing` |
| `read_upstream_catalogue` | проекция upstream-каталога | без HTTP, `app/pricingruntime` |
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
- Сборка — `internal/app/pricing.go` (`NewPricingHTTP`), один раз через `sync.OnceValue` в `controller/pricing_scenarios.go`; аудит — `recordManageAudit` из `controller/audit.go`.
- Адаптеры: `adapter/pricing` (`Reader`, `ConsumerCache`, `RatioSettings`, `RatioChannels`, плагины), `adapter/pricing/upstream` (HTTP-клиенты ratio sync и нативных цен, override, нормализация), `adapter/gorm/pricing` (опции, каталог, неизменяемые записи, наблюдения).
- Настройки: `setting/ratio_setting`, `setting/billing_setting`, account group policy в settings/accounts. Leaf `app/pricingruntime` собирает статические сценарии once; `adapter/pricing/state` владеет единым опубликованным snapshot, revision/generation/observation state и lock order. DB, колонки, настройки, registry и clock читаются поздно.
- Потребители: `controller/pricing*.go`, `ratio_sync.go`, `ratio_config.go`, `model_pricing_config.go` (HTTP и алиасы типов для тестов); `app/pricingruntime` и технические adapters (сценарии без HTTP).

## Границы и долг

- Модулю не принадлежат каналы (модуль `channels`) и метаданные моделей (модуль `catalog`).
- ⚠ Каналы для ratio sync читаются через `adapter/gorm/channels` в `app/pricing.go` (`GetChannelsByIds`, `GetAllChannels`, `GetChannelById` + `GetNextEnabledKey` для ключа OpenRouter), а не через сценарии `channels`.
- ⚠ `adapter/gorm/pricing` и `adapter/pricing/upstream` используют записи `adapter/gorm/catalog/record` и `adapter/gorm/channels/record` напрямую.
- `domain` и сценарии транзитивно не зависят от `common`, `billingexpr`, `jsplugin` и `catalogsync` и не импортируют gateway DTO. Единственный разрешённый gateway leaf — прежняя цепочка `constant → relaykit/types → relaykit/relayconvert/kitutil` для EndpointType/FinishReason. Правило `pricing-constant-alias-leaf` проверяет точные пакеты и рёбра; direct business imports gateway и любые другие gateway paths запрещены. Выделение constant leaf относится к необязательной волне 9. Offer/document/schema/tier/display facts принадлежат pricing; `Capabilities` явно передаёт codecs, expression/catalog readers и endpoint rules. Арифметика и expression engine остаются в прежних пакетах.
- `adapter/pricing` преобразует native facts по полям с сохранением wire-представления. Enum labels кодируются прежним `jsplugin.LocalizedText.MarshalJSON` в raw display facts, поэтому nil label остаётся `{}`; description сохраняет map и прежний `omitempty`. Typed persisted catalog decoder сохраняет прежние SDK wire-типы и исходные UnmarshalTypeError Type/Struct/Field/Offset facts для domain containers, включая точные тексты ошибок. Alias resolver собирается один раз на snapshot и вызывает прежний `catalogsync.ResolveAlias`.
- Ratio request/result DTO принадлежат своим сценариям; общий `RatioDifference` — domain. HTTP передаёт прежний Gin binder codec-адаптеру: тот использует исходный wire request и validation, затем отображает его в plain Request. Поэтому названия типов в диагностике binding остаются прежними. `AdvancedEndpoints` и `ChannelCodec` оставляют relay DTO и их parsing в адаптере.
- Сценарии создаёт app. `adapter/settings/publication` предоставляет один publisher/lock для settings, legacy pricing options и native publication; каталожные удаления получают same-tx техническую capability.
- Бизнес-ошибки принадлежат pricing/domain; model сохраняет только строгие table aliases для immutable catalog records.
- Имена пресетов «官方倍率预设» / «models.dev 价格预设» в `list_syncable_channels` — идентификаторы, с которыми сравнивает клиент (`client/src/features/system-settings/models/constants.ts`); не локализуются.
- `consumerContext` продублирован в пяти сценариях чтения (`get_personal_pricing`, `get_group_pricing`, `get_model_pricing`, `get_pricing_overview`, `get_public_models`).

ResetModelRatios пишет через settings owner из app, сохраняя дополнительную PublishRatios после Persist.
Pricing/catalog composition принадлежит leaf app; технические records/cache/publication state находятся
в adapters. Одноразовая request-specific SQL mutation capability передаётся shared mutation usecase
при Execute, без отдельного postcommit вызова и без общего изменяемого request state.

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
