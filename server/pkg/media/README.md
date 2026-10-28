# Media и файлы

Технический владелец загрузки и декодирования audio/image/file, MIME/HEIF inspection и request-local file cache. Gateway/tokenusage считает токены поверх этих данных; gateway/mapping подключает этот же loader к существующему relaykit MediaResolver.

URL и Base64 cache keys, source locks, cleanup registration, disk threshold/counters и fallback в memory сохранены. HTTP downloads выполняет pkg/httpclient; пакет не импортирует app или feature adapters. Бизнес-решения о задачах и хранение generated artifacts ему не принадлежат.
