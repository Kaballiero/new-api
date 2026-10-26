package accountsadapter

import (
	"github.com/QuantumNous/new-api/common"
	notification "github.com/QuantumNous/new-api/internal/adapter/notifications"
	"github.com/QuantumNous/new-api/internal/notifications/domain"
	"github.com/QuantumNous/new-api/model"
)

type NotificationRecipients struct{}

func (NotificationRecipients) Root() domain.Recipient {
	user := model.GetRootUser().ToBaseUser()
	return domain.Recipient{ID: user.Id, Email: user.Email, Preferences: func() domain.Preferences { return notification.Preferences(user.GetSetting()) }}
}
func (NotificationRecipients) Watchers() ([]domain.Recipient, error) {
	var users []model.User
	if err := model.DB.Select("id", "email", "role", "status", "setting").Where("status = ? AND role >= ?", common.UserStatusEnabled, common.RoleAdminUser).Find(&users).Error; err != nil {
		return nil, err
	}
	recipients := make([]domain.Recipient, 0, len(users))
	for _, user := range users {
		recipients = append(recipients, domain.Recipient{ID: user.Id, Email: user.Email, Preferences: func() domain.Preferences { return notification.Preferences(user.GetSetting()) }})
	}
	return recipients, nil
}
