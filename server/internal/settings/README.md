# Настройки

## Ответственность

Модуль владеет общим чтением, записью и перечитыванием options, root-редактором и публичной
проекцией конфигурации. Значения хранятся в таблице `options`; `setting/*` предоставляет технические
runtime-настройки. Pricing, payments и taskplugins сохраняют собственные бизнес-правила.

## Возможности

| Сценарий | Действие и доступ |
|---|---|
| get_options | GET `/api/option/`, RootAuth, session/PAT; список с исключением секретов и effective billing maps |
| update_option | PUT `/api/option/`, RootAuth, session/PAT; coercion, guards и audit изменённого key |
| write_option | внутренняя запись одного option с выбором legacy, batch или pricing пути |
| write_option_batch | aliases conflict/expansion, validation, sorted atomic DB commit, затем публикация |
| sync_options | один reload с canonical alias precedence; ошибки отдельных значений не останавливают публикацию остальных |
| get_status | публичный GET `/api/status`, aggregate projection, optional panels и OAuth metadata |
| get_notice / get_about / get_home_page_content | публичные GET `/api/notice`, `/api/about`, `/api/home_page_content` |
| get_user_agreement / get_privacy_policy | публичные GET `/api/user-agreement`, `/api/privacy-policy` |

`update_option` проверяет ввод HTTP-редактора и передаёт запись в `write_option`, который выбирает путь сохранения для всех writers.

Content handlers используют late readers без пустых usecases. Общий transport задаёт weak ETag,
Cache-Control, Vary и ответ 304. GET редактора исключает записи по case-sensitive secret suffixes,
добавляет effective billing maps и CompletionRatioMeta; список не сортируется. Ratio validators
могут изменить RAM до сохранения; audit содержит только key.

## Зависимости и сборка

`internal/app/settings` собирает usecases и handlers once, передавая поздние DB/runtime readers.
Чужие business domain модуль не импортирует. Внешние writers подключаются в app через узкие порты:

- Payments: `app/payments.options.Set` вызывает `Default().Write`, `Bulk` вызывает `Default().Bulk`;
  их используют compliance confirmation и сохранение Pancake config.
- Pricing: reset model ratios использует `Default().Write`; pricing transaction и catalog publication
  остаются у прежнего владельца.
- Taskplugins: `app/taskplugins` передаёт `Default().Write` в `Settings.WriteOption` для marketplace
  sources и disabled factory keys; registry update и RAM-before-persist принадлежат taskplugins.
- Setup: `app/systemruntime` записывает SelfUseModeEnabled и DemoSiteEnabled через `Default().Write`.

Порты Store сценариев реализует `adapter/gorm/settings`: Option record, чтение всех строк,
legacy FirstOrCreate/Save и checked batch transaction. Single path игнорирует SQL errors
(TECH_DEBT #58); batch публикует значения после commit. `model.Option` — alias технического record.
Единственные publisher/lock/alias facts принадлежат `adapter/settings/publication`; pricing writer и observation подключаются из leaf `app/pricingruntime`. `adapter/settings/settings` реализует
defaults, codecs, validators, editor и late status readers; `adapter/gateway/settings` читает FX status.
`adapter/runtime/system.Facts` даёт late version/start/setup для GetStatus без I/O.

`Initialize` сбрасывает defaults из текущих globals и namespace exports, затем выполняет reload.
`SyncOptions` на каждом узле сначала Sleep(frequency), затем log → reload → pricing observation;
ошибка reload не отменяет observation. Расписание и запуск принадлежат app.

## Границы и долг

- `controller/option.go` и delegates в `controller/misc.go` связывают существующие router registrations
  с settings handlers; удалить при сборке handlers непосредственно в router.
- `adapter/settings/publication` владеет общим group-ratio mutex, alias map и единственным `PublishOption`. Settings single/bulk/reload и native pricing используют этот же publisher. PricingCatalog публикуется до общего OptionMap lock, как и раньше.
- Leaf `app/pricingruntime` собирает pricing/options/catalog capabilities без импорта parent app/settings. Reset writer связан со settings во внешнем app; `SyncOptions` после reload вызывает общий observer.
- `model.Option` остаётся строгим record alias для bootstrap/migrations.
- Setup, health, uptime и performance принадлежат system; deployments, catalog и user notifications
  остаются за границей модуля. Direct runtime readers в `setting/*` сохранены.
- Legacy pricing composition exceptions удалены; новых исключений нет. TECH_DEBT #58 и #86–#87 остаются открытыми.
