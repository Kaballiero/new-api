# Billing — расчёт стоимости и жизненный цикл резерва

## Ответственность

Модуль владеет request-local резервом (`domain.Reservation`), выбором кошелька или подписки, квотой API-ключа, допрезервом, итоговой дельтой, возвратом и условиями quota notifications. Также владеет расчётом text/audio/realtime cost, нормализацией billing tokens, tiered settlement policy, обновлением выбранной группы и capture/apply FX. Cost-сценарии возвращают quota, clamp и факты для логов; сами не пишут storage. Конфигурация цен остаётся у pricing/settings, provider discovery — в шлюзе.

## Возможности

| Сценарий | Действие и потребитель |
|---|---|
| `calculate_text_cost` | Ratio/fixed/cache/image/input-audio, отсортированные tool fees после OtherRatios, tiered single-round composition и legacy fallback; display quota отдельно от charge |
| `calculate_audio_cost` | Text/audio modalities, late ratios, fixed/tiered и FX; сохраняет прежний отсутствующий ModelPrice на PostAudio |
| `calculate_realtime_cost` | Validation → strict tiered → обязательный legacy audio clamp guard → final zero-token policy; предварительный debit оркестрирует gateway через ту же domain audio formula |
| `prepare_tiered_cost` | Recompose выбранной pure group с frozen FX; strict rounding до обновления snapshot и дополнительного резерва |
| `capture_fx_basis` | Validate accounting и RAM publication; captured basis переиспользуется без повторного чтения publication |
| `create_reservation` | Выбор источника и fallback; wallet preflight; trust; token → funding reserve, компенсация token при отказе funding |
| `increase_reservation` | Поднять hold до target: funding → token; rollback funding при отказе token; доверенный резерв остаётся пропущенным, кроме перехода к атомарному OpenRouter reserve |
| `settle_reservation` | Actual−hold, refund/debit дельты, независимые funding/token leg realtime, terminal guards и applied outcome при известной стоимости OpenRouter |
| `refund_reservation` | Один async job после синхронного terminal gate; subscription initial refund до трёх попыток, затем extraReserved и token; wallet без повторного additive credit |
| `apply_quota_delta` | Legacy no-session debit/refund с partial-applied flags; также no-session terminal realtime с независимыми leg |
| `notify_quota` | Позднее чтение snapshot/порога внутри прежней async job; wallet/subscription eligibility; доставка через порт |

Собственных HTTP-маршрутов и фоновых циклов нет. Потребители — существующие relay API с API-ключом, realtime, отправка задач, legacy Midjourney/violation fee и `/pg/chat/completions` с существующей сессией (PAT playground по-прежнему отклоняет). Порядок middleware и полномочия не менялись.

## Зависимости и сборка

Domain/usecase не импортируют Gin, GORM, Redis, common, model, service, relay или relaykit. Узкие storage/notifier порты объявлены рядом со сценариями; общий `domain.TokenReserver` нужен initial reserve и increase. `internal/app/billing.New` явно собирает stateless usecase-объекты один раз при подключении gateway-пакета.

- `internal/adapter/gorm/billing.Store` вызывает существующие model quota primitives. Там остаются SQL, Lua, hydration/fallback, batch queue, компенсация и subscription transaction/locking. Новой схемы, миграций и Redis namespaces нет.
- `internal/adapter/billing.Notification` переводит данные notice в прежний NotifyUser transport: language получателя, English по умолчанию; wire DTO принадлежат адаптеру, не бизнесу.
- `relay/billing` — владелец gateway mapping: RelayInfo ↔ domain state, mutex request-local session, старый BillingSettler, Gin Refund, i18n → NewAPIError и error options. Business-state PostDelta переносится в RelayInfo без изменения consume-log JSON. Язык ошибок разрешается только при ошибке из сохранённых входов языка запроса; при nil Context/Request используется English.
- `service.BillingGateway` — один временный интерфейс в терминах старого потребителя. `relay/billing.init` регистрирует единственную production реализацию; `relay/relay_adaptor.go` явно подключает её для всех relay entry points, включая plugin adapters. Service-only и adaptor-only binaries подключают `relay/billing` явно; тестовые binaries используют тот же механизм. Отдельного wiring в main или contracttest нет.

