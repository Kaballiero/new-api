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

Stripe: quote_stripe_topup, create_stripe_topup и receive_stripe_webhook. POST /api/user/stripe/amount
и /pay используют UserAuth session/PAT, pay — CriticalRateLimit; POST /api/stripe/webhook public
с исходной проверкой подписи SDK. Subscription completion/expiration вызывается первым; только
not-found разрешает top-up fallback. Verified webhook всегда получает прежний ACK 200 empty.

Creem: create_creem_topup (POST /api/user/creem/pay, UserAuth session/PAT + CriticalRateLimit) и
receive_creem_webhook (POST /api/creem/webhook, public HMAC). Local order создаётся до remote checkout.
Subscription-first dispatch предшествует onetime check. Автоматическая quota равна Amount;
manual completion сохраняет legacy Amount × QuotaPerUnit.
