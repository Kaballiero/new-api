# Асинхронные задачи

Модуль владеет durable Task/Midjourney, статусами, учётом и последующим расчётом/возвратом,
опросом провайдеров, dashboard lists/detail, выдачей артефактов и video content. Отправка upstream,
выбор канала, provider wire DTO, retry и plugin runtime остаются в шлюзе. Batch принадлежит отдельному internal/batch,
управление task plugins, origin/query и protocol observation policy — `internal/taskplugins`.

## Сценарии и входы

| Сценарии | Вход и доступ |
|---|---|
| list_own_tasks / admin_list_tasks | GET /api/task/self (UserAuth), GET /api/task (AdminAuth) |
| list_own_midjourney_tasks / admin_list_midjourney_tasks | GET /api/mj/self и /api/mj/; UserAuth / AdminAuth |
| get_own_task | GET /v1/tasks/:key, TokenAuth, только собственная задача |
| list_task_artifacts | GET /v1/tasks/:key/artifacts (TokenAuth), /api/task/:task_id/artifacts (UserAuth) |
| fetch_task_artifact | GET/HEAD /v1/tasks/:key/artifacts/:artifact_key/content; прежний TokenOrTaskArtifactAccessAuth |
| fetch_video_content | GET/HEAD /v1/videos/:task_id/content; TokenAuth |
| record_task_submission | Фиксация execution/tariff/account после принятия upstream, до durable INSERT |
| record_midjourney_submission | Подготовка billing markers до INSERT и debit после persistence, без нового pre-reserve |
| settle_task / refund_task / refund_midjourney_task | Терминальные расчёты и возвраты с прежним порядком funding/token/counters/log/markers |
| begin_sync_task / get_sync_task / commit_sync_reservation | Intent до hold, проверка identity, передача существующего резерва |
| configure_sync_submission / record_sync_generation / record_sync_receipt | Frozen metadata/account и correlation, без повторного paid POST |
| complete_sync_task / fail_sync_task / reconcile_sync_task | Независимые delivery и financial outcome, revision CAS, metadata-only reconciliation |
| hold_sync_task | Сохранение резерва за недоставленный chat-result до ручного решения |
| apply_sync_operator_decision | POST /api/task/:task_id/openrouter-sync/release и /openrouter-sync/settle; RootAuth, подтверждённая нулевая/явная стоимость и аудит каждой попытки |
| poll_tasks / poll_midjourney_tasks | Один отменяемый прогон, без собственного scheduler/ticker |

Router и порядок middleware сохранены. Dashboard допускает прежние session/PAT; API-ключи не становятся
сессиями панели. Midjourney submit/fetch/image/seed/swap и native task routes сохраняют gateway wire
форматы. Закомментированный notify route не добавлен. Capability HMAC, concurrency admission и
удаление access из query остаются в прежних middleware/service; reader лишь использует владельца tasks.

## Хранение и финансовые границы

Records/codecs и SQL — internal/adapter/gorm/tasks. model/task.go и model/midjourney.go содержат только
same-name aliases. Таблицы, миграции и индексы прежние; private JSON дополнен первой причиной результата,
классом последнего lookup и необратимым решением оператора. Чистые domain
facts отделены от records; технические conversion сохраняют raw FX/group presence, null и malformed
history, а также ссылки на state при обновлении revision. Неполная современная история не становится legacy.

Обычный completion/refund сохраняет прежнюю неатомарную последовательность. Task-specific adapter
вызывает существующий billing/apply_quota_delta; lookup token key происходит после funding, ошибки
и partial-applied outcome не маскируются. Tiered/token/FX rounding и quota saturation используют прежних
владельцев billing/engine/common. Новых billing formulas нет.

