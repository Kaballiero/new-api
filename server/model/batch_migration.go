package model

import "gorm.io/gorm"

// migrateBatchLogScope explicitly removes the deployed item-only NOT NULL
// constraint. Some drivers do not relax it during AutoMigrate. Re-running the
// model migration restores indexes after SQLite's required table rebuild.
func migrateBatchLogScope(db *gorm.DB) error {
	columns, err := db.Migrator().ColumnTypes(&BatchLogEvent{})
	if err != nil {
		return err
	}
	for _, column := range columns {
		if column.Name() != "item_id" {
			continue
		}
		nullable, known := column.Nullable()
		if known && !nullable {
			if err := db.Migrator().AlterColumn(&BatchLogEvent{}, "ItemID"); err != nil {
				return err
			}
			return db.AutoMigrate(&BatchLogEvent{})
		}
	}
	return nil
}
