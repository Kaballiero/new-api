# personal_access_tokens — персональные токены доступа к панели

## Ответственность

Модуль владеет персональным токеном доступа (PAT) пользователя к API панели управления: одно поколение токена на аккаунт (`users.access_token`, `users.access_token_created_at`), его выпуск, статус, отзыв и проверку при входе.

Модуль отвечает за инварианты токена:
- у аккаунта не больше одного действующего токена; выпуск заменяет предыдущее поколение;
- выпуск и отзыв требуют повторной проверки личности в сессии панели (`middleware.RequireSecurityProof`), PAT не может управлять сам собой;
- токен никогда не попадает в аудит и ответ статуса: поколение идентифицируется отпечатком `Fingerprint` (SHA-256 без хвостовых пробелов CHAR-колонки PostgreSQL);
- отзыв идёт под блокировкой строки, повторный отзыв ничего не меняет и не пишет аудит;
- «последнее использование» берётся из журнала аудита только по текущему поколению; для не-root владельца учитываются только действия обычных пользователей и администраторов.

Понятия и бизнес-ошибки описаны в `domain/`:
- `Account`, `Usage`, `AuditRoles`, `Fingerprint`;
- ошибки (`errors.go`).

## Возможности

Все маршруты идут через `UserAuth` и `DisableCache` группы `/api/user/`.

| Сценарий | Что делает | Маршрут |
|---|---|---|
| `get_status` | есть ли токен, его отпечаток `token_ref`, время выпуска, время и IP последнего использования | `GET /api/user/token/status` |
| `generate_token` | выпуск нового токена (29–32 символа) вместо прежнего, ответ — сам токен, аудит `access_token.generate` с `token_ref` | `GET` и `POST /api/user/token` + `CriticalRateLimit`, `UserCriticalRateLimit("access-token")`, повторная проверка `access_token.generate` |
| `revoke_token` | отзыв токена, аудит `access_token.revoke` с `token_ref` (только если токен был) | `DELETE /api/user/token` + `CriticalRateLimit`, `UserCriticalRateLimit("access-token")`, повторная проверка `access_token.revoke` |
| `validate_token` | нормализация PAT, живое чтение владельца; неизвестный/отозванный PAT → nil, сбой хранения → `domain.Internal`; возвращает `Caller` без секрета | проверка PAT в `middleware.UserAuth` / `TryUserAuth` и `AccessTokenAudit` |

Сценарий проверки PAT не проверяет dashboard-сессии и не заменяет последующие проверки статуса/роли владельца в middleware. Аудит использования остаётся в middleware и получает из сценария идентификатор, имя, роль и отпечаток токена.

Ошибки сценариев — значения из `domain/errors.go`. Их переводит в HTTP общий `transport.WriteError`:
- `ErrTokenDuplicate` (сгенерированный токен уже занят) → 200 `{success:false}` с i18n-сообщением `common.uuid_duplicate`;
- всё остальное, включая `domain.Internal(err)`, передаётся без изменений общему обработчику ошибок операций безопасности identity (`identity/transport.Writer.WriteError`, подключается в `app` через `NewIdentityErrors`): отсутствующий аккаунт (`gorm.ErrRecordNotFound`) → 401 `AUTH_UNAUTHORIZED`, прочие сбои → 500 `AUTH_INTERNAL_ERROR` с записью в лог; в обоих случаях `security_error_code` = `AUTH_INTERNAL_ERROR` для аудита;
- ответы повторной проверки (`SECURITY_PROOF_*`) пишет сам `middleware.RequireSecurityProof` до вызова usecase.

OpenAPI-генератор (`cmd/gen-admin-openapi/personal_access_tokens.go`) читает DTO и `http.go` сценариев и сохраняет публичные имена схем (`UserAccessTokenStatus`).

## Зависимости

- Чужие `domain` модуль не импортирует.
- Проверка PAT собирается в `internal/app/authentication/personal_access_tokens.go`; app/authentication.PersonalAccessTokenAuth создаёт её один раз через `sync.OnceValue` для dashboard auth и аудита.
- `internal/adapter/gorm/personal_access_tokens.Authentication` выполняет прежний поиск `users.access_token` с прежним подавлением SQL-логов; выборка и soft-delete semantics сохранены. Секрет не выходит из адаптера в результат сценария.
- Сборка — `internal/app/personal_access_tokens.go`, один раз через `sync.OnceValue` в `app/httpapi/handlers.go`.
- Хранение: `internal/adapter/gorm/personal_access_tokens` (`Store`) поверх GORM-записи `users` из `internal/adapter/gorm/accounts/record` и таблицы аудита; экземпляр один раз собирает `app/patstorage` (поздние DB/LOG_DB getters, shared `locking.LockForUpdate`).
- Повторная проверка: `middleware.RequireSecurityProof` со скоупами `identity/domain.VerificationScopeAccessTokenGenerate` / `...Revoke`.
- Ошибки операций безопасности: `internal/identity/transport` (`Writer.WriteError`, собирается `app.NewIdentityErrors`).
- Аудит: callback app/httpapi к app/audit.Recording.
- Потребители: `app/httpapi/handlers.go` (HTTP), `middleware.AccessTokenAudit` (отпечаток из `Caller`).

## Границы и долг

- Модулю не принадлежат проверка dashboard-сессий, аудит использования и чтение журнала аудита (`GET /api/audit*`, `controller.GetAuditLogs`).
- Model persistence factories удалены; SQL и record capabilities собирает app. GetAPI issuance принадлежит `accounts/initialize_pat`, dashboard PAT lifecycle остаётся здесь.
- Ошибки хранения и формат ответа 401/500 принадлежат identity (`identity/transport.Writer.WriteError`); модуль только передаёт их дальше.

PAT validation регрессии находятся рядом с владельцами и в controller; production consumers используют сценарий через app/authentication.
