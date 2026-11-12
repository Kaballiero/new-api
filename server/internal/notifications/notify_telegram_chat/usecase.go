package notify_telegram_chat

import "fmt"

type TelegramGateway interface {
	Send(text string) error
}

type Usecase struct {
	gateway TelegramGateway
	log     func(string)
}

func NewUsecase(gateway TelegramGateway, log func(string)) Usecase {
	return Usecase{gateway, log}
}
func (u Usecase) Execute(subject string, content string) error {
	err := u.gateway.Send(subject + "\n\n" + content)
	if err != nil {
		u.log(fmt.Sprintf("failed to notify telegram chat: %s", err.Error()))
	}
	return err
}
