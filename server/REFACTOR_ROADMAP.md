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
| 2 | Граница шлюза: проверка ключа, billing, запись использования | ⬜ | высокий |
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

## Волна 2 — граница шлюза ⬜

Что шлюзу нужно от бизнеса — сценарии владельцев; код шлюза (`relay/`) остаётся снаружи `internal`.

- [ ] Разбор: `middleware/auth.go` (`TokenAuth`, `TokenAuthReadOnly`, `TokenOrUserAuth`, `SetupContextForToken`), `model.ValidateUserToken`, `service/billing_session.go`, `funding_source.go`, `quota.go`, `text_quota.go`, `tiered_settle.go`, `billing_fx.go`, `model/quota_reserve.go`, запись логов потребления.
- [ ] `apikeys`: проверка ключа для шлюза на сценарии `validate_key` → типизированный `Caller`; `TokenAuth` становится тонкой обёрткой (порядок поиска ключа и ошибки прежние; прежние ключи контекста сохраняются).
- [ ] `personal_access_tokens`: сценарий проверки PAT вместо `model.ValidateAccessToken`.
- [ ] Новый модуль `billing`: `reserve`, `settle`, `refund`, `cost`; источник оплаты (кошелёк, подписка); Lua-скрипты резерва → `adapter/redis/billing`.
- [ ] `BillingSession` без `*gin.Context`: шлюз вызывает сценарии `billing` через узкий интерфейс.
- [ ] Новый модуль `usage`: сценарий записи потребления (лог запроса), его вызывает шлюз.
- [ ] Контрактные тесты: списание и возврат на типовых сценариях (обычный запрос, ошибка провайдера, подписка, realtime). ⏳ Подтвердить.

## Волна 3 — учёт и аналитика ⬜

- [ ] `usage`: `controller/log.go`, `log_query.go`, `usedata.go`, `usedata_bucket.go`, `usage_aggregate.go`, `rankings.go`, `model/log*.go`, агрегаты `model.UpdateQuotaData`, пакетная запись счётчиков `model.InitBatchUpdater`.
- [ ] `audit`: `controller/audit.go`, `model/audit_*.go`.
- [ ] Отделить запросы к `LOG_DB` в адаптер (отдельная БД логов сохраняется).

## Волна 4 — платежи, подписки, коды ⬜

- [ ] `payments`: `controller/topup*.go` (Stripe, Creem, Waffo, Waffo Pancake, Epay), `payment_webhook_availability.go`, `payment_compliance.go`, `model/topup.go`. Каждый провайдер — адаптер, вебхук — сценарий.
- [ ] `subscriptions`: `controller/subscription*.go`, `model/subscription.go`, задача сброса квоты подписок.
- [ ] `redemption`: `controller/redemption.go`, `model/redemption.go`.
- [ ] Контрактные тесты вебхуков (подпись, повтор, идемпотентность). ⏳ Подтвердить.

## Волна 5 — асинхронные задачи и batch ⬜

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

## Открытые решения

- [ ] PR на каждую волну вместо одного большого PR в конце.
- [ ] Контрактные тесты для billing и платежей.
- [ ] Волны 8 и 9 — делать или нет.

## Чистота репозитория

После слияния волны 1 удалить служебное (только с подтверждения):
- [ ] worktree `.worktrees/wave1-*` и ветки `wave1/*`;
- [ ] worktree `.claude/worktrees/agent-*`, ветки `worktree-agent-*` и `refactor/pricing-wave2`;
- [ ] теги `backup/pre-rebase-20261021`, `backup/pre-fixup-20261021`;
- [ ] `.worktrees/main-baseline`;
- [ ] stash `accounts-channels-before-rebase-main-20261020`;
- [ ] неотслеживаемые `client/` и `server/` в корневом checkout (остатки сборки).
