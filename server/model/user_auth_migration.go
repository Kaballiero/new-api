package model

// InitializeUserAuthVersions runs after AutoMigrate and retains the existing backfill order.
func InitializeUserAuthVersions() error {
	return DB.Model(&User{}).Where("auth_version IS NULL OR auth_version < ?", 1).Update("auth_version", 1).Error
}
