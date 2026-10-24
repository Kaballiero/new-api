package tasks

import (
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// BillingStore maps records for claims and applies mutations returned by SQL.
type BillingStore struct{ Store Store }

func (s BillingStore) ClaimRefund(task *domain.Task, quota int) (bool, error) {
	r := record.FromDomain(task)
	won, err := s.Store.ClaimReconciledTaskRefund(r, quota)
	record.RestoreDomain(task, r)
	return won, err
}
func (s BillingStore) Transition(task *domain.Task, from domain.TaskStatus) (bool, error) {
	r := record.FromDomain(task)
	won, err := s.Store.UpdateTaskWithStatus(r, record.TaskStatus(from))
	record.RestoreDomain(task, r)
	return won, err
}
func (s BillingStore) UpdateQuota(task *domain.Task) error {
	return s.Store.UpdateTaskQuota(record.FromDomain(task))
}
func (s BillingStore) Apply(task *domain.Task) (string, error) {
	r := record.FromDomain(task)
	key, err := s.Store.ApplyTaskCostSettlement(r)
	record.RestoreDomain(task, r)
	return key, err
}
func (s BillingStore) SyncCache(task *domain.Task, key string) error {
	return s.Store.SyncTaskCostSettlementCache(record.FromDomain(task), key)
}
func (s BillingStore) Finalize(task *domain.Task) error {
	r := record.FromDomain(task)
	err := s.Store.FinalizeTaskCostSettlement(r)
	record.RestoreDomain(task, r)
	return err
}
func (s BillingStore) UpdateRevision(task *domain.Task, revision int) error {
	r := record.FromDomain(task)
	err := s.Store.UpdateOpenRouterSync(r, revision)
	record.RestoreDomain(task, r)
	return err
}
func (s BillingStore) UpdateMidjourneyBilling(task *domain.Midjourney) error {
	return s.Store.UpdateMidjourneyBillingState(MidjourneyFromDomain(task))
}
