package send_notification

import (
	"fmt"

	"github.com/QuantumNous/new-api/internal/notifications/domain"
)

type Delivery interface {
	Email(string, domain.Notice) error
	Webhook(string, string, domain.Notice) error
	Bark(string, domain.Notice) error
	Gotify(string, string, int, domain.Notice) error
}
type Usecase struct {
	limit    func(int, string) (bool, error)
	delivery Delivery
	log      func(string)
}

func NewUsecase(limit func(int, string) (bool, error), delivery Delivery, log func(string)) Usecase {
	return Usecase{limit, delivery, log}
}
func (u Usecase) Execute(userId int, userEmail string, userSetting domain.Preferences, data domain.Notice) error {
	notifyType := userSetting.NotifyType
	if notifyType == "" {
		notifyType = domain.Email
	}

	// Check notification limit
	canSend, err := u.limit(userId, data.Type)
	if err != nil {
		u.log(fmt.Sprintf("failed to check notification limit: %s", err.Error()))
		return err
	}
	if !canSend {
		return fmt.Errorf("%w for user %d with type %s", domain.ErrLimitExceeded, userId, notifyType)
	}

	switch notifyType {
	case domain.Email:
		emailToUse := userSetting.NotificationEmail
		if emailToUse == "" {
			emailToUse = userEmail
		}
		if emailToUse == "" {
			u.log(fmt.Sprintf("user %d has no email, skip sending email", userId))
			return nil
		}
		return u.delivery.Email(emailToUse, data)
	case domain.Webhook:
		webhookURLStr := userSetting.WebhookUrl
		if webhookURLStr == "" {
			u.log(fmt.Sprintf("user %d has no webhook url, skip sending webhook", userId))
			return nil
		}

		webhookSecret := userSetting.WebhookSecret
		return u.delivery.Webhook(webhookURLStr, webhookSecret, data)
	case domain.Bark:
		barkURL := userSetting.BarkUrl
		if barkURL == "" {
			u.log(fmt.Sprintf("user %d has no bark url, skip sending bark", userId))
			return nil
		}
		return u.delivery.Bark(barkURL, data)
	case domain.Gotify:
		gotifyUrl := userSetting.GotifyUrl
		gotifyToken := userSetting.GotifyToken
		if gotifyUrl == "" || gotifyToken == "" {
			u.log(fmt.Sprintf("user %d has no gotify url or token, skip sending gotify", userId))
			return nil
		}
		return u.delivery.Gotify(gotifyUrl, gotifyToken, userSetting.GotifyPriority, data)
	}
	return nil
}
