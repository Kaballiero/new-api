package list_own_topups

import (
	apikeys "github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type Input struct {
	UserID, Start, Limit int
	Keyword              string
}

type Usecase struct {
	reader domain.Reader
	now    func() int64
}

func NewUsecase(reader domain.Reader, now func() int64) Usecase {
	return Usecase{reader: reader, now: now}
}

// Execute selects the legacy owner/window/pattern policy inside the read transaction.
func (u Usecase) Execute(in Input) (domain.Page, error) {
	var page domain.Page
	err := u.reader.Read(func(reader domain.PageReader) error {
		query := domain.Query{}
		query.UserID = &in.UserID
		query.Cutoff = u.now() - 30*24*60*60
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
