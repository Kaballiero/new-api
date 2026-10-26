package notify_root_user

import (
	"fmt"

	"github.com/QuantumNous/new-api/internal/notifications/domain"
)

type Usecase struct {
	root func() domain.Recipient
	send func(int, string, domain.Preferences, domain.Notice) error
	log  func(string)
}

func NewUsecase(root func() domain.Recipient, send func(int, string, domain.Preferences, domain.Notice) error, log func(string)) Usecase {
	return Usecase{root, send, log}
}
func (u Usecase) Execute(t string, subject string, content string) error {
	user := u.root()
	err := u.send(user.ID, user.Email, user.Preferences(), domain.Notice{Type: t, Title: subject, Content: content, Values: nil})
	if err != nil {
		u.log(fmt.Sprintf("failed to notify root user: %s", err.Error()))
	}
	return err
}
