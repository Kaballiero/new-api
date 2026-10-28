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
| `charge_violation_fee` | Выбор fee по поздним настройкам, deduction через request-bound port; gateway пишет counters/log после успеха |
| `notify_quota` | Позднее чтение snapshot/порога внутри прежней async job; wallet/subscription eligibility; доставка через порт |

Собственных HTTP-маршрутов и фоновых циклов нет. Потребители — существующие relay API с API-ключом, realtime, отправка задач, legacy Midjourney/violation fee и `/pg/chat/completions` с существующей сессией (PAT playground по-прежнему отклоняет). Порядок middleware и полномочия не менялись.

## Зависимости и сборка

Domain/usecase не импортируют Gin, GORM, Redis, common, model, service, relay или relaykit.
Порты storage/notifier принадлежат потребителям. `app/billing` собирает stateless scenarios;
`relay/billing` отображает Gin/RelayInfo и хранит request-local mutex/state. Gateway подключается
непосредственно потребителями: один default без mutable registry или init-registration.

`adapter/gorm/billing` владеет reserve SQL/Redis fallback и subscription delta; `app/quotaruntime`
подключает один account/key cache state. Shared row lock — `adapter/gorm/locking`, пять counter maps —
`adapter/gorm/counters`; flush меняет maps под mutex до I/O и читает DB getter для каждого SQL update.
`adapter/billing/cost_math.go` вызывает прежние checked quota conversions и expression engine.
Формулы, rounding, clamp и SysError не копируются. Pure math/engine leaf — отдельный долг #32.

Canonical provider usage выбирает `gateway/tokenusage.EffectiveBillingUsage`; те же facts использует Batch.
FX capture читает опубликованную RAM pricing через settings adapter; frozen basis хранится в request.
`domain.PublishedUSD` проверяет currency validity, не меняя funding или denomination policy.
Usage имеет единственный serializer; `relay/usage.Recording` — обычная явная gateway composition.
Task/Batch передают FX/clamp/tiered/plugin metadata напрямую. Delivery/limiter принадлежат notifications;
`app/billing` передаёт Send в renderer с прежними recipient-language templates и values.

`charge_violation_fee` читает поздние настройки и выполняет request-bound deduction. Gateway сохраняет
refund → fee deduction → counters/log и captured group ratio; decimal conversion — adapter/billing.
Task funding использует `apply_quota_delta` через task-scoped SQL capabilities на переданном tx;
durable status/claim/finalize оркестрирует tasks. Plugin observation принадлежит taskplugins,
protocol submission и detached bounded execution — gateway/transport.

## Границы и долг

Request-local защита не даёт durable exact-once после crash. Initial wallet/OpenRouter reserve учитывает
held; ordinary wallet increase и terminal debit сохраняют возможность долга (#26).
Ошибка token после успешного funding settle не запускает refund funding. WSS повторяет только
неуспешную terminal leg; known-cost OpenRouter failures сохраняют applied outcome для ремонта.
Subscription initial refund допускает прежние три попытки с 200/400ms; остальные legs — одну.
Nested subscription refund #27, notification snapshot #28 и webhook renderer #29 сохранены.
Text/audio accounting требует ненулевой Gin context; отсутствие Context.Request допустимо.
Nil Gin context поддержан только прежними lifecycle/error entrypoints.

Чистые decision/failure-order регрессии находятся рядом со сценариями, gateway mapping — в relay/billing.
Добавленная snapshot suite удалена по решению владельца; новые проверки используют прямые assertions.
Production service facades и BillingGateway registry отсутствуют.
