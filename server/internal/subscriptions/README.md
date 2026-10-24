# Подписки

Сейчас перенесены GORM records и общие типы планов в `adapter/gorm/subscriptions/record` и `domain`.
Планы, orders, user subscriptions и preconsume ledger сохраняют прежние tags/hooks/таблицы.
Legacy workflow пока остаётся в controller/model/service и переносится следующими slices.

Model aliases сохраняют существующие migration/relay/task/test consumers; нормализация nullable flags
при чтении имеет единственного владельца domain, record делегирует ему для прежнего cache reader.
HTTP маршруты, schema, запросы, финансовые операции и тексты на этом foundation шаге не менялись.

Чтение plans/admin plans, self/all/active, admin user subscriptions и preference теперь принадлежит
сценариям модуля; SQL — gorm/subscriptions, сборка — app/subscriptions, controller — singleton bridge.
Self сохраняет независимые reads и подавление отдельных ошибок в defaults/empty lists.

Create/update/status планов принадлежат сценариям модуля. Validation/default/group/reset normalization
общие в domain; late group/config readers инжектируются. Update сохраняет transaction/zero values/
optional pointers и успех при отсутствии row; cache invalidation после write, без schema изменений.
