# accounts — аккаунты пользователей

## Ответственность

Модуль владеет аккаунтом пользователя (`users`): имя, отображаемое имя, роль, статус, группа, квота и её расход, реферальный код и реферальная квота, настройки (уведомления, боковая панель, язык), привязки внешних аккаунтов, ежедневная отметка (`checkins`).

Модуль отвечает за инварианты аккаунта:
- администратор управляет только аккаунтами с ролью ниже своей; root управляет всеми, кроме удаления другого root (`delete_account` проверяет роль строго);
- root нельзя отключить, удалить, понизить; повышать до администратора может только root;
- роль не меняется через обычное редактирование аккаунта;
- ручная корректировка квоты ограничена `common.MaxWalletQuota`, в кэш уходит только зафиксированная разница;
- массовая смена группы — не больше `domain.MaxBulkGroupBatch` (1000) аккаунтов и только в существующую группу;
- реферальная квота переводится не меньше минимума и не больше остатка;
- отметка — не чаще раза в день, награда в диапазоне настроек.

Понятия и бизнес-ошибки описаны в `domain/`:
- административная проекция аккаунта (`User`, `UserBatchRow`, `AdminTarget`, `AdminRoles`), права на управление (`CanManageTargetRole`, `UpdateAdminPermissions`);
- профиль (`SelfProfile`) и разделы панели по роли (`DashboardPermissions`, `DashboardSidebarConfig`);
- группы пользователя (`Groups`);
- отметки (`Checkin`, `CheckinPolicy`), удаление (`DeletionSession`), регистрация (`InitialAccount`);
- ошибки (`errors.go`).

## Возможности

Все маршруты идут через общие middleware `/api` (`AccessTokenAudit`, `GlobalAPIRateLimit`). Личные маршруты `/api/user/*` — через `DisableCache` + `UserAuth` (сессия панели или PAT), административные — через `AdminAuth`.

| Сценарий | Что делает | Маршрут |
|---|---|---|
| `list_accounts` | список аккаунтов постранично, сортировка | `GET /api/user/` (`AdminAuth`) |
| `search_accounts` | поиск по ключевому слову, группе, роли, статусу | `GET /api/user/search` (`AdminAuth`) |
| `get_accounts_batch` | пакетное чтение (до 100 id), скрывает аккаунты с ролью не ниже своей | `GET /api/user/batch` (`AdminAuth`) |
| `get_account` | один аккаунт с правами администратора | `GET /api/user/:id` (`AdminAuth`) |
| `create_account` | создание аккаунта с ролью ниже своей, права администратора, аудит `user.create` | `POST /api/user/` (`AdminAuth`) |
| `update_account` | изменение аккаунта, права администратора, отзыв сессий при смене пароля или группы, аудит `user.update` | `PUT /api/user/` (`AdminAuth`) |
| `manage_account` | `disable` / `enable` / `delete` / `promote` / `demote`, аудит `user.manage`; `add_quota` передаётся в `adjust_quota` | `POST /api/user/manage` (`AdminAuth`) |
| `adjust_quota` | ручное изменение квоты (`add` / `subtract` / `override`), аудит операции и запись в лог получателя | через `POST /api/user/manage` с `action: add_quota` |
| `delete_account` | полное удаление аккаунта, аудит `user.delete` | `DELETE /api/user/:id` (`AdminAuth`) |
| `clear_binding` | снятие привязки (email, GitHub, Discord, OIDC, WeChat, Telegram, LinuxDO), аудит `user.binding_clear` | `DELETE /api/user/:id/bindings/:binding_type` (`AdminAuth`) |
| `change_group` | массовая смена группы, пропускает чужие по роли | `POST /api/user/group/batch` (`AdminAuth`) |
| `get_groups` | список групп (`List`) и группы, доступные пользователю (`ForUser`) | `GET /api/group/` (`AdminAuth`); `GET /api/user/groups`, `GET /api/user/self/groups` (`UserAuth`) |
| `get_profile` | свой профиль с разделами панели и правами | `GET /api/user/self` |
| `update_profile` | свои настройки (боковая панель, язык), имя и отображаемое имя или смена пароля через `identity`, аудит `user.password_change` | `PUT /api/user/self` + `CriticalRateLimit`, `DisableCache` |
| `get_account_models` | модели, доступные группам пользователя | `GET /api/user/models` |
| `get_affiliate_code` | реферальный код, создаёт при первом запросе | `GET /api/user/aff` |
| `transfer_affiliate_quota` | перевод реферальной квоты в обычную (требует подтверждения платёжного соответствия) | `POST /api/user/aff_transfer` + `UserCriticalRateLimit("aff-transfer")` |
| `update_notification_preferences` | настройки уведомлений о квоте (email, webhook, Bark, Gotify) | `PUT /api/user/setting` |
| `get_checkin_status` | статистика отметок за месяц | `GET /api/user/checkin` |
| `check_in` | ежедневная отметка с наградой, запись в лог пользователя | `POST /api/user/checkin` + `TurnstileCheck` |
| `provision_account` | создание аккаунта и PAT для интеграции GetAPI, ответ 201 | `POST /api/getapi/users` + `GetAPIAuth("getapi.users.provision")` |
| `register_account`, `persist_account`, `bind_email` | регистрация, сохранение аккаунта с отзывом сессий при изменении безопасности, привязка email | без HTTP, через `model` (`User.Insert`, `User.Update`/`Edit`, привязка email) |

