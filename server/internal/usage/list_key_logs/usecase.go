package list_key_logs

import (
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/dto"
)

type Reader interface {
	ListKeyLogs(int, int, domain.VisibilityPolicy) ([]*dto.Log, error)
}

type Usecase struct {
	reader Reader
	limit  func() int
}

func NewUsecase(reader Reader, limit func() int) Usecase {
	return Usecase{reader: reader, limit: limit}
}

func (u Usecase) Execute(tokenID int) ([]*dto.Log, error) {
	return u.reader.ListKeyLogs(tokenID, u.limit(), domain.LogVisibility(domain.AudienceUser))
}
