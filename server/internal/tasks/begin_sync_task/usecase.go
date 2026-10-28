package begin_sync_task

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// Store inserts the durable intent before any funding effect.
type Store interface {
	Insert(ctx context.Context, task *domain.Task) error
}

// Accounts freezes the selected account binding without retaining a usable key.
type Accounts interface {
	Prepare(task *domain.Task, input domain.SyncSubmissionAccount) error
}
type Projection interface{ Project(task *domain.Task) }
type Input struct {
	Task                  *domain.Task
	Quota                 int
	RequestID, Preference string
	Account               domain.SyncSubmissionAccount
	Native                *domain.SyncNativeImage
}
type Usecase struct {
	store    Store
	accounts Accounts
	codec    Projection
	now      func() time.Time
}

func NewUsecase(store Store, accounts Accounts, codec Projection, now func() time.Time) Usecase {
	return Usecase{store: store, accounts: accounts, codec: codec, now: now}
}

func (u Usecase) Execute(ctx context.Context, input Input) (*domain.Task, error) {
	task := input.Task
	task.Quota = 0
	task.PrivateData.BillingSource = ""
	task.PrivateData.SubscriptionId = 0
	task.PrivateData.Reconciliation.ReservedQuota = 0
	now := u.now()
	state := &domain.OpenRouterSyncTask{RequestID: input.RequestID, BillingPreference: input.Preference, PlannedQuota: input.Quota, LeaseUntil: now.Add(5 * time.Minute).Unix(), Deadline: now.Add(24 * time.Hour).Unix(), ResultReasonTracked: true}
	if task.Platform == string(domain.TaskPlatformOpenRouterAudio) {
		task.PrivateData.OpenRouterAudio = state
	} else {
		task.PrivateData.OpenRouterSync = state
	}
	if task.Platform == string(domain.TaskPlatformOpenRouterChatImage) {
		if err := u.accounts.Prepare(task, input.Account); err != nil {
			return nil, err
		}
	}
	if native := input.Native; native != nil {
		task.PrivateData.Key = ""
		state.ProviderTag, state.ReserveUSD, state.PricingUnknown = native.ProviderTag, native.ReserveUSD, native.PricingUnknown
		if native.ReserveUSD > 0 {
			state.ReservationSource = "channel_explicit_reserve"
		}
	}
	u.codec.Project(task)
	if err := u.store.Insert(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}
