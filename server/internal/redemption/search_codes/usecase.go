package search_codes

import (
	"strconv"

	"github.com/QuantumNous/new-api/internal/redemption/domain"
)

// Reader runs filtered Count and Find in the same read transaction, including legacy panic recovery.
type Reader interface {
	Read(run func(reader domain.PageReader) error) error
}

type Input struct {
	Keyword, Status string
	Start, Limit    int
}

type Usecase struct {
	reader Reader
	// now captures the expiry cutoff inside the read transaction.
	now func() int64
}

func NewUsecase(reader Reader, now func() int64) Usecase {
	return Usecase{reader: reader, now: now}
}

// Execute owns the Count/Find transaction boundary, including the legacy partial result on a recovered panic.
func (u Usecase) Execute(in Input) (domain.Page, error) {
	var page domain.Page
	err := u.reader.Read(func(reader domain.PageReader) error {
		query := domain.Query{}
		if in.Keyword != "" {
			query.Prefix = in.Keyword
			if id, err := strconv.Atoi(in.Keyword); err == nil {
				query.ID = &id
			}
		}
		if in.Status != "" {
			query.Now = u.now()
			status := 0
			switch in.Status {
			case "expired":
				status = domain.StatusEnabled
				query.Expiry = domain.PastExpiry
			case strconv.Itoa(domain.StatusEnabled):
				status = domain.StatusEnabled
				query.Expiry = domain.ActiveExpiry
			case strconv.Itoa(domain.StatusDisabled):
				status = domain.StatusDisabled
			case strconv.Itoa(domain.StatusUsed):
				status = domain.StatusUsed
			}
			if status != 0 {
				query.Status = &status
			}
		}
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
