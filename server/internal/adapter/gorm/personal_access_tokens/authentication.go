package personal_access_tokens

import (
	"errors"
	"fmt"

	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/domain"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type Authentication struct{ db func() *gorm.DB }

func NewAuthentication(db func() *gorm.DB) Authentication { return Authentication{db: db} }
func (s Authentication) Lookup(token string) (*domain.Caller, error) {
	user := &record.User{}
	err := s.db().Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}).Where("access_token = ?", token).First(user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("database error: %v", err)
	}
	return &domain.Caller{Id: user.Id, Username: user.Username, Role: user.Role, TokenRef: domain.Fingerprint(token)}, nil
}
