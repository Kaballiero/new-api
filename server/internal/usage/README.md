# Usage — запись, чтение и аналитика потребления

## Ответственность

Модуль владеет записью результата gateway-запроса: used quota/request counters, consume/error rows и фактами для их content/other. Цена, расчёт списания, резерв, settlement/refund и политика неизвестной стоимости остаются в billing/gateway. Чтение логов, usedata, агрегаты и рейтинги входят в модуль. Аудит — отдельная волна 3b; Task/Batch/Midjourney/violation billing — волна 5.

## Возможности

| Сценарий | Действие и потребитель |
|---|---|
| `count_consumption` | Увеличить user used quota/request count, затем channel used quota через прежние model primitives; ordinary billable до settlement, OpenRouter applied quota после settlement, включая zero |
| `record_consumption` | Записать consume row через прежний writer; text, audio, realtime и channel test. Disabled consume не отключает counters/settlement |
| `record_failure` | Записать upstream-attempt или delivery-failure row; сценарий владеет early upstream admission и late delivery gate |

Запись вызывают relay endpoints, playground и channel-test handler; общий upstream writer используется также task-submission error path. HTTP read handlers находятся рядом со сценариями. Пути, middleware и полномочия не меняются; controller сохраняет однострочные bindings; once-сборка находится в app/httpapi. Фоновые циклы собирает internal/app/usage, а запускает main в прежних точках.

## Зависимости и сборка

