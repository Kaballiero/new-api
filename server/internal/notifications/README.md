# Notifications — доставка уведомлений

## Ответственность

Модуль владеет выбором способа доставки, лимитом по пользователю и событию, уведомлениями root и подписавшихся администраторов об изменении upstream-моделей. `domain.Notice` содержит уже подготовленные title/content/values, `Preferences` — факты назначения. Аккаунты, язык billing-шаблона и решение о запуске billing notification принадлежат своим модулям.

## Возможности

| Сценарий | Поведение | Вызов |
|---|---|---|
| `send_notification` | Пустой канал → email; лимит события → выбор адреса → email/webhook/Bark/Gotify | Billing, root, watchers; отдельного HTTP нет |
| `notify_root_user` | Читает root и его настройки, передаёт готовый текст общей доставке, логирует ошибку | Изменение статуса и проверка каналов |
| `notify_model_update_watchers` | Читает enabled admin/root, последовательно декодирует предпочтения и доставляет opted-in пользователям; ошибка одного не останавливает остальных | Завершение upstream model scan |

## Зависимости

`app/notifications` один раз собирает сценарии и общую delivery capability; не зависит от parent app, billing или channelruntime. Получатели — `adapter/gorm/accounts.NotificationRecipients`: прежние users, SQL filters без дополнительной сортировки, декодирование настроек в момент обработки получателя. Общий порт доставки передаётся обоим fan-out сценариям без импорта соседнего сценария.

`adapter/http/notifications` сериализует webhook, Bark и Gotify; protected HTTP client, URL validator и worker request передаются из app и сохраняют единственные реализации в service. SMTP остаётся `common.SendEmail`. Webhook сохраняет прежний renderer, HMAC, timestamp и отдельные values; worker добавляет Bearer, direct — только подпись. Gotify сохраняет различие User-Agent direct/worker и явный priority=0.

Лимитер читает RedisEnabled и настройки поздно. Redis adapter сохраняет GET/SET/INCR/TTL, RAM state и lazy cleanup принадлежат `adapter/notifications.MemoryCounter`; `app/notifications` только выбирает RAM/Redis counter. Cleanup делает sleep перед часовым проходом и запускается при первом memory access. Смена алгоритма на атомарный или устранение счётчика rejected-вызовов не входят в модульный перенос.

## Границы и долг

`app/channelruntime` собирает существующие enable/disable scenarios channels и root notifier, выбирает язык root и тип события; relay передаёт plain input. Billing adapter сохраняет свои шаблоны и получает `notifications.Send` через `app/billing`. Новых service фасадов нет. Общие download/HTTP/worker primitives, account security mail и channel failure policy остаются вне notifications.

Отсутствующее назначение и неизвестный канал по-прежнему расходуют лимит и возвращают nil. Ошибки fan-out логируются; sent-count включает nil skip. Сохраняются неатомарные counters и webhook placeholder bug (`TECH_DEBT.md` #29, #92); политики повторов и исправление renderer требуют отдельного решения.
