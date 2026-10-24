# Payments — пополнения через платёжных провайдеров

## Ответственность

Модуль владеет top-up orders: историей, расчётом суммы, созданием checkout, подтверждением через webhook,
переходами pending/success/failed/expired и ручным завершением. Источник истины — top_ups в main DB.
Он также владеет payment availability, подтверждением compliance и конфигурационным потоком Pancake.
Подписки, их планы, completion и quota reset принадлежат internal/subscriptions.

## Возможности

| Сценарий | Маршрут | Доступ / особенность |
|---|---|---|
| get_topup_info | GET /api/user/topup/info | UserAuth, session/PAT; enable_redemption сохраняется |
| list_own_topups | GET /api/user/topup/self | UserAuth; свой user ID, окно 30 дней |
| admin_list_topups | GET /api/user/topup | AdminAuth, session/PAT; история всех пользователей без окна |
| complete_topup | POST /api/user/topup/complete | AdminAuth; прежняя manual formula и repeat receipt |
| quote_epay_topup | POST /api/user/amount | UserAuth; decimal quote |
| create_epay_topup | POST /api/user/pay | UserAuth + CriticalRateLimit; подписанная форма, без исходящего HTTP |
| receive_epay_notify | GET/POST /api/user/epay/notify | Public; SDK signature, POST использует только PostForm |
| quote_stripe_topup | POST /api/user/stripe/amount | UserAuth; group multiplier входит в credited quota |
| create_stripe_topup | POST /api/user/stripe/pay | UserAuth + CriticalRateLimit; remote checkout перед local Insert |
| receive_stripe_webhook | POST /api/stripe/webhook | Public; подпись SDK, subscription-first dispatch |
| create_creem_topup | POST /api/user/creem/pay | UserAuth + CriticalRateLimit; local Insert перед checkout |
| receive_creem_webhook | POST /api/creem/webhook | Public; HMAC, subscription-first до onetime check |
| quote_waffo_topup | POST /api/user/waffo/amount | UserAuth; прежний float quote |
| create_waffo_topup | POST /api/user/waffo/pay | UserAuth + CriticalRateLimit; Enabled до bind, method index имеет приоритет |
| receive_waffo_webhook | POST /api/waffo/webhook | Public; SDK verify и signed ACK; любой non-success переводит pending в failed |
| quote_pancake_topup | POST /api/user/waffo-pancake/amount | UserAuth; decimal quote |
| create_pancake_topup | POST /api/user/waffo-pancake/pay | UserAuth + CriticalRateLimit; authenticated checkout, два параллельных SDK calls |
| receive_pancake_webhook | POST /api/waffo-pancake/webhook/:env | Public; подпись, env и buyer identity; prefix dispatch |
| confirm_compliance | POST /api/option/payment_compliance | RootAuth; только dashboard session, PAT запрещён |
| list_pancake_catalog | GET /api/option/waffo-pancake/catalog | RootAuth, session/PAT; fallback creds только при двух пустых typed полях, active products |
| create_pancake_pair | POST /api/option/waffo-pancake/pair | RootAuth; store → product → publish, partial orphan-store result |
| save_pancake_config | POST /api/option/waffo-pancake/save | RootAuth; atomic bulk options, blank private key сохраняет текущий |

Итого 22 сценария и 23 method/path registrations. Router, middleware и permissions не менялись.
User endpoints сохраняют DisableCache; public POST callbacks — AnonymousRequestBodyLimit.
Lists принимают только keyword и прежнюю pagination (p/page_size/ps/size); остальные фильтры игнорируются.
Search pattern принадлежит apikeys/domain. Count и Find остаются в одной read transaction с прежним
rollback-only panic recovery и partial destinations; count limit поиска остаётся 10000.

## Зависимости и транзакции

Domain содержит чистые Order/User/provider facts, availability, pricing и общую Completion policy.
Он не содержит GORM/JSON stored shape и не зависит от Gin, common или SDK. Общие payment statuses и
method/provider constants определены здесь; record/model/common сохраняют необходимые прямые aliases.
Сценарии объявляют consumer-owned ports, получают зависимости через NewUsecase и возвращают error. Чистая info projection возвращает Info без фиктивной ошибки.
HTTP хранит wire DTO и сопоставляет domain errors через errors.Is/errors.As с i18n.MsgPayments*.
Raw storage/SDK/bind ошибки не распознаются по тексту.

SQL, records и projection — adapter/gorm/payments; исходящие API/signature codecs — adapter/http/payments
с отдельными epay/stripe/creem/waffo/pancake. Настройки читаются поздно через adapter/settings/payments.
Сборка — app/payments, controller singleton — sync.OnceValue. Она работает в любом linked binary и не
требует main-only регистрации. Диагностика получает request context явно и выдерживает nil Gin/Request.

