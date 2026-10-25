# Roadmap рефакторинга backend

Цель — привести весь backend к раскладке из [ARCHITECTURE.md](ARCHITECTURE.md), чтобы по папкам было понятно, как и что работает. Правила оформления модулей — [internal/AGENTS.md](internal/AGENTS.md), найденные баги и оптимизации — [TECH_DEBT.md](TECH_DEBT.md).

## Неизменные условия

- Пути, тела ответов, коды ошибок и заголовки API не меняются.
- Схема БД, миграции и семантика запросов не меняются; `model/*.go` правится только как Go-код.
- Баги и оптимизации не исправляются по ходу: записываются в `TECH_DEBT.md` и решаются отдельно. Сразу убираются только дубли, строки без эффекта и мёртвый код.
- Тексты для клиента — через i18n (en + ru, zh сохраняется); логи — на английском.
- Бизнес-модули не импортируют `relay`/`relaykit`.
- Защищённые идентификаторы проекта не меняются.

## Как проходит каждая волна

1. **Разбор**: текущее поведение, потребители, тесты в `origin/main`, риски. Итог — список сценариев волны.
2. **Перенос** по правилам `internal/AGENTS.md`; коммит на сценарий или модуль.
3. **Независимое ревью** свежим исполнителем: контракт, поведение, БД.
4. **Проверки**: gofmt, `go vet ./...`, полный `go test ./...` (`TMPDIR=/dev/shm`), архитектурный тест, сборка `relaykit`, OpenAPI без изменений (`GOWORK=off go run ./cmd/gen-admin-openapi && git status --short docs`), нет новых находок `staticcheck -checks U1000`.
5. **PR** с пометкой: проверено только на PostgreSQL, SQLite и MySQL не проверялись.

Для модулей с деньгами (billing, платежи) — контрактные тесты с эталонами, записанными на `origin/main` (как для apikeys, identity, pricing). Требует подтверждения перед волной.

## Статус

Легенда: ✅ готово · 🔄 в работе · ⏳ ждёт решения · ⬜ не начато.

| # | Волна | Статус | Риск |
|---|---|---|---|
| 0 | Карта системы и roadmap | ✅ | нет |
| 1 | Бизнес-модули панели: apikeys, accounts, catalog, authorization + PAT, identity, pricing, channels | ✅ (не слито в main) | — |
| 2 | Граница шлюза: проверка ключа, billing, запись использования | ✅ (2a, 2b1, 2b2, 2c) | высокий |
| 3 | Учёт и аналитика: логи, usedata, агрегаты, рейтинги, аудит | ✅ (3a, 3b) | средний |
| 4 | Платежи, подписки, коды пополнения | ✅ (4a коды, 4b платежи, 4c подписки) | высокий |
| 5 | Асинхронные задачи и batch (бизнес-часть) | ✅ (5a задачи; 5b batch; 5c плагины задач) | средний |
| 6 | Настройки и система | ⬜ | низкий |
| 7 | Доводка: остатки в `service`/`model`, алиасы, список исключений → 0 | ⬜ | средний |
| 8 | *(по желанию)* Раскладка внутри шлюза | ⬜ | средний |
| 9 | *(по желанию)* Общий слой платформы | ⬜ | средний |

## Волна 0 — карта системы ✅

- [x] `server/ARCHITECTURE.md`: целевая раскладка, два входа, путь запроса шлюза, pricing и billing, таблицы БД, фоновые задачи, язык текстов.
- [x] `server/REFACTOR_ROADMAP.md` (этот файл).
- [x] Правила языка логов и фоновых текстов в `internal/AGENTS.md`.

## Волна 1 — бизнес-модули панели ✅

Ветка `refactor/accounts-clean-20261020`, worktree `.worktrees/internal-refactor`.

