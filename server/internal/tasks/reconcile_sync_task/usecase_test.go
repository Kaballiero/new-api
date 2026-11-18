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
	events      []string
	metadata    domain.SyncMetadata
	claimError  error
	settleError error
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
	return f.settleError
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
			events: []string{"metadata", "prepare", "project", "revision", "settle", "project", "revision"}},
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

func TestReconciliationRetriesSettlementBeforeTerminalResult(t *testing.T) {
	now := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)
	cancelled := false
	newTask := func(reconciliation *domain.TaskReconciliation) (*domain.Task, *domain.OpenRouterSyncTask) {
		state := &domain.OpenRouterSyncTask{LeaseUntil: now.Unix() - 1, Deadline: now.Add(24 * time.Hour).Unix(), MetadataURL: "https://provider.invalid/v1/generation",
			ResultReady: true, ResultMimeType: "audio/mpeg", Attempts: 1}
		return &domain.Task{TaskID: "task-sync", Platform: string(domain.TaskPlatformOpenRouterAudio), Action: domain.TaskActionAudio, Status: domain.TaskStatusInProgress, Progress: "10%", Quota: 50,
			Properties:  domain.Properties{UpstreamModelName: "author/model"},
			PrivateData: domain.TaskPrivateData{UpstreamTaskID: "gen-fixture", OpenRouterAudio: state, Reconciliation: reconciliation},
		}, state
	}
	metadata := domain.SyncMetadata{ID: "gen-fixture", Model: "author/model", APIType: "tts", Cancelled: &cancelled, CostPresent: true, CostValid: true, CostUSD: 0.0000074}

	t.Run("unapplied settlement is retried without a new lookup", func(t *testing.T) {
		task, state := newTask(&domain.TaskReconciliation{ReservedQuota: 50})
		state.Attempts = 0
		fixture := &syncFixture{settleError: errors.New("settlement interrupted"), metadata: metadata}
		current := now
		usecase := NewUsecase(fixture, fixture, fixture, fixture, func() time.Time { return current })

		require.ErrorIs(t, usecase.Execute(t.Context(), task), fixture.settleError)
		assert.Equal(t, []string{"metadata", "prepare", "project", "revision", "settle"}, fixture.events, "the retry time is claimed before money moves")
		assert.Equal(t, domain.TaskStatus(domain.TaskStatusInProgress), task.Status, "an unapplied settlement keeps the Task pollable")
		assert.Equal(t, "10%", task.Progress)
		assert.True(t, task.PrivateData.Reconciliation.SettlementPending)
		assert.Equal(t, now.Add(time.Minute).Unix(), state.NextPoll)

		fixture.events, fixture.settleError = nil, nil
		require.NoError(t, usecase.Execute(t.Context(), task))
		assert.Empty(t, fixture.events, "the retry waits for its due time")
		current = now.Add(time.Minute)
		require.NoError(t, usecase.Execute(t.Context(), task))
		assert.Equal(t, []string{"project", "revision", "settle", "project", "revision"}, fixture.events)
		assert.Equal(t, domain.TaskStatus(domain.TaskStatusSuccess), task.Status)
		assert.Equal(t, "100%", task.Progress)
		assert.Equal(t, 1, state.Attempts)
	})
	t.Run("applied balances are not replayed", func(t *testing.T) {
		task, _ := newTask(&domain.TaskReconciliation{ReservedQuota: 50, TargetQuota: 3, HasUpstreamCost: true, UpstreamCostConfirmed: true, SettlementPending: true, MainApplied: true})
		fixture := &syncFixture{settleError: errors.New("settlement log failed")}
		require.ErrorIs(t, NewUsecase(fixture, fixture, fixture, fixture, func() time.Time { return now }).Execute(t.Context(), task), fixture.settleError)
		assert.Equal(t, []string{"project", "revision", "settle", "project", "revision"}, fixture.events)
		assert.Equal(t, domain.TaskStatus(domain.TaskStatusSuccess), task.Status, "repair is left to an operator")
	})
	t.Run("finalized settlement only records the terminal result", func(t *testing.T) {
		task, _ := newTask(&domain.TaskReconciliation{ReservedQuota: 50, TargetQuota: 3, HasUpstreamCost: true, UpstreamCostConfirmed: true, MainApplied: true, Finalized: true})
		fixture := &syncFixture{}
		require.NoError(t, NewUsecase(fixture, fixture, fixture, fixture, func() time.Time { return now }).Execute(t.Context(), task))
		assert.Equal(t, []string{"project", "revision", "settle", "project", "revision"}, fixture.events)
		assert.Equal(t, domain.TaskStatus(domain.TaskStatusSuccess), task.Status)
	})
	t.Run("stale retry claim moves no money", func(t *testing.T) {
		task, _ := newTask(&domain.TaskReconciliation{ReservedQuota: 50, TargetQuota: 3, HasUpstreamCost: true, UpstreamCostConfirmed: true, SettlementPending: true})
		stale := errors.New("stale revision")
		fixture := &syncFixture{claimError: stale}
		require.ErrorIs(t, NewUsecase(fixture, fixture, fixture, fixture, func() time.Time { return now }).Execute(t.Context(), task), stale)
		assert.Equal(t, []string{"project", "revision"}, fixture.events)
	})
}