## Границы и долг

Запись consume/error и metadata перенесена в `internal/usage`; gateway cost projections находятся в relay/billing, plain usage snapshots — в relay/usage. Audio/realtime billing возвращает Billable как готовый факт; usage не повторяет zero/fixed charge rule. Usage читает billing domain Outcome/ToolSurchargeItem как понятия владельца без копий и без обратной зависимости. Count scenario выбирает эффект по явным facts: для text до/после settlement, для audio/realtime только до, как до переноса. Gateway сохраняет межмодульную последовательность count/settle/record и прежние perf jobs. Durable Task settlement принадлежит internal/tasks; Batch settlement принадлежит internal/batch; выбор канала, discovery и upstream submission сохраняют gateway adapters. Provider-cost conversion использует существующий common primitive через QuotaMath без собственной формулы. Batch не переводится на Reservation: held quota учитывается прежними primitives.

Initial wallet и OpenRouter additional reserve проверяют доступный остаток с held. Обычный wallet increase и terminal wallet/token debit сохраняют возможность долга. Ошибка token после успешного обычного funding settle не запускает refund funding; known-cost OpenRouter failures сохраняют applied outcome для ручного ремонта. WSS может повторить только неуспешную terminal leg. Это request-local защита, не durable exact-once при падении процесса.

Subscription initial refund повторяется максимум 3 раза с 200/400 ms; extraReserved/token/wallet — одна попытка. `TECH_DEBT.md` #26–29 описывают legacy debt, nested subscription refund transaction, notification snapshot и несовместимость placeholders с webhook renderer. Шаблон и отдельное поле values quota webhook сохранены. Общие cache primitives остаются в model до волны 7.

Мосты: service/billing.go оставляет PreConsumeBilling/SettleWssBilling/PostConsumeQuota для immutable billing/WSS tests и violation; ordinary SettleBilling удалён после переезда recording. PostText/cache inference нужны immutable billing oracle, Pre/PostWss/CaptureFX — WSS characterization; TokenParams/CurrentFX/ApplyFX сохраняются только при оставшихся immutable/violation/FX consumers; Batch использует billing domain и core FX composition. Gateway Task/MJ и owner regressions используют usage непосредственно; прежние FX/plugin/MJ/saturation delegates bridge удалены. Batch-only clamp/tiered delegates удалены: Batch получает serializer напрямую через usage adapter. Ручного test-only production mapping нет. Task/Batch передают конкретные FX/clamp/tiered/plugin metadata в единственный usage serializer. Второго registry и новых architecture exceptions нет. Mutable tests используют владельцев; отдельные facades ради них не сохраняются.

## Проверки

62 HTTP/state goldens в `testdata/golden` записаны на origin/main до переноса; их нельзя редактировать или перезаписывать. `TestBillingHTTPContract` проверяет реальные middleware/relay → fake HTTP provider, DB/Redis accounting и logs, нужен host с sockets. Нормализуются только оговорённые timestamp/header Date и log durations, финансовые поля сохраняются.

Сценарные регрессии на fakes узких ports находятся в `<scenario>/usecase_test.go`: preferences/trust/held admission, delta/partial settlement, increase rollback, refund/retry и notification eligibility. `relay/billing/billing_test.go` проверяет только gateway projection/outcome, error mapping en/ru, lazy nil-safe язык и сериализацию concurrent Session calls под gateway mutex. `adapter/billing/notification_test.go` проверяет прежний webhook payload; `adapter/gorm/billing/TestBillingAdapterPostgres` требует изолированный `TEST_POSTGRES_DSN` и сообщает фактическую версию PostgreSQL. Default HTTP fixture — SQLite. Реальный PostgreSQL 15, SSE/WSS и полный suite запускает host-оркестратор; отсутствие такого прогона нельзя считать доказанной совместимостью.

## Расчёт и технические адаптеры

