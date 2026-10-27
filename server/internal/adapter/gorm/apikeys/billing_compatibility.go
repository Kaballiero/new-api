package apikeys

import (
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
)

type CompatibilityReader struct {
	ReadToken     func(int) (*record.Token, error)
	ReadQuota     func(int, bool) (int, error)
	ReadUsedQuota func(int) (int, error)
}

func (reader CompatibilityReader) Token(id int) (*domain.BillingToken, error) {
	token, err := reader.ReadToken(id)
	if token == nil {
		return nil, err
	}
	return &domain.BillingToken{ExpiredTime: token.ExpiredTime, RemainQuota: token.RemainQuota, UsedQuota: token.UsedQuota, UnlimitedQuota: token.UnlimitedQuota}, err
}
func (reader CompatibilityReader) UserQuota(id int) (int, error) { return reader.ReadQuota(id, false) }
func (reader CompatibilityReader) UsedQuota(id int) (int, error) { return reader.ReadUsedQuota(id) }
