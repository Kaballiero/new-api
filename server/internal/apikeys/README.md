# apikeys — API-ключи пользователей

## Ответственность

Модуль владеет API-ключами (`tokens`), с которыми клиенты ходят в шлюз: имя, ключ, статус, срок действия, остаток и расход квоты, ограничения по моделям и IP, группа и auto-группы, повтор в другой группе.

Модуль отвечает за инварианты ключа:
- имя не длиннее 50 байт;
- квота ограниченного ключа лежит в диапазоне `0..MaxQuota`;
- число ключей пользователя ограничено `operation_setting.GetMaxUserTokens`;
- `cross_group_retry` и auto-группы есть только у группы `auto`;
- просроченный или исчерпанный ключ нельзя включить;
- ключ отдаётся клиенту только с маской, полный ключ — только через сценарии раскрытия с аудитом.

Понятия и бизнес-ошибки описаны в `domain/`:
- `Token`, статусы, `MaskKey`;
- правила auto-групп (`AutoGroupRules`);
- правила LIKE-поиска (`SanitizeSearchPattern`);
- ошибки (`errors.go`).

## Возможности

Все маршруты `/api/token/*` идут через `UserAuth` (сессия панели или PAT владельца) и `TokenOperationAudit`. Пользователь видит и меняет только свои ключи.

| Сценарий | Что делает | Маршрут |
|---|---|---|
| `list_keys` | список ключей с маской, постранично | `GET /api/token/` |
| `search_keys` | поиск по имени и ключу (`%` — нечёткий поиск, не больше двух `%`; при числе ключей больше лимита — только точный поиск) | `GET /api/token/search` + `SearchRateLimit` |
| `get_key_groups` | auto-группы, доступные пользователю, и их максимум | `GET /api/token/auto-groups` |
| `get_key` | один ключ с маской | `GET /api/token/:id` |
| `reveal_key` | полный ключ, аудит `token.key_view` | `POST /api/token/:id/key` + `CriticalRateLimit`, `DisableCache` |
| `create_key` | создание ключа, ответ 201, аудит `token.create` | `POST /api/token/` |
| `update_key` | изменение ключа или только статуса (`?status_only=`), аудит `token.update` / `token.status_update` со списком изменённых полей | `PUT /api/token/` |
| `delete_key` | удаление ключа, аудит `token.delete` | `DELETE /api/token/:id` |
| `delete_keys_batch` | пакетное удаление, аудит `token.delete_batch` | `POST /api/token/batch` |
| `reveal_keys_batch` | полные ключи пачкой (до 100 id), аудит `token.key_view_batch` | `POST /api/token/batch/keys` + `CriticalRateLimit`, `DisableCache` |
| `get_key_usage` | остаток и расход квоты по самому ключу из `Authorization: Bearer sk-…` | `GET /api/usage/token/` + `CORS`, `CriticalRateLimit`, `TokenAuthReadOnly` |
| `get_key_status` | сводка `credit_summary` по ключу | маршрута нет (как и в `origin/main`); обработчик `controller.GetTokenStatus` остаётся в OpenAPI-генераторе |
| `validate_key` | проверка ключа и владельца, срока/квоты с прежней записью статуса, IP, группы, ограничений моделей и pin канала; возвращает типизированный `Caller` | `TokenAuth`, ключевая ветка `TokenOrUserAuth`; `TokenAuthReadOnly` допускает expired/exhausted, но отклоняет disabled |
| `validate_key.PrepareContext` | контекст доверенного временного ключа playground, включая auto-группы и pin; те же правила, что у ключа шлюза | мост `middleware.SetupContextForToken` для `controller/playground.go` |
| `disable_model_limits` | снимает ограничения по моделям у ключа | без HTTP, через `model.DisableModelLimits` |
| `delete_owned_key` | удаляет ключ владельца по id | без HTTP, через `model.DeleteTokenById` |

Ошибки сценариев — значения из `domain/errors.go`. Их переводит в HTTP общий `transport.WriteError`:
- `ErrKeyNotFound` → 404 `token_not_found`;
- `domain.Internal(err)` → 500 `internal_error` с текстом причины;
- ошибки поиска и `ErrKeyIDRequired` → 500 `internal_error` с i18n-сообщением (статус и код как в `origin/main`, текст раньше был захардкожен на китайском);
- остальные бизнес-ошибки → свои статус, код и i18n-сообщение;
- ошибка без доменного смысла → 200 `{success:false}` с её текстом.

