package notify_quota

import (
	"fmt"

	"github.com/QuantumNous/new-api/internal/billing/domain"
)

type Notifier interface{ Deliver(domain.Notice) error }
type Input struct {
	State              func() domain.NotificationState
	Delta, PreConsumed int
	Subscription       bool
}
type Usecase struct {
	notifier  Notifier
	threshold func() int
	submit    func(func())
	log       func(string)
}

func NewUsecase(notifier Notifier, threshold func() int, submit func(func()), log func(string)) Usecase {
	return Usecase{notifier, threshold, submit, log}
}
func (u Usecase) Execute(input Input) error {
	if input.Delta+input.PreConsumed == 0 {
		return nil
	}
	u.submit(func() {
		state := input.State()
		notice, needed := domain.QuotaNotice(state, input.Delta, input.PreConsumed, u.threshold(), input.Subscription)
		if !needed {
			return
		}
		if err := u.notifier.Deliver(notice); err != nil {
			kind := "quota"
			if input.Subscription {
				kind = "subscription quota"
			}
			u.log(fmt.Sprintf("failed to send %s notify to user %d: %s", kind, state.Recipient.UserID, err))
		}
	})
	return nil
}
