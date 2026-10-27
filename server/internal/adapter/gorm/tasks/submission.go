package tasks

import (
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// SubmissionConfirmation retains the exact record and its hook-mutated ID and
// timestamps across the durable insert, settlement and marker update.
type SubmissionConfirmation struct {
	Store Store
	Task  *record.Task
}

func (s SubmissionConfirmation) ConfirmReservation(task *domain.Task) (bool, error) {
	state := task.PrivateData.Reconciliation
	s.Task.PrivateData.Reconciliation.ReservationPending = state.ReservationPending
	s.Task.PrivateData.Reconciliation.Required = state.Required
	return s.Store.UpdateTaskWithStatus(s.Task, s.Task.Status)
}
