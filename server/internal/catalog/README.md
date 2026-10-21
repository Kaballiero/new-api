# catalog — каталог моделей и вендоров

## Ответственность

Модуль владеет метаданными моделей (`models`) и вендоров (`vendors`), которые видят администраторы и клиенты на странице моделей: имя и правило сопоставления имени (exact / prefix / suffix), описание, иконка, теги, эндпоинты, видимость в каталоге, флаг официальной синхронизации, привязка к вендору.

Модуль отвечает за инварианты каталога:
- имя модели не пустое и уникально; имя вендора не пустое, уникально без учёта регистра, имя и иконка не длиннее 128 символов;
- видимость — только `0` или `1`, правило имени — одно из известных, эндпоинты — корректный JSON (путь начинается с `/`, метод HTTP из списка);
- изменение вендора и операции над вендорами (`assign` / `merge` / `delete`) применяются только к той версии, которую показал предпросмотр (`version`, `expected_version`); иначе — конфликт;
- вендор, на которого ссылаются модели, нельзя удалить;
- синхронизация метаданных из upstream применяется только к выбранным в предпросмотре моделям и только к неизменённым с тех пор записям; модели с `sync_official = 0` не синхронизируются;
- удалять цены модели вместе с моделью может только root.

Понятия и бизнес-ошибки описаны в `domain/`:
- `Model`, `Vendor`, состояния в каталоге (`ModelSquareState`) и их расчёт по каналам;
- план операции над вендорами (`VendorOperationPlan`) и ошибка ссылок (`VendorReferenceError`);
- значения и версии синхронизации метаданных (`MetadataValues`, `MetadataRecordVersion`, `VendorRecordVersion`);
- ошибки (`errors.go`).

## Возможности

Все маршруты идут через `AdminAuth` (сессия панели или PAT администратора). Сценарии с аудитом (см. таблицу) пишут его через `recordManageAudit` из `controller/audit.go`; создание и изменение моделей аудит не пишут.

| Сценарий | Что делает | Маршрут |
|---|---|---|
| `list_models` | список моделей с состоянием в каталоге, каналами и счётчиками по вендорам; фильтры `status`, `sync_official`, `square_state`, `include_channel_models` | `GET /api/models/` |
| `search_models` | то же с поиском по `keyword` и `vendor` | `GET /api/models/search` |
| `get_model` | одна модель с каналами и состоянием в каталоге | `GET /api/models/:id` |
| `create_model` | создание модели, ответ 201 | `POST /api/models/` |
| `update_model` | изменение модели или только видимости (`?status_only=true`) | `PUT /api/models/` |
| `delete_model` | удаление модели; `remove_from_channels` убирает exact-модель из каналов, `remove_pricing` (только root) — её цены; аудит `model.delete` | `DELETE /api/models/:id` |
| `delete_models_batch` | пакетное удаление (до 1000 id) с теми же флагами; аудит `model.delete_batch` | `POST /api/models/delete` |
| `list_missing_models` | включённые в каналах модели без записи в каталоге | `GET /api/models/missing` |
| `preview_metadata_sync` | предпросмотр синхронизации метаданных из upstream (`?locale=`): кандидаты, поля, версии | `GET /api/models/sync_upstream/preview` |
| `apply_metadata_sync` | применение выбранных изменений; создаёт недостающих вендоров; аудит `model.metadata.sync` | `POST /api/models/sync_upstream` |
| `list_vendors` | список вендоров с версией | `GET /api/vendors/` |
| `search_vendors` | поиск вендоров (`keyword`, `association`) | `GET /api/vendors/search` |
| `get_vendor` | один вендор с версией | `GET /api/vendors/:id` |
| `create_vendor` | создание вендора, ответ 201; аудит `vendor.metadata.save` | `POST /api/vendors/` |
| `update_vendor` | изменение вендора с проверкой версии; аудит `vendor.metadata.save` | `PUT /api/vendors/` |
| `delete_vendor` | удаление вендора без ссылок; аудит `vendor.metadata.delete` | `DELETE /api/vendors/:id` |
| `preview_vendor_operation` | предпросмотр `assign` / `merge` / `delete` над вендорами и его версия | `POST /api/vendors/operations/preview` |
| `apply_vendor_operation` | применение операции по версии предпросмотра; аудит `vendor.<action>` | `POST /api/vendors/operations` |
| `enrich_models` | дополняет модели каналами, группами и состоянием в каталоге | без HTTP, `app.NewEnrichModelsUsecase` |
| `list_configured_models` | модели каталога вместе с моделями каналов без записи | без HTTP, через `model.SearchModelsWithChannels` |
| `insert_model_record`, `update_model_record` | запись модели с проверкой вендора | без HTTP, через `model.Model.Insert` / `Update` |
| `insert_vendor_record`, `update_vendor_record` | запись вендора с проверкой имени и версии | без HTTP, через `model.Vendor.Insert` / `Update` |
| `remove_vendors` | удаление вендоров по id | без HTTP, через `model.DeleteVendors` и `model.Vendor.Delete` |
| `apply_metadata_records` | применение обновлений метаданных | без HTTP, через `model.ApplyMetadataSync` |

