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
| 36 | usage storage и Gin | По решению A3 domain/usecases принимают context.Context, но gorm/usage adapter снимает собственную wrapper с raw cost/affinity metadata и проверяет прежний исходный *gin.Context для model writer (без fake Gin и без обязательного Request); nil/другой context возвращает техническую ошибку до effects, без panic. В волне 3 выделить plain request metadata/storage primitive с прежними ID/IP/log timing; не создавать новый business bridge в model | Транспортная связь технического adapter; DB writers по-прежнему не возвращают durable результат; unsupported context явно отклоняется |


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
| 24 | `channels` уведомления без запроса | Язык root читается отдельным запросом `GetRootUser` перед каждым уведомлением; `NotifyRootUser` читает root ещё раз |
| 25 | `personal_access_tokens/validate_token`, dashboard `AccessTokenAudit` + auth | Один PAT проверяется отдельным SQL-чтением в общем аудите и ещё раз в auth; при отзыве между чтениями аудит и результат аутентификации могут относиться к разным моментам. Отдельно спроектировать переиспользование проверенного результата в запросе с явной гарантией отзыва; изменятся число/момент чтений, поэтому wave 2a сохраняет оба вызова |

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
- **authorization**: модели `model/casbin_rule.go`, `model/authz_role.go` — алиасы для миграций.
- **pricing**: `domain` импортирует `common` и `relaykit` (`domain-transitive`, все `usecase-transitive: internal/pricing/*`, 8 `business-imports-gateway`); каналы для ratio sync читаются из `model` в `app/pricing.go`, а не через `channels`; `adapter/gorm/pricing` и `adapter/pricing/upstream` используют записи `catalog`/`channels` напрямую; `model/*` и `service/catalog_sync.go` создают сценарии pricing сами; `consumerContext` продублирован в пяти сценариях чтения.
- **channels**: `domain` импортирует `common` (1 domain-transitive + 35 usecase-transitive); `service/channel.go` собирает `disable_channel` / `enable_channel` и формирует тексты уведомлений root, `service/codex_*` и `model/channel.go` создают сценарии (legacy-imports-internal); ошибки маппинга моделей приходят из `model` функцией `ModelUpdateRules.Errors`; `Check` и `Apply` продублированы в парах сценариев detect/scan и apply/apply_all; `adapter/gorm/channels.Store` хранит загруженные записи и поэтому собирается на каждый запрос; сводку обновления моделей получают все администраторы на языке root, а не каждый на своём.
- **`oauth/telegram.go` импортирует `internal/identity/domain`** (алиасы ошибок); архитектурный тест пакет `oauth` не проверяет.

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
- ошибки шаблона поиска (`ErrSearch*`) в поиске логов и пополнений уходят клиенту английским текстом без перевода, пока эти модули не перенесены;
- метки групп «用户分组» / «自动» для потребителей вне accounts (service, pricing, relay) по-прежнему китайские: `accountssettings.UserGroups()` берёт их из zh-CN; локализован только `GET` групп пользователя;
- те же старые тексты в legacy-файлах: `quota 不能为负数！` (`model/user.go`, `model/quota_reserve.go`), `id 为空！` (`model/user.go`, `model/redemption.go`);
- сообщения успеха в identity переводятся через `TranslateMessage`, и AST-генератор OpenAPI не выводит их тип: схема `message: string` для 5 обработчиков зафиксирована вручную в `preserveIdentityHandlerContract`.

## i18n

- Язык сохраняемых текстов (логи начислений, имя начального токена) — язык того, кто выполняет запрос; при создании аккаунта админом это язык админа. Лучше хранить тип события и параметры и переводить при показе.
- OAuth-регистрация берёт язык из gin-контекста прямо в адаптере `adapter/identity/oauth/callback.go`, а не через `Input` usecase.
- `i18n.Translate` до `i18n.Init` возвращает ключ; `bundle` читается без блокировки (на практике `Init` один раз при старте).

## Нестабильные тесты

Падают и на `origin/main`:

- `controller/TestSecurityAccountDeletionConcurrentRequestsHaveOneWinner` — `SQLITE_BUSY`, ≈ 3 из 30 запусков;
- `controller/TestSecurityEnrollmentTelegramAndWeChatFirstFactor` — зависит от порядка, ≈ 1 из 4 полных прогонов;
- `relay/channel/task/jsplugin/TestTaskAdaptorReDecodesFinalCandidateAndRejectsModelDrift` — под нагрузкой полного прогона.
- `controller/TestTelegramOAuthRejectsInvalidTokens` — упал 1 раз из 28 прогонов `controller` на ветке рефакторинга (на `main-baseline` 0 из 16, отдельно 40 из 40 зелёные); причина не найдена. В `oauth/telegram.go` рефакторинг менял только алиасы ошибок (тексты прежние) — наблюдать.
