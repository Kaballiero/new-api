package batch

import (
	"github.com/QuantumNous/new-api/internal/batch/domain"

	"gorm.io/gorm"
)

func (s Store) ByOwnerKey(userID int, key string) (*domain.Job, error) {
	var j BatchJob
	e := s.db().Where("user_id = ? AND idempotency_key = ?", userID, key).First(&j).Error
	v := domain.Job(j)
	return &v, notFound(e)
}
func (s Store) ByPublicID(id string) (*domain.Job, error) {
	var j BatchJob
	e := s.db().Where("public_id = ? AND deleted_at = 0", id).First(&j).Error
	v := domain.Job(j)
	return &v, notFound(e)
}
func (s Store) ByID(id int64) (*domain.Job, error) {
	var j BatchJob
	e := s.db().First(&j, id).Error
	v := domain.Job(j)
	return &v, notFound(e)
}
func (s Store) Inventory(now int64) ([]domain.Job, error) {
	var jobs []BatchJob
	err := s.db().Where("billing_status <> ? OR purchase_status <> ? OR status NOT IN ? OR (deleted_at > 0 AND cleanup_done = ?) OR (expires_at <= ? AND cleanup_done = ?)",

		"settled", "settled", []string{"completed", "failed", "expired", "cancelled"}, false,
		now, false).Order("updated_at asc").Limit(20).Find(&jobs).Error
	var out []domain.Job
	if jobs != nil {
		out = make([]domain.Job, len(jobs))
		for i := range jobs {
			out[i] = domain.Job(jobs[i])
		}
	}
	return out, err
}
func (s Store) ReloadClaim(id int64, owner string) (domain.Job, error) {
	var j BatchJob
	e := s.db().Where("id = ? AND lease_owner = ?", id, owner).First(&j).Error
	return domain.Job(j), e
}
func (s Store) FinishClaim(job *domain.Job, owner string, now int64, message *string) error {
	updates := map[string]any{"lease_until": 0, "lease_owner": "", "updated_at": now}
	if message != nil {
		updates["last_error"] = *message
	}
	return s.db().Model(&BatchJob{}).Where("id = ? AND lease_owner = ?", job.ID, owner).Updates(updates).Error
}
func (s Store) ReleaseClaim(id int64, owner string) error {
	return s.db().Model(&BatchJob{}).Where("id = ? AND lease_owner = ?", id, owner).Updates(map[string]any{"lease_until": 0, "lease_owner": ""}).Error
}
func (s Store) DeleteLocal(job *domain.Job, now int64) error {
	r := BatchJob(*job)
	e := s.db().Model(&r).Update("deleted_at", now).Error
	*job = domain.Job(r)
	return e
}
func (s Store) RequireReconciliation(job *domain.Job) error {
	r := BatchJob(*job)
	e := s.db().Model(&r).Update("billing_status", "reconciliation_required").Error
	*job = domain.Job(r)
	return e
}
func (s Store) MarkTerminalIncomplete(job *domain.Job) error {
	r := BatchJob(*job)
	e := s.db().Model(&r).Where("settled_count < request_count").Update("billing_status", "reconciliation_required").Error
	*job = domain.Job(r)
	return e
}
func (s Store) RequestCancel(job *domain.Job) error {
	r := BatchJob(*job)
	e := s.db().Model(&r).Where("submit_state = ?", "submitted").Update("submit_state", "cancel_requested").Error
	*job = domain.Job(r)
	return e
}
func (s Store) MarkCancelSent(job *domain.Job) error {
	r := BatchJob(*job)
	e := s.db().Model(&r).Where("submit_state = ?", "cancel_requested").Updates(map[string]any{"submit_state": "cancel_sent",
		"status": "cancelling"}).Error
	*job = domain.Job(r)
	return e
}
func (s Store) Items(id int64) ([]domain.Item, error) {
	var items []BatchItem
	e := s.db().Where("batch_id = ?", id).Order("ordinal asc").Find(&items).Error
	var out []domain.Item
	if items != nil {
		out = make([]domain.Item, len(items))
		for i := range items {
			out[i] = domain.Item(items[i])
		}
	}
	return out, e
}
func changed(r *gorm.DB) error {
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return domain.ErrBatchConflict
	}
	return nil
}
func (s Store) MarkPreparing(job *domain.Job, owner string) error {
	r := BatchJob(*job)
	e := changed(s.db().Model(&r).Where("submit_state = ? AND lease_owner = ?", "queued", owner).Update("submit_state", "preparing"))
	*job = domain.Job(r)
	return e
}
func (s Store) SaveInputFile(job *domain.Job, owner, id string) error {
	r := BatchJob(*job)
	e := changed(s.db().Model(&r).Where("lease_owner = ? AND submit_state = ?", owner, "preparing").Update("input_file_id", id))
	*job = domain.Job(r)
	return e
}
func (s Store) RejectCreate(job *domain.Job) error {
	r := BatchJob(*job)
	e := s.db().Model(&r).Where("submit_state = ?", "submission_unknown").Update("submit_state", "rejected").Error
	*job = domain.Job(r)
	return e
}
func (s Store) BindSubmission(job *domain.Job, u domain.SubmissionUpdate) error {
	updates := map[string]any{"upstream_id": u.ID, "submit_state": "submitted", "status": u.State}
	if u.LastError != nil {
		updates["last_error"] = *u.LastError
	}
	r := BatchJob(*job)
	e := changed(s.db().Model(&r).Where("submit_state = ?", "submission_unknown").Updates(updates))
	*job = domain.Job(r)
	return e
}
func (s Store) SavePoll(job *domain.Job, owner string, u domain.PollUpdate) error {
	updates := map[string]any{"status": u.State, "output_file_id": u.OutputFileID, "error_file_id": u.ErrorFileID, "upstream_usage": u.Usage}
	if u.LastError != nil {
		updates["last_error"] = *u.LastError
	}
	r := BatchJob(*job)
	e := s.db().Model(&r).Where("lease_owner = ?", owner).Updates(updates).Error
	*job = domain.Job(r)
	return e
}
func (s Store) LocalCustomID(id int64, providerID string) (string, error) {
	var item BatchItem
	e := s.db().Select("custom_id").Where("batch_id = ? AND provider_custom_id = ?", id, providerID).First(&item).Error
	return item.CustomID, e
}
func (s Store) CountProviderID(id int64, providerID string) (int64, error) {
	var n int64
	e := s.db().Model(&BatchItem{}).Where("batch_id = ? AND provider_custom_id = ?", id, providerID).Count(&n).Error
	return n, e
}
func (s Store) RejectUnsubmitted(job *domain.Job, reason string) error {
	r := BatchJob(*job)
	e := changed(s.db().Model(&r).Where("submit_state IN ? AND upstream_id = '' AND lease_owner = ?",
		[]string{"queued", "preparing", "rejected"}, job.LeaseOwner).Updates(map[string]any{"submit_state": "rejected",
		"status": "failed", "last_error": reason}))
	*job = domain.Job(r)
	return e
}
func (s Store) UnsettledIdentities(id int64) ([]domain.Item, error) {
	var items []BatchItem
	e := s.db().Select("custom_id").Where("batch_id = ? AND billing_status <> ?", id, "settled").Find(&items).Error
	var out []domain.Item
	if items != nil {
		out = make([]domain.Item, len(items))
		for i := range items {
			out[i] = domain.Item(items[i])
		}
	}
	return out, e
}
func (s Store) ClearResults(job *domain.Job) error {
	if e := s.db().Model(&BatchItem{}).Where("batch_id = ?", job.ID).Updates(map[string]any{"result": "",
		"body": "", "upstream_body": ""}).Error; e != nil {
		return e
	}
	r := BatchJob(*job)
	e := s.db().Model(&r).Update("cleanup_done", true).Error
	*job = domain.Job(r)
	return e
}
func (s Store) BindRecovery(job *domain.Job, id, owner string, state domain.State) error {
	r := BatchJob(*job)
	e := changed(s.db().Model(&r).Where("submit_state = ? AND upstream_id = '' AND lease_owner = ?",
		"submission_unknown", owner).Updates(map[string]any{"upstream_id": id, "submit_state": "submitted",
		"status": string(state), "billing_status": "reserved", "last_error": ""}))
	*job = domain.Job(r)
	return e
}
func (s Store) NextResult(id, cursor int64) (domain.Item, error) {
	var item BatchItem
	e := s.db().Where("batch_id = ? AND id > ? AND result_hash <> ''", id, cursor).Order("id asc").First(&item).Error
	return domain.Item(item), notFound(e)
}
func (s Store) NextUnsettled(id, cursor int64) (domain.Item, error) {
	var item BatchItem
	e := s.db().Where("batch_id = ? AND id > ? AND result_hash <> '' AND billing_status <> ?", id, cursor, "settled").Order("id asc").First(&item).Error
	return domain.Item(item), notFound(e)
}
func (s Store) MarkItemUnknown(id int64) error {
	return s.db().Model(&BatchItem{ID: id}).Update("billing_status", "reconciliation_required").Error
}
func (s Store) NextPurchase(id, cursor int64) (domain.Item, error) {
	var item BatchItem
	e := s.db().Select("id", "purchase_status", "financial_details").Where("batch_id = ? AND id > ?", id, cursor).Order("id asc").First(&item).Error
	return domain.Item(item), notFound(e)
}
func (s Store) ReconcilePurchase(job *domain.Job, cost, evidence string) error {
	r := BatchJob(*job)
	e := s.db().Model(&r).Where("purchase_status <> ?", "settled").Updates(map[string]any{"purchase_status": "settled",
		"purchase_usd": cost, "purchase_evidence": evidence}).Error
	*job = domain.Job(r)
	return e
}