- `domain` содержит plain UsageRecord/Consumption/RequestFacts/metadata types и правила delivery priority, usage source, wildcard модели, subscription display и selection/finalization notes. Нет Gin, ORM, Redis, common и relay/relaykit. Settlement Outcome и ToolSurchargeItem читаются из domain модуля-владельца billing по правилу межмодульных зависимостей; копий этих типов нет. ProviderBasis и Clamp — типизированные понятия billing. Raw upstream cost details и открытый affinity audit передаются вне business domain через технический recording context; JSON tags/RawMessage/хранимые basis/tool/settlement/diagnostic/rule shapes принадлежат adapter. Serializer и тип logMetadata закрыты; их JSON assertions находятся в тестах adapter, gateway tests проверяют mapping facts.
- Узкие Counter/Writer ports объявлены у потребителей. Record Execute принимает context.Context и Input/usage facts. Count Execute принимает явные facts и момент BeforeSettlement/AfterSettlement, сам выбирает эффект. Это осознанное исключение из формы Execute/error: model primitives работают fire-and-forget, складывая batch updates в RAM либо логируя direct-write error внутри; результата для возврата у Counter нет. Writer возвращает техническую ошибку при неподдерживаемом context; сценарии и gateway её обрабатывают. Ошибки DB прежние model writers логируют без возврата, поэтому nil не доказывает durable запись. RecordConsumption имеет один Execute: Describe выбирает и фиксирует notes/display reads до settlement, Persist завершает notes/model и пишет row. Audio price readers инъецированы в этот сценарий и наружу не выдаются. RecordFailure владеет delivery reason и обеими enabled gates. Единственный Execute имеет этапы AdmitUpstream/Persist: первый выдаёт Admission с закрытым полем в прежней ранней точке, второй использует этот снимок для upstream либо читает late gate для delivery. Нулевой Admission не разрешает upstream write; создать разрешение извне нельзя. Renderer форматирует и соединяет все notes в одном месте. Consumption.ProbeLabel — вход channel test; RenderedContent — результат рендеринга для storage, роли не смешаны.
- `relay/usage.Recording()` — явный временный composition bridge через sync.OnceValue(app.New), без package-init construction. Только app знает конкретные storage/renderer implementations; gateway получает usecases и TaskMetadata port. `internal/app/usage.New` собирает stateless scenarios; настройки consume/export/error и display ratios читаются поздно через функции, а не при package init.
- `relay/usage` переводит Gin/RelayInfo/DTO snapshots в usage facts. Gateway billing сохраняет последовательность cost → count → settle → record. Failure metadata capture защищён от отсутствующего Request; cancellation/path fallback не обращаются к нему без проверки.
- `internal/adapter/gorm/usage` владеет SQL чтения main/LOG_DB, проекцией DTO и exact metadata codec в record/log_other.go. Типы Log/QuotaData находятся в record, model/usage_records.go оставляет чистые type/const aliases для миграций и legacy writers. Schema/tags, LOG_SQL_DSN routing и таблицы прежние. Plain model.WriteConsumeLog содержит одну body и по одному чтению consume/export в прежних точках; legacy RecordConsumeLog делегирует ей для оставшихся Task/MJ/violation consumers. Неиспользуемый RecordErrorLog и одноразовый RecordConsumeLogWithSettings удалены. Legacy metadata consumers создают new(model.LogOther) через разрешённый type alias, без model factory bridge.
- Числовую FX validity adapter проверяет через billing domain PublishedUSD/ApplyBillingFX; локально проверяются только согласованность captured rate/factor и наличие audit metadata.
- Технический adapter снимает metadata и plain LogObservation с context.Context, не импортирует Gin и не восстанавливает его (#36 закрыт). relay/usage.RecordingContext захватывает Identity/ClientIP callbacks; model plain writers читают их в прежних поздних точках. Ненулевой Gin без Request поддерживается на gateway boundary; запись работает и с plain observation без Gin. Неподдерживаемый context возвращает прежнюю техническую ошибку до effects. URLPresent отличает отсутствующий URL от существующего URL с пустым Path.

## Порядок и совместимость

Billing возвращает Billable как готовый факт для audio/realtime, usage не повторяет zero/fixed rule. Для text gateway вызывает count до и после settlement; для audio/realtime — только до, как в старом коде; сценарий выбирает billable ordinary до settlement либо provider applied quota после settlement (включая zero). Ordinary text/audio counters идут до settlement; ошибки text/audio settlement логируются, а consume продолжает записываться. OpenRouter counters/log quota берутся из funding-applied outcome после settlement. При realtime terminal error уже обновлённые counters сохраняются, consume row не пишется. Политика zero/fixed audio отличается от realtime и сохранена.

Consume записывается синхронно, затем отдельная delivery error row с тем же other; при техническом отказе consume adapter повторная попытка delivery с тем же неподдерживаемым context не выполняется; perf job остаётся gateway-эффектом в прежней gopool границе. Upstream errors пишутся на каждом failed attempt до retry/deferred refund. Новый общий transaction/retry/rollback или durable exact-once не добавлен. Data export остаётся RAM QuotaData после попытки consume insert, даже при insert error; при disabled consume export пропускается.

Persisted templates request consumption переведены на fixed English отдельным изменением после Chinese move. Logger владеет единым quota formatter: legacy LogQuota сохраняет китайские bytes, LogQuotaEnglish использует английские unit words и ASCII $ только для USD; ¥ и custom symbol не меняются. Currency math и числовое форматирование прежние. Исторические rows не мигрируются. По решению пользователя A6 channel-test label остаётся без изменений: MsgChannelTestLogName уже переводится на язык пользователя-получателя и не является китайским literal. Content и token_name сохраняют прежнее равенство, immutable тест не меняется. Task/MJ/violation texts — волна 5.

Единственный service.BillingGateway bridge остаётся для реальных Task/Batch/MJ/violation consumers и immutable billing/WSS facades. TaskMetadata явно помечен BRIDGE с consumers service Task/Batch и relay MJ. Конкретные MidjourneyOther/AppendTaskBillingFX/QuotaSaturation/Clamp/Tiered/Plugin делегируют единственному usage serializer. Старые service RecordTextCost/AudioCost/RealtimeCost, formatting helpers и ordinary SettleBilling facade удалены; mutable tests перепривязаны к владельцу. Новых architecture allowlist entries нет.

## Проверки и границы доказательств

28 usage_ goldens записаны на origin/main, повторены дважды и сравнены с веткой до переноса (commit 2dd83ad14); вместе с 126 billing goldens — immutable 154-file baseline. Oracle и fixture находятся рядом в `internal/billing/{contract,usage_contract}_test.go`, не копируются в usage. Новые unit tests проверяют count policy до/после settlement, порядок user/channel, applied zero/nonzero, note selection/finalization и display-read order, renderer joining и точные stored shapes. RecordFailure имеет собственный usecase_test.go для early upstream admission, delivery late gate и error propagation; прежние delivery/source/tool tests перенесены в `relay/usage`.

`adapter/gorm/usage/TestUsageAdapterPostgres` требует TEST_POSTGRES_DSN с CREATE SCHEMA; создаёт две собственные schemas, задаёт search_path всем pooled connections, проверяет версию/current_schema и удаляет schemas при cleanup. Без DSN это SKIP, не PASS.

Sandbox checks не заменяют host HTTP/Redis/WSS/full suite и real MySQL/PostgreSQL. Сценарии и SQL fixtures используют отдельные SQLite DB, PostgreSQL schemas с search_path на каждом соединении и временные MySQL databases. DSN без прав изоляции — ошибка; отсутствие DSN — SKIP, не доказательство совместимости. Optional ClickHouse проверяется отдельным LOG fixture.

## Read side (волна 3a)

| Сценарии | Решение | Хранилище |
|---|---|---|
| `admin_list_logs`, `admin_query_user_logs` | admin/root visibility; strict bounded query, dedup IDs | LOG_DB; token ownership — main |
| `list_own_logs`, `list_key_logs` | ownership до чтения, user visibility, literal model для token-filter, count/recent limits | LOG_DB; credentials — main |
| `admin_get_log_stats`, `get_own_log_stats` | consume-only; own username из authenticated context; rate window clock читается после проверки filters | LOG_DB |
| `admin_list_usage_data`, `admin_list_user_usage_data`, `list_own_usage_data` | legacy/bucket choice, grouping и прежние разные range/identity rules | main quota_data |
| `admin_get_usage_flow`, `get_own_usage_flow` | role dimensions, token/channel name enrichment, fallback labels | main quota_data/users/tokens/channels |
| `aggregate_logs`, `aggregate_usage_data` | bounded scope, deleted-user inclusion, row/integer limits | main users + LOG_DB logs / main quota_data |
| `get_rankings` | rolling/previous periods, vendor mapping, rank/share/history, cache TTL | main quota_data + catalog scalar metadata; memory cache |

`transport` содержит общие parsers/envelopes/i18n, не SQL и не orchestration. Deprecated search handlers — прямой transport response, без пустого usecase. domain.VisibilityPolicy описывает разрешения, не persisted JSON; codec применяет их с сохранением raw lexemes и исторического rejection precedence. Adapter не выбирает роль/лимиты/ранжирование. HTTP DTO и record раздельны.

Quota export сохраняет один RAM cache в `adapter/gorm/usage`, общий batch queue — `adapter/gorm/counters` (A2, волна 7j). usage app запускает quota flush немедленно при старте и далее через DataExportInterval минут с поздним export gate; общая counter composition internal/app/background.go запускает batch writer со sleep-first interval и прежнее shutdown поведение. Adapter flush сохраняет lock-through-I/O, ignored errors и clear-on-failure. Пустого flush usecase нет. system_task orchestration принадлежит internal/system; CountOldLog/DeleteOldLogBatch — adapter/gorm/usage.Cleanup с поздним LOG_DB, прежним ClickHouse branch и LIMIT semantics. Main task-state и log deletion не atomic; real-dialect acceptance остаётся отдельной проверкой (#41).

347 `usage_read_*` goldens и default-locale origin baseline из commit 5ca688857 immutable. Parity cases посылают explicit zh-CN/zh-TW. English default и ru проверяются отдельно exact old→new таблицей; неизвестные storage errors не нормализуются. Commit 0 (05edd5ad1) устраняет wave-1 регрессию raw search errors до записи baseline.

Tool note рендерится как `%s calls: %d, cost %s` (прежде `%s called %d times, cost %s`); исторические DB rows не мигрируются.

Исправление review 1: admin_list_user_usage_data и list_own_usage_data явно сбрасывают Username,
поскольку /api/data/users и /api/data/self никогда не принимали этот фильтр. Остальные query dimensions
сверены с прежними handlers/SQL. Ошибки сопоставляются по errors.Is/errors.As с i18n.MsgUsage*;
InvalidRankingPeriod несёт period отдельным полем. Единственное usage Translate сохраняет старые
Simplified bytes для zh-TW. Domain policy содержит только permissions; malformed/rejection codec
правила выводит сам record codec. Log kind constants имеют одного владельца domain; record экспортирует
их для model alias compatibility (кратчайшая цепь при запрете model→domain импорта).

## Технические владельцы после волны 7j

`adapter/gorm/usage.Writer` содержит единственные log writer/observation и RAM quota-export cache.
`app/usagestorage` передаёт поздние DB/LOG_DB, username и settings capabilities; отдельный LOG_DB сохраняется.
Flush export сохраняет lock-through-I/O, clear-on-failure и ignored-error порядок; counter queue живёт отдельно
в `adapter/gorm/counters` и сохраняет swap-before-I/O. `adapter/gorm/dbclock` сохраняет SQL dialect branches.
Task/Batch/payment/notification adapters получают account/key/writer capabilities через app; копий состояния нет.

`get_billing_usage` отвечает за `/v1/dashboard/billing/usage`: account/token read order, прежнее перетирание
ошибки второго чтения, integer sum before float, единицы и error envelope сохранены. Handler собирается
`app.NewBillingCompatibilityHTTP`; controller entrypoint остаётся bridge до router binding. Native observation
test перемещён из model к writer owner; новые raw cases находятся в accounts compatibility corpus.
Host/PostgreSQL и настоящий Redis проверяются отдельно. SQLite/MySQL not checked.
