package complete_topup

import (
	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type Input struct{ TradeNo, CallerIP string }

type Usecase struct {
	completion domain.Completion
	admission  domain.Admission
}

func NewUsecase(completion domain.Completion, admission domain.Admission) Usecase {
	return Usecase{completion: completion, admission: admission}
}

func (u Usecase) Execute(in Input) error {
	u.admission.Lock(in.TradeNo)
	defer u.admission.Unlock(in.TradeNo)
	_, err := u.completion.Complete(domain.CompletionInput{TradeNo: in.TradeNo, CallerIP: in.CallerIP, Manual: true})
	return err
}