Completion выбирает порядок lookup/provider/status/quota/Save/credit; adapter выполняет их в одном tx.
FOR UPDATE использует прежний model.LockForUpdate: MySQL/PostgreSQL получают clause, SQLite пропускает.
Общий adapter/wallet владеет capacity check, единственным atomic ceiling credit и post-commit cache sync;
тот же tx передаёт redemption. Lua/key/schema не копируются. Cache credit не создаёт partial hash и не
отменяет committed payment при ошибке. Receipt идёт после commit в LOG_DB; Pancake сохраняет RecordLog,
остальные — RecordTopupLog с прежними payment metadata/IP. Audit продолжает писать существующий middleware.

Сохранены разные quota formulas: Stripe — Money × QuotaPerUnit; Creem automatic — Amount как quota;
Epay/Waffo/Pancake — Amount × QuotaPerUnit; manual — Stripe Money, остальные Amount × QuotaPerUnit.
Повтор Epay/Waffo/Pancake не кредитует снова; Stripe/Creem сохраняют прежние status errors.
Manual repeat сохраняет zero/user0 receipt. Проверенный Stripe webhook подтверждается 200 empty даже при
ошибке completion; Pancake возвращает retry/500; provider ACK constants не локализуются.

## Границы и мосты

Webhook usecase выбирает subscription dispatch и fallback через consumer-owned domain.Subscriptions.
app/payments/subscription_dispatch.go вызывает реальный app/subscriptions.Orders и отображает typed
sentinels через errors.Is. Stripe/Creem продолжают top-up только при subscription not-found; Pancake
выбирает prefix и проверяет provider/buyer identity до process-local lock. Прежний legacy bridge удалён.

Обе композиции явно используют один app/shared.Orders; его техническое тело — adapter/admission.
Shared protocol adapters остаются в adapter/http/payments; subscriptions добавляет только profile mapping.
Stripe subscription profile сохраняет quantity1/plan price/mode/redirects; top-up profile не меняется.
Active catalog filter общий в domain.LoadActivePancakeCatalog. Subscription history projector находится
в adapter/gorm/payments и получает исходный tx и typed факты без вызова top-up usecase.

Прежний BRIDGE block app/payments/http.go, controller GetEpayClient/LockOrder/UnlockOrder,
CreemProduct и requirePaymentCompliance удалены вместе с реальными потребителями. Controller top-up
entrypoints сохраняются; model/topup.go и common status constants — строгие aliases. Shared
model.LockForUpdate/SyncCreditUserQuotaCache/CacheDecrUserQuota ждут отдельного quota-cache owner.

Новых architecture exceptions нет. Schema, migrations, tags/indexes, money arithmetic, routes, ordering,
query semantics и audit policy не менялись. Сохраняются известные #50–59 в TECH_DEBT; #52 и #55 —
financial/security candidates. Отдельное исправление operational logging подготовлено вне дерева.

После отдельного content-only изменения HTTP ошибки получают English default/en и ru; обе zh
сохраняют прежние bytes. Payment receipts используют язык получателя, English fallback; обе zh
сохраняют legacy content. Три inherited search-pattern ошибки остаются English во всех локалях.
Operational events сохраняют уровни и места вызова; их English messages применены отдельным content-only изменением.
Старые rows не мигрируются.

## Проверки и границы evidence

615 immutable HTTP/state/outbound oracles записаны до переноса; прежние 972 oracles не обновляются.
Матрица фиксирует реальные routes/middleware, подписи, повторы, out-of-order events, quota ceiling,
rollback/commit failures, subscription effects, split main/LOG_DB и исходящие provider requests.
Отдельные 204 locale snapshots сохраняют default/en/ru/обе zh/unknown. App regressions используют
изолированные main/log PostgreSQL schemas и startup search_path каждого pool connection.
MySQL без DSN явно SKIP; его совместимость этим не подтверждается.

Waffo outbound evidence проходит через **подставленный SDK transport**: byte-identical SDK mirror,
alternate modfile и overlay заменяют только три Transport bindings. Serialization/signatures/response
verification прежние. Fake закрыт для неизвестных destinations; реальная сеть/provider payment не нужны.
Обычный full suite без этих artifacts явно SKIP-ит payments contract; он не заменяет dedicated run:

```sh
go test -mod=readonly -modfile="$PAYMENTS_CONTRACT_MODFILE" \
  -overlay="$PAYMENTS_CONTRACT_OVERLAY" ./internal/payments/contract \
  -run '^TestPaymentsContract$' -count=1 -update=false
```

Транспортные artifacts проверяются внешним guard по hashes и точному diff. Sandbox parity/review не
являются host/PostgreSQL/Redis/WSS/production acceptance. Все host проверки запускаются отдельно;
никакой durable exactly-once после crash этим переносом не заявлен.

Raw bind/GORM/SDK errors сохраняют прежнюю raw форму. В частности, data.error у Pancake pair —
динамический SDK error-chain вместе с прежними English prefixes create/publish/store-created;
полный перевод этих внешних errors на ru не заявляется. Они не распознаются по err.Error().
Фиксированный Creem read-query error, в отличие от raw cause, имеет собственный compatibility i18n key.

Transport default err.Error() для трёх search-pattern ошибок — явно принятый inherited exception: English во всех локалях сохраняется.
CreditProfile передаёт optional Stripe customer/email facts; имена SQL columns принадлежат GORM adapter.
