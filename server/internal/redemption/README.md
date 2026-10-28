# Redemption — коды пополнения

## Ответственность

Модуль владеет выпуском, поиском, изменением, удалением и погашением кодов. Источник истины —
redemptions в main DB. Погашение атомарно меняет enabled → used и начисляет пользователю квоту;
платёжных заказов, webhook и подписок модуль не создаёт.

## Возможности

| Сценарий | Маршрут | Доступ и поведение |
|---|---|---|
| list_codes | GET /api/redemption/ | AdminAuth, admin/root, session/PAT; Count и Find в read transaction |
| search_codes | GET /api/redemption/search | Тот же доступ; keyword ID-or-prefix, status и прежняя пагинация |
| get_code | GET /api/redemption/:id | Тот же доступ; raw storage ошибки, domain id=0 error |
| create_codes | POST /api/redemption/ | Тот же доступ; compliance до bind; 1–100 отдельных inserts, partial-result сохраняется |
| update_code | PUT /api/redemption/ | Тот же доступ; непустой status_only, включая false, включает status-only |
| delete_code | DELETE /api/redemption/:id | Тот же доступ; lookup, затем soft delete |
| delete_codes | POST /api/redemption/batch | Тот же доступ; 1–1000 IDs, один soft delete, duplicates сохраняются в audit |
| delete_invalid_codes | DELETE /api/redemption/invalid | Тот же доступ; used/disabled и enabled с истёкшим сроком |
| redeem_code | POST /api/user/topup | DisableCache → UserAuth → CriticalRateLimit; compliance, per-user admission, bind, redeem |

Права и middleware остаются в router. Дополнительного Casbin permission для CRUD нет. List игнорирует
keyword/status; search принимает только keyword/status и common pagination. Новых query filters нет.
Read transaction сохраняет прежний rollback-only recover. Тривиальный list остаётся сценарной границей
Count/Find и его результата, без отдельного fake-теста ради pass-through.

Redeem: пустой key → userId=0 → RandomSleep → transaction → locked lookup → status → expiry → CAS →
единый wallet credit → commit → cache increment → legacy topup log. Любая transaction error скрывается
за MsgRedeemFailed. Expiry equality разрешена; disabled считается used внутри сценария. CAS и credit
работают в одном tx; PostgreSQL/MySQL используют FOR UPDATE, SQLite пропускает clause и сохраняет CAS.
Кеш инкрементируется, а не удаляется; cache miss не создаёт неполный hash, failed sync не отменяет credit.

Manual audit.create и audit.delete_batch подавляют fallback только после полного успеха; partial create
оставляет generic failure fallback. Остальные admin writes сохраняют fallback, PAT request event независим.
UserAuth topup не пишет operation audit; receipt идёт в LOG_DB и имеет собственный request ID.

## Зависимости

Domain содержит чистые Code/Query/Redemption/ошибки без ORM/JSON stored shape; HTTP DTO сохраняет field
order, Count=0 и DeletedAt=null. Record с прежними tags находится в adapter/gorm/redemption/record,
model/redemption.go — строгий same-name type alias. Schema, AutoMigrate и индексы не менялись.

Scenarios объявляют consumer-owned Reader/Store/Tx/Receipt/Admission; общие PageReader/Query принадлежат
двум read scenarios. GORM adapter содержит SQL, transaction и projection. App/redemption связывает
поздние DB getters, часы/UUID/задержку, compliance, process-local admission и audit app. Constructors
не зависят от main, middleware или скрытой регистрации. Once-сборка HTTP handlers находится в app/httpapi; controller содержит однострочные bindings.

Wallet credit и post-commit cache sync подключаются через общий technical adapter `internal/adapter/wallet`.
Wallet владеет единственным credit body и capacity guard; redemption передаёт тот же tx. Чужие account workflows не вызываются.
Receipt использует `adapter/gorm/usage.Writer.RecordLog`; аудит — уже существующий audit owner, не новый writer.

## Границы и долг

Payments/webhooks/subscriptions, generic quota primitives и usage/audit policy остаются у владельцев.
Controller содержит однострочные router bindings; callbacks собираются в app/httpapi.
Shared row lock принадлежит adapter/gorm/locking, credit cache sync — adapter/cache/accounts; model forwarders отсутствуют.

Единственная новая default delta: id=0 error `id is empty`, ru `ID не указан`; обе zh сохраняют старые bytes.
Дополнительно ru quota errors переведены: «Квота кода пополнения должна быть положительной» и
«Квота кошелька превышает {{.Max}}»; default/en/zh этих ошибок не менялись.
Остальные ошибки сопоставляются по errors.Is, raw bind/Atoi/storage errors не распознаются по тексту.
Persisted receipt и quota display fragment audit.create используют язык получателя, English fallback;
обе zh сохраняют прежние bytes. Этот ранее принятый content contract не меняется при подключении wallet.

Долг: partial create #45; management/redeem races и re-enable #46; маска кода в operational error log (#47 закрыт)
(redaction применён); retained admission entries #48; post-commit cache/log #49.
Новые guards, retries, repair и очищение lock map не добавлялись.

Сценарные и SQL-регрессии — у владельцев. Snapshot suite удалена по решению владельца; расширение покрытия — TECH_DEBT #99.
