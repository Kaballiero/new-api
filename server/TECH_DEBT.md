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

## Оптимизации (без изменений схемы БД)

| # | Где | Проблема |
|---|---|---|
| 7 | `catalog/list_models`, `catalog/search_models` с `square_state` / `include_channel_models` | Загружают все модели (`limit -1`), фильтруют и пагинируют в памяти |
| 8 | `catalog/delete_model(s)` с `remove_from_channels` | Читают все каналы внутри транзакции метаданных |
| 9 | `catalog/list_models` | Ошибка `VendorCounts()` молча игнорируется (`vendorCounts, _ :=`) |
| 10 | `authorization/sync_policy` | Бесконечный цикл без контекста и остановки (горутина из `main.go`) |
| 11 | `adapter/identity` WeChat `GetIDByCode` | Новый `http.Client` на каждый запрос |
| 15 | `pricing/fetch_upstream_ratios` | `timeout` из тела запроса и число upstream не ограничены сверху (параллельно до 8 ответов по 10 МБ); повтор в `adapter/pricing/upstream` ждёт через `time.Sleep`, не глядя на отмену контекста |
| 16 | `pricing/list_syncable_channels` | Читает все каналы со всеми полями, включая ключи (`GetAllChannels(0, 0, true, false)`), ради id, имени и `base_url` |
| 17 | `pricing/fetch_upstream_ratios` | Ответ upstream `{"success":false}` без `message` (или с пустым) засчитывается как успех: `test_results[].status = "success"`, канал попадает в успешные с пустыми данными | Отказ upstream показывается администратору как успешный |

## Архитектурный долг

Текущий список — `server/internal/architecture_known_violations_test.go` (только сокращается). Крупные пункты:

- **`model` и `service` создают usecase'ы и импортируют `internal`** (`legacy-imports-internal`): apikeys (`model/token.go`: `type Token record.Token`, `ValidateUserToken`, `SearchUserTokens`, `DisableModelLimits`, `DeleteTokenById`, `sanitizeLikePattern`), accounts, catalog (24 записи), identity (сессии и проверки в `service/*`), PAT (`NewPersonalAccessTokenPersistence` из-за приватного `lockForUpdate`; `AccessTokenFingerprint` для `middleware.AccessTokenAudit`). Разбирается вместе с волной шлюза: `middleware` не может импортировать `internal/app`, потому что `app` импортирует `middleware`.
- **`service/authz` (14 записей)**: сборка не может переехать в `app`, пока `middleware.RequirePermission` вызывает `authz.Can`.
- **`catalog/domain` импортирует `common`** (`common.Marshal` для хешей версий, `UnmarshalJsonStr`) — 1 domain-transitive + 26 usecase-transitive записей. Нужен общий подход к JSON в `domain`; та же проблема у channels и pricing.
- **accounts**: usecase-transitive у `adjust_quota`, `transfer_affiliate_quota` (импорт `common`), `get_profile`, `update_notification_preferences`, `update_profile`, `dto` (relaykit `UserSetting`).
- **accounts/provision_account**: ошибки GetAPI по-прежнему через `model.ProvisionErrors` и фабрику `invalid func() error` (общий контракт `model.ErrGetAPI*` / `writeGetAPIError`).
- **accounts/update_profile**: ошибка смены пароля идёт в `writeSecurityError` из controller, а не через `transport.WriteError`.
- **accounts/password_login**: `PasswordLoginLookup` в `adapter/gorm/accounts/record` по-прежнему отдаёт фабрики ошибок для identity.
- **identity**: `http.go` passkey / complete_oauth / login_wechat зависят от `*model.User` (7 записей); пакет `adapter/identity/http` не удалён целиком (`PrimaryLogin`, `GetAuthenticatedUser`, `WriteOAuthError`, `SetAuthNoStore`).
- **authorization**: модели `model/casbin_rule.go`, `model/authz_role.go` — алиасы для миграций.
- **pricing**: `domain` импортирует `common` и `relaykit` (`domain-transitive`, все `usecase-transitive: internal/pricing/*`, 8 `business-imports-gateway`); каналы для ratio sync читаются из `model` в `app/pricing.go`, а не через `channels`; `adapter/gorm/pricing` и `adapter/pricing/upstream` используют записи `catalog`/`channels` напрямую; `model/*` и `service/catalog_sync.go` создают сценарии pricing сами; `consumerContext` продублирован в пяти сценариях чтения.
- **`oauth/telegram.go` импортирует `internal/identity/domain`** (алиасы ошибок); архитектурный тест пакет `oauth` не проверяет.

## Генератор OpenAPI

- `make openapi-check` только валидирует, не сравнивает: настоящая проверка — `make openapi` без диффа в `server/docs`.
- Схема `PasskeyVerifyFinish` зафиксирована вручную (`preserveIdentityHandlerContract`), реальный ответ — SecurityProof.

## Мёртвый код

- `apikeys`: `GetTokenStatus` без маршрута (так и в `origin/main`), обработчик остаётся только для OpenAPI-генератора.
- Мёртвый код из `origin/main` (35 находок `staticcheck -checks U1000`; в baseline 38, 3 ушли при переносе) намеренно не удаляем, чтобы не создавать конфликтов при слиянии с upstream.

## Китайский текст вне перенесённых модулей

В перенесённых модулях (apikeys, accounts, identity, catalog, authorization, PAT, pricing) и в middleware (`secure_verification`, `turnstile-check`, `email-verification-rate-limit`) китайский в ответах, письмах и логах убран. Остальное — в своих волнах:

- `internal/channels` и его адаптеры — волна channels;
- в pricing остались имена пресетов ratio sync «官方倍率预设» / «models.dev 价格预设» (`list_syncable_channels`): клиент сравнивает с ними имя канала (`client/src/features/system-settings/models/constants.ts`), поэтому они не локализуются; те же тексты — в неиспользуемых константах `pricing/domain/ratio_sync.go`;
- legacy `controller/`, `service/`, `model/`, `common/`, `middleware/` (≈ 700 строк вне комментариев на 2026-10-21) — по мере переноса соответствующих модулей;
- ошибки шаблона поиска (`ErrSearch*`) в поиске логов и пополнений уходят клиенту английским текстом без перевода, пока эти модули не перенесены;
- метки групп «用户分组» / «自动» для потребителей вне accounts (service, pricing, relay) по-прежнему китайские: `accountssettings.UserGroups()` берёт их из zh-CN; локализован только `GET` групп пользователя;
- те же старые тексты в legacy-файлах: `quota 不能为负数！` (`model/user.go`, `model/quota_reserve.go`, `service/quota.go`), `id 为空！` (`model/user.go`, `model/redemption.go`), «安全验证» в `controller/channel.go`;
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
