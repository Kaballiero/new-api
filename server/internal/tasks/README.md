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
| refuse_sync_reservation | Закрывает intent, резерв которого отклонён (нехватка квоты токена, кошелька или подписки) до передачи hold: `FAILURE`, расчёт завершён без списания, причина `reservation_refused`; отказ с неизвестным исходом оставляет intent для ручной сверки. Также закрывается intent, чей Commit упал до записи reserved quota: запрос возвращает резерв сессии биллинга точно; если reserved quota уже записана, intent остаётся у Task |
| configure_sync_submission / record_sync_generation / record_sync_receipt | Frozen metadata/account и correlation, без повторного paid POST. Задача кроме нативной картинки хранит только hash ключа, индекс ключа и hash конфигурации канала; lookup стоимости аутентифицируется ключом канала в момент запроса, а при смене ключа или конфигурации уходит в ручную сверку (`account_binding_changed`) |
| clear_sync_task_credentials | Однократный прогон при старте master: удаляет из private_data синхронных задач ключ аккаунта, заголовки и transport канала, которые сохраняли прежние версии. Незавершённую задачу без привязки к аккаунту он сначала привязывает (hash и индекс ключа, hash конфигурации канала) по сохранённому ключу, если ключ ещё принадлежит каналу, чтобы lookup стоимости продолжился; иначе задача уходит в ручную сверку при lookup. Строка переписывается под блокировкой с увеличением revision, повторный прогон ничего не меняет |
| complete_sync_task / fail_sync_task / reconcile_sync_task | Независимые delivery и financial outcome, revision CAS, metadata-only reconciliation. Задача с подтверждённой стоимостью становится терминальной только после settlement; settlement, не сдвинувший балансы, остаётся незавершённым и повторяется poller раз в минуту |
| apply_sync_operator_decision | POST /api/task/:task_id/openrouter-sync/release и /openrouter-sync/settle; RootAuth, подтверждённая нулевая/явная стоимость и аудит каждой попытки. Release закрывает незакоммиченный резерв без settlement, только если planned и reserved quota равны 0 (подписка при нулевом planned всё же списывает 1 единицу квоты); иначе сессия биллинга могла списать planned, и hold ждёт ручной проверки денег. Терминальную задачу с незавершённым settlement без решения root повторяет, указав подтверждённую стоимость |
| release_unidentified_sync_holds | Фоновый прогон в async_task_poll (master-only): через час после submit_time освобождает по нулевой стоимости hold с неизвестным исходом отправки и без generation id, если он доступен release оператора. Hold, который не подходит или не может быть рассчитан, один раз помечается `auto_release_unavailable` и остаётся root; если после записи решения не прошёл расчёт, root повторяет тот же release (`cost_usd=0`) |
| settle_sync_estimates | Фоновый прогон в async_task_poll (master-only) между release_unidentified_sync_holds и alert_estimate_settlements. Сначала повторяет `SettleDurable` для решений по оценке, расчёт которых не сдвинул балансы: решение записывает `fail_reason` «OpenRouter cost settlement by estimate pending», применение расчёта в той же транзакции меняет его на «OpenRouter cost settled by estimate», и повтор выбирает только pending. Затем новый hold в ручной сверке (ещё без `alerted_at`) с доставленным результатом и без стоимости провайдера рассчитывается автоматическим решением settle по оценке, округлённой до 12 знаков: текст после исчерпанного поиска стоимости — токены usage по максимальному тарифу резерва, не больше резерва (`usage_estimate_usd`); нативная картинка — резерв, если у endpoint есть опубликованная цена. Без тарифа, с нулевой оценкой, с оценкой выше резерва или при нереализуемом расчёте hold остаётся root. Расчёт пишет строку ledger и при нулевой дельте, с пометкой `upstream_cost_estimated` |
| alert_estimate_settlements | Фоновый прогон в async_task_poll (master-only) после settle_sync_estimates: одной сводкой в Telegram-чат (язык root) сообщает о списаниях по оценке за последний час, о которых ещё не сообщали (hold, рассчитанные по оценке, и видео, рассчитанное по тарифному резерву при терминальном опросе), и ставит им `estimate_alerted_at`. Кандидаты выбираются по колонкам (`platform`, `status`, `fail_reason`) с keyset по (`finish_time`, `id`). Недоставленная сводка повторяется в пределах часа; списание, ушедшее из окна без сводки, пишется в WARN |
| alert_sync_reconciliation | Фоновый прогон в async_task_poll (master-only) после release_unidentified_sync_holds: одной сводкой в Telegram-чат (tg-gateway, язык root) сообщает о hold в ручной сверке (кроме незакоммиченного резерва, у которого planned и reserved равны 0; сумма незакоммиченного резерва — большее из них) и о settlement, который poller повторяет дольше 30 минут, о которых ещё не сообщали, и ставит им `alerted_at` в private JSON. Hold с `submission_outcome_unknown` ждёт автоснятия и попадает в сводку только после `auto_release_unavailable`. Недоставленная сводка повторяется в следующем прогоне. Ещё 2 минуты после перехода hold в ручную сверку async_task_poll планируется даже без незавершённых задач |
| poll_tasks / poll_midjourney_tasks | Один отменяемый прогон, без собственного scheduler/ticker |

Router и порядок middleware сохранены. Dashboard допускает прежние session/PAT; API-ключи не становятся
сессиями панели. Midjourney submit/fetch/image/seed/swap и native task routes сохраняют gateway wire
форматы. Закомментированный notify route не добавлен. Capability HMAC, concurrency admission и
удаление access из query остаются в прежних middleware/service; reader лишь использует владельца tasks.

## Хранение и финансовые границы

Records/codecs и SQL — internal/adapter/gorm/tasks. model/task.go и model/midjourney.go содержат только
same-name aliases. Таблицы, миграции и индексы прежние; private JSON дополнен первой причиной результата,
классом последнего lookup, необратимым решением оператора и отметкой о сводке ручных hold. Чистые domain
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
