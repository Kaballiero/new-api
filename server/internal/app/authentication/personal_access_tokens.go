package authentication

import (
	adapter "github.com/QuantumNous/new-api/internal/adapter/gorm/personal_access_tokens"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/validate_token"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

func NewPATAuth() validate_token.Usecase {
	return validate_token.NewUsecase(adapter.NewAuthentication(func() *gorm.DB { return model.DB }))
}
