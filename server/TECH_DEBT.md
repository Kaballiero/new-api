# Технический долг

Находки рефакторинга `server/internal` (ветка `refactor/accounts-clean-20261020`). В рамках рефакторинга их не исправляли: контракт API и поведение должны были остаться прежними. Каждый пункт — отдельное решение.

## Баги и безопасность

| # | Где | Проблема | Последствие |
|---|---|---|---|
| 1 | `POST /api/user/reset` (`identity/reset_password`) | Сгенерированный пароль возвращается в теле ответа | Пароль виден в логах прокси, истории браузера и т. п.; OWASP рекомендует, чтобы новый пароль задавал сам пользователь по ссылке |
| 2 | `identity/register_password` | Неожиданная ошибка `Insert` уходит клиенту сырым текстом (`common.ApiError`) | Возможна утечка деталей БД |
| 3 | `accounts/check_in` на SQLite | Создание отметки и начисление квоты идут без транзакции; компенсирующий `Delete` игнорирует свою ошибку | Может остаться отметка без начисленной квоты |
| 4 | `accounts/check_in` вне SQLite | `go IncreaseCache(...)` без контроля, ошибка игнорируется | Кэш квоты может разойтись с БД |
| 5 | `authorization/get_user_overrides`, `domain.ExplicitSubjectEffect` | Ошибка `GetFilteredPolicy` превращается в пустую карту / «нет правила» | Сбой хранилища политик выглядит как отсутствие прав, а не как ошибка |
| 6 | `accounts/clear_binding` | Для несуществующего пользователя 200 с текстом «record not found», остальные админские сценарии отдают 404 | Несогласованный контракт |
| 12 | `pricing/reset_model_ratios` | `ModelRatio` пишется в БД до публикации в runtime; ошибка `UpdateModelRatioByJSONString` оставляет БД изменённой, а ответ — `success:false` | БД и память расходятся до следующей синхронизации опций |
| 13 | `pricing/update_pricing_config` | Ошибка `Publish` после коммита транзакции отдаётся как 400; ошибка `common.Marshal` при публикации игнорируется | Клиент видит ошибку, хотя цены уже сохранены |
| 14 | `pricing/transport.WriteError` (ветка по умолчанию) | Неклассифицированные ошибки чтения (FX, снимок каталога, ревизия) уходят клиенту как 503 с сырым текстом | Возможна утечка внутренних деталей |
| 15 | `pricing/fetch_upstream_ratios` | Ответ upstream `{"success":false}` без `message` (или с пустым) засчитывается как успех: `test_results[].status = "success"`, канал попадает в успешные с пустыми данными | Отказ upstream показывается администратору как успешный |
| 16 | `channels`: `record.GetSetting` / `GetOtherSettings` | Чтение настроек канала с неверным JSON сбрасывает их (`nil` / `"{}"`) и сохраняет канал — в том числе в `preview_channel_models`, при расчёте отпечатка загрузки моделей и чтении прокси | Операция чтения меняет данные; сбрасываются все настройки, а не только неверное поле |
| 17 | `channels/get_codex_usage`, `get_codex_reset_credits`, `reset_codex_usage` | Ошибки обновления токена и `SetKey` игнорируются (`_ = SetKey`), запрос повторяется со старым ключом | Обновлённый токен может потеряться, причина отказа не видна |
| 18 | `channels/test_channel` | `go SaveResponseTime(...)` без recover и без ожидания | Паника в сохранении роняет процесс |
| 19 | `channels/create_channel`, `update_channel`, `edit_tag_channels`, `manage_multi_keys`, `update_channel_balance`, codex-сценарии | Ошибки хранения и upstream уходят клиенту сырым текстом с HTTP 200 | Возможна утечка деталей БД и upstream |
| 26 | billing: ordinary top-up и terminal wallet/token debit | Admission reserve учитывает held, но последующий безусловный debit может дать отрицательный остаток, например wallet=100, hold=80, actual=120 → wallet=-20. Сохранено в 2b1. Предлагается отдельно определить политику deficit/admission и unresolved accounting, затем атомарные guards и regression tests; не менять финансовый исход уже оплаченного upstream молча | Возможен долг и расходование остатка, удержанного другим процессом |
| 27 | `model.RefundSubscriptionPreConsume` | Внешняя transaction меняет refund record, но `PostConsumeUserSubscriptionDelta` запускает отдельную transaction через глобальный DB. При сбое фиксации outer record возможен повтор уже применённой delta; на SQLite возможна конкуренция писателей. Это риск по чтению кода, runtime-воспроизведения в 2b1 нет. Исправление: единая tx для delta+record и fault/crash regression; отдельное решение, SQL/locking в 2b1 не менялись | Потенциальный повтор возврата либо отказ refund |
| 28 | billing quota notification snapshot | Wallet eligibility/display используют pre-request UserQuota; legacy subscription delta инициирует wallet notification. Сохранено в 2b1; локализация текста уже отделена от этой политики. Исправление отдельно: определить итоговый snapshot и тип notification, зафиксировать условия и момент чтения тестом | Порог и показанный остаток могут не совпадать с остатком после settlement |
| 29 | `service.SendWebhookNotify`, quota notifications | Legacy quota templates содержат `{{value}}`, а webhook renderer применяет `fmt.Sprintf` к каждому аргументу. Поэтому payload.content может сохранять markers и получать fmt diagnostics; values при этом остаётся отдельным полем. Сохранено при переносе 2b1, сравнение wire payload закреплено regression test. Исправление отдельно: согласовать renderer и placeholder contract с потребителями webhook, затем тест точного content/signature | Некорректный текст quota webhook; исправление изменит подписанный payload |
| 30 | PostAudio ModelPrice | Gateway сохраняет прежнее отсутствие ModelPrice при вызове audio calculation, хотя UsePrice передан; fixed legacy audio cost равна нулю при ненулевом usage без успешного tiered result. Зафиксировано cost golden до переноса 2b2. Отдельно передать price и проверить reservation/settlement/log | Изменение исправит деньги; в 2b2 не исправлялось |
| 31 | Text tool/audio display под FX | Charge использует effective group G, persisted content — public Gp. При FX≠1 показанная quota отличается от charge; 2b2 возвращает отдельно display facts и сохраняет bytes. Определить display contract и исправить отдельным решением; 2c сохраняет суммы и пояснение (#31) | Несовпадение пояснения в логе и суммы списания |
| 32 | Чистая quota-math/expression библиотека | `common` checked conversions и `pkg/billingexpr` транзитивно зависят от инфраструктуры. 2b2 применяет narrow QuotaMath/ExpressionEvaluator adapters; правила rounding/clamp/SysError не копируются. В волне 9 выделить чистую библиотеку с единственным владельцем и сохранить audit/error contract | Архитектурный долг адаптера, широкое выделение вне 2b2. R4: до переноса task/log в 2c / волне 5 legacy `service/billing_fx.go` сохраняет две denomination/quota константы рядом с domain-значениями: service → internal/domain запрещён замороженным allowlist, domain → service нарушает независимость бизнеса. Не обходить запрет новым общим пакетом или портом для констант |
| 33 | Native Gemini non-stream grounding | `GeminiTextGenerationHandler` не вызывает Google Search marker, в отличие от stream и Chat handlers. Origin/main native non-stream case списывает 120 вместо 7120 при groundingMetadata; оба native пути закреплены immutable goldens. Отдельно решить correction policy и тесты | Доступная в ответе Google Search операция не добавляет surcharge на non-stream пути |

| 34 | Исторические persisted log texts | Новые request templates 2c переведены на English отдельным разрешённым patch; старые rows остаются китайскими без миграции. Task/MJ/violation тексты переводятся в волне 5. Персональная локализация key+params/render-on-read — отдельное будущее улучшение | Смешанный язык исторических данных; смена новых content меняет /api/log тела и требует content-only проверки |
| 35 | Recording counters / settlement / rows / export | Прежняя последовательность неатомарна: realtime counters уже применены при failed settle без consume; text/audio могут писать после settlement error; export пополняется даже после failed insert. Realtime failed-token case закреплён immutable usage contract; остальные риски установлены чтением кода. Не исправлять rollback/retry/idempotency молча; отдельно определить обязательность evidence/repair и fault/crash tests | Возможное расхождение counters, финансового состояния, rows и RAM export при сбое |


| 36 | Закрыто в 3a: usage storage и Gin | Adapter получает plain request observation, ID/IP callbacks читаются поздно в техническом writer; Gin recovery удалён. Поддержаны direct/no-Request boundaries и unsupported-context rejection | Durable DB result остаётся прежним ограничением #35 |
| 37 | Quota export RAM cache / adapter quota_cache | Flush держит lock во время SQL, ошибки First/update/create не возвращаются; cache очищается и при write failure. Сохранено по A2 | Возможна потеря export data и задержка producers; durability/retry требуют отдельного решения |
| 38 | Batch counter RAM queue | Не durable, write errors логируются без requeue; scheduling/очерёдность прежние | Потеря counters при crash/ошибке записи; перенос единственной queue из model — волна 7 |
| 39 | Log stats / own identity | Прежние stats принудительно считают consume независимо от type; rate window не ограничен запрошенным historical range; own stats/legacy quota используют username | Исторические/переименованные данные и rate window могут удивлять клиента; исправление меняет контракт |
| 40 | Rankings | При равных totals порядок зависит от map traversal; labels зависят от local time, cache не объединяет concurrent misses | Нестабильный tie order и повторные запросы; политика не менялась в 3a |
| 41 | system_task log cleanup | Ownership перенесён в internal/system + adapter/gorm/usage.Cleanup; SQL LOG_DB, ClickHouse branch и прежние dialect LIMIT semantics сохранены, main/log writes не atomic | Closure реального SQL behavior требует PostgreSQL/MySQL/ClickHouse evidence; SQLite contracts этого не доказывают |


| 60 | Subscription provider availability | Checkout и callback требуют разные настройки: Pancake plan checkout допускает отсутствие gateway product; Creem test-mode checkout допускает пустой webhook secret | Оплата может быть принята при недоступном callback; нужна отдельная admission/config policy |
| 61 | Subscription completion history | History upsert предшествует Epay actual-method update; новый history provider пуст, balance history отсутствует | Отчётность может отличаться от order/receipt; backfill и policy отдельно |
| 62 | Cached subscription plan внутри tx | Global/tx lookup использует cache, может публиковать uncommitted facts; старый pending order использует изменённый plan | Snapshot/invalidation policy требует отдельного решения |
| 63 | Duration/reset/catch-up | Custom seconds не ограничены, time.Duration может переполниться, catch-up линейный, maintenance может повторять full batch без прогресса | **Financial/resource candidate**: bounds/progress policy и deterministic regression отдельно |
| 64 | Subscription preconsume ledger | request_id reuse не проверяет user/amount; duplicate-create fallback использует candidate facts; cleanup7d удаляет dedup history | Ownership/retention policy отдельно, без replay |
| 65 | Subscription delta | Нет active/expiry проверки или overflow guard для used+delta; SQLite пропускает row lock | **Financial/resource candidate**: bounds/transaction/CAS policy отдельно |
| 66 | Subscription silent errors | Downgrade/expiry probes трактуют SQL error как отсутствие competing subscription; plan writes не проверяют affected rows; self errors маскируются пустыми списками | Возможны неверные state/transition; error semantics сохранены |
| 67 | Task billing log parameter order | `relay/taskrecord/submission.go` собирает `logs.content` обходом Go maps без сортировки; одинаковые billing facts дают разные bytes | Contract harness канонизирует только порядок параметров. Candidate fix: сортировать ключи; меняет видимый текст, нужно отдельное решение |
| 68 | Sora legacy multipart field order | `common/gin.go:419–426`, `relay/channel/task/jsplugin/adaptor.go:311–325, 1327–1329` передают Go maps в JS; `plugins/tasks/sora/plugin.js:108–116` собирает parts через Object.keys и files, writer сохраняет их порядок | Одинаковые поля дают разный порядок multipart parts. Contract harness канонизирует только порядок parts; сортировка в production — отдельное решение |


| 69 | Task/MJ legacy settlement | Funding/token/counters/log/quota-clear не atomic; write/crash после денег оставляет repeat-credit/partial-token риск | Financial candidate; durable claim/repair policy отдельно, CAS статуса не делает standalone refund atomic |
| 70 | Task/MJ missing channel/null upstream ID | Bulk FAILURE100% обходит CAS/refund и исключает row из inventory | Financial candidate; сохранённый hold/overwrite concurrent success, terminal uncertainty policy отдельно |
| 71 | Gateway realtime fetch | tryRealtimeFetch пишет terminal status до completion billing, CAS result ignored; polling больше не выберет row | Financial candidate; пропущенные delta/refund требуют отдельного решения |
| 72 | MJ action parsing | relay/midjourney/wire.go индексирует split/customId и simple-change без достаточных length guards | Security/resource candidate; malformed request может panic; guard/wire outcome отдельно |
| 73 | Task/MJ operational logs | DEBUG/raw provider body и untrusted Data сохраняют legacy leakage, redaction различается reconciliation/nonreconciliation | Security/resource candidate; новый leakage запрещён, отдельный точный redaction package по решению |
| 74 | Task DB test fixture | Прежний service TestMain принимал shared FX_TASK DSNs и удалял rows без private schema | Test-only fixture перенесён в internal/contracttest/taskfixture; PostgreSQL имеет own main/log schemas и startup search_path каждого pool connection. R5-b: FX helper восстановлен побайтно из HEAD; cleanup ограничен3FXтаблицами и captured handle, child-process runner удалён. SQLite trigger fault-injection явно использует private SQLite main/log с восстановлением окружения. Host/PG R5-b pending; MySQL legacy DSNs unchecked |

| 75 | Bulk task fail_reason localization (C1) | Один legacy UPDATE пишет общий текст задачам разных владельцев. В отдельном content-пакете общий текст получает English default, а не язык root; structural bytes прежние | Именованный gap: per-owner язык требует группировки по языку и отдельного SQL-патча с DB-проверкой; количество/порядок запросов в5a не меняется |
| 76 | Порядок FX service tests | Process-global monotonic FX publication переживает cleanup per-test SQL rows: version SQL/cache расходятся при перестановке тестов. Orchestrator подтвердил на clean git archive HEAD df33308eb: FX_TEST_DIALECT=sqlite go test ./service -run '^TestModelCostFX' -shuffle=1792866150416781940 — TaskRunSuccessAndFailure FAIL; -shuffle=1 — RefreshRejectsConcurrentWinnerWithoutRetry FAIL; seeds2/3 PASS | Pre-existing HEAD order dependence. R5-b acceptance без shuffle: default/SQLite PASS, count2 на minimal тоже FAIL (cache/version conflict), требует отдельного решения; исправление lifecycle cache/SQL и возвращение shuffle — отдельная задача без production reset API  Дополнение wave5b host: whole-1 PASS, whole-2 (`-count=2`) FAIL в controller/service; clean HEAD archive `/dev/shm/head5b-count2.log` подтверждает те же четыре TestModelCostFX* и controller/TestGetStatusMarksBillingFXUnavailableWithoutRAMSnapshot, TestGetEffectivePricingReturnsPerUsingGroupProjection; TestBatchBillingUsesProviderBase перенесён из service в internal/batch/create_batch. Evidence candidate: `/dev/shm/refactor-plans/wave5b-host-host.wj2_bsrr/whole-2.log`. Повтор внутри одного процесса (`-count>1`) сохраняет FX RAM/cache publication после SQL cleanup; whole-1/full-suite остаются строгими, whole-2 разрешает только закреплённые baseline roots; cache/test lifecycle не исправлен |

| 77 | Batch lease/fencing | Lease120s не продлевается при upload/create/poll/file requests с timeout90s; часть writes не проверяет RowsAffected/owner | Сохранённый concurrency risk. Heartbeat/fencing и deterministic regressions — отдельное решение, money receipts сами не защищают execution state |
| 78 | Batch DB fixtures | Прежние native fixtures использовали fixed prefixes на supplied PG DSN; private SQLite worker tests могли потерять shared TestMain facts при переносе | Test-only isolation перенесена в pgfixture/batchfixture: private schemas/startup search_path/pool probes/reopen, локальная FX100. Реальный host/PG readback обязателен; MySQL unchecked |
| 79 | Batch OpenAI upload recovery | Потеря acknowledgement до persist input_file_id разрешает повторный upload и orphan file; native Delete не удаляет OpenAI input file | Retention/recovery policy отдельно; это никогда не разрешает повторять paid create POST |


| 80 | Task plugin first-active / activation | `adapter/gorm/taskplugins`: Count+Create первого active и deactivate/activate версий не сериализуют весь key; отдельные concurrent transactions могут публиковать несколько active rows. Это code-reading risk, PG concurrency reproduction не выполнен | Нужны отдельные PG regression и политика key-level serialization; schema/locking в переносе не менялись |
| 81 | Task plugin status cascade / publication | Каналы, factory option, plugin row и runtime publication меняются последовательно без общей transaction. Поздний lookup/update/sync failure оставляет partial disabled channels или RAM/DB divergence | Отдельная lifecycle/rollback policy; это не повтор #58 об ignored option SQL errors. Existing outcome сохранён |
| 82 | Responses background flag | `adapter/gorm/tasks.MarkResponsesBackground` выполняет best-effort whole `private_data` UPDATE без revision/CAS; ошибка только логируется, pending response возвращается. Concurrent task state может быть overwritten | Нужен отдельный task-owner locked/CAS design и deterministic regression; здесь сохранены SQL, suppression и момент write |
| 83 | Protocol native test fixture / Redis | Fresh-process `controller` protocol selection на HEAD089e9ba5c и candidate падает в `TestServeTaskPluginProtocolDisconnectBeforeDurableBarrierPersistsAndSettlesWithoutRefund`: default RedisEnabled=true при nil Redis client; downstream task-log language lookup паникует | Fixture не инициализирует нужный cache state. Baseline evidence wave5c-head-protocol-native.log; не новая production regression, assertions/production не ослаблялись. Отдельно исправить test fixture |


| 84 | Memory rate-limit native fixtures | `middleware/TestMemoryModelRateLimitReportsSuccessLimitInEnglish` и `TestMemoryModelRateLimitReportsTotalLimitInEnglish` повторно используют process-global limiter и fixed user IDs. В count=2 второй запуск получает 429 вместо 204; воспроизведено на clean HEAD089e9ba5c | Сохранённый test-only isolation debt. Host whole-1/full-suite строгие; whole-2 сравнивается со свежим HEAD и разрешает только эти точные roots, если они упали в HEAD. Sandbox multiset не является gate |

| 85 | Тест повторного декодирования task-плагина / состояние JS VM | `relay/channel/task/jsplugin/TestTaskAdaptorReDecodesFinalCandidateAndRejectsModelDrift` хранит `calls` в JS-модуле и ожидает ту же VM при двух вызовах. `pkg/jsplugin.Engine` использует `sync.Pool`: после GC или выбора другой VM счётчик начинается заново, drift отсутствует и `taskErr == nil`. На HEAD `089e9ba5c` и candidate исходный тест прошёл по 150/150 (`-count=50` и `-count=50 -cpu=1,4`); отдельная диагностика с тем же JS и двумя GC между вызовами на обоих деревьях даёт nil, контроль без GC — отклонение модели | Исходный дефект предположения теста, рост частоты на candidate не выявлен. Evidence: `wave5c-r2-adaptor-matrix.json`, `wave5c-r2-pool-probe-results.json` в `/dev/shm/refactor-plans`. Конкретный сбой полного sbx/PG-прогона R1 не трассировался: GC/смена VM — подтверждённый механизм, а не измеренная причина того события. Отдельно сделать fixture независимой от повторного использования VM; тест и production-поведение в этой волне не менялись, исключение в acceptance guards не добавлено |

| 86 | GET options/custom OAuth order | Editor и public custom providers обходят map без стабильного порядка. На двух layouts по пять repeats до переноса доказан order noise; settings harness канонизирует только array order, сохраняя key multiset, values, raw object bytes и CompletionRatioMeta last | Production sorting не добавлено; API ordering policy требует отдельного решения. Evidence: wave6a-preflight-results.json, SETTINGS_ORDER logs |
| 87 | Raw/effective option divergence | Reflection parse errors и unknown fields могут сохранить DB/raw map со success при прежнем effective runtime; malformed legacy collections могут частично сбросить RAM. Закреплено settings contracts namespace/sync/failure cases | Отдельно определить validation/publication outcome и recovery policy; перенос сохраняет исходную последовательность, #58 не дублируется |

| 88 | Setup bootstrap partial states | Guard/root probe/Create/options/flag/setup row не atomic; ошибки lookup считаются absence, failed setup INSERT оставляет RAM flag true | Риск concurrent bootstrap/partial initialization по коду; отдельный security fix с concurrency regression, assurance не заявляется |
| 89 | Uptime Kuma outbound policy | Public endpoint читает root-configured URL через default client/redirects без destination guard/body cap; UI disabled не выключает fetch | SSRF/resource risk по коду, deployment exploit не проверялся; отдельная policy, перенос сохраняет wire behavior |
| 90 | System order noise | Summary equal-request ties зависят от map order; независимые Uptime requests приходят в разном порядке (≥5 repeats на origin/current) | Только harness order canonicalization; payload bytes и redirect parent→child сохраняются, production sorting не добавлен |
| 91 | Metrics consistency | RAM drain/upsert/query и удаление старого bucket не atomic; concurrent snapshots и removal могут расходиться, Redis writes Query не читает | Риск по коду без stress reproduction; A3=B technical owner остаётся pkg/perf_metrics до решения волны 7 |

| 92 | Notifications: RAM/Redis event limiter и fan-out | RAM Load→increment→Store и Redis GET→SET/INCR неатомарны; RAM увеличивает rejected-вызовы, неизвестный канал и отсутствующее назначение расходуют лимит. Watchers считают nil skip успешной доставкой. Перенос сохраняет эти outcomes; отдельно определить admission/delivery policy и атомарность | Concurrent sends могут расходиться с лимитом; sent-count не подтверждает получение |
| 93 | Deployments search/status projection | Search фильтрует только полученную страницу и считает её total; list считает status_counts по странице, all берёт provider-wide total. Zero CreatedAt заменяется текущим временем. Запросы не наследуют cancellation входящего HTTP и не имеют нового body cap | Неполный поиск/счётчики и потребление ресурсов — прежний API/SDK контракт; решение о глобальном поиске и resource policy отдельно |
| 94 | Catalog prefill Save/uniqueness | Update сохраняет весь record через Save и может создать отсутствующий ID; duplicate precheck отделён от write, soft-deleted uniqueness зависит от исходного dialect/index. Stale payload может перезаписать поля | Сохранены schema, SQL и upsert outcomes. Отдельно определить concurrency/partial-update policy; не исправлять её в переносе |
| 95 | Deployments quantity/error boundaries | duration/hardware arithmetic не ограничена, отрицательный hardware estimate принимается; provider failures/details возвращаются в прежнем HTTP200 envelope | Input/error/security policy требует отдельного решения; локального quota debit этот модуль не выполняет |
| 96 | Catalog contract order noise | AWS presets data.33 обходится из Go map, два upstream metadata GET запускаются параллельно. Pre-move characterizations на обоих layouts подтвердили перестановки при равных остальных bytes; host recording и metadata x20 stable | Harness сортирует только доказанные permutations: один AWS array и GET pair внутри каждого metadata round. Production ordering не меняется, причинный порядок rename GET→PUT и порядок metadata rounds сохраняются; source/order evidence описаны в wave6c report |
| 97 | Catalog ownership | `NewCatalogPersistence`, private metadata mutex/transaction и PricingMutation callback остаются model; domain root использует common. A3/A4 cleanup в волне 7: единый transaction/lock composer и чистый codec/hash boundary, затем объединение domain leaves | До cleanup ownership неполон; удалять lock/composer по частям нельзя |

## Оптимизации (без изменений схемы БД)

| # | Где | Проблема |
|---|---|---|
| 7 | `catalog/list_models`, `catalog/search_models` с `square_state` / `include_channel_models` | Загружают все модели (`limit -1`), фильтруют и пагинируют в памяти |
| 8 | `catalog/delete_model(s)` с `remove_from_channels` | Читают все каналы внутри транзакции метаданных |
| 9 | `catalog/list_models` | Ошибка `VendorCounts()` молча игнорируется (`vendorCounts, _ :=`) |
| 10 | `authorization/sync_policy` | Бесконечный цикл без контекста и остановки (горутина из `main.go`) |
| 11 | `adapter/identity` WeChat `GetIDByCode` | Новый `http.Client` на каждый запрос |
| 20 | `pricing/fetch_upstream_ratios` | `timeout` из тела запроса и число upstream не ограничены сверху (параллельно до 8 ответов по 10 МБ); повтор в `adapter/pricing/upstream` ждёт через `time.Sleep`, не глядя на отмену контекста |
| 21 | `pricing/list_syncable_channels` | Читает все каналы со всеми полями, включая ключи (`GetAllChannels(0, 0, true, false)`), ради id, имени и `base_url` |
| 22 | `channels/search_channels` | Загружает все совпадения (в режиме тегов — все каналы каждого тега), фильтрует по статусу и типу и пагинирует в памяти |
| 23 | `channels/update_channels_balances`, `run_channel_tests` | Загружают все каналы с ключами одним запросом (`GetAllChannels(0, 0, true, false)`) |
| 24 | `channels` уведомления без запроса | Язык root читается отдельным запросом `GetRootUser` перед каждым уведомлением; `notifications.Root` читает root ещё раз |
| 25 | `personal_access_tokens/validate_token`, dashboard `AccessTokenAudit` + auth | Один PAT проверяется отдельным SQL-чтением в общем аудите и ещё раз в auth; при отзыве между чтениями аудит и результат аутентификации могут относиться к разным моментам. Отдельно спроектировать переиспользование проверенного результата в запросе с явной гарантией отзыва; изменятся число/момент чтений, поэтому wave 2a сохраняет оба вызова |

| 42 | `audit/record_event` и вызывающие операции | Business write и audit row не атомарны; ошибка audit только логируется, без retry, manual marker всё равно подавляет fallback. Сохранено при переносе | Можно потерять evidence; durable recording и repair требуют отдельного решения |
| 43 | `audit` Count/Find | Два чтения без общей snapshot transaction; concurrent inserts могут менять total/items между запросами. Риск по коду, отдельного concurrency-воспроизведения нет | Несогласованная страница; не менять SQL semantics без отдельного решения |
| 44 | PAT last-use reader | PAT adapter напрямую читает audit_logs и применяет свою actor-role policy. По решению A3 SQL оставлен прежним; audit → PAT зависимости нет. Это вопрос владения reader, отдельно от двойной validation #25 | Пересмотреть узкий audit-owned reader при следующем изменении границ |

| 45 | redemption/create_codes | Отдельные inserts оставляют partial batch при success=false; manual audit отсутствует, fallback не перечисляет выданные keys | Отдельно определить atomic batch или partial-result policy; в переносе rollback не добавлен |
| 46 | redemption management / redeem | Update/delete неатомарны с lookup; stale full update или status-only могут вернуть used code в enabled, used_user_id сохраняется | Возможное повторное credit; state transition/CAS/locking исправлять отдельно с regression |
| 47 | Закрыто в 4a: redemption operational failure log | Прежний TopUp logger включал полный submitted key при ошибке; теперь пишется маска `abcd***wxyz` | Ответ API и goldens не менялись |
| 48 | redemption admission | Process-local map сохраняет entry каждого userId навсегда, включая malformed request после compliance | Рост RAM; bounded lifecycle требует отдельного решения, простое удаление на Unlock может сломать mutual exclusion |
| 49 | redemption post-commit cache / receipt | DB commit предшествует best-effort cache increment и log INSERT, без durable repair | Credit может остаться без актуального кеша/usage evidence; отдельная crash/fault policy, не гарантия durability |


| 50 | Закрыто в 4b: payments operational logs | Подписи, raw webhook bodies, Epay form/link, данные покупателя Creem/Pancake и query strings писались в логи; теперь пишутся размер тела и path (commit ea1ab44b4) | Ответ API и goldens не менялись |
| 51 | Pancake catalog GET private_key | Typed private key передаётся в query и может попасть в history/proxy/access logs | Security candidate; миграция API к body/session config отдельно, GET контракт пока сохранён |
| 52 | payments manual completion Creem / repeat | Manual считает Amount × QuotaPerUnit вместо automatic Amount; повтор success пишет user0/zero receipt | Financial/security candidate: возможен избыточный credit и ложная receipt; отдельная formula/repeat policy |
| 53 | Waffo notification transitions | Любой status != PAY_SUCCESS переводит pending в failed, поздний paid затем отвергается | Возможна потеря пополнения при промежуточном/переупорядоченном event; state machine отдельно |
| 54 | Stripe async-fail | Plain First → Save вне locked transaction может перезаписать concurrent success | Риск stale failed status после credit; отдельный race regression и transition fix |
| 55 | Payment callback invoice validation | Wallet callbacks не сверяют paid amount/currency с local order; Epay merchant pid отдельно не сверяется сверх signature | Financial/security candidate: подписанные неверные invoice facts; provider-specific validation требует отдельного решения |
| 56 | Payment admission asymmetry | Quote и Epay/Stripe/Creem pay не имеют общей compliance/enabled проверки; info/webhook строже | Order/link может быть создан при недоступном webhook; поведение сохранено |
| 57 | Remote checkout / local order boundary | Stripe remote-first, Creem local-first; failed Save Waffo/Pancake игнорируется | Orphan/ambiguous orders; reconciliation без auto replay отдельно. Postcommit evidence/cache — также #49 |
| 58 | Compliance option publication | Пять отдельных UpdateOption, map order и ignored SQL errors; bulk save DB-first/runtime-second | Возможны ложное подтверждение и расхождение runtime/DB; shared settings policy отдельно |
| 59 | Payment concurrency on SQLite | FOR UPDATE пропускается, top-up status transition без CAS | Межпроцессная/crash exactly-once гарантия не доказана; отдельная durable state-transition policy |

## Архитектурный долг

Текущий список — `server/internal/architecture_known_violations_test.go` (только сокращается). Крупные пункты:

- **`model` и `service` создают usecase'ы и импортируют `internal`** (`legacy-imports-internal`): apikeys (`model/token.go`: `type Token record.Token`, `SearchUserTokens`, `DisableModelLimits`, `DeleteTokenById`, `sanitizeLikePattern`), accounts, catalog (24 записи), identity (сессии и проверки в `service/*`), PAT (`NewPersonalAccessTokenPersistence` из-за приватного `lockForUpdate`). Разбирается вместе с волной шлюза: `middleware` не может импортировать `internal/app`, потому что `app` импортирует `middleware`.
- **`service/authz` (14 записей)**: сборка не может переехать в `app`, пока `middleware.RequirePermission` вызывает `authz.Can`.
- **`catalog/domain` импортирует `common`** (`common.Marshal` для хешей версий, `UnmarshalJsonStr`) — 1 domain-transitive + 26 usecase-transitive записей. Нужен общий подход к JSON в `domain`; та же проблема у channels и pricing.
- **accounts**: usecase-transitive у `adjust_quota`, `transfer_affiliate_quota` (импорт `common`), `get_profile`, `update_notification_preferences`, `update_profile`, `dto` (relaykit `UserSetting`).
- **accounts/provision_account**: ошибки GetAPI по-прежнему через `model.ProvisionErrors` и фабрику `invalid func() error` (общий контракт `model.ErrGetAPI*` / `writeGetAPIError`).
- **accounts/update_profile**: ошибка смены пароля идёт в `writeSecurityError` из controller, а не через `transport.WriteError`.
- **accounts/password_login**: `PasswordLoginLookup` в `adapter/gorm/accounts/record` по-прежнему отдаёт фабрики ошибок для identity.
- **identity**: `http.go` passkey / complete_oauth / login_wechat зависят от `*model.User` (7 записей); пакет `adapter/identity/http` не удалён целиком (`PrimaryLogin`, `GetAuthenticatedUser`, `WriteOAuthError`, `SetAuthNoStore`).
- **authorization**: чистые record aliases `model/casbin_rule.go`, `model/authz_role.go` для миграций проходят узкое AST-правило, их две allowlist-записи удалены; исполняемый код этим исключением не разрешается.
- **pricing**: `domain` импортирует `common` и `relaykit` (`domain-transitive`, все `usecase-transitive: internal/pricing/*`, 8 `business-imports-gateway`); каналы для ratio sync читаются из `model` в `app/pricing.go`, а не через `channels`; `adapter/gorm/pricing` и `adapter/pricing/upstream` используют записи `catalog`/`channels` напрямую; `model/*` и `service/catalog_sync.go` создают сценарии pricing сами; `consumerContext` продублирован в пяти сценариях чтения.
- **channels**: `domain` импортирует `common` (1 domain-transitive + 35 usecase-transitive); `service/channel.go` собирает `disable_channel` / `enable_channel` и формирует тексты уведомлений root, `service/codex_*` и `model/channel.go` создают сценарии (legacy-imports-internal); ошибки маппинга моделей приходят из `model` функцией `ModelUpdateRules.Errors`; `Check` и `Apply` продублированы в парах сценариев detect/scan и apply/apply_all; `adapter/gorm/channels.Store` хранит загруженные записи и поэтому собирается на каждый запрос; сводку обновления моделей получают все администраторы на языке root, а не каждый на своём.
- **`oauth/telegram.go` импортирует `internal/identity/domain`** (алиасы ошибок); архитектурный тест пакет `oauth` не проверяет.

- **usage 3a**: model/usedata.go и model/utils.go содержат единственные технические RAM cache/queue для legacy producers до волны 7 (A2); legacy writers/cleanup — A4, #35/#37/#38/#41. Pure record aliases разрешены строгим AST-правилом, allowlist не расширялся.

- **audit**: records/codec вынесены в adapter, model/audit_records.go — alias-only. Migration helper остаётся technical residue без изменения DDL; controller/middleware bridges перечислены в internal/audit/README.md.

## Генератор OpenAPI

- `make openapi-check` только валидирует, не сравнивает: настоящая проверка — `make openapi` без диффа в `server/docs`.
- Схема `PasskeyVerifyFinish` зафиксирована вручную (`preserveIdentityHandlerContract`), реальный ответ — SecurityProof.

## Мёртвый код

- `apikeys`: `GetTokenStatus` без маршрута (так и в `origin/main`), обработчик остаётся только для OpenAPI-генератора.
- Мёртвый код из `origin/main` (35 находок `staticcheck -checks U1000`; в baseline 38, 3 ушли при переносе) намеренно не удаляем, чтобы не создавать конфликтов при слиянии с upstream.

## Китайский текст вне перенесённых модулей

В перенесённых модулях (apikeys, accounts, identity, catalog, authorization, PAT, pricing, channels с контроллерами `controller/channel*.go`) и в middleware (`secure_verification`, `turnstile-check`, `email-verification-rate-limit`) китайский в ответах, письмах и логах убран. Остались только данные, а не сообщения:

- в pricing — имена пресетов ratio sync «官方倍率预设» / «models.dev 价格预设» (`list_syncable_channels`): клиент сравнивает с ними имя канала (`client/src/features/system-settings/models/constants.ts`), поэтому они не локализуются; те же тексты — в неиспользуемых константах `pricing/domain/ratio_sync.go`;
- в channels — суффикс имени копии по умолчанию `_复制` (`copy_channel`), он сохраняется в имени канала;

Остальное — в своих волнах:

- legacy `controller/`, `service/`, `model/`, `common/`, `middleware/` (≈ 700 строк вне комментариев на 2026-10-21) — по мере переноса соответствующих модулей;
- payments сохраняет inherited English search-pattern errors из apikeys/domain; новые payment HTTP/receipt templates находятся в locale files и пока сохраняют legacy bytes до отдельного content package;
- метки групп «用户分组» / «自动» для потребителей вне accounts (service, pricing, relay) по-прежнему китайские: `accountssettings.UserGroups()` берёт их из zh-CN; локализован только `GET` групп пользователя;
- те же старые тексты в legacy-файлах: `quota 不能为负数！` (`model/user.go`, `model/quota_reserve.go`), `id 为空！` (`model/user.go`, `model/redemption.go`);
- сообщения успеха в identity переводятся через `TranslateMessage`, и AST-генератор OpenAPI не выводит их тип: схема `message: string` для 5 обработчиков зафиксирована вручную в `preserveIdentityHandlerContract`.

## i18n

- Язык сохраняемых текстов (логи начислений, имя начального токена) — язык того, кто выполняет запрос; при создании аккаунта админом это язык админа. Лучше хранить тип события и параметры и переводить при показе.
- OAuth-регистрация берёт язык из gin-контекста прямо в адаптере `adapter/identity/oauth/callback.go`, а не через `Input` usecase.
- `i18n.Translate` до `i18n.Init` возвращает ключ; `bundle` читается без блокировки (на практике `Init` один раз при старте).

## Нестабильные тесты

Падают и на `origin/main`:

- `controller/TestSecurityAccountDeletionConcurrentRequestsHaveOneWinner` — `SQLITE_BUSY`, ≈ 3 из 30 запусков; wave5b host `whole-2` (`-count=2`) снова воспроизвёл это падение (`wave5b-host-host.wj2_bsrr/whole-2.log`), при этом whole-1 PASS. По evidence оркестратора повтор `-count=30` на clean HEAD и candidate даёт ту же частоту. Это известный flake SQLite concurrent deletion, не новая регрессия Batch; тест/locking/retry не менялись. Для whole-2 разрешён только этот конкретный дополнительный root; whole-1 и full-suite строгие.
- `controller/TestSecurityEnrollmentTelegramAndWeChatFirstFactor` — зависит от порядка, ≈ 1 из 4 полных прогонов;
- `controller/TestSecurityEnrollmentRejectsChangedFirstFactorPolicy/telegram/setup` — fake Telegram-сервер получает посторонний запрос `/` (`unexpected Telegram endpoint: /`) под нагрузкой `whole-2`; воспроизведено дважды на чистом HEAD `8a12bf350`, отдельно 20 из 20 зелёные;
- `relay/channel/task/jsplugin/TestTaskAdaptorReDecodesFinalCandidateAndRejectsModelDrift` — под нагрузкой полного прогона.
- `controller/TestTelegramOAuthRejectsInvalidTokens` — упал 1 раз из 28 прогонов `controller` на ветке рефакторинга (на `main-baseline` 0 из 16, отдельно 40 из 40 зелёные); причина не найдена. В `oauth/telegram.go` рефакторинг менял только алиасы ошибок (тексты прежние) — наблюдать.
