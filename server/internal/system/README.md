# Система

## Ответственность

Модуль владеет первоначальной настройкой, системными задачами и их lease/history/progress,
сведениями об инстансах и обслуживанием runtime. Settings владеет options, accounts — root row,
usage — строками логов. System координирует эти операции через узкие порты.

## Возможности

| Сценарии | Вход и доступ |
|---|---|
| get_setup / complete_setup | GET/POST `/api/setup`, public; POST ограничен AnonymousRequestBodyLimit |
| check_setup | startup после main DB/password init, до settings/log DB/Redis |
| test_database_health | GET `/api/status/test`, AdminAuth, session/PAT; cached main DB ping и shared HTTP counters |
| list_system_instances | GET `/api/system-info/instances`, RootAuth, session/PAT; last_seen DESC и stale projection |
| sweep_stale_instances / delete_system_instance | DELETE `/api/system-info/stale-instances`, `/api/system-info/instances/:node_name`, RootAuth |
| report_system_instance | immediate heartbeat и каждые 30s на каждом узле; once |
| list_system_tasks / get_current_system_task / get_system_task | GET `/api/system-task/list`, `/current`, `/:task_id`, RootAuth |
| enqueue_log_cleanup | POST `/api/system-task/log-cleanup`, RootAuth; active reuse/create/wakeup |
| enqueue_system_task | внутреннее enqueue с dedup, без проверки registration/type |
| schedule_system_tasks / claim_system_tasks | один pass scheduler/claim; интервалы и dispatch принадлежат app |
| purge_old_logs | один run, recount/progress, удаление в LOG_DB и terminal state в main DB |
| get_performance_stats / clear_disk_cache / reset_performance_stats / force_gc | RootAuth: GET `/api/performance/stats`, DELETE `/disk_cache`, POST `/reset_stats`, `/gc` |
| list_log_files / delete_log_files | GET/DELETE `/api/performance/logs`, RootAuth; filename/day retention и active-file exclusion |
| get_perf_metrics / get_perf_metrics_summary | GET `/api/perf-metrics`, `/summary`; HeaderNavModulePublicOrUserAuth(pricing) |
| get_uptime_status | GET `/api/uptime/status`, public; configured groups, parallel status/heartbeat reads |

Прямые чтения и технические действия используют HTTP handler с injected reader/effect без пустого usecase.
Setup выполняет guard → root probe → bind → validation/hash/root insert → RAM modes → два option writes →
setup flag → setup row. Общей transaction и request mutex нет; SQL lookup errors трактуются как absence.
Root insert пропускается при существующем root, session/login setup не создаёт.

## Зависимости и сборка

`app/system` собирает единственную registry, store, enqueue/cleanup/scheduler/claim и runner. Runner
once включает master check: первый follower call расходует once. Первый pass немедленный,
idle/wakeup 15s, scheduler 15s, stale sweep 30s, lease TTL 60s, heartbeat 20s. Каждый type dispatch
идёт отдельно, Run сам завершает task. Progress writes сохраняют throttle 2s и first/final updates.

`app/systemruntime` собирает HTTP/setup/reporter и handlers восьми task types. Существующий
`controller.RegisterScheduledSystemTasks` явно регистрирует все handlers до запуска runner;
повторная регистрация заменяет type. FX регистрируется явно вместе с остальными handlers.
Channels получают ChannelEffects и отдельную сборку stateful store на каждый run;
Midjourney/async tasks, Batch и pricing вызывают своих владельцев.
Batch HTTP и scheduled polling используют общую `app/batchruntime.Default` сборку. Финансовые решения не принадлежат system.

- `adapter/gorm/system` реализует task CAS/lease/JSON codecs, setup и instance storage, cached health.
  Records/hooks/tags задают хранение; `model` содержит только technical aliases для migrations.
- `adapter/gorm/accounts.BootstrapRoot` выполняет прямой root INSERT и account defaults.
- `app/settings.Default().Write` — два последовательных setup writes с legacy SQL suppression.
- `adapter/gorm/usage.Cleanup` работает только с поздним LOG_DB и сохраняет ClickHouse branch/LIMIT semantics.
- `adapter/runtime/system` читает OS/cache/files/config facts и technical metrics owner. GetStatus в settings
  получает version/start/setup через late facts, без I/O.
- `adapter/http/system` создаёт request-scoped Uptime client, timeouts, redirects, два параллельных GET,
  закрытие body и JSON decode. Ошибка одной пары даёт пустую группу при success HTTP envelope.

Domain/usecases не зависят от Gin, GORM, common, model, service или AI gateway. Между модулями
передаются plain facts и узкие capabilities, без JSON round trips для копирования данных.

## Границы и долг

- `controller` delegates нужны существующим router registrations; удалить, когда router собирает handlers напрямую.
  `RegisterScheduledSystemTasks` остаётся мостом для main/scheduler entrypoints до прямой передачи ChannelEffects startup owner.
- `model/setup.go`, `system_task.go`, `system_instance.go`: aliases нужны migrations и legacy record consumers;
  удалить после их перехода на system record. Исполняемых model/service → internal мостов нет.
- `app/systemmetrics` содержит единственный core чтения/записи и sleep-first flush loop. Aggregation
  counters/state реализованы `adapter/runtime/systemmetrics`, SQL — `adapter/gorm/systemmetrics`,
  Redis write-only projection — `adapter/redis/systemmetrics`. Init запускает цикл при каждом вызове;
  interval/enabled/retention читаются поздно, без нового once/master gate. Redis не участвует в Query.
  Ошибка SQL flush восстанавливает drained counters; прежние окна согласованности сохраняются.
- `gateway/metrics` преобразует RelayInfo в plain system Sample. Prometheus channel collector
  находится в `adapter/runtime/system`, его isolated registry собирает leaf `app/systemmetrics`.
- FX refresh принадлежит `pricing/refresh_cost_fx`, replica reload — `app/pricingfx` на каждом узле.
  System task вызывает собранный scenario; master gating остаётся прежним runner policy.
- Deployments, catalog leftovers и user notifications находятся вне модуля. Common disk/cache/monitor singleton
  остаётся техническим владельцем; второго state/cache/counter нет.
- Известные setup partial states, Uptime destination/body policy, metrics concurrency и order noise — TECH_DEBT #88–91.
  Cleanup main/log writes не atomic; реальная PostgreSQL/MySQL/ClickHouse проверка отличается от SQLite contracts.