OpenRouter confirmed settlement: один locked main transaction claim → subscription/user/channel/token;
любой отказ откатывает все DB legs. Owner-checked subscription capability выполняется в том же tx.
После commit следуют cache delta → обязательная финансовая запись → отдельный finalize CAS. MainApplied
не воспроизводится автоматически после сбоя cache/log. Missing/null/invalid cost остаётся unknown;
только подтверждённый terminal zero разрешает соответствующий legacy refund. Definitive 4xx без generation
освобождает audio hold, кроме timeout/rate-limit. Chat text без terminal cost передаёт резерв durable Task:
metadata lookup или root-решение завершают расчёт; отправленная клиенту часть stream учитывается отдельно.
Release разрешён лишь для доказанно недоставленного результата, settle принимает положительную стоимость
до 50 USD на frozen basis; CAS фиксирует решение до движения средств, повтор того же решения не списывает
повторно. Evidence хранится только в root audit; публичная проекция его не содержит. Paid submission не replay-ится.
Standalone subscription delta теперь имеет единственное тело в adapter/gorm/billing; nested refund #27 сохранён.

Usage сохраняет единственный metadata serializer и прежние обычный/required writers.
Task identity/plugin mapping имеет одно тело в adapter/gorm/usage для gateway и settlement. Исторические
логи не переписываются. Структурный слой сохраняет operational/persisted/HTTP text bytes; English/ru
изменения готовятся отдельно неприменённым content package.

## Сборка и технические адаптеры

app/tasks собирает gateway-independent storage/billing/sync capabilities. app/taskruntime подключает
provider factory, HTTP/media adapters и HTTP/poll handlers; controller содержит однострочные bindings, once-сборка находится в app/httpapi.
Фабрика relay/taskregistry — единственный leaf owner platform→plugin resolution. main-only factory,
service.GetTaskAdaptorFunc и новый global locator отсутствуют; tests/binaries получают ту же сборку.

Scheduled system handlers собраны в app/systemruntime, runner/registry — app/system: прежние enabled predicates, DB lease, interval15s,
progress/history и cancellation. Main сохраняет прежний порядок регистрации и запуска. Task poll
сначала timeout sweep, затем обычные platforms, затем до16 due sync tasks с4 workers; MJ отдельный tick.
Media adapter сохраняет streaming ownership, redirect/SSRF/header/range/status rules и закрытие body.

TaskPlatform и canonical action vocabulary остаются leaf-owned в constant; domain использует aliases.
Leaf-пакеты constant/common/dto/types/relaykit не импортируют internal (architecture guard).

## Границы и долг

Task alias view имеет одного владельца adapter/taskplugins/aliases; SQL reader находится у channels adapter,
сборка pricing publication — app/pricingruntime. Shared quota/cache/lock принадлежат
adapter/cache/{accounts,apikeys}, adapter/gorm/{billing,locking,counters}; app/taskbillingruntime явно
собирает TaskDelta/TaskPrices/Midjourney/TaskText, включая recipient-language capability.
Log writer и export state принадлежат usage adapter. Gateway task metadata обращается к usage напрямую.
app/tasks предоставляет узкие insert/origin/fetch/protocol capabilities без пустых pass-through usecases.
confirm_submission_reservation владеет marker decision после durable settlement/log: без reconciliation
нет CAS; flags меняются до CAS и сохраняются при ошибке/проигрыше. Operation adapter удерживает исходный
record pointer, ID/timestamps/hooks и прежний status predicate; новый request context к update не добавляется.
Filesystem artifact backend принадлежит adapter/artifacts; plugin CRUD — taskplugins, protocol/runtime — gateway.

TECH_DEBT #69–75 описывают сохранённые financial/security/resource риски.
Сценарные регрессии находятся у owning runtime/scenario packages; snapshot suite удалена по решению владельца.

Persisted bulk reasons: общие bulk fail_reason получают English default, без выбора языка root или владельца.
Один UPDATE и порядок SQL неизменны; per-owner локализация требует отдельного DB-изменения (#75).

Общий frozen-account hash/static-header helper находится в adapter/http/batchaccount; tasksync и Batch используют одно тело. Batch-only metadata delegates BillingGateway удалены.


Task-plugin observation передаёт plain `domain.ProtocolSubmission`, а не private gateway outcome.
`adapter/gorm/tasks.MarkResponsesBackground` сохраняет прежний best-effort whole-private-data UPDATE;
его вызывает per-request gateway session после durable barrier. Это не CAS и не новая транзакция (#82).
Ownership, platform-scoped reads, публичный task ID и порядок persisted/billing effects сохранены.
Общий gateway projector/provenance перенесён из service в `relay/taskview`; финансовые сценарии не менялись.
