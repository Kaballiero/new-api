package notify_model_update_watchers

import (
	"fmt"

	"github.com/QuantumNous/new-api/internal/notifications/domain"
)

type Usecase struct {
	watchers func() ([]domain.Recipient, error)
	send     func(int, string, domain.Preferences, domain.Notice) error
	log      func(string)
}

func NewUsecase(watchers func() ([]domain.Recipient, error), send func(int, string, domain.Preferences, domain.Notice) error, log func(string)) Usecase {
	return Usecase{watchers, send, log}
}
func (u Usecase) Execute(subject string, content string) error {
	users, err := u.watchers()
	if err != nil {
		u.log(fmt.Sprintf("failed to query upstream update notification users: %s", err.Error()))
		return err
	}

	notification := domain.Notice{Type: domain.ChannelUpdate, Title: subject, Content: content, Values: nil}
	sentCount := 0
	for _, user := range users {
		userSetting := user.Preferences()
		if !userSetting.UpstreamModelUpdateNotifyEnabled {
			continue
		}
		if err := u.send(user.ID, user.Email, userSetting, notification); err != nil {
			u.log(fmt.Sprintf("failed to notify user %d for upstream model update: %s", user.ID, err.Error()))
			continue
		}
		sentCount++
	}
	u.log(fmt.Sprintf("upstream model update notifications sent: %d", sentCount))
	return nil
}
