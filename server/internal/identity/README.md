# identity — вход и доказательство личности

## Ответственность

Модуль владеет тем, как пользователь доказывает, что он — владелец аккаунта: учётные данные (пароль), сессии панели и refresh-токены, второй фактор (TOTP и резервные коды), passkey (WebAuthn), внешние входы (OAuth, Telegram, WeChat), привязка и отвязка внешних аккаунтов и e-mail, восстановление пароля и одноразовые доказательства (security proof) для чувствительных действий.

Модуль отвечает за инварианты:
- сессия действительна, только пока совпадают версия аутентификации пользователя (`auth_version`) и версия сессии; смена фактора или пароля продвигает текущую сессию и отзывает остальные;
- число активных сессий и частота их выпуска ограничены (`UserSessionActiveLimit`, `UserSessionIssuanceLimit`), лимиты читаются в момент решения;
- одноразовые потоки (`auth_flows`) и доказательства (`security proof`) действуют один раз и до истечения срока; повтор — ошибка, а не повторный успех;
- чувствительное действие требует доказательства с совпадающими scope, контекстом и методом;
- ответ на запрос сброса пароля не раскрывает, зарегистрирован ли адрес.

Понятия и бизнес-ошибки описаны в `domain/`:
- `AuthIdentity`, `SessionState`, `SessionPolicy`, области проверки (`VerificationScope*`), контексты привязки;
- правила e-mail (`ValidateEmail`) и пароля (`PasswordCredential`);
- ошибки (`errors.go`): значения, которыми пользуются сценарии, transport и сохранённые имена records в `model` (алиасы).

## Возможности

`UserAuth` — сессия панели или PAT владельца, `AdminAuth` — администратор. `CriticalRateLimit` и `DisableCache` указаны там, где они стоят на маршруте; у группы `/api/user/` с `UserAuth` всегда есть `DisableCache`.