- [x] Перенос backend в `server/`, frontend в `client/`.
- [x] Архитектурный тест со списком исключений, который только сокращается.
- [x] Контрактный тест HTTP для apikeys; русская локаль backend.
- [x] apikeys, accounts, catalog, authorization + PAT, identity, pricing, channels: доменные ошибки, transport `WriteError`, сборка один раз через `sync.OnceValue`.
- [x] Из channels убраны импорты relay.
- [x] Китайские тексты перенесённых модулей → i18n.
- [x] Удалён мёртвый код (151 символ), дубли в генераторе OpenAPI.
- [x] Найденные баги и оптимизации записаны в `TECH_DEBT.md`.
- [ ] Повторить прогоны `TestTelegramOAuthRejectsInvalidTokens` (на ветке 1 падение из 28, на main 0 из 16).
- [ ] Rebase на свежий main, полный прогон проверок.
- [ ] PR. ⏳ Решение: PR на волну вместо одного PR в конце.

## Волна 2 — граница шлюза 🔄

### 2a — аутентификация credentials

Готово: коммиты `31e1f5821` (контракт) и `ef2e5e291` (перенос). Эталоны независимо перезаписаны на `origin/main` и совпали; полный `go test ./...` зелёный.

Что шлюзу нужно от бизнеса — сценарии владельцев; код шлюза (`relay/`) остаётся снаружи `internal`.

- [x] Разбор `TokenAuth`, `TokenAuthReadOnly`, `TokenOrUserAuth`, `SetupContextForToken`, потребителей чтения ключа и PAT.
- [x] HTTP goldens всех источников ключа, отказов и типов контекста записаны на `origin/main` до переноса.
- [x] `apikeys/validate_key` → типизированный `Caller`; middleware сохраняет порядок извлечения ключа, ответы и context keys. Read-only сохраняет фактический запрет disabled.
- [x] `personal_access_tokens/validate_token` вместо `model.ValidateAccessToken` в auth и audit middleware.
- [x] Удалены `model/token.go -> internal/apikeys/validate_key` и ставший неиспользуемым `model/audit_log.go -> personal_access_tokens/domain`; остальные существующие зависимости сохранены по решению архитектора.
- [x] Полный зелёный прогон `go test ./...`, коммиты.
- [x] PostgreSQL 15.19 (sbx): полный `go test ./...` на ветке и на `origin/main` с одинаковым окружением — новых падений нет. Общие для обоих 10 падений — тесты, которым нужна своя база (`usage_granularity_test`, `FX_TASK_*_DSN`); `service/authz` с Postgres-вариантом требует свежую базу на тест и проходит так целиком.

### 2b — billing

- [x] **2b1 implementation**: `internal/billing` — create/increase/settle/refund/apply delta/notify; wallet/subscription и token quota; gateway mapping в `relay/billing`, сборка `internal/app/billing`, один временный service bridge.
- [x] Контракт до переноса: commit `11b9dc672`, 62 goldens на origin/main, 3 стабильных повтора; goldens не изменяются.
- [x] Host acceptance после переноса: неизменные 62 HTTP/state goldens, WSS/full suite и реальный PostgreSQL 15.19 — без регрессий относительно origin/main.
- [x] **2b2 implementation**: text/audio/realtime cost, token/cache normalization, tiered policy, selected-group refresh и FX в flat action-пакетах `internal/billing`; plain gateway mapping и late readers. `ExpressionEvaluator`/`QuotaMath` adapters используют прежний engine/rounding. Provider discovery/unknown policy не переносились.
- [x] Cost baseline до переноса: `fa7f25e43`, 64 goldens, origin/main record + два повтора + branch compare; прежние 62 неизменны.
- [x] **2b2 host acceptance**: все 126 immutable goldens, неизменный WSS characterization, full suite, vet/OpenAPI/U1000; PostgreSQL 15.19 full suite совпадает с origin/main, изолированные adapter/WSS/cost/task проверки зелёные. SQLite/MySQL полной матрицей не проверялись.
- [ ] Удалить единственный service BillingGateway bridge после переноса log orchestration в 2c и Batch/Task/Midjourney/violation в волну 5; WSS facades пока нужны неизменному characterization.

