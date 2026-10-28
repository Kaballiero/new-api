# Batch API

## Ответственность

Модуль владеет native BatchJob/BatchItem, admission hold кошелька/API-токена,
состояниями исполнения и расчёта, результатами, settlement/release и procurement evidence.
Batch не использует subscription funding и request-local Reservation. Отсутствующая или
некорректная стоимость остаётся unknown; повторный poll не разрешает повторный paid POST.

## Сценарии и доступ

| Сценарий | Вход и правило |
|---|---|
| prepare_batch | POST /v1/batches до Distribute: envelope, custom_id, bounded native item, owner idempotency |
| create_batch | POST /v1/batches после прежнего Distribute: frozen pricing/provider/account, wallet/token hold; 202 новый, 200 duplicate |
| get_own_batch | GET /v1/batches/:id: собственная undeleted job; прямой injected reader без пустого usecase |
| list_own_batches | GET /v1/batches: owner cursor, id desc, limit+1 и прежняя empty shape |
| fetch_batch_results | GET /v1/batches/:id/results: owner/expiry/readiness, raw NDJSON, один row за чтение, Flush/cancellation |
| cancel_batch | POST /v1/batches/:id/cancel: capability до terminal shortcut; marker, без refund |
| delete_batch | DELETE /v1/batches/:id: terminal и оба financial states settled; local hide |
| recover_batch_identity | POST /api/batches/:id/recover: bounded root input, lease и private provider identities; только bind, без replay/debit |
| confirm_batch_purchase | POST /api/batches/:id/purchase: bounded decimal/evidence, terminal immutable procurement receipt |
| poll_batches | provider_batch_poll: один последовательный pass, submit/poll/results/settlement/outboxes/reconcile/release/cleanup |

Первые шесть registrations используют прежние TokenAuth/ModelRequestRateLimit; create сохраняет
Prepare → Distribute → Create. Root management registrations сохраняют RootAuth,
CriticalRateLimit и DisableCache, session/PAT audit. Новых routes/permissions/rate limits нет.
Публичного Batch files/upload/admin-list API нет. Пользовательский JSON проецируется через dto.Job;
provider IDs, channel/account secrets, snapshots, receipts и lease fields остаются private.

## Хранение и финансовые границы

adapter/gorm/batch хранит прежние queries, locks, transactions и outboxes. Records имеют прежние
имена, field order и JSON/GORM tags; model/batch*.go — технические aliases. Единственный migration
helper остаётся model/batch_migration.go; схема, DDL, индексы и порядок startup не менялись.
Domain получает чистые Job/Item/Accounts; pure dispatch/charge validation выполняется внутри
адаптера после прежних locks, а не на предварительном snapshot.

Reservation атомарно создаёт job/items, holds, account versions и два cache events без debit.
Per-item settlement атомарно пишет wallet/token/channel counters, immutable details, log payload
outbox и cache events. Hold освобождается после acknowledgement. OpenRouter v2 использует один
подтверждённый total/receipt на job; сумма не распределяется между output rows. Legacy v1 остаётся
per-item. Unknown/null/invalid cost не становится нулём; explicit zero остаётся допустимым.

Billing domain/Expression/CostMath и существующий FX owner сохраняют normalization, rounding и
формулы. Usage владеет единственными metadata/log builders; payload строится внутри money tx,
LOG_DB receipt+log атомарны отдельно от main acknowledgement. ClickHouse writing/readback не replay-ит
ambiguous INSERT. Обычный consumption writer и quota_data export не добавлены.

## Сборка и адаптеры

app/batch — core storage/math/usage composition без gateway imports. app/batchruntime — outer
composition HTTP/poll/system handler с adapter/gateway/batch. Gateway сохраняет native request
builders/DTO/stream decoders в relay/batch, mapping/token count/discovery, bounded staging/HTTP,
90s/no-redirect account policy и frozen headers. Порядок upload → persist input ID → marker → paid
POST, partial ID+decode error и identity verification до staged-result persistence сохранены.

adapter/http/batchaccount — одно тело config hash/static-header policy для Batch и tasksync.
adapter/redis/batch — единственные Batch Lua bodies; namespaces, mutation fence, schema и hydration
принадлежат adapter/cache/accounts и adapter/cache/apikeys. App/batch передаёт
ReadCapabilities явно. Raw cache readers под locks не заменены hydration.

provider_batch_poll регистрируется явно из app/httpapi.RegisterScheduledSystemTasks. Root server
и Electron достигают этого существующего entrypoint; тест регистрации вызывает его же.  UpdateTask, 15s interval, master/once, lease/heartbeat и history
принадлежат app/system runner; SystemHandler использует его finish/fail capability. Direct poll собирается без main.

## Границы и долг

- controller/batch.go содержит однострочные router bindings; once-сборка handlers принадлежит app/httpapi.
- model сохраняет только табличные aliases для startup migration; account deletion использует owning adapters.
- model/batch_conflict.go удалён в7j. Account deletion получает финансовую проверку через
  adapter/gorm/batch.AccountDeletionFinancialDecision на исходном tx; единственный sentinel — pkg/batchstate.ErrConflict.
  App/accountscore собирает capability, model forwarding отсутствует.
- Shared lock/cache primitives принадлежат adapter/gorm/locking и adapter/cache/{accounts,apikeys};
  reserve orchestration — billing adapter, counter queue — adapter/gorm/counters. Production service и BillingGateway registry отсутствуют.
- TECH_DEBT #77–#79: lease/fencing, прежние DB-fixtures и orphan upload. Существующие bugs не исправляются.

## Контракты и тексты

HTTP/state/outbound fixtures находятся в `contract`, polling/recovery — рядом со сценариями,
регистрация scheduler — в `app/system`. Immutable fixtures в `service/testdata` не образуют Go package.
HTTP status/code/default mapping принадлежит transport. Persisted bulk reasons используют English default;
персональная локализация одного bulk UPDATE требует отдельного решения, без изменения порядка SQL (#75).
