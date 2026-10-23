package admin_list_topups

import (
	apikeys "github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type Input struct {
	Start, Limit int
	Keyword      string
}

type Usecase struct {
	reader domain.Reader
}

func NewUsecase(reader domain.Reader) Usecase {
	return Usecase{reader: reader}
}

// Execute selects the legacy owner/window/pattern policy inside the read transaction.
func (u Usecase) Execute(in Input) (domain.Page, error) {
	var page domain.Page
	err := u.reader.Read(func(reader domain.PageReader) error {
		query := domain.Query{}
		if in.Keyword != "" {
			pattern, err := apikeys.SanitizeSearchPattern(in.Keyword)
			if err != nil {
				return err
			}
			query.Pattern = pattern
			query.Search = true
			query.CountLimit = 10000
		}
		if err := reader.Count(query, &page.Total); err != nil {
			return err
		}
		return reader.Find(query, in.Start, in.Limit, &page.Orders)
	})
	if err != nil {
		return domain.Page{}, err
	}
	return page, nil
}
