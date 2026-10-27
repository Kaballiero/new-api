package confirm_submission_reservation

import "github.com/QuantumNous/new-api/internal/tasks/domain"

type Writer interface {
	ConfirmReservation(*domain.Task) (bool, error)
}
type Usecase struct{ writer Writer }

func NewUsecase(writer Writer) Usecase { return Usecase{writer: writer} }

// Execute confirms the reservation only after the gateway has settled and
// recorded its durable submission. A failed claim retains the mutated flags,
// matching the original record's observable state.
func (u Usecase) Execute(task *domain.Task) (bool, error) {
	if state := task.PrivateData.Reconciliation; state != nil {
		state.ReservationPending = false
		state.Required = false
		return u.writer.ConfirmReservation(task)
	}
	return true, nil
}