func TestReconciliationMatchesGenerationOfTaskAction(t *testing.T) {
	now := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)
	generationTime := float64(1)
	for _, tc := range []struct {
		name      string
		platform  domain.TaskPlatform
		action    string
		apiType   string
		cancelled bool
		// class is the lookup outcome: empty settles, incomplete_metadata
		// waits, identity_or_invalid_cost parks the hold for root.
		class string
	}{
		{name: "chat generation settles", platform: domain.TaskPlatformOpenRouterChatImage, action: domain.TaskActionChat, apiType: "completions"},
		{name: "completion generation settles", platform: domain.TaskPlatformOpenRouterChatImage, action: domain.TaskActionCompletions, apiType: "completions"},
		{name: "embedding generation settles", platform: domain.TaskPlatformOpenRouterChatImage, action: domain.TaskActionEmbeddings, apiType: "embeddings"},
		{name: "decisions generation settles", platform: domain.TaskPlatformOpenRouterChatImage, action: domain.TaskActionDecisions, apiType: "decisions"},
		{name: "rerank generation settles", platform: domain.TaskPlatformOpenRouterRerank, action: domain.TaskActionRerank, apiType: "rerank"},
		{name: "completion with another api type is held", platform: domain.TaskPlatformOpenRouterChatImage, action: domain.TaskActionCompletions, apiType: "embeddings", class: "identity_or_invalid_cost"},
		{name: "embedding with another api type is held", platform: domain.TaskPlatformOpenRouterChatImage, action: domain.TaskActionEmbeddings, apiType: "completions", class: "identity_or_invalid_cost"},
		{name: "decisions with another api type is held", platform: domain.TaskPlatformOpenRouterChatImage, action: domain.TaskActionDecisions, apiType: "completions", class: "identity_or_invalid_cost"},
		{name: "rerank with another api type is held", platform: domain.TaskPlatformOpenRouterRerank, action: domain.TaskActionRerank, apiType: "completions", class: "identity_or_invalid_cost"},
		{name: "cancelled completion settles", platform: domain.TaskPlatformOpenRouterChatImage, action: domain.TaskActionCompletions, apiType: "completions", cancelled: true},
		{name: "cancelled embedding waits", platform: domain.TaskPlatformOpenRouterChatImage, action: domain.TaskActionEmbeddings, apiType: "embeddings", cancelled: true, class: "incomplete_metadata"},
		{name: "cancelled decisions waits", platform: domain.TaskPlatformOpenRouterChatImage, action: domain.TaskActionDecisions, apiType: "decisions", cancelled: true, class: "incomplete_metadata"},
		{name: "cancelled chat image waits", platform: domain.TaskPlatformOpenRouterChatImage, action: domain.TaskActionChatImage, apiType: "completions", cancelled: true, class: "incomplete_metadata"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &domain.Task{TaskID: "task-sync", Platform: string(tc.platform), Action: tc.action, Status: domain.TaskStatusInProgress, Quota: 50, StartTime: now.Unix() - 31,
				Properties: domain.Properties{UpstreamModelName: "author/model"},
				PrivateData: domain.TaskPrivateData{UpstreamTaskID: "gen-fixture", Reconciliation: &domain.TaskReconciliation{ReservedQuota: 50},
					OpenRouterSync: &domain.OpenRouterSyncTask{Deadline: now.Add(24 * time.Hour).Unix(), MetadataURL: "https://provider.invalid/v1/generation", ResultReady: true}},
			}
			fixture := &syncFixture{metadata: domain.SyncMetadata{ID: "gen-fixture", Model: "author/model", APIType: tc.apiType,
				Cancelled: &tc.cancelled, GenerationTime: &generationTime, CostPresent: true, CostValid: true, CostUSD: .01}}
			require.NoError(t, NewUsecase(fixture, fixture, fixture, fixture, func() time.Time { return now }).Execute(t.Context(), task))
			assert.Equal(t, tc.class, task.PrivateData.OpenRouterSync.LastError)
			assert.Equal(t, tc.class == "", task.PrivateData.Reconciliation.HasUpstreamCost)
			assert.Equal(t, tc.class == "identity_or_invalid_cost", task.PrivateData.Reconciliation.Required)
		})
	}
}
