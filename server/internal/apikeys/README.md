# apikeys — API-ключи пользователей

## Ответственность

Модуль владеет API-ключами (`tokens`), с которыми клиенты ходят в шлюз: имя, ключ, статус, срок действия, остаток и расход квоты, ограничения по моделям и IP, группа и auto-группы, повтор в другой группе.

Модуль отвечает за инварианты ключа:
- имя не длиннее 50 байт и хранится без пробелов по краям (`strings.TrimSpace`);
- имя уникально среди неудалённых ключей пользователя без учёта регистра и пробелов по краям (`domain.NameTaken`, Unicode-сравнение в Go, поэтому работает и для кириллицы на SQLite). Проверяется только при создании и переименовании: `create_key` и `update_key` (кроме `status_only`) сохраняют ключ через `InsertWithUniqueName` / `UpdateWithUniqueName`, где проверка и запись идут в одной транзакции с блокировкой пользователя (`SELECT id FROM users WHERE id = ? FOR UPDATE` на MySQL/PostgreSQL, на SQLite — запись (`UPDATE tokens … WHERE 1 = 0`) до первого чтения, она берёт блокировку записи всей БД). Переименование — только если обрезанное новое имя отличается от обрезанного сохранённого; имя пишет только `UpdateWithUniqueName`, обычный `Update` колонку `name` не трогает, поэтому параллельная правка не вернёт устаревшее имя. Удалённые ключи имя не занимают; существующие дубли не исправляются. Нарушение — `ErrNameDuplicate`;
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
| `get_key_status` | сводка `credit_summary` по ключу | HTTP-маршрута нет; native fixture вызывает owning HTTP напрямую, а OpenAPI parser читает его AST |
| `validate_key` | проверка ключа и владельца, срока/квоты с прежней записью статуса, IP, группы, ограничений моделей и pin канала; возвращает типизированный `Caller` | `TokenAuth`, ключевая ветка `TokenOrUserAuth`; `TokenAuthReadOnly` допускает expired/exhausted, но отклоняет disabled |
| `validate_key.PrepareContext` | контекст доверенного временного ключа playground, включая auto-группы и pin; те же правила, что у ключа шлюза | `middleware.SetupContextForToken` для `gateway/transport/playground.go` |
| `disable_model_limits` | снимает ограничения по моделям у ключа | без HTTP, напрямую через сценарий |
| `delete_owned_key` | удаляет ключ владельца по id | без HTTP, напрямую через сценарий |

Ошибки сценариев — значения из `domain/errors.go`. Их переводит в HTTP общий `transport.WriteError`:
- `ErrKeyNotFound` → 404 `token_not_found`;
- `ErrNameDuplicate` → 409 `token_name_duplicate`;
- `domain.Internal(err)` → 500 `internal_error` с текстом причины;
- ошибки поиска и `ErrKeyIDRequired` → 500 `internal_error` с i18n-сообщением (статус и код как в `origin/main`, текст раньше был захардкожен на китайском);
- остальные бизнес-ошибки → свои статус, код и i18n-сообщение;
- ошибка без доменного смысла → 200 `{success:false}` с её текстом.

OpenAPI-генератор (`cmd/gen-admin-openapi/apikeys.go`) берёт коды ошибок из этого `switch` и из ссылок `domain.ErrX` в `usecase.go`.

## Зависимости

- Чужие `domain` модуль не импортирует. `domain.Caller`, `Owner`, `KeyContext` не содержат Gin/ORM/relay-типы.
- Gateway-сборка — `internal/app/authentication/apikeys.go`; узкий пакет сборки не импортирует middleware и предотвращает цикл с прежним `app`. app/authentication.GatewayKeyAuth держит её один раз через `sync.OnceValue`; настройки/БД читаются при запросе.
- `internal/adapter/apikeys.Authentication` адаптирует прежние кэш/хранилище, снимок владельца, admin-проверку и декодирование auto-групп; бизнес-проверки находятся в сценарии.
- Сборка — `internal/app/apikeys.go`, один раз через `sync.OnceValue` в `app/httpapi/handlers.go`.
- Хранение: `internal/adapter/gorm/apikeys` (`Store`) поверх `internal/adapter/gorm/apikeys/record` (GORM-запись `tokens`, кэш-fence при записи, чтение по ключу через Redis-кэш).
- Настройки: `internal/adapter/settings/apikeys` (`MaxQuota`), `operation_setting.GetMaxUserTokens`, `setting.GetMaxTokenAutoGroups`.
- Группы: settings adapters групп и `app/accountstorage.Cache`.
- Аудит: audit/transport.TokenParameters и BatchParameters, собранные в app/httpapi; запись события делает middleware `TokenOperationAudit`.
- Потребители: `app/httpapi/handlers.go` (HTTP), gateway и app assembly через явные capabilities.

## Границы и долг

- `model.Token` — строгий alias `adapter/gorm/apikeys/record.Token`. Record хранит таблицу и codec; `record.Store` выполняет mutations, `KeyReader` — чтение. Старые model factories и test-only facades удалены.
- `adapter/cache/apikeys` владеет token hash/fence/init/delta; `adapter/gorm/apikeys.QuotaStore` — SQL и прежним enqueue. Единственные пять counter maps и flush принадлежат `adapter/gorm/counters`, reserve orchestration — billing adapter. Redis keys, Lua и TTL сохранены.
- `app/apikeystorage` собирает persistence/reader/quota один раз, с поздними DB/key-column getters. Gateway credential adapter и Batch получают реальные reader/quota capabilities.
- Gateway key checks собирает app/authentication.GatewayKeyAuth; middleware.SetupContextForToken выполняет реальную context projection для router/playground. Временных transport bridges и model forwarding layers нет.
- Ограничения моделей передаются сценарием в контекст; фактический запрошенный model по-прежнему проверяет `middleware.Distribute` до выбора канала.
- Общие ошибки шаблона поиска принадлежат apikeys/domain; usage и payments adapters сохраняют их прежний English error text.

`get_billing_subscription` владеет совместимым `/v1/dashboard/billing/subscription`: порядок чтения,
unlimited 1e8, expiry и currency projection сохранены. `app.NewBillingCompatibilityHTTP` собирает handler
и технический reader; `controller/billing.go` содержит однострочные router bindings.
Shared quota/cache регрессии находятся у `adapter/cache/accounts` и `adapter/gorm/billing`.
