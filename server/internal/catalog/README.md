# Каталог моделей

Модуль владеет model/vendor metadata, административными списками, синхронизацией выбранных
метаданных, prefill groups и политикой доступности моделей. Pricing владеет ценами, channels —
маршрутизацией; billing и relay execution находятся вне модуля.

## Сценарии

| Группа | Сценарии и вход |
|---|---|
| Model metadata | list_models, search_models, get_model, create_model, update_model, delete_model, delete_models_batch: `/api/models/`, `/search`, `/:id`, `/delete`, AdminAuth |
| Vendor metadata | list_vendors, search_vendors, get_vendor, create_vendor, update_vendor, delete_vendor, preview_vendor_operation, apply_vendor_operation: `/api/vendors`, AdminAuth |
| Metadata sync | preview_metadata_sync / apply_metadata_sync: `/api/models/sync_upstream/preview` и `/sync_upstream`, AdminAuth |
| Missing metadata | list_missing_models: `/api/models/missing`, AdminAuth; literal names, прежнее различие `[]` и `null` |
| Availability | list_available_models: существующие gateway model-list routes; group/auto-group selection, token limit, configured-price/reserve eligibility и preferred owner |
| Static lookup | get_registered_model: gateway retrieve без availability filtering; существующий unknown-model HTTP200 error |
| Presets | list_model_presets: GET `/api/models`, UserAuth; live plugin overwrite поверх linking-time snapshot |
| Prefill | list_prefill_groups, create_prefill_group, update_prefill_group, delete_prefill_group: `/api/prefill_group`, AdminAuth |

Metadata scenarios сохраняют проверки имени/видимости/name-rule/endpoints, версии vendor/sync,
запрет удаления referenced vendor и root-only removal of pricing. Модельные удаления сохраняют
locked exact selections, routing/abilities writes в том же transaction и refresh после commit.
Vendor operations поддерживают assign/merge/delete; sync проверяет всю selection до первой записи.

Prefill сохраняет exact type filter, updated_time DESC, global name uniqueness check вне записи,
whole-record Save с upsert и пустыми/нулевыми полями, soft-delete и raw JSON Items.
Metadata и prefill используют разные status/error envelopes; транспорт сопоставляет domain errors.

## Владельцы и сборка

- `internal/app/catalog.go` собирает metadata handlers и единый upstream runtime. Request audit
  выполняется HTTP handler по Output после успешного Execute; usecase не получает audit
  callback или Gin context. Порядок refresh → audit → response сохраняется.
- `internal/app/catalog_availability.go` собирает availability reader/policy и gateway writers;
  `catalog_prefill.go` — prefill handlers. Все используют once assembly и поздние runtime readers.
- `adapter/gorm/catalog/record` владеет models/vendors/prefill records, SQL codecs и persistence.
  `adapter/gorm/catalog` даёт consumer ports и owner/reserve queries с прежней фильтрацией/порядком.
- `adapter/catalog.Codec` декодирует endpoints в типизированные факты и формирует версии тем же JSON encoder и SHA256; metadata preview получает через него прежнее JSON-представление полей.
- `adapter/catalog/upstream` — HTTP/ETag/body cache, concurrent model/vendor fetch и source versions.
- `adapter/gorm/accounts` сохраняет account cache/DB preference/group reads. Цена и alias matching
  приходят через capabilities прежних владельцев; алгоритмы pricing не копируются.
- `relay/modelcatalog` владеет единственным linking-time snapshot: first-wins list, last-wins lookup,
  отдельные live dashboard overrides. `relay/modelview` владеет OpenAI/Anthropic/Gemini wire formats.
  Бизнес использует plain facts через `adapter/gateway/catalog`, без relaykit imports.

Domain содержит чистые metadata/enrichment rules, business errors и общие availability/prefill facts
(`AvailableModel`, `PrefillGroup`). JSON codecs и version hashing приходят через явные capabilities.
MetadataMutation принадлежит apply_metadata_sync; create_model и update_model объявляют собственные
узкие ModelMutation для своих транзакций.
Сериализация SQL JSON и HTTP raw JSON принадлежат соответствующим codecs; поля копируются явно.

## Границы

Controller содержит однострочные router bindings; HTTP callbacks собираются в app/httpapi.
model.Model, Vendor, PrefillGroup — строгие table aliases для bootstrap/migrations.
Constants, JSONValue, BoundChannel и ModelSquareState consumers используют у record owner,
без non-table re-exports в model.

`adapter/gorm/catalog` владеет единственными metadata mutex, transaction и option-row anchor.
Leaf `app/pricingruntime` передаёт техническую PricingMutation capability: deletion и правки pricing
выполняются на одном tx, с прежним порядком блокировок и publication после commit. Routing/ability
записи обслуживает channels adapter на том же tx. GroupColumn читается поздно из единственного
bootstrap state в model. Domain/usecases транзитивно не зависят от common, транспорта, ORM и Redis.