Ошибки сценариев — значения из `domain/errors.go`. Их переводит в HTTP общий `transport.WriteError`:
- `ErrAccountNotFound` → 404 `user_not_found`;
- `ErrManageForbidden` / `ErrViewForbidden` → 403 `permission_denied` (тексты «update» и «access» как раньше);
- ошибки действий над root и ролями → 403/409 со своими кодами (`cannot_disable_root`, `user_already_admin`, …);
- `ErrUsernameTaken` → 409 `username_already_taken` (только `update_account`);
- ошибки корректировки квоты → 400 `quota_change_zero` / `invalid_params`, `ErrQuotaLimitExceeded` → 200 с текстом `wallet quota limit exceeded`;
- `domain.Internal(err)` → 500 `internal_error` с текстом причины; `ErrAccountIDRequired` (нулевой id) → тот же статус и код, но с i18n-сообщением `user.id_required` (без обёртки `Internal` — 200 `{success:false}`);
- ошибки массовой смены группы → 400/403/404 без кода, `domain.GroupChangeFailed(err)` → 500 без кода с текстом причины;
- ошибки отметки, перевода реферальной квоты, настроек уведомлений и профиля → 200 `{success:false}` с i18n-сообщением; `domain.AffiliateTransferFailed(err)` вкладывает переведённую причину в «Transfer failed …»;
- ошибка без доменного смысла → 200 `{success:false}` с её текстом.

Исключения: `update_profile` передаёт ошибку смены пароля (`domain.ErrPasswordChangeFailed`) в обработчик ошибок операций безопасности из `controller`; `provision_account` пишет ошибки через обработчик GetAPI (`writeGetAPIError`).

Тексты отметки и перевода реферальной квоты раньше были захардкожены на китайском; теперь это i18n-ключи `checkin.*`, `user.transfer_quota_minimum`, `user.invite_quota_insufficient` (китайский текст сохранён в `zh-CN`).

OpenAPI-генератор (`cmd/gen-admin-openapi/accounts.go`) берёт статус и код из `switch` в `transport/errors.go`; список доменных ошибок каждого обработчика задан в `accountHandlerErrors` в порядке прежнего `switch`.

## Зависимости

- `domain` модуля чистый: не импортирует Gin, GORM, Redis и `common`; роли передаются через `AdminRoles`.
- Сборка — `internal/app/accounts.go`, один раз через `sync.OnceValue` в `controller/accounts.go`.
- Хранение: `internal/adapter/gorm/accounts` (административные операции, запросы, профиль) поверх `internal/adapter/gorm/accounts/record` (GORM-запись `users`, `checkins`, транзакции).
- Настройки: `internal/adapter/settings/accounts` (группы), `internal/adapter/settings/checkin` (отметка), `ratio_setting.ContainsGroupRatio`.
- Права администраторов: `service/authz` (`SetUserPermissionsInTx`, `ClearUserAuthorizationInTx`, `ReloadPolicy`, `Capabilities`).
- Смена пароля: `internal/adapter/identity/accounts` реализует `update_profile.PasswordChanger` через сценарий `identity/change_self_password`.
- Аудит и ошибки безопасности: функции `controller` (`recordManageAuditFor`, `recordUserSecurityAudit`, `writeSecurityOperationError`, `accountQuotaAudit`), передаются через `app.AccountsEffects`.
- Потребители: `controller/user.go`, `checkin.go`, `group.go`, `getapi_users.go`, `user_quota.go` (HTTP); `model` (регистрация, сохранение, удаление, отметка, перевод реферальной квоты); `identity` использует `accounts/domain`.

## Границы и долг

- Модулю не принадлежат вход, сессии, 2FA, passkey и OAuth (`identity`), права администраторов (`authorization`), API-ключи (`apikeys`), пополнение баланса и списание квоты шлюзом.
- ⚠ `model` всё ещё импортирует `internal` по аккаунтам (записи `legacy-imports-internal` для `model/user.go`, `accounts_*.go`, `checkin.go`): GORM-запись `User` живёт в адаптере, а `model` создаёт usecase регистрации, сохранения, удаления, отметки, перевода реферальной квоты, корректировки квоты и выдачи GetAPI-аккаунта.
- `provision_account` получает ошибки через интерфейс `Errors` (`model.ProvisionErrors`) и фабрику `invalid func() error`: это контракт интеграции GetAPI (`model.ErrGetAPI*`, `writeGetAPIError`), общий с другими её маршрутами; тест `TestAccountsPersistenceCompatibility` фиксирует позднее чтение этих ошибок.
- `adjust_quota`, `transfer_affiliate_quota` импортируют `common` (лимит и преобразование квоты), `update_notification_preferences`, `update_profile`, `dto` — `relaykit/dto` (`UserSetting`): записи `usecase-transitive` и `business-imports-gateway` остаются.
- Метки групп («用户分组» — описание собственной группы, «自动» — коэффициент группы `auto`) переводятся на язык запроса только в ответе `get_groups`; остальные потребители `accountssettings.UserGroups()` (service, pricing, relay) получают прежние zh-CN значения. Записи лога пользователя (регистрация, приглашение, отметка) пишутся на языке запроса того, кто выполняет действие.
- Ответы ошибок `check_in` и `get_checkin_status` раньше писались структурой (`success`, `message`), теперь `common.ApiErrorI18n` / `ApiError` (`gin.H`): тот же JSON, другой порядок ключей в теле.
