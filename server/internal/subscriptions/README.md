# Подписки

Модуль владеет планами, пользовательскими подписками, subscription orders, четырьмя checkout,
Epay notify/return, завершением/истечением заказов и maintenance. Billing сохраняет funding policy,
reserve/settle/refund orchestration и прежние Store interfaces; его SQL adapter использует records
subscriptions и единственные чистые календарные правила subscriptions/domain.

## Сценарии и доступ

| Вход | Сценарии | Доступ |
|---|---|---|
| GET /api/subscription/plans | list_plans | UserAuth, enabled plans, sort_order desc/id desc |
| GET /api/subscription/self | get_own_subscriptions | UserAuth, active/all + preference; отдельные read errors подавляются |
| PUT /api/subscription/self/preference | update_billing_preference | UserAuth, прежняя нормализация |
| POST /api/subscription/balance/pay | purchase_with_balance | UserAuth + CriticalRateLimit, compliance, atomic wallet debit/grant/order |
| POST /api/subscription/{epay,stripe,creem,waffo-pancake}/pay | create_*_order | UserAuth + CriticalRateLimit, compliance |
| GET/POST /api/subscription/epay/{notify,return} | receive_epay_callback | Public, signature, shared order admission |
| GET/POST /api/subscription/admin/plans | admin_list_plans / create_plan | AdminAuth |
| PUT /api/subscription/admin/plans/:id | update_plan | AdminAuth |
| PATCH /api/subscription/admin/plans/:id | update_plan_status | AdminAuth |
| POST /api/subscription/admin/bind | bind_subscription | AdminAuth, compliance |
| GET/POST /api/subscription/admin/users/:id/subscriptions | admin_list_user_subscriptions / bind_subscription | AdminAuth; create проверяет compliance |
| POST /api/subscription/admin/{plans/:id,users/:id}/subscriptions/reset | reset_subscriptions | AdminAuth, beneficiary audit + manage audit |
| DELETE /api/subscription/admin/user_subscriptions/:id | delete_subscription | AdminAuth |
| POST /api/subscription/admin/user_subscriptions/:id/invalidate | invalidate_subscription | AdminAuth |
| POST /api/option/waffo-pancake/subscription-product | create_pancake_product | RootAuth |
| GET /api/option/waffo-pancake/subscription-product-options | list_pancake_products | RootAuth |

25 method/path registrations остаются в прежнем router с прежними middleware/permissions/body limits.
Точные маршруты и 23 прежних handler entrypoints: router/api-router.go;
два bind-входа и два reset-входа используют общие сценарии с реальным решением, без пустых facades.

## Границы и порядок

Сценарии объявляют узкие consumer ports; HTTP разбирает wire DTO и отображает typed errors.
Чистый domain определяет duration/reset/calendar, grant snapshot/purchase limit и downgrade policy.
GORM records/hooks/SQL и dialect locks находятся в adapter/gorm/subscriptions. Четыре records сохраняют
таблицы, field order, JSON/GORM tags и hooks. model/subscription.go — строго aliases, без workflows.
Сборка app/subscriptions читает DB/settings поздно и не требует регистрации из main.

Grant блокирует пользователя до lifetime purchase count. Complete: locked order → provider/status →
глобальный cached plan → locked user → grant → same-tx history → actual-method/status/order save.
Повтор success ничего не добавляет. Disabled plan продолжает завершать старые orders. History projector
в adapter/gorm/payments получает typed facts и тот же *gorm.DB; Epay history сохраняет исходный method
до actual-method замены. Cache refresh и purchase receipt — только после успешного commit.
Expire изменяет только pending order своего provider. Billing math/Store contracts не менялись.

Balance: cached plan внутри tx → availability/strict decimal ceil → locked wallet → debit → grant →
success order. TopUp history не создаётся. Guarded cache debit после commit использует единственное
тело model.CacheDecrUserQuota. Ошибка cache не меняет committed result.

Stripe вызывает shared protocol client до INSERT, subscription mode/quantity1/plan price ID, wallet
redirects, без top-up promotion flag. Creem вставляет pending до provider, quota metadata=0; ошибка
сохраняет pending. Pancake вставляет pending, делает authenticated checkout (два concurrent SDK calls),
при ошибке сохраняет failed. Epay генерирует подписанную форму без HTTP; failure пытается expire.
Root product создаёт и публикует product в существующем store; CreateStore/DB save отсутствуют.
Active catalog filter единственный — payments/domain.LoadActivePancakeCatalog.

Payments сохраняет signature/dispatch/ACK через собственный domain.Subscriptions port; app/payments
реализует его реальным app/subscriptions.Orders. app/subscriptions не импортирует app/payments.
Обе композиции явно получают один shared.Orders с исходной reference-counted admission mechanics.
Прежние 4b checkout/client/address/catalog/controller bridges удалены.

## Maintenance и реальные остатки

app/subscriptions.StartSubscriptionQuotaResetTask сохраняет sync.Once, master-only gate внутри Do,
первый немедленный tick, interval1m, atomic running guard. Tick делает expiry batches300, reset batches300,
затем cleanup старше7d не чаще30m; lastCleanup обновляется только при успехе. ExpireDue сохраняет
map traversal/partial count до failed commit; reset re-lock проверяет прежние id/reset predicates.
Отдельные expire_due/reset_due/cleanup_preconsume usecases принимают SQL capabilities; scheduling — app.

Реальные // BRIDGE: остатки:

- model/subscription_plan_cache.go: фактический plan/info HybridCache + lookup/invalidation для billing,
  subscriptions adapters и immutable WSS. Удаление — общий технический cache owner и отдельное решение
  о перемещении immutable caller; новых forwarders нет.
- model/subscription_quota.go: единственное PostConsumeUserSubscriptionDelta для billing и legacy task
  settlement. Перенос вместе с durable task settlement (roadmap5); nested refund transaction #27 сохранён.
- model/user_cache.go: единственное CacheDecrUserQuota, механический export существующего guarded body.
  Удаление — общий quota-cache owner рядом с LockForUpdate/SyncCreditUserQuotaCache (roadmap7).
- controller/subscription*.go: прежние router entrypoints → sync.OnceValue app handlers.

Сохраняются TECH_DEBT60–66; #63 и #65 — financial/resource candidates. Structural перенос сохраняет все
тексты, включая15 operational events и purchase/reset receipts. English default/en + ru и English logs
подготовлены отдельным неприменённым content-only package; исторические rows не мигрируются.

## Проверка

519 immutable HTTP/state/outbound goldens +13 deterministic ticks;132 locale snapshots.
Прежние1705 goldens (включая24 relaykit) не меняются; всего2237. Tick harness перемещён с private body,
13 oracle files физически остаются service/testdata/subscription-maintenance. Mutable owner regressions
проверяют resets, group/auth_version/session/cache и provider guard. SQLite/PostgreSQL fixture matrix
проверяет repeat completion, history method, rollback grant/group, balance ceil/no-history и reset.
PostgreSQL использует private schema, startup search_path на всех connections, два pool probes и cleanup
с новым context. Real PostgreSQL/Redis/WSS/full suite — отдельная host evidence; MySQL не проверен.
