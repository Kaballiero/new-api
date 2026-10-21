# personal_access_tokens — персональные токены доступа к панели

## Ответственность

Модуль владеет персональным токеном доступа (PAT) пользователя к API панели управления: одно поколение токена на аккаунт (`users.access_token`, `users.access_token_created_at`), его выпуск, статус и отзыв.

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

Аутентификация запросов по PAT (`middleware.UserAuth`, `model.ValidateAccessToken`) и запись аудита использования (`middleware.AccessTokenAudit`) модулю пока не принадлежат: они живут в `middleware/` и `model/`.

Ошибки сценариев — значения из `domain/errors.go`. Их переводит в HTTP общий `transport.WriteError`:
- `ErrTokenDuplicate` (сгенерированный токен уже занят) → 200 `{success:false}` с i18n-сообщением `common.uuid_duplicate`;
- всё остальное, включая `domain.Internal(err)`, передаётся без изменений общему обработчику ошибок операций безопасности (`identityhttp.WriteSecurityOperationError`, подключается в `app`): отсутствующий аккаунт (`gorm.ErrRecordNotFound`) → 401 `AUTH_UNAUTHORIZED`, прочие сбои → 500 `AUTH_INTERNAL_ERROR` с записью в лог; в обоих случаях `security_error_code` = `AUTH_INTERNAL_ERROR` для аудита;
- ответы повторной проверки (`SECURITY_PROOF_*`) пишет сам `middleware.RequireSecurityProof` до вызова usecase.

OpenAPI-генератор (`cmd/gen-admin-openapi/personal_access_tokens.go`) читает DTO и `http.go` сценариев и сохраняет публичные имена схем (`UserAccessTokenStatus`).

## Зависимости

- Чужие `domain` модуль не импортирует.
- Сборка — `internal/app/personal_access_tokens.go`, один раз через `sync.OnceValue` в `controller/access_token.go`.
- Хранение: `internal/adapter/gorm/personal_access_tokens` (`Store`) поверх GORM-записи `users` из `internal/adapter/gorm/accounts/record` и таблицы аудита; экземпляр создаёт `model.NewPersonalAccessTokenPersistence` (`model.DB`, `model.LOG_DB`, `lockForUpdate`).
- Повторная проверка: `middleware.RequireSecurityProof` со скоупами `service.VerificationScopeAccessTokenGenerate` / `...Revoke`.
- Ошибки операций безопасности: `internal/adapter/identity/http` (`WriteSecurityOperationError`).
- Аудит: `recordUserSecurityAudit` из `controller/audit.go`.
- Потребители: `controller/access_token.go` (HTTP), `model/audit_log.go` (`AccessTokenFingerprint` для `middleware.AccessTokenAudit`).

## Границы и долг

- Модулю не принадлежат аутентификация по PAT, аудит использования и чтение журнала аудита (`GET /api/audit*`, `controller.GetAuditLogs`).
- ⚠ `model` всё ещё импортирует `internal` (2 записи в списке исключений архитектурного теста):
  - `model/personal_access_tokens.go` → `adapter/gorm/personal_access_tokens`: `NewPersonalAccessTokenPersistence` нужен приватный `lockForUpdate`, а `UpdateUserAccessToken` используют тесты `model/` и `controller/`;
  - `model/audit_log.go` → `personal_access_tokens/domain`: `AccessTokenFingerprint` для `middleware.AccessTokenAudit` (`middleware` не может импортировать `internal/app`).
- Ошибки хранения и формат ответа 401/500 принадлежат identity (`WriteSecurityOperationError`); при переносе этого обработчика в `identity/transport` меняется только подключение в `app`.
