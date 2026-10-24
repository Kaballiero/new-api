package receive_epay_callback

import (
	"errors"

	payments "github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
)

type Orders interface {
	Complete(domain.CompletionInput) error
}
type Usecase struct {
	factory   payments.EpayFactory
	orders    Orders
	admission payments.Admission
	provider  string
}

func NewUsecase(factory payments.EpayFactory, orders Orders, admission payments.Admission, provider string) Usecase {
	return Usecase{factory, orders, admission, provider}
}
func (u Usecase) Execute(params map[string]string) (bool, error) {
	if len(params) == 0 {
		return false, errors.New("empty callback")
	}
	client := u.factory.Client()
	if client == nil {
		return false, domain.ErrEpayConfig
	}
	event, err := client.Verify(params)
	if err != nil || !event.Verified {
		return false, errors.New("invalid callback")
	}
	if event.Status != "TRADE_SUCCESS" {
		return false, nil
	}
	u.admission.Lock(event.TradeNo)
	defer u.admission.Unlock(event.TradeNo)
	err = u.orders.Complete(domain.CompletionInput{
		TradeNo:          event.TradeNo,
		ProviderPayload:  event.Verification,
		ExpectedProvider: u.provider,
		ActualMethod:     event.Method,
	})
	return true, err
}