Ошибки сценариев — значения из `domain/errors.go`. Их переводит в HTTP общий `transport.WriteError`:
- `ErrModelNotFound` / `ErrVendorNotFound` → 404 `model_not_found` / `vendor_not_found`, текст `model not found` / `vendor not found`;
- `ErrModelIDMissing`, `ErrModelNameEmpty`, `ErrVendorIDMissing`, `ErrVendorNameEmpty` → 400 со своим кодом; `ErrModelNameExists`, `ErrVendorNameExists` → 409 со своим кодом; текст из i18n (`model.*`, `vendor.*`; раньше был захардкожен на китайском);
- `ErrModelNameRequired` (пустое имя при изменении) → 200 `{success:false}` с i18n-текстом `model.name_empty`;
- `ErrInvalidSquareState`, `ErrInvalidPagination` → 400 `invalid_params`;
- `ErrPricingRootOnly` → 403 без кода;
- `ErrSyncSourceChanged`, `ErrSyncModelUnavailable` → 409 без кода; `domain.RejectSync(err)` → 409 при `ErrMetadataSyncConflict` в цепочке, иначе 400, текст причины;
- `domain.RejectVendorChange(err, missing)` → 404 `vendor_not_found` для отсутствующей записи; 409 `VENDOR_REFERENCED` с `reference_counts`; 409 `VENDOR_CONFLICT` при `ErrVendorConflict` в цепочке; иначе 400 без кода; текст причины;
- `domain.Internal(err)` → 500 `internal_error` с текстом причины;
- ошибка без доменного смысла (проверки значений, ошибки upstream) → 200 `{success:false}` с её текстом.

OpenAPI-генератор (`cmd/gen-admin-openapi/catalog.go`) берёт DTO и `http.go` сценариев, а коды ошибок — из своей таблицы `catalogHandlerSources`.

## Зависимости

- Чужие `domain` модуль не импортирует.
- Сборка — `internal/app/catalog.go` (`NewCatalogHTTP`), один раз через `sync.OnceValue` в `controller/catalog_scenarios.go`.
- Хранение: `internal/adapter/gorm/catalog` (`Store`, `VendorOperationStore`) поверх `internal/adapter/gorm/catalog/record` (GORM-записи `models`, `vendors`, транзакции и блокировки метаданных из `model`).
- Upstream-метаданные: `internal/adapter/catalog/upstream` (HTTP-клиент с ETag-кэшем; общий `upstream.Runtime` в `app`).
- Побочные эффекты: `model.RefreshPricing`, `model.InitChannelCache`, `model.GetEnabledModels`, `model.GetModelQuotaTypes`, `model.GetModelSupportEndpointTypes`.
- Потребители: `controller/model_meta.go`, `vendor_meta.go`, `model_sync.go`, `missing_models.go` (HTTP); `model/*` (legacy-код: кроме сценариев без HTTP, `model.DeleteModelMetadata` вызывает `delete_models_batch`, `model.PreviewVendorOperation` / `ApplyVendorOperation` — сценарии операций над вендорами, `model.GetMissingModels` — `list_missing_models`); модуль `pricing` читает `catalog/domain` (`Model`, `Vendor`, `ResolveModelMetadata`).

## Границы и долг

- Модулю не принадлежат цены моделей (модуль `pricing`) и каналы (модуль `channels`).
- ⚠ `delete_model` / `delete_models_batch` сами переписывают список моделей каналов (`domain.ChannelRouting`, `model.Channel.UpdateAbilities`) и удаляют цены (`RemovePrices`) в своей транзакции, а не через модули-владельцы.
- ⚠ `adapter/gorm/pricing` использует `adapter/gorm/catalog/record` напрямую.
- ⚠ `model/*` всё ещё импортирует `internal` (записи `legacy-imports-internal` для `model/catalog_persistence.go`, `missing_models.go`, `model_meta.go`, `model_metadata_sync.go`, `pricing_build.go`, `pricing_default.go`, `pricing_inputs.go`, `vendor_management.go`, `vendor_meta.go`): GORM-записи `model.Model` / `model.Vendor` живут в адаптере, а `model` создаёт сценарии каталога внутри себя.
- ⚠ `domain` импортирует `common` (JSON для версий и проверки эндпоинтов), поэтому `domain-transitive: internal/catalog/domain` и все `usecase-transitive: internal/catalog/*` остаются в списке исключений.
- `model.ErrVendorConflict` и `model.ErrMetadataSyncConflict` — алиасы `domain.ErrVendorConflict` и `domain.ErrMetadataSyncConflict`.
