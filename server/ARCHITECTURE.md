# Архитектура backend

Документ описывает, как устроен backend сейчас и к какой раскладке мы его приводим. Правила оформления модулей — [internal/AGENTS.md](internal/AGENTS.md), карта уже перенесённых модулей — [internal/README.md](internal/README.md), порядок работ — [REFACTOR_ROADMAP.md](REFACTOR_ROADMAP.md).

Неизменные условия рефакторинга: пути, тела ответов, коды ошибок и заголовки API не меняются; схема БД и семантика запросов не меняются.

## Целевая раскладка

```
server/
  main.go, cmd/            # entry points: wiring and startup only
  internal/                # business: one folder per business area
    <module>/
      domain/              # concepts, rules, business errors; no Gin/GORM/Redis
      <scenario>/          # one use case = one package
        usecase.go         # logic + narrow interfaces it needs
        http.go            # parse request → call usecase → write response
      transport/           # module-wide error → HTTP status/code/i18n
      dto/                 # wire format shared by several scenarios
    adapter/
      gorm/<module>/record/  # DB table structs (GORM tags) and conversions
      gorm/<module>/         # stores over GORM
      redis/<module>/        # atomic cache operations (Lua)
      settings/<module>/     # reading options
      gateway/<impl>/        # what business needs from the gateway (channel test, upstream models)
    app/<module>.go        # wiring: usecases + adapters, background tasks Start<Task>
  relay/ + relay middleware  # gateway: replaceable implementation (see below)
  relaykit/                # separate Go module: shared gateway types for plugins
  pkg/billingexpr/         # standalone billing expression library
  i18n/, web/, docs/       # translations, embedded frontend, OpenAPI
```

Чтобы понять фичу, достаточно открыть папку сценария: в `usecase.go` логика, в `http.go` вход, интерфейсы показывают, что сценарию нужно снаружи, адаптеры — куда уходят данные.

Что сейчас лежит в старых слоях и куда переезжает:

| Сейчас | Куда |
|---|---|
| `controller/*` | `http.go` сценариев; в `controller/` остаётся только связка с роутером |
| `service/*` | `usecase.go` сценариев и `domain/` |
| `model/*` (GORM-структуры и запросы) | `internal/adapter/gorm/<module>/`; в `model/` остаются алиасы для ещё не перенесённого кода и список миграций |
| `router/*` | без изменений: пути и цепочки middleware — часть контракта |
| `middleware/auth.go` | тонкие обёртки над сценариями `identity`, `personal_access_tokens`, `apikeys`, `authorization` |
| `common/`, `setting/`, `logger/`, `constant/` | пока на месте; перенос в общий слой платформы — отдельное решение (см. roadmap) |

## Два входа: шлюз и панель

У backend два независимых способа доступа. Их принципалы — разные типы, сценарии одного входа не получают принципала другого.

| Вход | Кто | Учётные данные | Middleware | Принципал |
|---|---|---|---|---|
| API шлюза: `/v1/*`, `/v1beta/*`, `/mj/*`, видео, задачи, batch | клиенты, SDK | API-ключ `sk-…` (таблица `tokens`) | `TokenAuth()` | `Caller`: ключ, пользователь, группа, ограничения моделей, закреплённый канал |
| API панели: `/api/*` | фронтенд, скрипты владельца | токен сессии или PAT | `UserAuth` / `AdminAuth` / `RootAuth` + `RequirePermission` | `Principal`: пользователь, роль, сессия, признак PAT |
| Смешанный | отдельные эндпоинты | любой из двух | `TokenOrUserAuth()` | один из двух, по виду учётных данных |
| Только чтение | запросы баланса и использования ключа | ключ, даже просроченный, отключённый или исчерпанный | `TokenAuthReadOnly()` | `Caller` в режиме чтения |

`TokenAuth` ищет ключ в фиксированном порядке: `Sec-WebSocket-Protocol` (realtime), `x-api-key` (для `/v1/messages`, `/v1/models`), `?key=` и `x-goog-api-key` (Gemini-пути), `Authorization: Bearer`, `mj-api-secret`. Формат `sk-<key>-<channelId>` закрепляет канал для администратора. Этот порядок и тексты ошибок — контракт.

