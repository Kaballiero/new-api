package getapi_logs

import "github.com/QuantumNous/new-api/internal/usage/domain"

type Reader interface {
	ReadGetAPIUsageLogs(GetAPIUsageLogsRequest, domain.VisibilityPolicy) (GetAPIUsageLogsResult, error)
}

type Usecase struct{ reader Reader }

func NewUsecase(reader Reader) Usecase { return Usecase{reader: reader} }

func (u Usecase) Execute(q GetAPIUsageLogsRequest) (GetAPIUsageLogsResult, error) {
	if err := domain.ValidateGetAPIUsageWindow(q.StartTimestamp, q.EndTimestamp); err != nil {
		return GetAPIUsageLogsResult{}, err
	}
	if q.UserID <= 0 || q.Limit < 1 || q.Limit > 100 ||
		q.TokenID != nil && *q.TokenID < 0 ||
		q.BeforeID != nil && *q.BeforeID <= 0 || q.AfterID != nil && *q.AfterID <= 0 ||
		q.BeforeID != nil && q.AfterID != nil {
		return GetAPIUsageLogsResult{}, domain.ErrGetAPIUsageInvalidQuery
	}
	if q.Types == nil {
		q.Types = []int{domain.LogTypeConsume, domain.LogTypeRefund}
	}
	if len(q.Types) == 0 {
		return GetAPIUsageLogsResult{}, domain.ErrGetAPIUsageInvalidQuery
	}
	for _, kind := range q.Types {
		if kind < domain.LogTypeTopup || kind > domain.LogTypeLogin {
			return GetAPIUsageLogsResult{}, domain.ErrGetAPIUsageInvalidQuery
		}
	}
	policy := domain.LogVisibility(domain.AudienceUser)
	policy.ReplaceDisplayIDs = false
	return u.reader.ReadGetAPIUsageLogs(q, policy)
}