| Сценарий | Что делает | Маршрут |
|---|---|---|
| `password_login` | вход по паролю (с шифрованием пароля, если включено) | `POST /api/user/login` + `CriticalRateLimit`, `TurnstileCheck` |
| `get_password_encryption_key` | публичный ключ шифрования пароля | `GET /api/user/login/encryption-key` |
| `register_password` | регистрация по паролю, проверка e-mail, токен по умолчанию | `POST /api/user/register` + `CriticalRateLimit`, `TurnstileCheck` |
| `verify_login_code` | второй шаг входа кодом 2FA | `POST /api/user/login/verify`, `POST /api/user/login/2fa` + `CriticalRateLimit` |
| `begin_login_passkey` / `finish_login_passkey` | второй шаг входа через passkey | `POST /api/user/login/passkey/{begin,finish}` + `CriticalRateLimit` |
| `begin_passkey_login` / `finish_passkey_login` | вход без пароля через passkey | `POST /api/user/passkey/login/{begin,finish}` + `CriticalRateLimit` |
| `refresh_session` | ротация refresh-токена, новый access-токен | `POST /api/user/auth/refresh` + `SessionCookieOriginGuard`, `CriticalRateLimit` |
| `logout` | отзыв текущей сессии и cookie | `POST /api/user/auth/logout` + `SessionCookieOriginGuard`, `CriticalRateLimit` |
| `list_sessions`, `revoke_session`, `revoke_other_sessions` | сессии пользователя (только сессия панели) | `GET /api/user/sessions`, `DELETE /api/user/sessions/:sid`, `POST /api/user/sessions/revoke-others` + `UserAuth` |
| `get_verification_requirements` | доступные методы проверки для scope | `GET /api/verify/methods` + `UserAuth` |
| `verify_security_input` | доказательство паролем или 2FA | `POST /api/verify` + `UserAuth`, `CriticalRateLimit`, `UserCriticalRateLimit` |
| `begin_passkey_verification` / `finish_passkey_verification` | доказательство через passkey | `POST /api/user/passkey/verify/{begin,finish}` + `UserAuth`, `UserCriticalRateLimit` |
| `get_passkey_status`, `begin_passkey_registration`, `finish_passkey_registration`, `delete_passkey` | passkey пользователя; регистрация и удаление требуют доказательства, аудит `user.passkey_register` / `user.passkey_delete` | `/api/user/passkey…` + `UserAuth` |
| `get_twofa_status`, `start_twofa_setup`, `finish_twofa_setup`, `disable_twofa`, `regenerate_backup_codes` | 2FA пользователя; изменения требуют доказательства и пишут аудит `user.2fa_*` | `/api/user/2fa/…` + `UserAuth` |
| `get_twofa_stats`, `admin_disable_twofa`, `admin_reset_passkey` | факторы пользователей для администратора; только для ролей ниже своей (root — для всех) | `GET /api/user/2fa/stats`, `DELETE /api/user/:id/2fa`, `DELETE /api/user/:id/reset_passkey` + `AdminAuth` |
| `start_oauth_flow` | начало OAuth/Telegram: вход, привязка (с доказательством) или проверка | `POST /api/oauth/state` + `CriticalRateLimit`, `TryUserAuth` |
| `complete_oauth` | callback провайдера: вход/регистрация, привязка, доказательство | `GET /api/oauth/:provider` + `CriticalRateLimit`, `TryUserAuth` |
| `login_wechat`, `bind_wechat` | вход и привязка WeChat | `GET /api/oauth/wechat`, `POST /api/oauth/wechat/bind` + `UserAuth` |
| `get_oauth_bindings`, `unlink_oauth_account`, `admin_unlink_oauth_account` | привязки OAuth пользователя и их отвязка | `/api/user/oauth/bindings…` + `UserAuth`, `/api/user/:id/oauth/bindings…` + `AdminAuth` |
| `start_email_binding`, `resend_email_binding`, `confirm_email_binding` | смена e-mail с кодами подтверждения | `POST /api/oauth/email/bind/{start,resend}`, `POST /api/oauth/email/bind` + `UserAuth` |
| `send_verification_email` | код подтверждения e-mail для регистрации | `GET /api/verification` + `EmailVerificationRateLimit`, `TurnstileCheck` |
| `request_password_reset`, `recover_password` | ссылка сброса пароля и новый пароль по ней | `GET /api/reset_password`, `POST /api/user/reset` + `CriticalRateLimit` |
| `delete_account` | удаление своего аккаунта с доказательством, аудит `user.account_delete` | `DELETE /api/user/self` + `UserAuth` |
| `create_session`, `validate_session`, `revoke_refresh_token`, `advance_session_security` | выпуск, проверка, отзыв и продвижение сессий | без HTTP, через leaf `app/identitycore` и `middleware` |
| `start_login_verification`, `require_login_verification`, `complete_login_verification`, `require_verification_method`, `complete_primary_login` | второй шаг входа | без HTTP, через `app/identitycore` и `adapter/identity/http.PrimaryLogin` |
| `verification_policy`, `bind_verification_operation`, `validate_authorization`, `consume_proof`, `verify_factor_code`, `complete_verification`, `start_oauth_verification`, `finish_oauth_verification` | правила и выдача доказательств | без HTTP, через `app/identitycore` и `middleware.RequireSecurityProof` |
| `create_passkey_flow`, `consume_passkey_flow` | одноразовые данные WebAuthn-церемоний | без HTTP, через `app/identitycore` и `adapter/identity/webauthn` |
| `list_custom_oauth_providers`, `get_custom_oauth_provider` | настройки custom OAuth без client secret; список без бизнес-правил использует узкий Reader | `GET /api/custom-oauth-provider/`, `GET /api/custom-oauth-provider/:id` + `RootAuth` (сессия/PAT) |
| `create_custom_oauth_provider`, `update_custom_oauth_provider`, `delete_custom_oauth_provider` | проверки slug и привязок, запись и последующая публикация в общем registry | `POST /api/custom-oauth-provider/`, `PUT/DELETE /api/custom-oauth-provider/:id` + `RootAuth` |
| `fetch_custom_oauth_discovery` | загрузка discovery по URL либо issuer, прежние ответы и ошибки провайдера | `POST /api/custom-oauth-provider/discovery` + `RootAuth` |
| `cleanup_auth_artifacts` | alert по часовому выпуску, затем независимая очистка expired sessions, старых revoked sessions и auth flows | фоновый запуск на master: сразу и каждый час, каждый вызов Start запускает прежний отдельный цикл |
| `change_password`, `change_self_password`, `reset_password` | запись пароля | без HTTP, через `model/account_security.go`, `model/user.go`, `adapter/identity/accounts` |