Целевое устройство:
- middleware остаётся тонким: достаёт учётные данные из запроса и вызывает сценарий-владелец (`apikeys/validate_key`, `identity/validate_session`; проверка PAT пока в `model.ValidateAccessToken` и станет сценарием `personal_access_tokens`);
- результат кладётся в контекст как типизированный принципал и, для совместимости, в прежние ключи контекста (`id`, `group`, `token_id`, `role`, …), пока их читает не перенесённый код;
- права администратора проверяет `authorization` через `RequirePermission`.

## Путь запроса шлюза

```
client
  │  router: CORS → Decompress → BodyStorageCleanup → Stats → RouteTag("relay")
  │          → SystemPerformanceCheck → TokenAuth → ModelRequestRateLimit → Distribute
  │
  ├─ TokenAuth ─────────► apikeys/validate_key            (business)  → Caller
  ├─ Distribute ────────► channel selection (gateway) over channel config from channels (business)
  │
  └─ relay handler (gateway)
        ├─ billing/reserve        (business)  hold quota on funding source + key
        ├─ provider adapter       (gateway)   relay/channel/<provider>: convert, call, parse usage
        │     └─ on failure: pick the next channel, retry
        ├─ billing/settle         (business)  actual cost → charge the delta or return the rest
        │  or billing/refund      (business)  release the hold on failure
        └─ usage/record           (business)  consumption log, statistics
```

### Граница со шлюзом

Шлюз (`relay/`, `relaykit/`, relay-цепочка `middleware/`) — заменяемая реализация: впоследствии его может заменить отдельный сервис. Поэтому:
- бизнес-модули не импортируют `relay`/`relaykit` (проверяет `internal/architecture_test.go`);
- всё, что шлюзу нужно от бизнеса, — обычные сценарии владельцев в `internal`: проверка ключа (`apikeys`), конфигурация каналов (`channels`), цены (`pricing`), резерв и расчёт (`billing`), запись использования (`usage`). Шлюз вызывает их, а не `model`/`service`;
- всё, что бизнесу нужно от шлюза (тест канала, загрузка моделей upstream), — узкий интерфейс в сценарии-потребителе, реализация в `internal/adapter/gateway/`.

Шлюз сам по себе может быть разложен по входам (chat, responses, claude messages, gemini, embeddings, images, audio, rerank, realtime, tasks) и реестру адаптеров провайдеров, но эта раскладка остаётся внутри шлюза и не делает его частью `internal`.

## Деньги: pricing и billing

- **`pricing`** отвечает, сколько стоит: цены, коэффициенты, каталог цен, публикация.
- **`billing`** отвечает, сколько списать с конкретного пользователя за конкретный запрос и с какого источника.

Жизненный цикл request-local резерва перенесён в `internal/billing`: создание, увеличение hold, settle, refund, legacy delta и notification eligibility. Gateway mapping и реализация существующего BillingSettler находятся в `relay/billing`; сборка — `internal/app/billing`; единственная реализация service bridge регистрируется в `relay/billing.init` при явном подключении gateway из relay entry points. Бизнес-модуль не зависит от Gin/GORM/Redis/relay/relaykit.

