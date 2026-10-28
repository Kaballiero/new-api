package tasks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	accountcache "github.com/QuantumNous/new-api/internal/adapter/cache/accounts"
	keycache "github.com/QuantumNous/new-api/internal/adapter/cache/apikeys"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/billing"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/recordutil"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/model"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Store executes task queries and mutations against the current primary database.
type Store struct {
	db       func() *gorm.DB
	accounts accountcache.Store
}

func NewStore(db func() *gorm.DB, accounts accountcache.Store) Store {
	return Store{db: db, accounts: accounts}
}

// UpdateOpenRouterSync guards same-status updates as well as terminal claims.
// SQLite's read/write transaction conflicts reject a stale writer; other
// engines lock the row before checking the private JSON revision.
func (s Store) UpdateOpenRouterSync(t *record.Task, expectedRevision int) error {
	if t.PrivateData.OpenRouterSyncState() == nil {
		return fmt.Errorf("missing OpenRouter synchronous state")
	}
	err := s.db().Transaction(func(tx *gorm.DB) error {
		var saved record.Task
		if err := locking.LockForUpdate(tx).First(&saved, t.ID).Error; err != nil {
			return err
		}
		state := saved.PrivateData.OpenRouterSyncState()
		if saved.Platform == constant.TaskPlatformOpenRouterChatImage && state != nil {
			nextState := t.PrivateData.OpenRouterSyncState()
			if state.RequestID != nextState.RequestID || state.CredentialHash != nextState.CredentialHash || state.ConfigHash != nextState.ConfigHash ||
				state.CredentialIndex != nextState.CredentialIndex || state.RoutingHash != nextState.RoutingHash ||
				state.MetadataURL != "" && state.MetadataURL != nextState.MetadataURL ||
				state.IdentityConflict && !nextState.IdentityConflict ||
				saved.PrivateData.UpstreamTaskID != "" && saved.PrivateData.UpstreamTaskID != t.PrivateData.UpstreamTaskID {
				return fmt.Errorf("OpenRouter chat-image account or generation binding changed")
			}
		}
		// An Image response can arrive after the sweeper retained a manual hold.
		// Only new confirmed cost may claim that hold, never another submission.
		prior, next := saved.PrivateData.Reconciliation, t.PrivateData.Reconciliation
		lateImageCost := (saved.Platform == constant.TaskPlatformOpenRouterImage || saved.Platform == constant.TaskPlatformOpenRouterChatImage) && saved.Status == record.TaskStatusFailure &&
			prior != nil && next != nil && !prior.ReservationPending && !prior.HasUpstreamCost && !prior.SettlementPending && !prior.Finalized &&
			next.HasUpstreamCost && next.UpstreamCostConfirmed && next.SettlementPending && !next.SubmissionPending && !next.ReservationPending
		lateImageReceipt := (saved.Platform == constant.TaskPlatformOpenRouterImage || saved.Platform == constant.TaskPlatformOpenRouterChatImage) && saved.Status == record.TaskStatusFailure && t.Status == record.TaskStatusFailure &&
			prior != nil && next != nil && prior.Required && !prior.ReservationPending && !prior.HasUpstreamCost && !prior.SettlementPending && !prior.Finalized && !next.HasUpstreamCost
		// A root decision may claim an eligible hold only as one confirmed cost
		// settlement: zero for a release, a positive cost for a settle.
		nextState := t.PrivateData.OpenRouterSyncState()
		decision := nextState.OperatorDecision
		// The claimed cost and target must be exactly the decision priced on the
		// frozen basis, never an arbitrary target written next to a decision.
		decisionPriced := false
		if decision != nil && next != nil && saved.PrivateData.BillingContext != nil && saved.PrivateData.BillingContext.OpenRouterCost != nil {
			cost, costErr := decimal.NewFromString(decision.CostUSD)
			target, clamp, quotaErr := saved.PrivateData.BillingContext.OpenRouterCost.Quota(next.UpstreamCostUSD)
			decisionPriced = costErr == nil && cost.InexactFloat64() == next.UpstreamCostUSD && quotaErr == nil && clamp == nil && target == next.TargetQuota
		}
		operatorDecision := state != nil && state.OperatorDecision == nil && decision != nil && decisionPriced && saved.OpenRouterSyncOperatorEligible(decision.Action) &&
			t.Status == record.TaskStatusFailure && t.PrivateData.UpstreamTaskID == saved.PrivateData.UpstreamTaskID && next != nil &&
			next.HasUpstreamCost && next.UpstreamCostConfirmed && next.SettlementPending && !next.MainApplied && !next.Finalized && next.QuotaSaturation == "" &&
			next.ReservedQuota == prior.ReservedQuota && t.Quota == saved.Quota &&
			(decision.Action == record.OpenRouterSyncOperatorRelease && next.UpstreamCostUSD == 0 && next.TargetQuota == 0 ||
				decision.Action == record.OpenRouterSyncOperatorSettle && next.UpstreamCostUSD > 0)
		// No other writer may add, replace or drop a recorded decision.
		decisionChanged := state != nil && (state.OperatorDecision == nil) != (decision == nil) ||
			state != nil && state.OperatorDecision != nil && decision != nil && *state.OperatorDecision != *decision
		if state == nil || state.Revision != expectedRevision || saved.TaskID != t.TaskID || decisionChanged && !operatorDecision ||
			saved.Status == record.TaskStatusSuccess || saved.Status == record.TaskStatusFailure && !lateImageCost && !lateImageReceipt && !operatorDecision ||
			saved.Platform != t.Platform || saved.Action != t.Action || saved.UserId != t.UserId || saved.ChannelId != t.ChannelId || saved.PrivateData.TokenId != t.PrivateData.TokenId || saved.Properties.UpstreamModelName != t.Properties.UpstreamModelName {
			return fmt.Errorf("OpenRouter synchronous task claim changed")
		}
		if lateImageReceipt {
			// A late header may add correlation to a manual Image receipt only.
			// Preserve every financial/result field from the locked saved snapshot.
			state.UpstreamRequestID = t.PrivateData.OpenRouterSyncState().UpstreamRequestID
			state.IdentityConflict = state.IdentityConflict || t.PrivateData.OpenRouterSyncState().IdentityConflict
			saved.PrivateData.UpstreamTaskID = t.PrivateData.UpstreamTaskID
			state.Revision = expectedRevision + 1
			if err := tx.Model(&record.Task{}).Where("id = ?", t.ID).Update("private_data", saved.PrivateData).Error; err != nil {
				return err
			}
			t.PrivateData = saved.PrivateData
			return nil
		}
		t.PrivateData.OpenRouterSyncState().Revision = expectedRevision + 1
		return tx.Model(&record.Task{}).Where("id = ?", t.ID).Updates(map[string]any{
			"private_data": t.PrivateData, "status": t.Status, "progress": t.Progress,
			"quota": t.Quota, "data": t.Data, "start_time": t.StartTime,
			"finish_time": t.FinishTime, "fail_reason": t.FailReason,
		}).Error
	})
	if err != nil {
		t.PrivateData.OpenRouterSyncState().Revision = expectedRevision
	}
	return err
}

