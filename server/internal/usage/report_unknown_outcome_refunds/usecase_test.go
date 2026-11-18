package report_unknown_outcome_refunds_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/report_unknown_outcome_refunds"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type refundLogs struct {
	refunds  domain.UnknownOutcomeRefunds
	from, to int64
	calls    int
}

func (l *refundLogs) UnknownOutcomeRefunds(_ context.Context, from, to int64) (domain.UnknownOutcomeRefunds, error) {
	l.calls++
	l.from, l.to = from, to
	return l.refunds, nil
}

type sentReports struct{ options map[string]string }

func (s *sentReports) ReadOption(_ context.Context, key string) (string, error) {
	return s.options[key], nil
}

func (s *sentReports) SaveBulk(_ context.Context, keys []string, values map[string]string) error {
	for _, key := range keys {
		s.options[key] = values[key]
	}
	return nil
}

type alerts struct {
	days []string
	sent []domain.UnknownOutcomeRefunds
	err  error
}

func (a *alerts) UnknownOutcomeRefunds(day string, refunds domain.UnknownOutcomeRefunds) error {
	if a.err != nil {
		return a.err
	}
	a.days = append(a.days, day)
	a.sent = append(a.sent, refunds)
	return nil
}

func TestDailyUnknownOutcomeRefundReport(t *testing.T) {
	const key = "openrouter_unknown_refunds_reported_day"
	moscow := time.FixedZone("MSK", 3*60*60)
	dayStart := time.Date(2026, 11, 17, 0, 0, 0, 0, moscow).Unix()
	dayEnd := time.Date(2026, 11, 18, 0, 0, 0, 0, moscow).Unix()
	refunded := domain.UnknownOutcomeRefunds{Text: domain.RefundTally{Count: 2, Quota: 300}, Image: domain.RefundTally{Count: 1, Quota: 50}}
	for _, tc := range []struct {
		name     string
		now      time.Time
		reported string
		refunds  domain.UnknownOutcomeRefunds
		alertErr error
		// queried and alerted describe the run; savedDay is the stored day after it.
		queried, alerted bool
		savedDay         string
		wantErr          bool
	}{
		{name: "before 10:00 Moscow time waits", now: time.Date(2026, 11, 18, 6, 59, 0, 0, time.UTC), reported: "2026-11-16", refunds: refunded, savedDay: "2026-11-16"},
		{name: "from 10:00 reports the previous Moscow day", now: time.Date(2026, 11, 18, 7, 0, 0, 0, time.UTC), reported: "2026-11-16", refunds: refunded, queried: true, alerted: true, savedDay: "2026-11-17"},
		{name: "first run reports the previous day", now: time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC), refunds: refunded, queried: true, alerted: true, savedDay: "2026-11-17"},
		{name: "a reported day is not sent again", now: time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC), reported: "2026-11-17", refunds: refunded, savedDay: "2026-11-17"},
		{name: "a day without refunds sends nothing", now: time.Date(2026, 11, 18, 7, 0, 0, 0, time.UTC), reported: "2026-11-16", queried: true, savedDay: "2026-11-17"},
		{name: "an undelivered summary is retried", now: time.Date(2026, 11, 18, 7, 0, 0, 0, time.UTC), reported: "2026-11-16", refunds: refunded, alertErr: errors.New("telegram down"), queried: true, savedDay: "2026-11-16", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &refundLogs{refunds: tc.refunds}
			sent := &sentReports{options: map[string]string{}}
			if tc.reported != "" {
				sent.options[key] = tc.reported
			}
			delivered := &alerts{err: tc.alertErr}
			err := report_unknown_outcome_refunds.NewUsecase(logs, sent, delivered, func() time.Time { return tc.now }).Execute(context.Background())
			if tc.wantErr {
				require.ErrorIs(t, err, tc.alertErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.queried, logs.calls == 1)
			if tc.queried {
				assert.Equal(t, [2]int64{dayStart, dayEnd}, [2]int64{logs.from, logs.to})
			}
			if tc.alerted {
				assert.Equal(t, []string{"2026-11-17"}, delivered.days)
				assert.Equal(t, []domain.UnknownOutcomeRefunds{tc.refunds}, delivered.sent)
			} else {
				assert.Empty(t, delivered.days)
			}
			assert.Equal(t, tc.savedDay, sent.options[key])
		})
	}
}
