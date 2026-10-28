# Gateway mapping

Шлюзовой владелец request/response/stream conversion diagnostics, upstream error/status mapping, task model naming.

Протокольные conversions выполняет прежний relaykit/relayconvert. Бывшие чистые service wrappers удалены: callers используют codec напрямую. Mapping сохраняет host RelayInfo diagnostics и при initialization подключает прежний pkg/media loader к MediaResolver; регистрация работает при прямом импорте provider adapter, независимо от main.

Gateway helpers не импортируют корневой relay или gateway/transport. Error texts и conversion diagnostics сохранены. HTTP response headers/body/status и закрытие response body принадлежат pkg/httpclient. Финансовые сценарии и durable task lifecycle принадлежат internal. Violation marker/code normalization находится здесь, charge policy — billing/charge_violation_fee; service facade/registry удалены.

Недостижимые legacy host helpers и чистые codec wrappers удалены; reachable conversion APIs и MediaResolver callbacks сохранены.

FX admission error/status wire facts принадлежат gateway mapping; relay/billing вызывает прежние billing FX scenarios напрямую. Service FX facades и BillingGatewayPort удалены. Экономика и frozen-basis policy остаются в billing.
