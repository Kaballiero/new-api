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
| 3 | Учёт и аналитика: логи, usedata, агрегаты, рейтинги, аудит | ⬜ | средний |
| 4 | Платежи, подписки, коды пополнения | ⬜ | высокий |
| 5 | Асинхронные задачи и batch (бизнес-часть) | ⬜ | средний |
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

## Волна 3 — учёт и аналитика ⬜

- [x] 3a: usage read side, aggregates, rankings перенесены в 14 action packages; SQL/records/codec — usage adapter, quota scheduling — usage app; общие counter jobs — app/background.go. Controller bridges сохранены. Host acceptance для текущего переноса и отдельный tool-note patch перечислены в wave3a-report; real MySQL/PostgreSQL/WSS/full suite ещё требуют host run.
- [ ] `audit`: `controller/audit.go`, `model/audit_*.go`.
- [x] Read queries к `LOG_DB` отделены, отдельная log DB сохранена; #36 закрыт plain request observation. Legacy writers/cleanup и общие RAM queues остаются до волн 5/7 (A2/A4), без нового locator.

## Волна 4 — платежи, подписки, коды ⬜

- [ ] `payments`: `controller/topup*.go` (Stripe, Creem, Waffo, Waffo Pancake, Epay), `payment_webhook_availability.go`, `payment_compliance.go`, `model/topup.go`. Каждый провайдер — адаптер, вебхук — сценарий.
- [ ] `subscriptions`: `controller/subscription*.go`, `model/subscription.go`, задача сброса квоты подписок.
- [ ] `redemption`: `controller/redemption.go`, `model/redemption.go`.
- [ ] Контрактные тесты вебхуков (подпись, повтор, идемпотентность). ⏳ Подтвердить.

## Волна 5 — асинхронные задачи и batch ⬜

- [ ] Перевести persisted Task/Batch/Midjourney/violation log texts на English (translate persisted log texts to English); удалить metadata delegates единственного bridge после переноса consumers.

Отправка задачи провайдеру — шлюз; учёт задач, их биллинг и опрос — бизнес.

- [ ] `tasks`: `controller/task.go`, `midjourney.go`, `video_proxy.go`, `task_plugin*.go`, `service/task_polling.go`, `task_billing.go`, `midjourney.go`, `model/task*.go`, `model/midjourney.go`. Опрос использует `billing.settle`/`refund`.
- [ ] `batch`: `controller/batch.go`, `service/batch_billing.go`, `model/batch*.go`.
- [ ] Плагины задач: `controller/plugin_protocol*.go`, `SyncTaskPlugins`.
- [ ] Связка `service.GetTaskAdaptorFunc` (разрыв цикла `service → relay`) заменяется узким интерфейсом с реализацией в `adapter/gateway`.

## Волна 6 — настройки и система ⬜

- [ ] `settings`: `controller/option.go`, `model/option.go`, `SyncOptions`.
- [ ] `system`: `setup.go`, `system_info.go`, `system_task*.go`, `performance.go`, `perf_metrics.go`, `uptime_kuma.go`, отчёт об инстансе.
- [ ] `deployments`: `controller/deployment.go`.
- [ ] Остатки каталога: `controller/model.go`, `vendor_meta.go`, `prefill_group.go`, `missing_models`.
- [ ] Уведомления пользователям: `service/user_notify.go`.

## Волна 7 — доводка ⬜

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
- [ ] Контрактные тесты платежей.
- [ ] Волны 8 и 9 — делать или нет.

## Чистота репозитория

После слияния волны 1 удалить служебное (только с подтверждения):
- [ ] worktree `.worktrees/wave1-*` и ветки `wave1/*`;
- [ ] worktree `.claude/worktrees/agent-*`, ветки `worktree-agent-*` и `refactor/pricing-wave2`;
- [ ] теги `backup/pre-rebase-20261021`, `backup/pre-fixup-20261021`;
- [ ] `.worktrees/main-baseline`;
- [ ] stash `accounts-channels-before-rebase-main-20261020`;
- [ ] неотслеживаемые `client/` и `server/` в корневом checkout (остатки сборки).
