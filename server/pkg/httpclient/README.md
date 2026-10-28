# Исходящие HTTP-клиенты

Технический владелец общих и proxy HTTP-пулов, transport policy и HTTP/2 shards, protected fetch и worker downloads. Relay/provider adapters, notifications, pricing acquisition и task media используют один cache клиентов.

`InitHttpClient` вызывается в прежней точке startup. Default client pointer стабилен после initialization; reset закрывает idle connections и заново регистрирует тот же client. Proxy identity, policy keys, timeout/TLS/HTTP1/HTTP2, redirect checks и позднее чтение fetch settings сохранены. Protected fetch проверяет URL и resolved IP; операторские provider endpoints используют общий client.

Пакет использует существующие common/settings/logger и ChannelSettings wire type; бизнес-сценарии и app не импортирует. HTTP response helpers здесь же сохраняют client projection, headers/body/status и закрытие response body через прежние Gin/common функции. Реальные HTTP2/proxy connections проверяются host transport tests.
