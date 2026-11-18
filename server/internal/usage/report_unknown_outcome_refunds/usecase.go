package report_unknown_outcome_refunds

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/internal/usage/domain"
)

// RefundLogs tallies the unknown outcome refunds recorded in [from, to).
type RefundLogs interface {
	UnknownOutcomeRefunds(ctx context.Context, from, to int64) (domain.UnknownOutcomeRefunds, error)
}

// SentReports keeps the last reported day.
type SentReports interface {
	ReadOption(ctx context.Context, key string) (string, error)
	SaveBulk(ctx context.Context, keys []string, values map[string]string) error
}

// Alerts delivers the daily summary to the operators.
type Alerts interface {
	UnknownOutcomeRefunds(day string, refunds domain.UnknownOutcomeRefunds) error
}

type Usecase struct {
	logs   RefundLogs
	sent   SentReports
	alerts Alerts
	now    func() time.Time
}

func NewUsecase(logs RefundLogs, sent SentReports, alerts Alerts, now func() time.Time) Usecase {
	return Usecase{logs, sent, alerts, now}
}

// reportedDayKey holds the Moscow date (YYYY-MM-DD) of the last reported day.
const reportedDayKey = "openrouter_unknown_refunds_reported_day"

// The summary covers the previous calendar day in Moscow time and is sent
// from 10:00 Moscow time.
var moscow = time.FixedZone("MSK", 3*60*60)

const reportHour = 10

// Execute reports the previous Moscow day once, from 10:00 Moscow time. A day
// without such refunds and estimate charges is marked reported without a
// message; a summary that is not delivered leaves the day for the next run.
func (u Usecase) Execute(ctx context.Context) error {
	now := u.now().In(moscow)
	if now.Hour() < reportHour {
		return nil
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, moscow)
	day := today.AddDate(0, 0, -1)
	dayKey := day.Format(time.DateOnly)
	reported, err := u.sent.ReadOption(ctx, reportedDayKey)
	if err != nil {
		return err
	}
	if reported >= dayKey {
		return nil
	}
	refunds, err := u.logs.UnknownOutcomeRefunds(ctx, day.Unix(), today.Unix())
	if err != nil {
		return err
	}
	if refunds.Text.Count+refunds.Image.Count+refunds.EstimatedCharges.Count > 0 {
		if err := u.alerts.UnknownOutcomeRefunds(dayKey, refunds); err != nil {
			return err
		}
	}
	return u.sent.SaveBulk(ctx, []string{reportedDayKey}, map[string]string{reportedDayKey: dayKey})
}