Ошибки сценариев — значения из `domain/errors.go`. В HTTP их переводит `transport.Writer` (собирается один раз в `app.NewIdentityErrors`):
- `WriteError` — ответ операции безопасности: `{success:false, code, message}` с прежними статусом и кодом (`EMAIL_*`, `PASSWORD_POLICY_REJECTED`, `ACCOUNT_SECURITY_STATE_CHANGED`, `TELEGRAM_*`, `SECURITY_*`, `TWOFA_*`, `PASSKEY_NOT_FOUND`, `AUTH_FLOW_INVALID`), значение `security_error_code` в контексте; исходы passkey-церемоний — `{success:false, message}` с i18n-сообщением;
- `WriteSessionError` — ответ сессии: статус и код (`AUTH_SESSION_LIMIT`, `AUTH_SESSION_ISSUANCE_LIMIT`, `AUTH_SESSION_MISMATCH`, `AUTH_REFRESH_RACE`, `AUTH_TOKEN_EXPIRED`, `AUTH_SESSION_REVOKED`, `AUTH_UNAUTHORIZED`), текст статуса как сообщение; прочие ошибки — 500 `AUTH_INTERNAL_ERROR` с записью причины в лог;
- исходы конкретного сценария (вход, регистрация, OAuth callback, админские операции) сопоставляет его `http.go`, остальное передаёт `WriteError`.

`transport.Foreign` распознаёт ошибки, значения которых принадлежат не identity: занятый e-mail (`model.ErrEmailAlreadyTaken`, владелец — accounts), ошибки протокола WebAuthn и `gorm.ErrRecordNotFound`.

OpenAPI-генератор (`cmd/gen-admin-openapi/identity.go`) читает `http.go` и DTO сценариев; опубликованные схемы, которые обход AST не выводит, зафиксированы в `preserveIdentityHandlerContract`.

## Зависимости

- `accounts/domain` — правило управления ролью цели (`CanManageTargetRole`) в админских сценариях.
- HTTP-сборка — `internal/app/identity.go`, один раз через `sync.OnceValue` в `controller/identity.go`; custom OAuth — `app/custom_oauth.go` и собственный once в `controller/custom_oauth.go`. Leaf `app/identitycore` один раз собирает session/proof/login/email/passkey-flow usecases и зависимости; каждый запрос получает отдельные operation adapters. `app.NewIdentityErrors` также используют `personal_access_tokens` и `controller.writeSecurityOperationError` (accounts, channels).
- Адаптеры: `internal/adapter/identity/*` поверх `model` (сессии, факторы, WebAuthn SDK, OAuth, e-mail, восстановление, регистрация). `authtoken` сохраняет JWT/HMAC и выпуск proof; `session` — record/bundle mapping и refresh crypto; `http` — cookie flags/сроки, `PrimaryLogin`, загрузка текущего пользователя и ошибки провайдеров. Исходящий discovery HTTP — `adapter/http/identity`; SQL/custom registry — `adapter/identity/customoauth`.
- Потребители: `controller/` (HTTP), `middleware` (проверка сессии и доказательств через leaf core и token adapter), `model` (сохранённые error/table aliases). Сценарии не получают SDK/ORM types; HTTP passkey/OAuth/WeChat использует минимальные `PasskeyAccount` facts и `domain.FlowAuthorization`. Bound factory сохраняет первоначальный user record внутри технической сборки, без дополнительного SQL или whole-user DTO.

## Границы и долг

- Модулю не принадлежат профиль и e-mail аккаунта как данные (accounts), права администраторов (authorization), PAT (personal_access_tokens).
- Session/token/proof/login/email/passkey service facades удалены; соответствующие 56 allowlist entries сняты. Middleware и оставшиеся controller delegates используют настоящих владельцев, новых model/service forwarding layers нет.
- SQL/locks/auth cache, records и migration/backfill ещё в `model/account_security.go`, `model/auth_flow.go`, `model/user_session.go`, факторах и account/key persistence; их общий storage lifecycle не перенесён этой частью.
- ⚠ В `model` остаются aliases `ErrUserSession*`, `ErrAuthFlow*`, `ErrTwoFA*`, `ErrPasskeyNotFound`, `ErrEmailBinding*`, `ErrExternalIdentityAlreadyClaimed`, ошибки пароля/привязок до переноса реальных SQL consumers. Service и OAuth aliases этих ошибок удалены вместе с callers.
- Controller callbacks login audit/profile и HTTP delegates сохраняются до перехода router на owning handlers. Identity core не зависит от parent app, middleware или HTTP adapters.
- `password_login.Authenticate` получает фабрики ошибок от `adapter/gorm/accounts/record.PasswordLoginLookup` (адаптер accounts) — граница с accounts не тронута.
- Сообщения об успехе и письма восстановления и подтверждения e-mail переводятся на язык запроса (zh-CN — прежние тексты). AST-генератор OpenAPI не выводит тип `common.TranslateMessage`, поэтому схема `message: string` этих ответов зафиксирована в `preserveIdentityHandlerContract`.
