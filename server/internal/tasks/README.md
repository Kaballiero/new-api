# Асинхронные задачи

Модуль владеет durable Task/Midjourney, статусами, учётом и последующим расчётом/возвратом,
опросом провайдеров, dashboard lists/detail, выдачей артефактов и video content. Отправка upstream,
выбор канала, provider wire DTO, retry и plugin runtime остаются в шлюзе. Batch принадлежит отдельному internal/batch,
управление task plugins и protocol observers — 5c.

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
| poll_tasks / poll_midjourney_tasks | Один отменяемый прогон, без собственного scheduler/ticker |

Router и порядок middleware сохранены. Dashboard допускает прежние session/PAT; API-ключи не становятся
сессиями панели. Midjourney submit/fetch/image/seed/swap и native task routes сохраняют gateway wire
форматы. Закомментированный notify route не добавлен. Capability HMAC, concurrency admission и
удаление access из query остаются в прежних middleware/service; reader лишь использует владельца tasks.

## Хранение и финансовые границы

Records/codecs и SQL — internal/adapter/gorm/tasks. model/task.go и model/midjourney.go содержат только
same-name aliases. Field order, JSON/GORM tags, таблицы, миграции и индексы не меняются. Чистые domain
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
только подтверждённый terminal zero разрешает соответствующий legacy refund. Paid submission не replay-ится.
Standalone subscription delta теперь имеет единственное тело в adapter/gorm/billing; nested refund #27 сохранён.

Usage сохраняет единственный metadata serializer и прежние обычный/required writers.
Task identity/plugin mapping имеет одно тело в adapter/gorm/usage для gateway и settlement. Исторические
логи не переписываются. Структурный слой сохраняет operational/persisted/HTTP text bytes; English/ru
изменения готовятся отдельно неприменённым content package.

## Сборка и технические адаптеры

app/tasks собирает gateway-independent storage/billing/sync capabilities. app/taskruntime подключает
provider factory, HTTP/media adapters и HTTP/poll handlers; controller bridges используют sync.OnceValue.
Фабрика relay/taskregistry — единственный leaf owner platform→plugin resolution. main-only factory,
service.GetTaskAdaptorFunc и новый global locator отсутствуют; tests/binaries получают ту же сборку.

Scheduled system handlers остаются у system owner: прежние enabled predicates, DB lease, interval15s,
progress/history и cancellation. Main сохраняет прежний порядок регистрации и запуска. Task poll
сначала timeout sweep, затем обычные platforms, затем до16 due sync tasks с4 workers; MJ отдельный tick.
Media adapter сохраняет streaming ownership, redirect/SSRF/header/range/status rules и закрытие body.

TaskPlatform и canonical action vocabulary остаются leaf-owned в constant; domain использует aliases.
Leaf-пакеты constant/common/dto/types/relaykit не импортируют internal (architecture guard).

## Остатки и проверка

model/task_model_alias.go остаётся executable routing/cache view для gateway/pricing/channel cache до5c;
это не record. Shared model quota/cache/lock primitives — до7. BillingGateway остаётся только для реальных
violation/immutable billing/WSS consumers; gateway task metadata обращается к usage непосредственно.
Filesystem artifact backend и plugin CRUD/protocol/runtime не перенесены.

TECH_DEBT69–75 описывает сохранённые financial/security/resource риски. Контракты:319 HTTP/state/outbound,
216 policy/ticks,47 MJ/scheduler/media и4 OAuth — всего586 immutable goldens. Вместе с2237 прежними —2823;
два locale baselines фиксируют42 snapshots каждый и ровно6 inherited locale deltas. Новые default deltas:0.
Package/call-site перемещение harness не разрешает правок oracle bytes. Runtime owner fixtures используют
shared test-only helper; PostgreSQL — отдельные main/log schemas и startup search_path каждого connection.
Sandbox проверки и подготовленные host scripts не доказывают host/PostgreSQL/MySQL/Redis/WSS/full-suite,
CI/live provider или production acceptance. Фактические результаты находятся в wave5a-report.md handoff.

C1 content gap: общие bulk fail_reason получают English default, без выбора языка root или владельца.
Один UPDATE и порядок SQL неизменны; per-owner локализация требует отдельного DB-изменения (#75).

Общий frozen-account hash/static-header helper находится в adapter/http/batchaccount; tasksync и Batch используют одно тело. Batch-only metadata delegates BillingGateway удалены.
