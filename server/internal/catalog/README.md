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
- `adapter/catalog/upstream` — HTTP/ETag/body cache, concurrent model/vendor fetch и source versions.
- `adapter/gorm/accounts` сохраняет account cache/DB preference/group reads. Цена и alias matching
  приходят через capabilities прежних владельцев; алгоритмы pricing не копируются.
- `relay/modelcatalog` владеет единственным linking-time snapshot: first-wins list, last-wins lookup,
  отдельные live dashboard overrides. `relay/modelview` владеет OpenAI/Anthropic/Gemini wire formats.
  Бизнес использует plain facts через `adapter/gateway/catalog`, без relaykit imports.

Domain содержит metadata rules, общие mutation/enrichment rules и business errors. Новые
availability/prefill facts находятся в чистых leaf packages `domain/availability`, `domain/prefill`.
Сериализация SQL JSON и HTTP raw JSON принадлежат соответствующим codecs; поля копируются явно.

## Мосты и остатки

Controller delegates обслуживают существующие router registrations; удалить при прямой router
assembly. `model.Model`, `Vendor`, `PrefillGroup` и сопутствующие constants/codecs — record aliases
для migrations и pricing readers, без исполнительных metadata фасадов.

`model.NewCatalogPersistence` сохраняет composition с единственными metadata mutex/transaction,
option-row anchor и private PricingMutation callback. Это общая граница с pricing/options до
появления их владельца; вся catalog/model cleanup не завершена. Root `catalog/domain` пока
импортирует common для codecs/version hashes: при cleanup нужно вынести зависимость и объединить
чистые leaf packages обратно, сохранив версии и байты. Новые availability/prefill usecases root
с этой зависимостью не импортируют.