| Область | Владелец |
|---|---|
| reserve → settle → refund, wallet/subscription fallback и token quota | `internal/billing`, [README](internal/billing/README.md) |
| RelayInfo, request-local mutex, NewAPIError, Gin Refund | `relay/billing` |
| прежние cost/task consumers | `service/billing.go`: один временный BillingGateway bridge (включая Task/Batch metadata → usage), регистрация в `relay/billing.init`; root relay подключает gateway явно |
| durable Task и legacy Midjourney после отправки | `service/task_billing.go`, `service/midjourney.go` |
| Batch holds и terminal settlement | `service/batch_billing.go`, `model/batch*`, `BatchHeldQuota` |
| text/audio/realtime cost, tiered policy и FX | `internal/billing` cost-сценарии/domain; plain mapping в `relay/billing`, late settings и RAM FX в app/adapters |
| consume log content/other и storage | `internal/usage`, mapping `relay/usage`, serializer/storage `internal/adapter/gorm/usage`; готовые quota/facts без расчёта стоимости |
| expression engine и централизованная quota conversion | narrow adapters `internal/adapter/billing` → прежние `pkg/billingexpr`/`common`; чистая библиотека — волна 9 |
| provider discovery и unknown/capability policy | `relay/channel/openrouter/cost.go`, вне переноса 2b2 |
| атомарные reserve/delta primitives, Redis Lua, DB fallback и batch queue | `model/quota_reserve.go`, generic mutators; вызываются через `internal/adapter/gorm/billing` |

Initial reserve и OpenRouter additional reserve проверяют available quota с held; ordinary top-up и terminal debit сохраняют legacy debt. Повторный terminal вызов не повторяет применённые leg; funding и token partial failures учитываются отдельно. Это не гарантия durable exact-once после падения процесса.

Refund: синхронный terminal gate и прежняя async job; subscription initial refund до 3 попыток с 200/400 ms, wallet/token/extraReserved — по одной попытке. Notification policy принадлежит billing, доставка через adapter → прежний NotifyUser, язык получателя с English fallback.

Общие model primitives не копируются и не импортируют новый модуль; их перенос в технические адаптеры отложен до волны 7. Расчёт стоимости 2b2 выделен в пять action-пакетов; accepted bridge остаётся для Batch/Task/Midjourney/violation и неизменных billing/WSS entrypoints до волны 5; recording metadata делегируется usage одним serializer через app-owned TaskMetadata port. Usage composition вызывается явно через relay/usage.Recording и sync.OnceValue; gateway не импортирует storage adapter. Инициализация не зависит от main: gateway регистрирует единственную реализацию при linking; pricing composition использует billing FX scenario непосредственно. Host acceptance 2b2 завершён: 126 immutable goldens, WSS, full suite, vet/OpenAPI/U1000 и PostgreSQL 15.19 (границы — REFACTOR_ROADMAP.md). Host acceptance 2c завершён: 154 goldens без -update, затем English content (только поле `content`), WSS, full suite и PostgreSQL 15.19.

## Таблицы БД

- Структура таблицы (GORM-теги) лежит в адаптере модуля-владельца: `internal/adapter/gorm/<module>/record/`. Пример: `record.Token` в `internal/adapter/gorm/apikeys/record/token.go`.
- В `model/` остаётся алиас для не перенесённого кода: `type Token record.Token` (`model/token.go`).
- Доменные типы (`<module>/domain`) — отдельные структуры без тегов; преобразование — в `record/conversion.go`. Структура таблицы не выходит за пределы адаптера, usecase видит только domain.
- Список миграций (`AutoMigrate` в `model/main.go`) остаётся на месте: состав и порядок таблиц — часть поведения БД.
- Сейчас в `internal/adapter/gorm/*/record/` лежат таблицы accounts, apikeys, authorization, catalog, channels, pricing, usage и audit; остальные пока в `model/`.

Код обязан работать на SQLite, MySQL и PostgreSQL (правила — корневой `CLAUDE.md`, Rule 2).

## Фоновые задачи

Запускаются из `main.go`. Задачи только для master-узла защищены `common.IsMasterNode`. Целевое место — `internal/app/<module>.go` (`Start<Task>`), сам прогон — обычный сценарий без тикеров.

