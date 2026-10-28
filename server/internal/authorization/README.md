# authorization — права администраторов (Casbin)

## Ответственность

Модуль владеет моделью прав панели управления: каталог ресурсов и действий, встроенные роли и их базовые разрешения, переопределения разрешений для отдельного пользователя и политика Casbin (`casbin_rule`, `authz_roles`).

Модуль отвечает за инварианты прав:
- роль `root` — суперпользователь: разрешено всё без записей в политике;
- базовые разрешения роли `admin` берутся из `DefaultRoles` каталога и пересоздаются при старте на master-узле;
- переопределение пользователя хранится только там, где оно отличается от базы роли `admin`; запрет (`deny`) пользователя сильнее разрешения;
- проверяются только разрешения из каталога; неизвестные ресурсы и действия игнорируются;
- без системной роли (`ResolveSystemRoles`) прав нет.

Понятия и бизнес-ошибки описаны в `domain/`:
- `Permission`, `PermissionsMap`, `Catalog`, `ResourceDefinition`, `ActionDefinition`, `RoleSpec`, `RoleDescriptor`;
- ресурсы `audit`, `channel`, `task_plugin` и встроенные роли;
- правила доступа (`AccessRules`) и переопределений (`UserOverridePolicies`);
- ошибки (`errors.go`).

## Возможности

| Сценарий | Что делает | Маршрут |
|---|---|---|
| `get_permission_catalog` | каталог ресурсов и действий, роли с базовыми разрешениями (для редактора прав) | `GET /api/authz/catalog` + `AdminAuth` |
| `check_permission` | разрешено ли действие пользователю | без HTTP, через `authorizationcore.Can` (`middleware.RequirePermission`) |
| `get_capabilities` | матрица разрешений пользователя по каталогу | без HTTP, через `authorizationcore.Capabilities` (профиль, карточка аккаунта) |
| `get_user_overrides` | явные переопределения пользователя | без HTTP, через сценарий `get_user_overrides` |
| `set_user_permissions` | записать переопределения в загруженную политику | без HTTP, через `authorizationcore.SetUserPermissions` |
| `set_user_permissions_in_tx` | записать переопределения в транзакции вызывающего (политика перечитывается после коммита) | без HTTP, через `authorizationcore.SetUserPermissionsInTx` (accounts) |
| `clear_user_permissions` | снять все переопределения пользователя в загруженной политике | без HTTP, через сценарий `clear_user_permissions` |
| `clear_user_permissions_in_tx` | снять переопределения в транзакции вызывающего | без HTTP, через `authorizationcore.ClearUserAuthorizationInTx` (accounts) |
| `initialize_authorization` | при старте: на master-узле upsert ролей и пересоздание базовых разрешений, затем создание enforcer | без HTTP, `authorizationcore.Init` из `main.go` |
| `reload_policy` | перечитать политику из БД | без HTTP, `authorizationcore.ReloadPolicy` (accounts после коммита) |
| `sync_policy` | фоновое перечитывание политики раз в `SyncFrequency` секунд с логом ошибок | фоновая задача `authorizationcore.StartPolicySync` из `main.go` |

Ошибки сценариев — значения из `domain/errors.go`:
- `ErrEnforcerNotInitialized` — операция с политикой до `initialize_authorization`, текст прежний (`authz enforcer is not initialized`);
- `domain.Internal(err)` — сбой хранилища политики или Casbin, текст причины.

Собственного `transport` у модуля нет: единственный HTTP-сценарий (`get_permission_catalog`) не возвращает ошибок, а ошибки записи прав доходят до клиента через сценарии accounts и их транспорт. Отказ в доступе (403 с `auth.insufficient_privilege`) пишет `middleware.RequirePermission`.

OpenAPI-генератор (`cmd/gen-admin-openapi/authorization.go`) читает типы `domain`, DTO и `http.go` сценария `get_permission_catalog` и сохраняет публичные имена схем (`AuthzCatalogResponse` и др.).

## Зависимости

- Чужие `domain` модуль не импортирует.
- Сборка обработчика каталога — `internal/app/authorization.go`, один раз через `sync.OnceValue` в `app/httpapi/handlers.go`.
- Leaf `internal/app/authorizationcore` собирает проверки, capabilities, запись загруженной политики и reload/sync один раз через `sync.OnceValue`; каталог и встроенные роли общие. Он не импортирует parent app, middleware или gateway, поэтому middleware и accounts используют одну сборку без цикла. Init получает текущую БД, операции in-tx — транзакцию вызывающего; эти адаптеры создаются на соответствующей границе вызова.
- Адаптеры:
  - `internal/adapter/casbin/authorization` — модель Casbin, единственное состояние enforcer и его `RWMutex`, публикация и перечитывание (`Engine`, `State`, `Policy`);
  - `internal/adapter/gorm/authorization` — Casbin `persist.Adapter` поверх `casbin_rule` и `Store` (upsert ролей, сброс базовой политики, запись переопределений в транзакции);
  - `internal/adapter/gorm/authorization/record` — GORM-записи `CasbinRule`, `AuthzRole` (`model` держит алиасы для миграций).
- Потребители: `middleware.RequirePermission`, профиль и карточка аккаунта (`authorizationcore.Capabilities`), изменение прав администратора в accounts, `main.go` (инициализация и синхронизация).

## Границы и долг

- Модулю не принадлежат системные роли пользователей (`common.Role*`) и решение, кому можно менять права: это accounts.
- `service/authz` удалён; legacy-исключений и мостов для authorization нет. `model/casbin_rule.go` и `model/authz_role.go` остаются строгими алиасами записей для миграций.
- `reload_policy` и `sync_policy` возвращают и логируют ошибку адаптера без классификации.

Инициализация сохраняет порядок: master upsert ролей → reset базовой политики → создание и загрузка enforcer → запись разрешений admin. Follower только создаёт и загружает enforcer. In-tx запись не меняет RAM; accounts завершает транзакцию и затем перечитывает политику. Rollback не публикует изменения. Синхронизация на каждом узле сначала ждёт `SyncFrequency`, затем выполняет reload; неположительный интервал отключает цикл.
