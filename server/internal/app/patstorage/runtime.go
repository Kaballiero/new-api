// Package patstorage assembles the shared personal access token persistence.
package patstorage

import (
	"errors"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/transport"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/personal_access_tokens"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

var shared = sync.OnceValue(func() repository.Store {
	return repository.NewStore(func() *gorm.DB { return model.DB }, func() *gorm.DB { return model.LOG_DB.Model(&model.AuditLog{}) }, locking.LockForUpdate, common.GetTimestamp)
})

func Persistence() repository.Store { return shared() }
func GetAPIErrors() transport.GetAPIErrors {
	return transport.GetAPIErrors{IsMissing: func(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }}
}
