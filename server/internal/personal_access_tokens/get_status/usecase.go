package get_status

import "github.com/QuantumNous/new-api/internal/personal_access_tokens/domain"

type Reader interface {
	Account(int) (domain.Account, error)
	LatestUsage(int, string, []int) (domain.Usage, error)
	IsMissing(error) bool
}
type Usecase struct {
	reader      Reader
	fingerprint func(string) string
	roles       domain.AuditRoles
}

func NewUsecase(reader Reader, fingerprint func(string) string, roles domain.AuditRoles) Usecase {
	return Usecase{reader: reader, fingerprint: fingerprint, roles: roles}
}
func (usecase Usecase) Execute(input Input) (*Output, error) {
	user, err := usecase.reader.Account(input.UserID)
	if err != nil {
		return nil, err
	}
	status := &Output{Exists: user.Token != ""}
	if !status.Exists {
		return status, nil
	}
	status.TokenRef = usecase.fingerprint(user.Token)
	status.CreatedAt = user.CreatedAt
	var roles []int
	if user.Role < usecase.roles.Root {
		roles = []int{usecase.roles.Common, usecase.roles.Admin}
	}
	latest, err := usecase.reader.LatestUsage(input.UserID, status.TokenRef, roles)
	if usecase.reader.IsMissing(err) {
		return status, nil
	}
	if err != nil {
		return nil, err
	}
	status.LastUsedAt = &latest.CreatedAt
	status.LastUsedIp = latest.Ip
	return status, nil
}
