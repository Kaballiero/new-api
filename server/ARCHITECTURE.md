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

Отдельный модуль нужен потому, что деньги списываются из нескольких мест, и у правил списания должен быть один владелец:

| Кто списывает | Где сейчас |
|---|---|
| обычный запрос шлюза | `service/billing_session.go` (`BillingSession`: Reserve → Settle → Refund) |
| асинхронные задачи: резерв при отправке, итог при опросе, возврат при сбое | `service/task_billing.go`, `service/midjourney.go` |
| batch: удержание квоты на весь пакет | `service/batch_billing.go`, поля `BatchHeldQuota` |
| realtime (WebSocket) | `BillingSession.SettleWss` |
| источник оплаты: кошелёк или подписка | `service/funding_source.go` |
| ступенчатая цена и фактическая стоимость провайдера | `service/tiered_settle.go`, `pkg/billingexpr`, `relay/channel/openrouter/cost.go` |
| атомарный резерв в кэше | `model/quota_reserve.go` (Lua-скрипты Redis) |

Инварианты `billing`: не списать дважды; резерв → итог → доплата или возврат разницы; нельзя уйти в минус с учётом удержанного; возврат при сбое повторяется до успеха.

Целевая раскладка:

```
internal/billing/
  domain/       # Reservation, FundingSource (wallet|subscription), settle/refund rules
  reserve/      # hold quota on funding source + key before the call
  settle/       # actual cost → charge the delta or return the rest
  refund/       # release the whole hold on failure
  cost/         # quota from usage: ratios, tiered expression (pkg/billingexpr), fx
internal/adapter/
  gorm/billing/record/   # quota rows, held quota, subscription usage
  redis/billing/         # Lua scripts from model/quota_reserve.go
```

`BillingSession` сейчас держит `*gin.Context` и `RelayInfo`; в целевой схеме резерв — доменный объект без Gin, состояние запроса передаётся в него явно. Шлюз, задачи и batch вызывают одни и те же сценарии.

## Таблицы БД

- Структура таблицы (GORM-теги) лежит в адаптере модуля-владельца: `internal/adapter/gorm/<module>/record/`. Пример: `record.Token` в `internal/adapter/gorm/apikeys/record/token.go`.
- В `model/` остаётся алиас для не перенесённого кода: `type Token record.Token` (`model/token.go`).
- Доменные типы (`<module>/domain`) — отдельные структуры без тегов; преобразование — в `record/conversion.go`. Структура таблицы не выходит за пределы адаптера, usecase видит только domain.
- Список миграций (`AutoMigrate` в `model/main.go`) остаётся на месте: состав и порядок таблиц — часть поведения БД.
- Сейчас в `internal/adapter/gorm/*/record/` лежат таблицы accounts, apikeys, authorization, catalog, channels и pricing; остальные пока в `model/`.

Код обязан работать на SQLite, MySQL и PostgreSQL (правила — корневой `CLAUDE.md`, Rule 2).

## Фоновые задачи

Запускаются из `main.go`. Задачи только для master-узла защищены `common.IsMasterNode`. Целевое место — `internal/app/<module>.go` (`Start<Task>`), сам прогон — обычный сценарий без тикеров.

| Задача | Сейчас | Владелец |
|---|---|---|
| кэш каналов | `model.SyncChannelCache` | channels |
| перечитывание настроек | `model.SyncOptions` | settings |
| синхронизация политики прав | `authz.StartPolicySync` | authorization |
| агрегаты для дашборда | `model.UpdateQuotaData` | usage |
| обновление баланса каналов | `controller.AutomaticallyUpdateChannels` (если задан `CHANNEL_UPDATE_FREQUENCY`) | channels |
| периодические системные задачи: тест каналов, модели upstream, опрос Midjourney и асинхронных задач, сборка каталога цен | `controller.RegisterScheduledSystemTasks` + `service.StartSystemTaskRunner` | channels, tasks, pricing; запуск — system |
| плагины задач | `controller.SyncTaskPlugins` | tasks |
| обновление Codex-учётных данных | `service.StartCodexCredentialAutoRefreshTask` | channels |
| сброс квоты подписок | `service.StartSubscriptionQuotaResetTask` | subscriptions |
| курсы валют для цен | `service.StartModelCostFX` | pricing |
| отчёт об инстансе | `service.StartSystemInstanceReporter` | system |
| очистка артефактов входа | `service.StartAuthArtifactCleanup` | identity |
| пакетная запись счётчиков | `model.InitBatchUpdater` | usage |

Интервалы, порядок шагов и master-only поведение при переносе не меняются.

## Язык текстов

- Ответы клиенту — через backend i18n (`i18n/locales`), ключи минимум en + ru.
- Логи — на английском, без i18n.
- Фоновые и сохраняемые тексты без запроса — на языке пользователя-получателя (для каналов — root), а если язык не задан — на английском.
