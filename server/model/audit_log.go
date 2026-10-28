package model

import "github.com/QuantumNous/new-api/common"

// MigrateAuditLogs also supports independently configured ClickHouse log stores.
// No TTL clause or usage-log cleanup integration is intentional.
func MigrateAuditLogs() error {
	if !common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return LOG_DB.AutoMigrate(&AuditLog{})
	}
	return LOG_DB.Exec(`CREATE TABLE IF NOT EXISTS audit_logs (
		id Int64 DEFAULT 0, event_id String, user_id Int64, username String, actor_role Int32,
		created_at Int64, category String, action String, token_ref String,
		auth_method String, ip String, user_agent String, method String, route String,
		status Int32, success UInt8, request_id String, content String, other JSON
	) ENGINE = MergeTree()
	PARTITION BY toYYYYMM(toDateTime(created_at))
	ORDER BY (created_at, event_id)`).Error
}