### 2c — запись использования

- [x] Baseline: commit 2dd83ad14, 28 usage goldens на origin/main, record + два стабильных повтора + branch compare; прежние 126 неизменны, всего 154.
- [x] Implementation: usage count_consumption/record_consumption/record_failure; plain gateway snapshots, один serializer/storage adapter, late settings seam в model без schema/queries changes. Ленивый sync.OnceValue bridge, app-owned metadata port, business note policy в usage и Billable fact от billing; unsupported context возвращает error без panic.
- [x] Общий upstream failure writer для relay/task submission; Task billing остаётся wave 5. Сохранены ordinary/provider/realtime write order, flags, DB routing и async jobs.
- [x] Применён отдельный English content patch: единый logger formatter, ASCII USD $, source/docs/regression test; branch -update и exact content-only checker запускает оркестратор после Chinese parity proof. Старые rows не мигрируются. Исключение A6 подтверждено пользователем: channel-test MsgChannelTestLogName остаётся на языке получателя, без изменения immutable теста.
- [x] Host acceptance переноса (commit 60ae79859): SHA154, все contracts без -update на plain Chinese tree, WSS, full suite, relaykit/OpenAPI/U1000; PostgreSQL 15.19 full suite совпадает с origin/main, изолированные usage/billing adapter, WSS, cost/task проверки зелёные. SQLite/MySQL полной матрицей не проверялись.
- [x] English (commit 6b22c3bbd): перезаписаны только 27 затронутых cases, checker подтвердил изменение только `content` по утверждённой таблице; финальный host suite зелёный.

## Волна 3 — учёт и аналитика ✅

- [x] 3a: usage read side, aggregates, rankings перенесены в 14 action packages; SQL/records/codec — usage adapter, quota scheduling — usage app; общие counter jobs — app/background.go. Controller bridges сохранены.
- [x] Host acceptance 3a (commit 514905572): 347 usage_read + 154 billing/usage goldens без -update, locale defaults, WSS, full suite, relaykit/OpenAPI/U1000; PostgreSQL 15.19 full suite совпадает с origin/main, изолированные usage adapter/app проверки (main + отдельная log schema) зелёные. SQLite/MySQL полной матрицей не проверялись.
- [x] Tool note (commit 36bcaf486): `<tool> calls: N, cost X`; перезаписаны только 8 затронутых billing cases, checker подтвердил изменение только `content`.
- [x] 3b implementation: audit admin_list_events/list_own_events/record_event; domain policy, transport snapshots, gorm records/codec, sync.OnceValue composition. Routes/permissions, LOG_DB и legacy subscription row shape сохранены.
- [x] 3b baseline (74c45a2d8): 257 audit goldens + default-locale baseline, origin/main record + два повтора + candidate parity; прежние 501 immutable, всего 758.
- [x] 3b host acceptance (commit f5e0d6daa): 758 goldens без -update, locales, WSS, full suite, relaykit/OpenAPI/U1000; PostgreSQL 15.19 full suite совпадает с origin/main, audit matrix (main + отдельная log schema, fresh/upgrade) и token audit matrix зелёные. SQLite/MySQL полной матрицей не проверялись.
- [x] Read queries к `LOG_DB` отделены, отдельная log DB сохранена; #36 закрыт plain request observation. Legacy writers/cleanup и общие RAM queues остаются до волн 5/7 (A2/A4), без нового locator.

## Волна 4 — платежи, подписки, коды ✅

