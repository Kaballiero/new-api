# Usage — запись, чтение и аналитика потребления

## Ответственность

Модуль владеет записью результата gateway-запроса: used quota/request counters, consume/error rows и фактами для их content/other. Цена, расчёт списания, резерв, settlement/refund и политика неизвестной стоимости остаются в billing/gateway. Чтение логов, usedata, агрегаты и рейтинги входят в модуль. Аудит принадлежит audit; Task/Batch/Midjourney accounting — tasks/batch и billing.

## Возможности

| Сценарий | Действие и потребитель |
|---|---|
| `count_consumption` | Увеличить user used quota/request count, затем channel used quota через capabilities adapter/gorm/counters; ordinary billable до settlement, OpenRouter applied quota после settlement, включая zero |
| `record_consumption` | Записать consume row через прежний writer; text, audio, realtime и channel test. Disabled consume не отключает counters/settlement |
| `record_failure` | Записать upstream-attempt или delivery-failure row; сценарий владеет early upstream admission и late delivery gate |

Запись вызывают relay endpoints, playground и channel-test handler; общий upstream writer используется также task-submission error path. HTTP read handlers находятся рядом со сценариями. Пути, middleware и полномочия не меняются; controller сохраняет однострочные bindings; once-сборка находится в app/httpapi. Фоновые циклы собирает internal/app/usage, а запускает main в прежних точках.

## Зависимости и сборка

Domain содержит plain UsageRecord/Consumption/RequestFacts и правила delivery priority, usage source,
wildcard модели, subscription display и selection/finalization notes. Outcome/ProviderBasis/Clamp
принадлежат billing/domain. Stored JSON/raw metadata — adapter/gorm/usage/record, без gateway types в бизнесе.

Сценарии объявляют узкие Counter/Writer ports. Count выбирает эффект до/после settlement; counter SQL
сохраняет fire-and-forget error contract. RecordConsumption разделяет Describe (notes/display reads)
и Persist (финализация/запись). RecordFailure сохраняет ранний Admission и поздний delivery gate.
`app/usage` собирает stateless scenarios, `relay/usage` отображает Gin/RelayInfo в plain facts и
получает одну явную Recording capability. Cost → count → settle → record порядок прежний.

`adapter/gorm/usage.Writer` владеет log writer/observation и одним RAM quota-export cache.
`app/usagestorage` передаёт поздние DB/LOG_DB, username и settings readers. Export flush держит lock
через I/O и очищает cache даже при ошибке; общий queue в adapter/gorm/counters делает swap-before-I/O.
`app/usage` запускает export немедленно, затем через DataExportInterval с поздним enabled gate;
общий counter scheduler/shutdown — app/background.go. `adapter/gorm/dbclock` сохраняет dialect branches.

## Чтение и доступ

| Сценарии | Правила | База |
|---|---|---|
| getapi_aggregate / getapi_logs | `POST /api/getapi/usage/aggregate` — пачка пользователей, нетто по моделям/времени/ключам; `POST /api/getapi/usage/logs` — история с keyset по настоящему id; GetAPI capability `getapi.usage.read` | main DB / LOG_DB |
| admin_list_logs / list_own_logs | роль/owner, filters, pagination, admin metadata projection | LOG_DB |
| admin_get_log_stats / get_own_log_stats | consume-only; own username из authenticated context, clock после filters | LOG_DB |
| admin_list_usage_data / admin_list_user_usage_data / list_own_usage_data | quota_data, owner/filter/period policy | main DB |
| admin_get_usage_flow / get_own_usage_flow / get_rankings | intervals, grouping, ranking windows и TTL | main DB и runtime cache |
| get_billing_usage | `/v1/dashboard/billing/usage`, прежний account/token read order и error overwrite, integer sum до float | main DB |

GetAPI-ручки читают расход по `user_id` без PAT владельца; capability выдаётся доверенной серверной интеграции. Логи сохраняют настоящие id, скрывают channel и привилегированные metadata; при ClickHouse возвращают `501 GETAPI_USAGE_UNSUPPORTED`.

Точные пакеты read-сценариев находятся в модуле; HTTP использует dto и transport без ORM.
Handlers собирает app/usage, compatibility — app.NewBillingCompatibilityHTTP, bindings — controller.
`model` содержит только migration-used Log/QuotaData aliases. LogOther и status constants consumers
обращаются прямо к owning record. Task/Batch/payment adapters получают writer capabilities через app.
Cleanup log SQL принадлежит adapter/gorm/usage.Cleanup; system координирует task-state в main DB
и отдельное удаление из LOG_DB, без общей transaction.

## Границы и долг

Counters, settlement, consume rows и export не atomic (#35); export clear-on-failure сохраняется (#37),
queue не durable и не requeue-ит failed writes (#38). Stats/rankings semantics и order/cache gaps — #39–40;
LOG_DB/ClickHouse LIMIT и cleanup consistency — #41. Nil context не расширяет прежний writer contract.
Исторические persisted texts не мигрируются; renderer использует текущие i18n templates.
Domain/usecase-регрессии находятся рядом с владельцами; metadata codec и SQL-регрессии — рядом с owning adapter.
Добавленная snapshot suite чтения и billing usage удалена по решению владельца.

Логи поддерживают `types` (1..7, comma-separated GET или массив POST); он заменяет одиночный `type`. Статистика возвращает gross `quota` и отдельный `refund_quota`; rpm/tpm считают только consume. Коррекции settlement и возвраты меняют quota_data без нового request count; refund имеет отрицательную quota и сохраняет исходный node. `/api/status` публикует `usage_refund_accounting: true`.
