package list_codes

import (
	"github.com/QuantumNous/new-api/internal/redemption/domain"
)

// Reader runs Count and Find in the same read transaction, including legacy panic recovery.
type Reader interface {
	Read(run func(reader domain.PageReader) error) error
}

type Input struct {
	Start, Limit int
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}

// Execute owns the Count/Find transaction boundary, including the legacy partial result on a recovered panic.
func (u Usecase) Execute(in Input) (domain.Page, error) {
	var page domain.Page
	err := u.reader.Read(func(reader domain.PageReader) error {
		query := domain.Query{}

		if err := reader.Count(query, &page.Total); err != nil {
			return err
		}
		return reader.Find(query, in.Start, in.Limit, &page.Codes)
	})
	if err != nil {
		return domain.Page{}, err
	}
	return page, nil
}