func (s Store) TaskGetAllUserTask(userId int, startIdx int, num int, queryParams record.SyncTaskQueryParams) []*record.Task {
	var tasks []*record.Task
	var err error

	// Build the existing filtered query.
	query := s.db().Where("user_id = ?", userId)

	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		// Keep timestamp filtering in the existing storage representation.
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// Read the requested page.
	err = query.Omit("channel_id").Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func (s Store) TaskGetAllTasks(startIdx int, num int, queryParams record.SyncTaskQueryParams) []*record.Task {
	var tasks []*record.Task
	var err error

	// Build the existing filtered query.
	query := s.db()

	// Apply the existing optional filters.
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// Read the requested page.
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func (s Store) GetTimedOutUnfinishedTasks(cutoffUnix int64, limit int) []*record.Task {
	var tasks []*record.Task
	err := s.db().Where("progress != ?", "100%").
		Where("status NOT IN ?", []string{record.TaskStatusFailure, record.TaskStatusSuccess}).
		Where("(platform IS NULL OR platform NOT IN ?)", []constant.TaskPlatform{constant.TaskPlatformOpenRouterAudio, constant.TaskPlatformOpenRouterRerank, constant.TaskPlatformOpenRouterImage, constant.TaskPlatformOpenRouterChatImage}).
		Where("submit_time < ?", cutoffUnix).
		Order("submit_time").
		Limit(limit).
		Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

// GetUnfinishedSyncTaskPage permits callers to skip attempts awaiting manual
// reconciliation without starving later tasks behind the query limit.
func (s Store) GetUnfinishedSyncTaskPage(limit int, afterID int64) []*record.Task {
	var tasks []*record.Task
	var err error
	// get all tasks progress is not 100%
	err = s.db().Where("id > ?", afterID).Where("progress != ?", "100%").Where("status != ?", record.TaskStatusFailure).Where("status != ?", record.TaskStatusSuccess).Limit(limit).Order("id").Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

// HasUnfinishedSyncTasks reports whether at least one async (Suno/video) task is
// still in progress. It is a cheap existence check (LIMIT 1) used to decide
// whether the async_task_poll system task needs to run; when no task is pending
// the scheduler skips creating a row entirely.
func (s Store) HasUnfinishedSyncTasks() bool {
	var id int64
	err := s.db().Model(&record.Task{}).
		Where("progress != ?", "100%").
		Where("status != ?", record.TaskStatusFailure).
		Where("status != ?", record.TaskStatusSuccess).
		Limit(1).
		Pluck("id", &id).Error
	return err == nil && id != 0
}

func (s Store) GetByOnlyTaskId(taskId string) (*record.Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	var task *record.Task
	var err error
	err = s.db().Where("task_id = ?", taskId).First(&task).Error
	exist, err := recordutil.RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, err
}

// GetUniqueByOnlyTaskId resolves a public task identifier only when exactly one
// row owns it. Historical task identifiers were not globally unique, so
// capability-based reads must fail closed instead of selecting an arbitrary
// tenant's row.
func (s Store) GetUniqueByOnlyTaskId(taskId string) (*record.Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	var tasks []*record.Task
	if err := s.db().Where("task_id = ?", taskId).Order("id").Limit(2).Find(&tasks).Error; err != nil {
		return nil, false, err
	}
	if len(tasks) != 1 {
		return nil, false, nil
	}
	return tasks[0], true, nil
}

func (s Store) GetByTaskId(userId int, taskId string) (*record.Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	var task *record.Task
	var err error
	err = s.db().Where("user_id = ? and task_id = ?", userId, taskId).
		First(&task).Error
	exist, err := recordutil.RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, err
}

func (s Store) GetByTaskIdsForPlatforms(userID int, platforms []constant.TaskPlatform, taskIDs []string) ([]*record.Task, error) {
	if len(platforms) == 0 || len(taskIDs) == 0 {
		return nil, nil
	}
	var tasks []*record.Task
	err := s.db().
		Where("user_id = ? AND platform IN ? AND task_id IN ?", userID, platforms, taskIDs).
		Find(&tasks).Error
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

// GetTaskForProtocolObservation reloads one public task through the ownership
// boundary used by long-lived plugin protocol observers. A missing task,
// foreign user, and wrong plugin platform are deliberately indistinguishable.
func (s Store) GetTaskForProtocolObservation(ctx context.Context, userID int, platform constant.TaskPlatform, taskID string) (*record.Task, bool, error) {
	if taskID == "" {
		return nil, false, nil
	}
	var task record.Task
	err := s.db().WithContext(ctx).
		Where("user_id = ? AND platform = ? AND task_id = ?", userID, platform, taskID).
		First(&task).Error
	exists, err := recordutil.RecordExist(err)
	if err != nil || !exists {
		return nil, exists, err
	}
	return &task, true, nil
}

func (s Store) InsertTask(ctx context.Context, task *record.Task) error {
	return s.db().WithContext(ctx).Create(task).Error
}

func (s Store) UpdateTask(task *record.Task) error {
	var err error
	err = s.db().Save(task).Error
	return err
}

func (s Store) UpdateTaskQuota(t *record.Task) error {
	return s.db().Model(t).Update("quota", t.Quota).Error
}

// ClaimReconciledTaskRefund claims the refund before any financial effect.
// Crashes and failed legs leave RefundPending for reconciliation instead of
// replaying a potentially completed transfer.
func (s Store) ClaimReconciledTaskRefund(t *record.Task, quota int) (bool, error) {
	t.PrivateData.Reconciliation.RefundPending = true
	t.PrivateData.Reconciliation.Required = true
	result := s.db().Model(t).Where("status = ? AND quota = ?", record.TaskStatusFailure, quota).
		Updates(map[string]any{"quota": 0, "private_data": t.PrivateData})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	t.Quota = 0
	return true, nil
}

// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Returns (true, nil) if this caller won the update, (false, nil) if
// another process already moved the task out of fromStatus. MySQL commonly
// reports changed rows rather than matched rows, so a same-value no-op update
// can also return false even when the status predicate still matched.
//
// Uses Model().Select("*").Updates() instead of Save() because GORM's Save
// falls back to INSERT ON CONFLICT when the WHERE-guarded UPDATE matches
// zero rows, which silently bypasses the CAS guard.
func (s Store) UpdateTaskWithStatus(t *record.Task, fromStatus record.TaskStatus) (bool, error) {
	if bc := t.PrivateData.BillingContext; bc != nil && bc.SettlementMode != "" && (fromStatus == record.TaskStatusSuccess || fromStatus == record.TaskStatusFailure) {
		return false, nil
	}
	result := s.db().Model(t).Where("status = ?", fromStatus).Select("*").Updates(t)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// TaskBulkUpdateByID performs an unconditional bulk UPDATE by primary key IDs.
// WARNING: This function has NO CAS (Compare-And-Swap) guard — it will overwrite
// any concurrent status changes. DO NOT use in billing/quota lifecycle flows
// (e.g., timeout, success, failure transitions that trigger refunds or settlements).
// For status transitions that involve billing, use Store.UpdateTaskWithStatus instead.
func (s Store) TaskBulkUpdateByID(ids []int64, params map[string]any) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db().Model(&record.Task{}).
		Where("id in (?)", ids).
		Updates(params).Error
}

// TaskCountAllTasks returns total tasks that match the given query params (admin usage)
func (s Store) TaskCountAllTasks(queryParams record.SyncTaskQueryParams) int64 {
	var total int64
	query := s.db().Model(&record.Task{})
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// TaskCountAllUserTask returns total tasks for given user
func (s Store) TaskCountAllUserTask(userId int, queryParams record.SyncTaskQueryParams) int64 {
	var total int64
	query := s.db().Model(&record.Task{}).Where("user_id = ?", userId)
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// ApplyTaskCostSettlement commits the main-database legs once. Cache and log
// uncertainty is deliberately left pending for manual reconciliation.
func (s Store) ApplyTaskCostSettlement(t *record.Task) (string, error) {
	tokenKey := ""
	err := s.db().Transaction(func(tx *gorm.DB) error {
		var saved record.Task
		if err := locking.LockForUpdate(tx).First(&saved, t.ID).Error; err != nil {
			return err
		}
		expected, state := t.PrivateData.Reconciliation, saved.PrivateData.Reconciliation
		if expected == nil || state == nil || !state.SettlementPending || state.MainApplied || state.Finalized ||
			saved.TaskID != t.TaskID || saved.UserId != t.UserId || saved.ChannelId != t.ChannelId ||
			saved.PrivateData.TokenId != t.PrivateData.TokenId || saved.PrivateData.BillingSource != t.PrivateData.BillingSource ||
			saved.PrivateData.SubscriptionId != t.PrivateData.SubscriptionId || saved.Status != t.Status ||
			saved.PrivateData.BillingContext == nil || (saved.PrivateData.BillingContext.SettlementMode != record.TaskSettlementOpenRouterCostV1 && saved.PrivateData.BillingContext.SettlementMode != record.TaskSettlementOpenRouterCostV2) ||
			state.TargetQuota != expected.TargetQuota || state.ReservedQuota != expected.ReservedQuota || saved.Quota != state.ReservedQuota {
			return fmt.Errorf("task settlement claim changed or already applied")
		}
		if saved.PrivateData.BillingSource != "wallet" && saved.PrivateData.BillingSource != "subscription" {
			return fmt.Errorf("unknown task funding source")
		}
		if saved.PrivateData.BillingSource == "subscription" && saved.PrivateData.SubscriptionId <= 0 {
			return fmt.Errorf("missing task subscription")
		}
		delta := state.TargetQuota - state.ReservedQuota
		// The conditional write also claims the SQLite row before any balances move.
		state.MainApplied = true
		result := tx.Model(&record.Task{}).Where("id = ? AND status = ? AND quota = ?", saved.ID, saved.Status, saved.Quota).
			Updates(map[string]any{"quota": state.TargetQuota, "private_data": saved.PrivateData})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("task settlement claim lost")
		}
		if delta == 0 {
			return nil
		}
		if saved.PrivateData.BillingSource == "subscription" && saved.PrivateData.SubscriptionId > 0 {
			if err := billing.AdjustTaskSubscription(tx, saved.PrivateData.SubscriptionId, saved.UserId, int64(delta)); err != nil {
				return err
			}
		}
		userUpdates := map[string]any{"used_quota": gorm.Expr("used_quota + ?", delta)}
		if saved.PrivateData.BillingSource != "subscription" {
			userUpdates["quota"] = gorm.Expr("quota - ?", delta)
		}
		result = tx.Model(&model.User{}).Where("id = ?", saved.UserId).Updates(userUpdates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("task user missing")
		}
		result = tx.Model(&model.Channel{}).Where("id = ?", saved.ChannelId).Update("used_quota", gorm.Expr("used_quota + ?", delta))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("task channel missing")
		}
		if saved.PrivateData.TokenId > 0 {
			var token model.Token
			if err := tx.Where("id = ? AND user_id = ?", saved.PrivateData.TokenId, saved.UserId).First(&token).Error; err != nil {
				return err
			}
			tokenKey = token.Key
			result = tx.Model(&token).Updates(map[string]any{"remain_quota": gorm.Expr("remain_quota - ?", delta), "used_quota": gorm.Expr("used_quota + ?", delta)})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("task token missing")
			}
		}
		return nil
	})
	if err == nil {
		t.Quota = t.PrivateData.Reconciliation.TargetQuota
		t.PrivateData.Reconciliation.MainApplied = true
	}
	return tokenKey, err
}

// SyncTaskCostSettlementCache applies only the committed additive delta, never
// a stale absolute balance (initial reservations may still be queued).
func (s Store) SyncTaskCostSettlementCache(t *record.Task, tokenKey string) error {
	delta := t.PrivateData.Reconciliation.TargetQuota - t.PrivateData.Reconciliation.ReservedQuota
	if delta == 0 || !common.RedisEnabled {
		return nil
	}
	if t.PrivateData.BillingSource != "subscription" {
		if err := s.accounts.CacheDecrUserQuota(t.UserId, int64(delta)); err != nil {
			return err
		}
	}
	if t.PrivateData.TokenId > 0 {
		_, err := keycache.CacheApplyTokenQuotaDelta(t.PrivateData.TokenId, tokenKey, -int64(delta))
		return err
	}
	return nil
}

// FinalizeTaskCostSettlement clears uncertainty only after cache and log success.
func (s Store) FinalizeTaskCostSettlement(t *record.Task) error {
	var committed record.TaskPrivateData
	var committedData json.RawMessage
	err := s.db().Transaction(func(tx *gorm.DB) error {
		var saved record.Task
		if err := locking.LockForUpdate(tx).First(&saved, t.ID).Error; err != nil {
			return err
		}
		state, expected := saved.PrivateData.Reconciliation, t.PrivateData.Reconciliation
		if state == nil || expected == nil || !state.MainApplied || !state.SettlementPending || state.Finalized ||
			saved.TaskID != t.TaskID || saved.Status != t.Status || saved.Quota != expected.TargetQuota ||
			state.TargetQuota != expected.TargetQuota || state.ReservedQuota != expected.ReservedQuota {
			return fmt.Errorf("task settlement finalization claim changed")
		}
		state.Finalized = true
		state.Required = false
		state.SettlementPending = false
		updates := map[string]any{"private_data": saved.PrivateData}
		if saved.PrivateData.OpenRouterSyncState() != nil {
			var data map[string]any
			if err := common.Unmarshal(saved.Data, &data); err != nil {
				return err
			}
			data["billing_status"] = "settled"
			encoded, err := common.Marshal(data)
			if err != nil {
				return err
			}
			updates["data"], committedData = json.RawMessage(encoded), encoded
		}
		result := tx.Model(&record.Task{}).Where("id = ? AND status = ? AND quota = ?", saved.ID, saved.Status, saved.Quota).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("task settlement finalization claim lost")
		}
		committed = saved.PrivateData
		return nil
	})
	if err == nil {
		t.PrivateData = committed
		if committedData != nil {
			t.Data = committedData
		}
	}
	return err
}

// MarkResponsesBackground preserves the existing whole-private-data update for protocol submission.
func (s Store) MarkResponsesBackground(task *record.Task) error {
	return s.db().Model(task).Update("private_data", task.PrivateData).Error
}

// CheckTaskCostSettlementFeasible reports, without moving funds, whether
// ApplyTaskCostSettlement can apply the frozen target to the current funding
// rows. A caller that must not leave an irreversible pending claim checks first.
func (s Store) CheckTaskCostSettlementFeasible(t *record.Task) error {
	r := t.PrivateData.Reconciliation
	if r == nil {
		return fmt.Errorf("missing task reconciliation")
	}
	source := t.PrivateData.BillingSource
	if source != "wallet" && source != "subscription" {
		return fmt.Errorf("unknown task funding source")
	}
	if source == "subscription" && t.PrivateData.SubscriptionId <= 0 {
		return fmt.Errorf("missing task subscription")
	}
	delta := r.TargetQuota - r.ReservedQuota
	if delta == 0 {
		return nil
	}
	if source == "subscription" {
		var sub model.UserSubscription
		if err := s.db().Where("id = ? AND user_id = ?", t.PrivateData.SubscriptionId, t.UserId).First(&sub).Error; err != nil {
			return fmt.Errorf("task subscription unavailable")
		}
		if used := max(sub.AmountUsed+int64(delta), 0); sub.AmountTotal > 0 && used > sub.AmountTotal {
			return fmt.Errorf("subscription used exceeds total")
		}
	}
	var count int64
	if err := s.db().Model(&model.User{}).Where("id = ?", t.UserId).Count(&count).Error; err != nil || count != 1 {
		return fmt.Errorf("task user missing")
	}
	if err := s.db().Model(&model.Channel{}).Where("id = ?", t.ChannelId).Count(&count).Error; err != nil || count != 1 {
		return fmt.Errorf("task channel missing")
	}
	if t.PrivateData.TokenId > 0 {
		var token model.Token
		if err := s.db().Where("id = ? AND user_id = ?", t.PrivateData.TokenId, t.UserId).First(&token).Error; err != nil {
			return fmt.Errorf("task token missing")
		}
	}
	return nil
}