| Задача | Сейчас | Владелец |
|---|---|---|
| кэш каналов | `model.SyncChannelCache` | channels |
| перечитывание настроек | `model.SyncOptions` | settings |
| синхронизация политики прав | `authz.StartPolicySync` | authorization |
| агрегаты для дашборда | `internal/app/usage.StartQuotaData` | usage |
| обновление баланса каналов | `controller.AutomaticallyUpdateChannels` (если задан `CHANNEL_UPDATE_FREQUENCY`) | channels |
| периодические системные задачи: тест каналов, модели upstream, опрос Midjourney и асинхронных задач, сборка каталога цен | `controller.RegisterScheduledSystemTasks` + `service.StartSystemTaskRunner` | channels, tasks, pricing; запуск — system |
| плагины задач | `controller.SyncTaskPlugins` | tasks |
| обновление Codex-учётных данных | `service.StartCodexCredentialAutoRefreshTask` | channels |
| сброс квоты подписок | `service.StartSubscriptionQuotaResetTask` | subscriptions |
| курсы валют для цен | `service.StartModelCostFX` | pricing |
| отчёт об инстансе | `service.StartSystemInstanceReporter` | system |
| очистка артефактов входа | `service.StartAuthArtifactCleanup` | identity |
| пакетная запись счётчиков | `internal/app.StartBatchUpdater` | shared counter composition |

Интервалы, порядок шагов и master-only поведение при переносе не меняются.

## Язык текстов

- Ответы клиенту — через backend i18n (`i18n/locales`), ключи минимум en + ru.
- Логи — на английском, без i18n.
- Фоновые и сохраняемые тексты без запроса — на языке пользователя-получателя (для каналов — root), а если язык не задан — на английском.

### Технические aliases records в model

Файл пакета `model` может импортировать ровно один пакет
`internal/adapter/gorm/<module>/record` без записи в architecture allowlist,
только если все его декларации кроме самого import — `type X = record.X`
либо прямые const re-exports `const X = record.X`. Имя re-export совпадает
с именем исходного символа. Других imports, funcs, methods, vars, init,
вычисляемых/неявных constants и defined types в таком файле быть не может.
Это проверяется по полному AST; отрицательные случаи покрыты тестом.
Исключение относится только к техническим aliases, а не к вызовам бизнеса
из model. `service` под исключение не попадает. Allowlist по-прежнему
может только сокращаться.

### Usage read side (3a)

Action-local HTTP handlers и узкие consumer-owned ports собираются в internal/app/usage.
Controller bridge использует sync.OnceValue; routers/middleware не меняются.
Log/QuotaData records и общий raw-preserving metadata codec принадлежат gorm/usage/record;
HTTP DTO и domain.VisibilityPolicy не зависят от хранимой JSON формы. Domain/scenarios выбирают
visibility, identity scope, validation, grouping, count limits, ranking windows и TTL; adapters
исполняют SQL/codec/cache operations. Read SQL использует прежние main/LOG_DB и dialect branches.

Планирование quota export — internal/app/usage; общих user/token/channel counter flush — internal/app/background.go. До волны 7 единственные RAM
cache/queue и технические producer primitives остаются в model (A2). Legacy Gin writers делегируют
plain LogObservation writers; usage adapter Gin не импортирует (#36 закрыт). system_task cleanup
остаётся technical service/model residue по A4; новый reverse import или locator не вводится.

Usage domain errors сопоставляются в transport через errors.Is/errors.As и i18n.MsgUsage*;
ranking period — typed error с полем Period. Storage-specific malformed/rejection правила
не передаются в VisibilityPolicy: codec выводит их из permissions. Legacy LogOther consumers
используют zero-value type alias, без дополнительного model constructor bridge.
Архитектурный тест, как и до 3a, пропускает файлы `_test.go`; AST-исключение применяется к production aliases.

### Audit

`internal/audit` владеет admin_list_events/list_own_events/record_event. Domain выбирает
owner/actor/category, metadata и visibility; transport захватывает plain request-local snapshots,
а единственный write Execute вызывается после операции. Records, JSON codec и SQL принадлежат
gorm/audit; model сохраняет только alias-only surface и отдельный migration helper с прежним DDL.
`internal/app/audit` не импортирует middleware/parent app; controller/middleware используют
sync.OnceValue bridges. Read/write остаются на LOG_DB, routes/auth ordering неизменны.
PAT last-use SQL пока остаётся в PAT adapter (#44), без встречной зависимости audit → PAT.
