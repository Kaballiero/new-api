# Артефакты задач

Технический владелец filesystem backend generated task artifacts и capability URLs. Использует существующие model.Task facts, host types и system settings; бизнес-сценарии не создаёт. Tasks HTTP adapter и gateway callers используют тот же store.

Startup-configured store, disabled backend, private os.Root, immutable atomic Link publication, byte limits и bounded checksum verification до headers сохранены. InitTaskArtifactStore остаётся startup-only; init регистрирует тот же disabled/filesystem backend для любого импортирующего binary/test.

Capability HMAC остаётся привязан к task ID и artifact key, со strict Base64 decoding и constant-time comparison. URL builder использует TaskPublicAddress либо ServerAddress; request Host не участвует. Срок действия, выдача, access middleware и permission semantics не меняются. Ошибки и публичные URL bytes сохранены.
