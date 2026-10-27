# Gateway routing

Владелец request channel constraints, retry state, pinned plugin channel filtering, channel affinity и Chat→Responses policy. Использует существующие model channel queries, jsplugin generation и settings; расположение relay adapters не меняется.

Affinity cache и usage-stat cache остаются единственными state с прежними namespace/keys/TTL/once/locks. Regex cache, override templates и context keys сохраняются.

Оставлен consumer-owned BRIDGE в channel_select.go: GetRequestAutoGroups читает прежнюю service group policy. Эта policy и её settings adapter переносятся в отдельном шаге channels/pricing/settings composition; после этого routing читает реального владельца, и зависимость от service удаляется. Нового service facade/registry нет.
