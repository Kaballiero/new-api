# Task plugins

Модуль владеет версиями task-плагинов, управлением активным override, синхронизацией runtime,
доступностью factory fallback, marketplace sources и списком плагинов для привязки каналов.

Management HTTP-сценарии находятся в одноимённых пакетах. Сборка — `internal/app/taskplugins`;
GORM record и запросы — `internal/adapter/gorm/taskplugins`; registry, compiler и настройки —
`internal/adapter/taskplugins`. `model.TaskPlugin` — только alias технического record, поэтому
AutoMigrate и прежнее имя таблицы сохраняются. Единственная registry остаётся `pkg/jsplugin.DefaultRegistry`.

API `/api/plugin/task` сохраняет прежнюю root/PAT-защиту router. Read-only versions и marketplace
sources используют injected reader без пустого usecase; остальные handlers вызывают сценарии
с проверками, выбором версий, fallback, сортировкой и последовательностью побочных эффектов.
`GET /api/task_plugin_options` сохраняет собственную прежнюю цепочку middleware.

Один sync pass выполняет `sync_task_plugins.Execute(ctx)`, общий mutex/hash/error/rebuild state
создаётся один раз при сборке. `StartSync` выполняет первый pass немедленно, затем каждые 30 секунд;
master/follower gating не добавляется. SQL, транзакции и порядок публикации generation не менялись.

`adapter/taskplugins/aliases.View` — единственный executable alias view; channels adapter создаёт
его один раз с собственным SQL reader. Pricing, gateway и channel-cache lifecycle используют этот
же view: прежние generation/TTL, ASCII folding, ambiguity/drop policy и rebuild timing.

Provider execution и wire protocol принадлежат шлюзу;
денежная политика и durable Task принадлежат billing/tasks. Общие option writes подключены через app к settings; собственные taskplugins guards и RAM-before-persist порядок сохранены. Общий publisher/lock принадлежит adapter/settings/publication.

Контрактные эталоны в `contract/testdata` записаны на origin до переноса; они неизменяемы.
Результаты sandbox не заменяют текущую host/PG-проверку; MySQL недоступен и не проверен.


## Возможности и доступ

| Сценарий | API / background | Доступ |
|---|---|---|
| list_task_plugins / upload_task_plugin | GET / POST / PUT `/api/plugin/task` | прежний RootAuth (session/PAT) |
| get_task_plugin / fetch_task_plugin_icon / list_task_plugin_versions | GET `/:key`, `/:key/icon`, `/:key/versions` | RootAuth |
| activate_task_plugin / set_task_plugin_status / delete_task_plugin_version | POST `/:key/activate`, POST `/:key/status`, DELETE `/:key/versions/:version` | RootAuth |
| dryrun_task_plugin / get_task_plugin_runtime | POST `/:key/dryrun`, GET `/runtime/status` | RootAuth |
| get_marketplace_sources / update_marketplace_sources | GET / PUT `/marketplace/sources` | RootAuth |
| list_bindable_task_plugins | GET `/api/task_plugin_options` | AdminAuth + TaskPluginBind |
| sync_task_plugins | startup + tick 30s, без master gate | app.StartSync |
| resolve_plugin_origins / query_plugin_tasks | pinned native route / task endpoint | прежняя gateway token chain, ownership/platform scope |
| observe_plugin_task / retrieve_plugin_response | host Responses bridge / GET `/v1/responses/:response_id` | прежняя gateway token chain |

## Зависимости, gateway и мосты

`tasks/domain` предоставляет plain Task и ProtocolSubmission. Technical TaskReader получает store
в конструкторе через app; channels/options остаются у прежних владельцев и читаются поздно.
Registry handles непрозрачны для сценариев: интерфейс Compiled отдаёт только display metadata,
VM/relay DTO в domain не попадают. JSON-поля nested metadata сохраняют raw bytes/field order;
record и domain не заменяют provider API. Типы metadata — read-only факты management, не второй registry.

`observe_plugin_task` владеет admission, detached bounded submission, проверкой durable owner/platform,
выбором background/stream/sync, deadline/load/backoff и порядком reload. Session port реализован gateway;
он владеет pinned generation, hook context, wire machine, protocol state, SSE/heartbeat и представлением
ошибок. Submit capability — метод Session, передаваемой каждому вызову stateless observer; общий limiter собран once. `TestObserverRequestIsolation` проверяет, что
отменённый observer и следующий запрос не разделяют session/outcome/admission.
`retrieve_plugin_response` выбирает pending/failure/final/events после owner lookup и protocol claim.

Оставшиеся bridges:

- controller/task_plugin.go: однострочные management delegates для router; удалить после перехода
  registrations на transport. Handler создаётся once, а не при запросе.
- controller/relay.go: private submission → plain task-domain boundary, пока ordinary relay consumer
  использует private outcome; durable insert/reservation bridge остаётся tasks store consumer.
- relay/relay_task.go и middleware/distributor.go: ordinary task origin/fetch/model authorization consumers
  tasks store; не являются копией taskplugins origin/query policy.

Middleware origin/query BRIDGE bodies удалены. Новых architecture allowlist entries нет.
TECH_DEBT80–82 сохраняют first-active concurrency, partial lifecycle publication и background whole-data
write без CAS; они не исправлялись. Mutable management/storage/limiter/projector tests перемещены
к владельцам. Immutable contract assertions сравнивают прежние bytes, новых golden updates нет.
