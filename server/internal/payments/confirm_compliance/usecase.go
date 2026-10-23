package confirm_compliance

import (
	"context"
	"strconv"

	"github.com/QuantumNous/new-api/internal/payments/domain"
)

// Options writes each compliance option separately with the original publication semantics.
type Options interface {
	Set(key string, value string) error
}

type Input struct {
	Confirmed bool
	UserID    int
	ClientIP  string
}

type Result struct {
	Terms  string
	Time   int64
	UserID int
}

type Usecase struct {
	diagnostics domain.Diagnostics
	options     Options
	terms       func() string
	now         func() int64
}

func NewUsecase(options Options, terms func() string, now func() int64, diagnostics domain.Diagnostics) Usecase {
	return Usecase{diagnostics: diagnostics, options: options, terms: terms, now: now}
}

func (u Usecase) Execute(ctx context.Context, in Input) (Result, error) {
	if !in.Confirmed {
		return Result{}, domain.ErrCompliance
	}
	now := u.now()
	terms := u.terms()
	updates := map[string]string{
		"payment_setting.compliance_confirmed":     "true",
		"payment_setting.compliance_terms_version": terms,
		"payment_setting.compliance_confirmed_at":  strconv.FormatInt(now, 10),
		"payment_setting.compliance_confirmed_by":  strconv.Itoa(in.UserID),
		"payment_setting.compliance_confirmed_ip":  in.ClientIP,
	}
	for key, value := range updates {
		if err := u.options.Set(key, value); err != nil {
			return Result{}, err
		}
	}
	u.diagnostics.Info(ctx, "payment compliance confirmed user_id=%d ip=%s terms_version=%s confirmed_at=%d", in.UserID, in.ClientIP, terms, now)
	return Result{Terms: terms, Time: now, UserID: in.UserID}, nil
}
