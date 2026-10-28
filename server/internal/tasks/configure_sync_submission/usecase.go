package configure_sync_submission

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

type Store interface {
	UpdateRevision(task *domain.Task, revision int) error
}

// Accounts verifies the frozen origin and exact static headers.
type Accounts interface {
	ValidateMetadata(task *domain.Task, endpoint string, headers map[string][]string) error
}
type Input struct {
	Task        *domain.Task
	MetadataURL string
	Headers     map[string][]string
	Transport   domain.TransportSettings
}
type Usecase struct {
	store    Store
	accounts Accounts
	now      func() time.Time
}

func NewUsecase(store Store, accounts Accounts, now func() time.Time) Usecase {
	return Usecase{store: store, accounts: accounts, now: now}
}

func (u Usecase) Execute(_ context.Context, input Input) error {
	task := input.Task
	a, r := task.PrivateData.OpenRouterSyncState(), task.PrivateData.Reconciliation
	if r.ReservationPending || !r.SubmissionPending || a.MetadataURL != "" || a.DispatchPrepared || u.now().Unix() >= a.LeaseUntil {
		return errors.New("OpenRouter synchronous submission intent is not available")
	}
	if task.Platform == string(domain.TaskPlatformOpenRouterImage) {
		a.DispatchPrepared = true
		a.LeaseUntil = u.now().Add(5 * time.Minute).Unix()
		return u.store.UpdateRevision(task, a.Revision)
	}
	endpoint, err := url.Parse(input.MetadataURL)
	if err != nil ||
		endpoint.Host == "" ||
		endpoint.User != nil ||
		endpoint.RawQuery != "" ||
		endpoint.Fragment != "" ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return errors.New("invalid OpenRouter synchronous metadata endpoint")
	}
	if task.Platform == string(domain.TaskPlatformOpenRouterChatImage) {
		if err := u.accounts.ValidateMetadata(task, input.MetadataURL, input.Headers); err != nil {
			return err
		}
		a.MetadataURL, a.DispatchPrepared = input.MetadataURL, true
		a.LeaseUntil = u.now().Add(5 * time.Minute).Unix()
		return u.store.UpdateRevision(task, a.Revision)
	}
	a.MetadataURL, a.Headers, a.Transport = input.MetadataURL, input.Headers, input.Transport
	a.LeaseUntil = u.now().Add(5 * time.Minute).Unix()
	return u.store.UpdateRevision(task, a.Revision)
}
