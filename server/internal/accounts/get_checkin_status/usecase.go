package get_checkin_status

import (
	"time"

	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

type Reader interface {
	Records(userID int, start, end string) ([]domain.Checkin, error)
	CheckedInOn(userID int, date string) (bool, error)
	Totals(userID int) (int64, int64)
}
type Usecase struct{ reader Reader }

func NewUsecase(reader Reader) Usecase { return Usecase{reader: reader} }
func (u Usecase) HasCheckedInToday(userID int) (bool, error) {
	return u.reader.CheckedInOn(userID, time.Now().Format("2006-01-02"))
}
func (u Usecase) Execute(userID int, month string) (map[string]any, error) {
	records, err := u.reader.Records(userID, month+"-01", month+"-31")
	if err != nil {
		return nil, err
	}
	values := make([]domain.CheckinRecord, len(records))
	for i, record := range records {
		values[i] = domain.CheckinRecord{CheckinDate: record.CheckinDate, QuotaAwarded: record.QuotaAwarded}
	}
	checked, _ := u.HasCheckedInToday(userID)
	count, quota := u.reader.Totals(userID)
	return map[string]any{"total_quota": quota, "total_checkins": count, "checkin_count": len(records), "checked_in_today": checked, "records": values}, nil
}
