# Gateway transport

Входящий шлюзовой transport: Relay, Midjourney и Task handlers, plugin protocol/session,
Responses retrieve и playground. Router связывает handlers непосредственно с этим пакетом;
маршруты, middleware, permissions и wire protocol остаются у прежних владельцев.

Пакет владеет request/disconnect orchestration, bounded submission context, durable barrier,
SSE framing/flush order, WebSocket upgrade и presentation. Protocol state machine и provider
adapters остаются в relay; business observation/retrieve policies — в taskplugins. Прямой импорт
transport не требует main или mutable linking registry.

Task insert, owned origin lookup и fetch CAS доступны через узкие технические capabilities
app/tasks. Они сохраняют исходный record pointer и GORM hooks. Решение о подтверждении
reservation marker принадлежит tasks/confirm_submission_reservation; adapter удерживает исходную
запись и меняет ровно прежние flags перед status CAS. Insert → settlement → consumption log →
confirmation сохраняет прежние ошибки и durable/no-refund границу.

Общие error/violation mapping принадлежат gateway/mapping; быстрые pricing token facts —
gateway/tokenusage. Channel failure handling используется также adapter/gateway/channels:
выделение этой orchestration в нижний helper создало бы цикл через provider/billing imports.
Probe engine и его request builder находятся в adapter/gateway/channels; channels видит только
Probe/ProbeOutcome. HTTP delegates channel management остаются в controller.

Billing policy принадлежит internal/billing; transport вызывает relay/billing напрямую.
Service facades и BillingGateway registry отсутствуют. Нового состояния, retry policy,
paid replay или scheduler этот transport не вводит.

Маршрут /messages/count_tokens остаётся disabled; его прежняя characterization fixture находится
только в relay_count_tokens_test.go и проверяет validator/token counter, а не наличие HTTP endpoint.
