# Payments — платёжные заказы

Модуль владеет историей top-up, ручным завершением и dashboard info. Providers переносятся последующими
slices; subscription completion остаётся legacy. `list_own_topups` задаёт user ID и окно 30 дней,
`admin_list_topups` — all-time history; оба выбирают только keyword и pagination. Read transaction
и generic search failures сохранены. `complete_topup` сохраняет provider-specific quota и повторную receipt.

Сборка: app/payments, SQL: adapter/gorm/payments, общий credit: adapter/wallet. Данные и настройки читаются
поздно. Controller singleton и общая order admission используются также legacy subscription callbacks.
Доменные понятия не зависят от GORM/Gin; общий search pattern принадлежит apikeys/domain.

Маршруты: GET /api/user/topup/info и /self — UserAuth session/PAT; GET /api/user/topup и
POST /api/user/topup/complete — AdminAuth session/PAT. Новых фильтров и permissions нет.

Epay: quote_epay_topup (POST /api/user/amount), create_epay_topup (POST /api/user/pay), receive_epay_notify
(GET/POST /api/user/epay/notify). Quote/pay — UserAuth session/PAT, pay дополнительно CriticalRateLimit;
notify — public provider signature. Epay Purchase формирует подписанную форму, сетевого вызова нет.
