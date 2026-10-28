# Архитектура backend

Backend разделён на бизнес-модули `internal/`, технические адаптеры и заменяемый AI gateway.
Правила структуры — [internal/AGENTS.md](internal/AGENTS.md), карта модулей —
[internal/README.md](internal/README.md), статус работ — [REFACTOR_ROADMAP.md](REFACTOR_ROADMAP.md).
Маршруты, middleware, JSON/status/header contract, SQL, схема и порядок миграций сохраняются.

## Раскладка

```text
server/
  main.go, cmd/              entrypoints, startup
  router/                    paths, middleware and permissions
  controller/                one-line router bindings, OpenAPI wire declarations
  internal/
    <module>/
      <scenario>/            http.go, dto.go, usecase.go and consumer-owned ports
      domain/                plain facts, business rules and errors
      dto/, transport/       shared module wire format and HTTP error mapping
    app/                     composition and background lifecycle
    adapter/                 GORM, Redis, codecs, settings, HTTP and gateway adapters
    contracttest/            shared test fixtures
  model/                     migration-used table aliases, DB bootstrap and migrations
  service/testdata/          immutable fixtures; no Go package
  gateway/                   ingress, routing, mapping, tokenusage, metrics
  relay/                     provider transports, billing mapping and protocol execution
  relaykit/                  independent module: gateway DTOs and conversion codecs
  pkg/                       HTTP/media/text helpers, expression engine and runtime libraries
  common/, setting/, logger/, constant/  existing shared infrastructure
```

Сценарий читается сверху вниз: HTTP разбирает ввод → usecase выбирает проверки и действия →
узкие порты выполняют I/O через adapters. App создаёт зависимости; request context передаётся
операции, а не shared constructor. Запросные channel stores сохраняют отдельный lifetime.

Controller не хранит state, constructors, business rules или callbacks. HTTP once-сборка, login/profile/
audit callbacks и channel startup effects находятся в `app/httpapi`. Router bindings и реальные
wire declarations OpenAPI остаются разрешённой границей. Production `service` и BillingGateway
registry отсутствуют; fixtures не образуют пустой пакет ради старых путей tests.

## Два входа

| Вход | Credentials | Middleware | Результат |
|---|---|---|---|
| Gateway `/v1`, `/v1beta`, `/mj`, tasks, Batch | API-ключ | TokenAuth | apikeys Caller и прежняя context projection |
| Панель `/api` | dashboard session или PAT | UserAuth/AdminAuth/RootAuth, RequirePermission | identity principal и авторизация владельца |
| Смешанные endpoint | API-ключ либо session/PAT | TokenOrUserAuth | principal выбранного credential path |
| Read-only usage ключа | API-ключ | TokenAuthReadOnly | expired/exhausted разрешены, disabled отклоняется |

TokenAuth сохраняет приоритет realtime Sec-WebSocket-Protocol → x-api-key для messages/models →
Gemini query/header → Authorization Bearer → mj-api-secret. Pin канала администратора и model/group/IP
ограничения сохраняются. `app/authentication` собирает gateway-key и PAT проверки без обратного
импорта middleware. Identity core — `app/identitycore`, SQL — `adapter/gorm/identity`, SDK/crypto/cookies —
identity adapters. Сервер проверяет версии сессий, scoped proofs, expiry/single-use и полномочия.
Casbin имеет один enforcer/mutex в adapter; `app/authorizationcore` собирает init, checks и sync.

## Граница со шлюзом

Gateway ingress, plugin session, playground и debug принадлежат `gateway/transport`.
Нижние mapping/routing/tokenusage leaves не импортируют transport. Channel probe —
`adapter/gateway/channels`, protocol DTO/convert engine — независимо собираемый `relaykit`.
Существующие provider files в `relay/channel` сохраняют расположение.

Бизнес не импортирует gateway DTO или transport. Для pricing разрешена только прежняя цепочка
`constant → relaykit/types → relaykit/relayconvert/kitutil` для EndpointType/FinishReason.
Правило `pricing-constant-alias-leaf` проверяет точные пакеты и рёбра; другие gateway paths,
транзитивные common/billingexpr/jsplugin/catalogsync запрещены. Выделение constant leaf — optional Wave 9.

Что gateway нужно от бизнеса, он получает через owning scenarios/app capabilities: проверка ключа,
каналы, цены, reserve/settle/refund и usage recording. Обратные операции (probe, upstream models)
объявляются узкими портами у потребителя; app подключает adapter/gateway.

## Владельцы данных и состояния

