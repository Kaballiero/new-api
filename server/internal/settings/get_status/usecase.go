package get_status

import "github.com/QuantumNous/new-api/internal/settings/domain"

type Reader interface {
	ReadStatus() domain.PublicStatus
	CustomOAuthProviders() any
}
type Usecase struct{ reader Reader }

func NewUsecase(reader Reader) Usecase { return Usecase{reader} }

func (u Usecase) Execute() (map[string]any, error) {
	status := u.reader.ReadStatus()
	for _, panel := range status.Panels {
		if panel.Enabled {
			status.Values[panel.Key] = panel.Read()
		}
	}
	if providers := u.reader.CustomOAuthProviders(); providers != nil {
		status.Values["custom_oauth_providers"] = providers
	}
	return status.Values, nil
}