`domain` содержит свои Usage/Prices/Snapshot/RequestInput/Clamp и общие нормализацию, overlap validation, audio arithmetic и tiered policy. В этих типах нет Gin, DTO, RelayInfo, GORM или Redis. Поздние readers собираются в `internal/app/billing/cost.go` над существующими settings; `adapter/settings/billing.ReadFX` читает только уже опубликованную RAM; source берётся из единственной константы publishing task `service.ModelCostFXSource`. `domain.PublishedUSD` — общая проверка USD для capture и admin status, status не применяет accounting/factor policy. Предварительный WSS и оба audio-сценария используют один поздний AudioPrices reader из app. Canonical DTO usage выбирает `service.EffectiveBillingUsage`, общий с Batch; правила DTO в domain не копируются.

`ExpressionEvaluator` и `QuotaMath` реализованы `internal/adapter/billing/cost_math.go` над `pkg/billingexpr` и `common` checked conversions. Конверсия, saturation/SysError и expression engine имеют прежнего единственного владельца; инфраструктурные транзитивные зависимости не попадают в usecase/domain. Чистая библиотека math/engine — TECH_DEBT #32, волна 9. Старые denomination/quota константы в service остаются до переноса task/log в 2c / волне 5: service → internal/domain создал бы запрещённое новое исключение замороженного allowlist; бизнес-domain не может импортировать инфраструктурный service. Это явное ограничение R4, а не новый FX policy owner.

Сохраняются: explicit provider zero, unknown policy в gateway; decimal multiplication/rounding order; first clamp (provider outcome по-прежнему может заменить его); HTTP estimate fallback и WSS reject; lost PostAudio ModelPrice (#30); public display ratio вместо effective FX ratio (#31); native Gemini non-stream grounding без surcharge (#33). На стадии move persisted bytes сохранены; отдельный English patch переводит request log content и USD display sign на ASCII $, сохраняя other, числа и деньги. Legacy quota formatter сохраняет китайские bytes для оставшихся consumers. Исторические rows не меняются.

Дополнительно 64 cost goldens зафиксированы commit `fa7f25e43`: origin/main record + два повтора + branch compare прошли до переноса. Вместе со старыми 62 они неизменны. 55 cases идут через production HTTP router, 4 Responses — через названный gateway test (production `/v1/responses` отсутствует, сначала проверяется 404), 5 — через legacy boundary. Для cost logs фиксируются decoded other и исходные raw bytes; меняются только разрешённые duration numeric lexemes. Generated request_id нормализован только в boundary после проверки формы. После переноса socket contracts, WSS и PostgreSQL требуют host run; исходная стабильность не доказывает итоговую реализацию.

Pure cost regressions находятся рядом со сценариями, token/cache validation — `domain/tiered_cost_test.go`. Перенесённые gateway tests проверяют canonical usage/semantic mapping, context tools, request freeze, selected-group reserve и прежние engine calls. Их DB cases используют отдельную свежую SQLite fixture; PostgreSQL matrix log/storage tests в service создают собственную schema для каждого subtest и задают search_path всем соединениям.

Text cost/accounting и usage writer требуют ненулевой `*gin.Context`, как до переноса; наличие `Context.Request` не требуется. Поддержка nil Gin context подтверждена только для прежних lifecycle/error entrypoints, а не для записи text cost. Pure tiered settlement tests находятся в domain; gateway mapping clamp/retry проверяется через настоящий TextCost.

Wave5a: task funding использует apply_quota_delta с task-scoped adapter и поздним token lookup.
Standalone subscription delta принадлежит gorm/billing; owner-checked task capability получает тот же
активный tx. Durable task status/claim/cache/log/finalize оркестрирует tasks, billing math неизменна.


Task plugins: observer admission/timeout/reload принадлежат `internal/taskplugins`; gateway submit
по-прежнему проходит обычные reserve → durable insert → settle и превращает результат в plain
`tasks/domain.ProtocolSubmission` на границе controller/relay.go. Отключение клиента прекращает
только observation; bounded detached submission сохраняется. Billing math, refunds, violation fees,
FX и same-transaction subscription predicates не менялись. Provenance projector теперь `relay/taskview`.

Delivery capability `notifications.Send` передаётся billing renderer через `app/billing`; renderer сохраняет язык пользователя, templates и values. Delivery/limiter принадлежат notifications, общие SMTP/HTTP/worker primitives остаются техническими зависимостями.
