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
| `sync_upstream_catalog` | синхронизация upstream-каталога в неизменяемые записи | без HTTP, `refresh_catalogue` и `service/catalog_sync.go` |
| `ensure_catalog_source`, `publish_catalog_selection` | источник upstream-каталога и публикация выбранных записей | без HTTP, `model/upstream_catalog.go` |
| `read_upstream_catalogue` | проекция upstream-каталога | без HTTP, `model/upstream_catalog_projection.go` |
| `rebuild_catalogue` | сборка снимка каталога цен | без HTTP, `model/pricing.go`, `model/pricing_build.go` |
| `read_catalogue` | ревизия опубликованного каталога | без HTTP, `model/pricing_catalogue_revision.go` |
| `publish_native_catalogue` | публикация нативных цен в опции | без HTTP, `model/pricing_catalog_option.go` |
| `observe_catalogue_inputs` | отпечаток входных данных каталога | без HTTP, `model/pricing_inputs.go` |
| `update_pricing_options`, `mutate_pricing_options` | запись опций цен вне HTTP-конфигурации | без HTTP, `model/model_pricing_config.go` |

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
- Настройки и runtime: `setting/ratio_setting`, `setting/billing_setting`, `service` (группы пользователя, FX), `model` (снимок каталога, кэш пользователя, опции).
- Потребители: `controller/pricing*.go`, `ratio_sync.go`, `ratio_config.go`, `model_pricing_config.go` (HTTP и алиасы типов для тестов); `model/*` и `service/catalog_sync.go` (сценарии без HTTP).

## Границы и долг

- Модулю не принадлежат каналы (модуль `channels`) и метаданные моделей (модуль `catalog`).
- ⚠ Каналы для ratio sync читаются прямо из `model` в `app/pricing.go` (`GetChannelsByIds`, `GetAllChannels`, `GetChannelById` + `GetNextEnabledKey` для ключа OpenRouter), а не через сценарии `channels`.
- ⚠ `adapter/gorm/pricing` и `adapter/pricing/upstream` используют записи `adapter/gorm/catalog/record` и `adapter/gorm/channels/record` напрямую.
- ⚠ `domain` импортирует `common` и `relaykit` (`dto`, `types`), сценарии `fetch_upstream_ratios`, `list_syncable_channels`, `rebuild_catalogue` — `relaykit/dto`: записи `business-imports-gateway`, `domain-transitive: internal/pricing/domain` и все `usecase-transitive: internal/pricing/*`.
- ⚠ `model/*` и `service/catalog_sync.go` создают сценарии модуля сами (`legacy-imports-internal`).
- `model.ErrModelPricingConflict` и `model.ErrCatalogSourceChanged` — алиасы `domain.ErrModelPricingConflict` и `domain.ErrCatalogSourceChanged`.
- Имена пресетов «官方倍率预设» / «models.dev 价格预设» в `list_syncable_channels` — идентификаторы, с которыми сравнивает клиент (`client/src/features/system-settings/models/constants.ts`); не локализуются.
- `consumerContext` продублирован в пяти сценариях чтения (`get_personal_pricing`, `get_group_pricing`, `get_model_pricing`, `get_pricing_overview`, `get_public_models`).