| Ответственность | Владелец / сборка |
|---|---|
| Аккаунты, lifecycle, check-in, GetAPI PAT initialization | accounts; gorm/accounts/core и record, app/accountscore/accountstorage |
| API keys, quota/hash/fence | apikeys; gorm/apikeys, cache/apikeys, app/apikeystorage |
| Sessions, proofs, factors, OAuth claims | identity; gorm/identity и identity adapters, app/identitycore/identitystorage |
| Dashboard PAT lifecycle | personal_access_tokens; app/patstorage и app/authentication |
| Authorization policy | authorization; Casbin/GORM adapters, app/authorizationcore |
| Model/vendor metadata, prefill, availability | catalog; gorm/catalog, app/catalog и app/pricingruntime |
| Channel records/cache/abilities/state/Codex | channels; gorm/channels, adapter/channels, app/channelcore/channelruntime |
| Plugin versions, origin/query, observation policy | taskplugins; gorm/taskplugins, adapter/taskplugins, app/taskpluginsruntime |
| Один alias generation/TTL view | adapter/taskplugins/aliases, channels SQL reader |
| Pricing publication/build/observation | pricing; adapter/pricing/state, app/pricingruntime |
| Option publication, aliases, group-ratio mutex | adapter/settings/publication; settings и native pricing используют один publisher |
| FX SQL/RAM/HTTP, refresh и replica reload | pricing adapters, refresh_cost_fx, app/pricingfx |
| Request-local billing | billing scenarios; app/billing, relay/billing mapping |
| Durable Task/MJ accounting, polling и media | tasks; gorm/tasks, app/tasks/taskruntime/taskbillingruntime |
| Native Batch holds/results/procurement/settlement | batch; gorm/redis/gateway adapters, app/batch/batchruntime |
| Top-up / subscription / redemption workflows | payments / subscriptions / redemption; общий adapter/wallet |
| Usage logs, metadata codec и RAM export | gorm/usage.Writer, app/usagestorage/usage |
| Пять counter maps и flush | gorm/counters, app/background.go |
| Shared row lock / SQL clock / plan cache | gorm/locking, gorm/dbclock, cache/subscriptions |
| Audit policy/recording | audit; app/audit.Recording, callbacks app/httpapi |
| Notification delivery/limiter/root/watchers | notifications; app/notifications, adapter/notifications RAM counter |
| Setup/system task registry/leases/history | system; app/system/systemruntime, gorm/system |
| Metrics core/loop, aggregation/SQL/Redis | app/systemmetrics, runtime/gorm/redis adapters; gateway/metrics projection |
| HTTP pools/proxy/protected fetch / media / text filter | pkg/httpclient / pkg/media / pkg/textfilter |
| Artifact HMAC и filesystem backend | adapter/artifacts |

Catalog metadata deletion и pricing edits выполняются на исходном tx через явную PricingMutation
capability, с прежним option-row anchor и lock order. Один publisher используется settings single/bulk/
reload и native pricing; PricingCatalog публикуется до общего OptionMap lock. State не копируется.

## Деньги и side effects

Pricing определяет цену; billing определяет списание и funding source конкретного запроса.
Gateway выполняет reserve → provider call → count/settle → usage record в прежнем порядке;
ретраи, frozen FX basis, quota rounding/clamp и unknown-cost policy принадлежат исходным владельцам.
Durable Task/Batch orchestration остаётся отдельно от request-local Reservation.
Unknown/null cost не считается нулём; explicit zero сохраняется.

Wallet credit выполняется в том же tx, что payment/redemption/subscription mutation. Cache sync и receipt
следуют после commit. Task status CAS и финансовые эффекты сохраняют прежние границы, без новой
exact-once гарантии. Ошибки и partial effects описаны в [TECH_DEBT.md](TECH_DEBT.md).

Usage export держит lock через I/O и очищается при failure; counter flush делает swap-before-I/O,
failed counters не requeue-ятся. Log writer и main/log cleanup не становятся атомарной транзакцией.
Audit — best-effort отдельный write после операции; credentials/codes/usable tokens в payload не добавляются.

## DB bootstrap и миграции

`model` содержит только 46 migration-used table aliases, DB/LOG_DB, dialect column state, bootstrap,
logger/dialect wrappers и существующие migrations/backfills. Alias-файл импортирует одного record owner
и содержит только `type X = record.X`; вспомогательные types, const/error re-exports отсутствуют.
Каждый alias имеет migration/bootstrap consumer. AutoMigrate и backfills сохраняют прежний порядок.
Auth-version initialization и Telegram claim backfill используют owning record primitive на том же tx.

Storage adapters сохраняют SQLite/MySQL/PostgreSQL branches и отдельный ClickHouse LOG_DB.
Принятый объём acceptance — PostgreSQL-only; **SQLite/MySQL not checked**. Source/fixture checks
не означают полную совместимость с БД или подтверждение production runtime.

## Фоновые задачи

Один pass — owning scenario. Тикеры/master gates/once/recover принадлежат app; интервалы и порядок прежние.
`app/httpapi.RegisterScheduledSystemTasks` регистрирует handlers перед `app/system.StartSystemTaskRunner`.
Runner проверяет master внутри once; channel cache sync остаётся sleep-first/seconds на каждом узле;
Codex once сохраняет master-check-inside-Do, immediate pass и 10m ticker; balance loop — sleep-first/minutes.
Auth cleanup — master check до goroutine, immediate pass и 1h, повторный Start не дедуплицируется.
FX replica reload — once/immediate/minute на каждом узле. Metrics flush — sleep-first с поздними настройками,
без нового master/once; notifications memory cleanup — lazy once и sleep-before-hourly-pass.

## Язык и проверки границ

HTTP errors используют backend i18n: English default, ru, сохранённые zh-CN/zh-TW тексты.
Новые логи — English; унаследованные error/log bytes сохраняются (см. TECH_DEBT).
Notifications/receipts используют язык получателя, channel notifications — root.
Исторические строки БД не мигрируются. Bulk task fail_reason остаётся общим English текстом (#75).

Architecture allowlist пуст. `TestInternalArchitecture` проверяет imports/purity/D1, а
`TestLegacyArchitectureClosure` — no BRIDGE, отсутствие service Go files и строгий model whitelist.
Контракты используют неизменяемые fixtures; OpenAPI проверяется также обычной генерацией без diff.
Host/real Redis/PostgreSQL evidence и статус приёмки фиксируются в roadmap и отчётах отдельно.
