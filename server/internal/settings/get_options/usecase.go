package get_options

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/settings/domain"
)

type Reader interface {
	Lock() func()
	Values() map[string]string
	EffectiveBilling() ([]domain.Option, error)
	CompletionMeta(map[string]string) string
}
type Usecase struct{ reader Reader }

func NewUsecase(reader Reader) Usecase { return Usecase{reader} }
func (u Usecase) Execute() ([]domain.Option, error) {
	var options []domain.Option
	values := make(map[string]string)
	unlock := u.reader.Lock()
	for key, value := range u.reader.Values() {
		if key == "theme.frontend" || key == "billing_setting.billing_mode" || key == "billing_setting.billing_expr" {
			continue
		}
		sensitive := strings.HasSuffix(key, "Token") || strings.HasSuffix(key, "Secret") ||
			strings.HasSuffix(key, "Key") || strings.HasSuffix(key, "secret") || strings.HasSuffix(key, "api_key")
		if sensitive {
			continue
		}
		options = append(options, domain.Option{Key: key, Value: value})
		values[key] = value
	}
	unlock()
	effective, err := u.reader.EffectiveBilling()
	if err != nil {
		return nil, err
	}
	options = append(options, effective...)
	options = append(options, domain.Option{Key: "CompletionRatioMeta", Value: u.reader.CompletionMeta(values)})
	return options, nil
}
