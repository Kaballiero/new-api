# Billing — жизненный цикл резерва квоты

## Ответственность

Модуль владеет request-local резервом (`domain.Reservation`), выбором кошелька или подписки, квотой API-ключа, допрезервом, итоговой дельтой, возвратом и условиями quota notifications. Все суммы передаются готовыми целыми quota units; расчёт цены и округление сюда не входят.

## Возможности

| Сценарий | Действие и потребитель |
|---|---|
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
- `service.BillingLifecycle` — один временный интерфейс в терминах старого потребителя. `relay/billing.init` регистрирует единственную production реализацию; `relay/relay_adaptor.go` явно подключает её для всех relay entry points, включая plugin adapters. Service-only и adaptor-only binaries подключают `relay/billing` явно; тестовые binaries используют тот же механизм. Отдельного wiring в main или contracttest нет.

## Границы и долг

Cost, FX, tiered expressions, usage/consume logs, durable Task/Batch settlement, выбор канала и upstream submission остаются у прежних владельцев. Batch не переводится на Reservation: его held quota лишь учитывается существующими атомарными reserve primitives.

Initial wallet и OpenRouter additional reserve проверяют доступный остаток с held. Обычный wallet increase и terminal wallet/token debit сохраняют возможность долга. Ошибка token после успешного обычного funding settle не запускает refund funding; known-cost OpenRouter failures сохраняют applied outcome для ручного ремонта. WSS может повторить только неуспешную terminal leg. Это request-local защита, не durable exact-once при падении процесса.

Subscription initial refund повторяется максимум 3 раза с 200/400 ms; extraReserved/token/wallet — одна попытка. `TECH_DEBT.md` #26–29 описывают legacy debt, nested subscription refund transaction, notification snapshot и несовместимость placeholders с webhook renderer. Шаблон и отдельное поле values quota webhook сохранены. Общие cache primitives остаются в model до волны 7.

Мосты: `service/billing.go` (PreConsumeBilling, SettleBilling, SettleWssBilling, PostConsumeQuota/partial result) — только для оставшихся cost/task/legacy consumers до волн 2b2/5. Новых исключений архитектурного теста нет.

## Проверки

62 HTTP/state goldens в `testdata/golden` записаны на origin/main до переноса; их нельзя редактировать или перезаписывать. `TestBillingHTTPContract` проверяет реальные middleware/relay → fake HTTP provider, DB/Redis accounting и logs, нужен host с sockets. Нормализуются только оговорённые timestamp/header Date и log durations, финансовые поля сохраняются.

Сценарные регрессии на fakes узких ports находятся в `<scenario>/usecase_test.go`: preferences/trust/held admission, delta/partial settlement, increase rollback, refund/retry и notification eligibility. `relay/billing/billing_test.go` проверяет только gateway projection/outcome, error mapping en/ru, lazy nil-safe язык и сериализацию concurrent Session calls под gateway mutex. `adapter/billing/notification_test.go` проверяет прежний webhook payload; `adapter/gorm/billing/TestBillingAdapterPostgres` требует изолированный `TEST_POSTGRES_DSN` и сообщает фактическую версию PostgreSQL. Default HTTP fixture — SQLite. Реальный PostgreSQL 15, SSE/WSS и полный suite запускает host-оркестратор; отсутствие такого прогона нельзя считать доказанной совместимостью.
