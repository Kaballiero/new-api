// Package dbclock binds the database clock to the current main database.
package dbclock

import (
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/dbclock"
	"github.com/QuantumNous/new-api/model"
)

func Now() int64 { return storage.GetDBTimestamp(model.DB) }