OpenAPI-генератор (`cmd/gen-admin-openapi/apikeys.go`) берёт коды ошибок из этого `switch` и из ссылок `domain.ErrX` в `usecase.go`.

## Зависимости

- Чужие `domain` модуль не импортирует. `domain.Caller`, `Owner`, `KeyContext` не содержат Gin/ORM/relay-типы.
- Gateway-сборка — `internal/app/authentication/apikeys.go`; узкий пакет сборки не импортирует middleware и предотвращает цикл с прежним `app`. `middleware.gatewayKeyAuth` держит её один раз через `sync.OnceValue`; настройки/БД читаются при запросе.
- `internal/adapter/apikeys.Authentication` адаптирует прежние кэш/хранилище, снимок владельца, admin-проверку и декодирование auto-групп; бизнес-проверки находятся в сценарии.
- Сборка — `internal/app/apikeys.go`, один раз через `sync.OnceValue` в `controller/token.go`.
- Хранение: `internal/adapter/gorm/apikeys` (`Store`) поверх `internal/adapter/gorm/apikeys/record` (GORM-запись `tokens`, кэш-fence при записи, чтение по ключу через Redis-кэш).
- Настройки: `internal/adapter/settings/apikeys` (`MaxQuota`), `operation_setting.GetMaxUserTokens`, `setting.GetMaxTokenAutoGroups`.
- Группы: `service.IsUserSelectableGroup`, `service.GetUserAutoGroup`, `model.GetUserGroup`.
- Аудит: функции `tokenAuditParams` / `tokenBatchAuditParams` из `controller/audit.go`; запись события делает middleware `TokenOperationAudit`.
- Потребители: `controller/token.go` (HTTP), `model/token.go` (шлюз и legacy-код).

## Границы и долг

- Модулю не принадлежат списание и возврат квоты ключа (`model.IncreaseTokenQuota` / `DecreaseTokenQuota`, batch updater) и кэш ключей в Redis — это часть биллинга шлюза, они остаются в `model`.
- ⚠ `model/token.go` всё ещё импортирует `internal` (6 записей в списке исключений архитектурного теста):
  - `type Token record.Token` — GORM-запись живёт в адаптере, а `model.Token` на неё ссылается;
  - `SearchUserTokens`, `DisableModelLimits`, `DeleteTokenById` создают usecase внутри `model`;
  - `sanitizeLikePattern` / `validateLikePattern` (их используют `model/log.go`, `model/topup.go`) берутся из `domain`.

  По решению архитектора wave 2a эти шесть зависимостей остаются вне scope. Удалён только импорт `validate_key`; `model.ValidateUserToken` и его stateful validation-адаптер удалены. `GetTokenByKey` остаётся общим техническим чтением для billing/batch; шлюз вызывает его только через адаптер сценария.
- Мосты wave 2a: `middleware.gatewayKeyAuth` для прежних router-фабрик; `middleware.SetupContextForToken` для playground и существующих context-тестов. Оба помечены `BRIDGE`. Технические мосты `model.GetTokenByKey` (billing/batch/credential adapter) и `model.NewTokenPersistence` (mutation adapters/legacy) также отмечены `BRIDGE`. Контекст сохраняет прежние имена, типы и условность записей, включая частичное заполнение при отказе.
- Ограничения моделей передаются сценарием в контекст; фактический запрошенный model по-прежнему проверяет `middleware.Distribute` до выбора канала.
- HTTP goldens `gateway_*` записаны на `origin/main` `6b0f439a4`; тест не обращается к upstream. Это проверка контракта, не подтверждение работы провайдера.
- Ошибки шаблона поиска (`ErrSearch*Wildcards`, `ErrSearchKeywordTooShort`) возвращает и `model.sanitizeLikePattern`, который используют поиск логов и пополнений. Там они уходят клиенту английским текстом ошибки без перевода, пока эти модули не перенесены.
