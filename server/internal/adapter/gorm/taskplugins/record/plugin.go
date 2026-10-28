package record

type TaskPlugin struct {
	Id         int64  `json:"id"`
	Key        string `json:"key" gorm:"size:128;not null;uniqueIndex:uk_task_plugin_key_version,priority:1"`
	APIVersion int    `json:"api_version" gorm:"not null"`
	Version    string `json:"version" gorm:"size:64;not null;uniqueIndex:uk_task_plugin_key_version,priority:2"`
	Source     string `json:"source" gorm:"type:text;not null"`
	SourceHash string `json:"source_hash" gorm:"size:64;not null"`
	// Icon is the plugin logo shipped as a sidecar icon.svg / icon.png next to
	// plugin.js, stored as a data URI so one column carries both the media
	// type and the bytes. It never travels inside list or detail JSON; the UI
	// loads it through GET /api/plugin/task/:key/icon. size matches the
	// 512 KiB icon cap and makes GORM emit mediumtext on MySQL (a bare TEXT
	// column there holds only 64 KiB), varchar(524288) on PostgreSQL, and text
	// on SQLite.
	Icon      string `json:"-" gorm:"size:524288"`
	Enabled   bool   `json:"enabled" gorm:"not null"`
	Active    bool   `json:"active" gorm:"not null;index"`
	CreatedAt int64  `json:"created_at" gorm:"not null"`
	Remark    string `json:"remark" gorm:"type:text"`
}
