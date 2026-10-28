package reconcile_sync_task

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type syncFixture struct {
	events     []string
	metadata   domain.SyncMetadata
	claimError error
}

func (f *syncFixture) UpdateRevision(task *domain.Task, _ int) error {
	f.events = append(f.events, "revision")
	if f.claimError != nil {
		return f.claimError
	}
	task.PrivateData.OpenRouterSyncState().Revision++
	return nil
}
func (f *syncFixture) Project(*domain.Task) { f.events = append(f.events, "project") }
func (f *syncFixture) Prepare(task *domain.Task) {
	f.events = append(f.events, "prepare")
	task.PrivateData.Reconciliation.SettlementPending = true
}
func (f *syncFixture) SettleDurable(*domain.Task) error {
	f.events = append(f.events, "settle")
	return nil
}
func (f *syncFixture) Generation(context.Context, *domain.Task, string) (domain.SyncMetadata, int, time.Duration, string) {
	f.events = append(f.events, "metadata")
	return f.metadata, 200, 0, ""
}
func (f *syncFixture) Model(context.Context, *domain.Task, string) (domain.SyncModelMetadata, int, time.Duration, string) {
	panic("fixture does not permit a second metadata request")
}

func TestReconciliationPreservesUnknownCostAndClaimsBeforeEffects(t *testing.T) {
	now := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)
	cancelled, generationTime := false, float64(1)
	stale := errors.New("stale revision")
	for _, tc := range []struct {
		name       string
		platform   domain.TaskPlatform
		action     string
		present    bool
		claimError error
		events     []string
	}{
		{name: "explicit terminal zero settles", platform: domain.TaskPlatformOpenRouterAudio, action: "audio", present: true,
			events: []string{"metadata", "prepare", "project", "revision", "settle"}},
		{name: "missing cost retains hold", platform: domain.TaskPlatformOpenRouterAudio, action: "audio",
			events: []string{"metadata", "project", "revision"}},
		{name: "stale chat claim cannot fetch or settle", platform: domain.TaskPlatformOpenRouterChatImage, action: "chat-image", present: true, claimError: stale,
			events: []string{"revision"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &domain.Task{TaskID: "task-sync", Platform: string(tc.platform), Action: tc.action, Status: domain.TaskStatusInProgress, Quota: 50,
				Properties:  domain.Properties{UpstreamModelName: "author/model"},
				PrivateData: domain.TaskPrivateData{UpstreamTaskID: "gen-fixture", Reconciliation: &domain.TaskReconciliation{ReservedQuota: 50}},
			}
			state := &domain.OpenRouterSyncTask{LeaseUntil: now.Unix() - 1, Deadline: now.Add(24 * time.Hour).Unix(), MetadataURL: "https://provider.invalid/v1/generation", ResultReady: true}
			if tc.platform == domain.TaskPlatformOpenRouterAudio {
				task.PrivateData.OpenRouterAudio = state
			} else {
				task.PrivateData.OpenRouterSync = state
			}
			fixture := &syncFixture{claimError: tc.claimError, metadata: domain.SyncMetadata{ID: "gen-fixture", Model: "author/model", APIType: "completions",
				Cancelled: &cancelled, GenerationTime: &generationTime, CostPresent: tc.present, CostValid: tc.present, CostUSD: 0}}
			usecase := NewUsecase(fixture, fixture, fixture, fixture, func() time.Time { return now })
			err := usecase.Execute(t.Context(), task)
			if tc.claimError != nil {
				require.ErrorIs(t, err, tc.claimError)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.events, fixture.events)
			assert.Equal(t, 50, task.Quota)
			assert.Equal(t, 50, task.PrivateData.Reconciliation.ReservedQuota)
			if !tc.present {
				assert.Equal(t, domain.TaskStatus(domain.TaskStatusInProgress), task.Status)
				assert.False(t, task.PrivateData.Reconciliation.HasUpstreamCost)
				assert.False(t, task.PrivateData.Reconciliation.SettlementPending)
				assert.Equal(t, now.Add(time.Minute).Unix(), state.NextPoll)
			}
		})
	}
}
