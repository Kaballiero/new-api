# Audit — события безопасности и действий

## Ответственность

Модуль владеет чтением audit_logs и выбором новых audit events: owner/actor, категория, action/content, metadata, status/success и role projection. Источник истины для сохранённых событий — отдельная таблица audit_logs в LOG_DB.

## Возможности

| Сценарий | Действие | Маршрут и доступ |
|---|---|---|
| `admin_list_events` | Читать события с actor-role admission и проекцией привилегированных полей | `GET /api/audit`; admin/root, `AdminAuth` + `AuditRead`, session/PAT |
| `list_own_events` | Выбрать owner из авторизации, игнорировать username, убрать admin/audit/root metadata даже для root | `GET /api/audit/self`; любой авторизованный пользователь, `UserAuth`, session/PAT |
| `record_event` | Один Execute после операции: категория, owner/actor, payload, status/success, suppression, defaults и нормализация роли | Внутренний вызов middleware/controllers; доступ задаётся исходным маршрутом, собственной HTTP-точки и фоновой задачи нет |

`GET /api/audit` сохраняет AdminAuth + AuditRead; `/api/audit/self` — UserAuth. Router и порядок middleware не менялись. HTTP-фильтры: username (только admin), category, token_ref, exclude_token_ref, request_id, start_timestamp, end_timestamp, success. Неизвестные параметры не создают фильтры. End=0 выключает верхнюю границу. Валидация сохраняет очередность pagination → category/fingerprints → timestamps → success; explicit false отличим от отсутствия.

Триггеры записи: successful login, security/manual management callbacks, admin fallback, API-key operations, валидный PAT на public/authenticated/rejected запросах, GetAPI integration, quota operator и legacy subscription reset. Manual management/quota маркируют fallback подавленным даже при write failure. Security callback этого не делает. Вложенный status/path и target-owned subscription rows сохраняют прежние различия. Аудит не принимает решения о доступе и не кеширует credential validation.

Ошибки валидации сопоставляются через errors.Is с i18n.MsgAudit*. Default/en/zh-CN/zh-TW остаются прежними английскими байтами; добавлен ru. Нераспознанные storage errors сохраняют прежний HTTP 200 + raw message. Write failure возвращается из Execute, логируется транспортным callback, не меняет ответ операции и не повторяется (#42). Count/Find не получают новую транзакцию (#43).

## Зависимости

`domain` содержит чистые события, роли, visibility, action/content и правила ограниченных batch params. Он не содержит хранимый JSON envelope и не зависит от common/Gin/GORM. Read DTO живут в `dto`; хранимые record/tags и raw-preserving metadata codec — в `internal/adapter/gorm/audit/record`. `model/audit_records.go` содержит только табличный alias AuditLog; migration helper остаётся в отдельном model файле с прежним DDL.

Модуль использует только собственный domain; зависимости на domain других бизнес-модулей нет. Read-сценарии объявляют узкий Reader.List, record_event — Store.Append/Username и Diagnostics.InvalidRole. GORM adapter реализует чтение/запись и username lookup через инжектированный callback, app — диагностику через logger. Transport получает Execute/error callbacks, часы и генератор ID; зависимости собирает internal/app/audit.

`transport.Recording` захватывает plain request facts и ограниченный response buffer; PAT actor/time/event ID и actor API-key операции фиксируются до handler. В usecase нет Begin/Complete или Stage. Предварительные решения — named domain functions IsWriteMethod/TokenAction. `internal/app/audit` собирает зависимости без middleware/parent app; app/audit.Recording держит единую once-сборку с поздним чтением DB; callbacks подключает app/httpapi.

Storage всегда использует LOG_DB. Count и Find остаются отдельными запросами; сортировка created_at DESC, event_id DESC, прежние offset/limit и ClickHouse settings. Переданный visibility policy определяет SQL actor roles и DTO projection. SQLite/MySQL/PostgreSQL/ClickHouse ветви, таблица, индексы, JSON порядок, raw integer precision и null/empty форма сохранены.

ClickHouse insert row encoding принадлежит record.InsertRow и используется production Store.Append и общей test fixture; Native insert обходит Valuer, поэтому JSON передаётся явно.

## Границы и долг

Usage logs, PAT lifecycle, аутентификация, финансы и подписки остаются у своих владельцев. Аудит фиксирует результат, не меняя их workflow. Для subscription reset domain operator передаётся напрямую; quota recipient usage writer получает operator envelope своего record owner.

PAT last-use reader остаётся в PAT adapter с прежним read-only SQL (#44); двойная PAT validation и race отзыва — отдельный долг #25. Нет audit → PAT зависимости.

Новых исключений архитектурного теста нет: model/audit_records.go разрешён общим строгим AST-правилом alias-only файлов, allowlist не расширялся. Migration helper остаётся техническим остатком с прежним DDL. Best-effort запись без атомарности/repair — #42; Count/Find без общей snapshot transaction — #43.

HTTP и middleware используют один app/audit.Recording с поздними storage readers.
Callback-сборка login/security/manage/quota находится в app/httpapi.
Quota recipient сохраняет прежний usage/top-up writer, operation content и operator envelope.
Controller содержит однострочные router bindings; временных audit callbacks в нём нет.
Middleware сохраняет прежние begin/finish и defer-порядок записи integration completion.

Аудит исключает usable credentials, codes, keys и сырые bodies/query; запись остаётся best-effort.
