package tasks

import (
	taskstore "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks"
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

// NewStore wires task persistence to the current primary database in every binary.
func NewStore() taskstore.Store {
	return taskstore.NewStore(func() *gorm.DB { return model.DB }, accountstorage.Cache())
}