- [x] `payments` implementation: 22 сценария, 23 регистрации, пять provider adapters, wallet owner, strict TopUp record alias; subscription workflow остаётся legacy до 4c (commits 1168444a1..20d2e7035).
- [x] `subscriptions` implementation: plans/user subscriptions, balance/provider checkout, complete/expire, pure calendar и app maintenance; model alias-only и согласованные cache/delta/debit остатки. Contract1 d0e1e5e97:519 HTTP +13 ticks,132 locales; всего2237 immutable golden files.
- [x] 4c host acceptance (commits 3fb737451..4227b8903): 2237 goldens без -update, Redis/WSS/full suite, relaykit/OpenAPI/U1000;
  PostgreSQL 15.19 full suite совпадает с 3b, изолированные subscriptions/billing/payments на PG зелёные. SQLite/MySQL полной матрицей не проверялись.
- [x] Content-only (commit 3b814056e): ошибки подписок — English по умолчанию + ru, receipts и аудит сброса плана на языке получателя, zh без изменений; 86 строк в 67 goldens.
- [x] Harness (commit 1bbbd1d51): Stripe `X-Stripe-Client-User-Agent` в goldens маскирует uname и версию Go хоста записи (37 goldens), контракты переносимы между машинами.
- [x] `redemption`: девять сценариев, record/SQL adapter, atomic CAS + единый wallet credit, late composition и thin bridges.
- [x] Baseline: commit 00c8bbd6e, 214 новых HTTP/state goldens, два стабильных origin повтора, candidate parity;
  прежние 758 неизменны, всего 972. Два locale baselines отделяют inherited ru переводы от новой ID delta.
- [x] Host acceptance переноса (commit a80d850ae): 972 goldens без -update, WSS, full suite, relaykit/OpenAPI/U1000;
  PostgreSQL 15.19 full suite совпадает с 3b/origin, изолированные redemption adapter/app/contract зелёные.
  SQLite/MySQL полной матрицей не проверялись.
- [x] Content-only (commit 1a7402149): запись о пополнении и quota в аудите redemption.create — на языке
  пользователя, English по умолчанию, zh без изменений; 31 golden, только content/quota.
- [x] Payments baseline: 615 HTTP/state/outbound goldens, два стабильных origin повтора, candidate parity; 204 locale snapshots. Waffo transport test-only substitution явно отделён от production SDK.
- [x] Payments host acceptance: 1681 immutable oracles без -update, Redis/WSS/full suite, relaykit/OpenAPI/U1000;
  PostgreSQL 15.19 full suite совпадает с 3b, изолированные wallet/payments/redemption/model/controller на PG зелёные.
  SQLite/MySQL полной матрицей не проверялись.
- [x] Content-only (commit 93ea992dc): ошибки платежей — English по умолчанию + ru, receipts на языке получателя,
  zh без изменений, ACK неизменны; 69 строк в 53 goldens.
