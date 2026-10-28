# HTTP composition

Пакет собирает HTTP handlers и межмодульные callbacks для dashboard и management API.
Каждый набор handlers создаётся один раз; настройки, DB и request context читаются
в прежних точках вызова. Controller содержит только однострочные router bindings
и wire-декларации для существующих потребителей OpenAPI.

Accounts получает audit, quota-recipient usage writer, payment compliance, GetAPI
error writer и sidebar defaults. Identity получает login audit, безопасный profile
projection и completion callbacks: primary login, Passkey с auth version и verified
login сохраняют прежние точки входа. Audit writer собирается единожды в app/audit.

Channels получает gateway probe/fetch, audit, security errors, refresh и system-task
enqueue capabilities. Состояние подавления повторных model-update notifications
принадлежит adapter/channels.ModelUpdateNotifications; HTTP и scheduler используют
один экземпляр. Request-local stores и usecases по-прежнему создаются своим app owner.

RegisterScheduledSystemTasks передаёт те же channel effects в app/systemruntime;
main вызывает его на каждом узле перед system runner; прежний master gate находится
внутри StartSystemTaskRunner.
AutomaticallyUpdateChannels сохраняет бесконечный sleep-first цикл в минутах,
позднее выполнение balance usecase и прежние logs, без нового master/once gate.

Тестовые convenience helpers находятся только в controller *_test.go.
Они вызывают настоящих владельцев; native channel fixtures передают fetch/throttle
через явный app.ChannelEffects, без изменяемого production hook.
Пакет не владеет бизнес-решениями, SQL, credential protocol или состоянием аудита.
