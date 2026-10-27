# Фильтрация текста

Технические строковые helpers, Aho-Corasick cache и sensitive-word matching. Сохраняются dictionary normalization, cache keys, позднее чтение settings и существующая обработка Unicode/positions. Request handler вызывает text-only CheckSensitiveText. Недостижимые legacy helpers удалены; пакет не импортирует app, бизнес-адаптеры или relaykit DTO.