- [x] Log redaction (commit ea1ab44b4): из платёжных логов убраны подписи, тела webhook, данные покупателя; goldens без изменений (#50).

## Волна 5 — асинхронные задачи и batch ✅

- [x] 5a implementation: standalone delta единственным телом в gorm/billing; same-tx task capability, nested refund #27 сохранён.

- [ ] Перевести persisted Task/Batch/Midjourney/violation log texts на English (translate persisted log texts to English); удалить metadata delegates единственного bridge после переноса consumers.

Отправка задачи провайдеру — шлюз; учёт задач, их биллинг и опрос — бизнес.

- [x] 5a implementation: Task/MJ records, accounting, polling, dashboard/media и OR durable sync — internal/tasks (commits df33308eb контракты, 1113bf66f перенос, 97de3dcf3 docs); plugin routing/cache model/task_model_alias.go сохраняется для pricing/channel-cache cleanup волны 7; alias ownership перенесено не полностью.
- [x] 5b implementation: Batch API, worker, billing и storage — internal/batch (commits 0caf7748d контракты, ec10c079c перенос, 3e761c3dd docs); provider_batch_poll регистрируется явно из controller.RegisterScheduledSystemTasks.
- [x] 5b host acceptance: 484 новых + все прежние goldens без -update по два повтора, full suite, relaykit/OpenAPI/U1000. -count=2 падает только на тех же roots, что и HEAD (#76, SQLITE_BUSY flake).
  PostgreSQL 15 full suite совпадает с 3b, целевые batch/tasks/billing на PG зелёные. SQLite/MySQL полной матрицей не проверялись.
- [x] 7f2be5459: два теста вне goldens переведены на English default тексты 5a.
- [x] Content-only (commit 9337cb7ab): ошибки и тексты логов batch — i18n keys, English по умолчанию + ru, zh без изменений; default goldens не менялись.
- [x] 5c structural implementation: `internal/taskplugins`, management/sync/origin/query/observation/retrieve policy;
  core app/taskplugins + outer app/taskpluginsruntime; provider/wire остаются gateway, durable Task — tasks.
  Commits: 089e9ba5c контракты (396 новых эталонов), cf4d4a7ad перенос, d96ff5df1 docs.
- [x] 5c acceptance: host full suite/WSS/relaykit/OpenAPI/U1000, все goldens без -update по два повтора;
  PostgreSQL 15 full suite совпадает с 3b, taskplugins/tasks/batch PG-тесты зелёные. SQLite/MySQL полной матрицей не проверялись.
- [x] Content-only (commit 3d03566a7): ошибки плагинов задач — i18n keys, English по умолчанию + ru, zh без изменений.
- [ ] Волна 7 cleanup: единый executable `model/task_model_alias.go` для model/pricing.go,
  model/model_pricing_config.go и InitChannelCache. Удалить, когда у pricing и channel-cache lifecycle
  появятся собственные владельцы; без новых model→internal imports/exceptions. Alias ownership не полностью перенесено.
- [x] 5a: polling consumer port → adapter/gateway/taskplugins → leaf relay/taskregistry; явная runtime assembly, main-only factory удалена.
- [x] 5a host acceptance: 586 новых + 2237 прежних goldens без -update, Redis/WSS/full suite, relaykit/OpenAPI/U1000;
  PostgreSQL 15.19 full suite совпадает с 3b, изолированные tasks/billing/subscriptions/payments на PG зелёные. SQLite/MySQL полной матрицей не проверялись.
- [x] Content-only (commit e2956edbd): ошибки задач — English по умолчанию + ru, тексты биллинга и причины сбоя на языке владельца, zh без изменений; 344 строки в 147 goldens. Общий fail_reason bulk UPDATE — English (#75).

## Волна 6 — настройки и система ⬜

- [x] 6a implementation: `internal/settings` — generic write/read/reload, root editor, public status/legal/content;
  `app/settings` сохраняет defaults→reload и sleep→reload→pricing observation. Contracts: commit 7899e7233, 749 новых goldens.
- [x] 6a commits: 215b6d917 перенос, 25890e094 docs.
- [x] 6a acceptance: host full suite/WSS/relaykit/OpenAPI/U1000, 4452 goldens и 16 locale baselines без -update;
  PostgreSQL 15 full suite совпадает с 3b, settings/group ratio/pricing publication/tasks/batch PG-тесты зелёные. SQLite/MySQL полной матрицей не проверялись.
- [x] Content-only (commit d7e8e42a7): ошибки редактора опций — i18n keys, English по умолчанию + ru, zh без изменений.
- [ ] 6a A2=B ownership gap: `model/option.go` перенесён не полностью; single publisher/mutex и pricing/catalog option composition остаются до волны 7.
- [ ] `system`: `setup.go`, `system_info.go`, `system_task*.go`, `performance.go`, `perf_metrics.go`, `uptime_kuma.go`, отчёт об инстансе.
- [ ] `deployments`: `controller/deployment.go`.
- [ ] Остатки каталога: `controller/model.go`, `vendor_meta.go`, `prefill_group.go`, `missing_models`.
- [ ] Уведомления пользователям: `service/user_notify.go`.

## Волна 7 — доводка ⬜

- [ ] Settings A2=B cleanup: перенести единственные `model.PublishOption`, `groupRatioOptionMutex`, alias facts и pricing/catalog option composition
  (`model/model_pricing_config.go`, `model/pricing_catalog_option.go`, `model/catalog_persistence.go`) вместе с реальными consumers
  NewPricingOptionsPersistence/native catalog publication; сохранить same-tx catalog deletion+pricing edits и один lock/publisher.
  До собственного владельца composition ownership gap открыт; новых model→internal imports/exceptions не вводить.

- [ ] Перенести оставшиеся model.LockForUpdate, model.SyncCreditUserQuotaCache и model.CacheDecrUserQuota вместе с shared row-lock/quota-cache primitives; без копирования Lua/key/schema.
- [ ] Общие quota cache/Lua/DB primitives из `model/quota_reserve.go`, user/token/task consumers → технические адаптеры с одним владельцем; 2b1 сохраняет primitives и SQL/Redis semantics без изменений.

- [ ] identity: логика сессий и проверок из `service/auth_session.go`, `security_verification.go`, `login_verification.go`, `twofa.go`, `email_binding.go`, `service/passkey` → сценарии; `model/account_security.go`, `auth_flow.go` → адаптеры.
- [ ] Убрать алиасы ошибок в `model`, `service`, `oauth`, когда у них не останется потребителей.
- [ ] Убрать `*model.User` и `*model.AuthFlowAuthorization` из transport identity.
- [ ] Мосты `// BRIDGE:` — удалить все.
- [ ] Список исключений архитектурного теста → 0.
- [ ] `controller/` — только связка с роутером; `service/` — пусто или удалён; `model/` — алиасы таблиц и миграции.
- [ ] Обновить `internal/README.md` и `ARCHITECTURE.md` по факту.

## Волна 8 — раскладка внутри шлюза (по желанию) ⬜

Остаётся снаружи `internal`, бизнес его не импортирует.

- [ ] Входы шлюза по папкам: chat, responses, claude messages, gemini, embeddings, images, audio, rerank, realtime, tasks (`controller/relay.go`, `relay/*_handler.go`).
- [ ] Реестр адаптеров провайдеров вместо switch в `relay/relay_adaptor.go`.
- [ ] Выбор канала (`middleware/distributor.go`) — отдельный компонент шлюза поверх конфигурации каналов из `channels`.
- [ ] Адаптеры `relay/channel/*` — единая структура файлов.

## Волна 9 — общий слой платформы (по желанию) ⬜

- [ ] `common/`, `setting/`, `logger/`, `constant/` → общий слой без бизнес-логики (БД, Redis, HTTP-клиент, конфиг, лимиты). Большой объём переименований импортов — только отдельным решением.

- [ ] Выделить чистую quota-math/expression библиотеку из `common`/`pkg/billingexpr` (TECH_DEBT #32); 2b2 использует narrow adapters, не дублирует conversion/saturation.

## Открытые решения

- [ ] PR на каждую волну вместо одного большого PR в конце.
- [x] Контрактные тесты billing: 62 lifecycle + 64 cost; неизменяемые baseline bytes.
- [x] Контрактные тесты платежей: полный объём, нарезка 4a/4b/4c.
- [ ] Волны 8 и 9 — делать или нет.

## Чистота репозитория

После слияния волны 1 удалить служебное (только с подтверждения):
- [ ] worktree `.worktrees/wave1-*` и ветки `wave1/*`;
- [ ] worktree `.claude/worktrees/agent-*`, ветки `worktree-agent-*` и `refactor/pricing-wave2`;
- [ ] теги `backup/pre-rebase-20261021`, `backup/pre-fixup-20261021`;
- [ ] `.worktrees/main-baseline`;
- [ ] stash `accounts-channels-before-rebase-main-20261020`;
- [ ] неотслеживаемые `client/` и `server/` в корневом checkout (остатки сборки).
