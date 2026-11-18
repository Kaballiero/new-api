package settle_sync_estimates

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/tasks/domain"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	tasks                 []*domain.Task
	feasibleErr, claimErr error
	claims                int
}

func (s *fakeStore) ManualHolds(afterID int64, limit int) ([]*domain.Task, error) {
	var page []*domain.Task
	for _, task := range s.tasks {
		if task.ID > afterID && strings.HasPrefix(task.FailReason, domain.SyncManualFailReason("")) && len(page) < limit {
			page = append(page, task)
		}
	}
	return page, nil
}

func (s *fakeStore) PendingEstimateSettlements(afterID int64, limit int) ([]*domain.Task, error) {
	var page []*domain.Task
	for _, task := range s.tasks {
		if task.ID > afterID && task.FailReason == domain.SyncEstimatePendingFailReason && len(page) < limit {
			page = append(page, task)
		}
	}
	return page, nil
}

func (s *fakeStore) Feasible(*domain.Task) error { return s.feasibleErr }

func (s *fakeStore) UpdateRevision(task *domain.Task, revision int) error {
	if s.claimErr != nil {
		return s.claimErr
	}
	s.claims++
	task.PrivateData.OpenRouterSync.Revision = revision + 1
	return nil
}

// fakeSettlement prices a cost at 1000 quota per USD and applies a claimed
// target unless it fails, switching an estimate claim to settled as the store
// does.
type fakeSettlement struct {
	settleErr error
	applied   int
}

func (s *fakeSettlement) Prepare(task *domain.Task) {
	r := task.PrivateData.Reconciliation
	r.TargetQuota, r.SettlementPending = int(math.Round(r.UpstreamCostUSD*1000)), true
}

func (s *fakeSettlement) SettleDurable(task *domain.Task) error {
	if s.settleErr != nil {
		return s.settleErr
	}
	r := task.PrivateData.Reconciliation
	r.MainApplied, r.Finalized, r.SettlementPending = true, true, false
	if task.FailReason == domain.SyncEstimatePendingFailReason {
		task.FailReason = domain.SyncEstimateSettledFailReason
	}
	s.applied++
	return nil
}

type fakeProjection struct{}

func (fakeProjection) Project(*domain.Task) {}

type fakeDiagnostics struct{ warnings []string }

func (d *fakeDiagnostics) Info(context.Context, string) {}
func (d *fakeDiagnostics) Warn(_ context.Context, message string) {
	d.warnings = append(d.warnings, message)
}

// imageHold is a delivered native image without its cost parked for root
// behind a 50-quota reserve of reserveUSD.
func imageHold(id int64, reserveUSD float64) *domain.Task {
	return &domain.Task{ID: id, TaskID: "task_image", Platform: string(domain.TaskPlatformOpenRouterImage), Action: domain.TaskActionImage,
		Status: domain.TaskStatusFailure, FailReason: domain.SyncManualFailReason("missing_generation_identity"), FinishTime: 10,
		PrivateData: domain.TaskPrivateData{
			BillingContext: &domain.TaskBillingContext{SettlementMode: domain.TaskSettlementOpenRouterCostV2,
				OpenRouterCost: &billingdomain.ProviderBasis{Version: domain.TaskSettlementOpenRouterCostV2, EstimatedUSD: reserveUSD}},
			Reconciliation: &domain.TaskReconciliation{ReservedQuota: 50, Required: true},
			OpenRouterSync: &domain.OpenRouterSyncTask{Revision: 3, LastError: "missing_generation_identity", ResultReady: true},
		}}
}

func TestExecuteSettlesDeliveredHoldsAtTheirEstimate(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tenth, fifth := .1, .2
	for _, tc := range []struct {
		name                  string
		reserveUSD            float64
		reservedQuota         int
		alerted               bool
		feasibleErr, claimErr error
		claimed, settled      bool
		wantCostUSD           string
		wantWarning           string
	}{
		{name: "settles at the reserve", reserveUSD: .05, claimed: true, settled: true, wantCostUSD: "0.05"},
		{name: "rounds the estimate before pricing", reserveUSD: tenth + fifth, reservedQuota: 300, claimed: true, settled: true, wantCostUSD: "0.3"},
		{name: "target above the reserve stays with root", reserveUSD: .06, wantWarning: "outside the reserve"},
		{name: "zero target stays with root", reserveUSD: .0001, wantWarning: "outside the reserve"},
		{name: "infeasible settlement stays with root", reserveUSD: .05, feasibleErr: errors.New("task user missing"), wantWarning: "not feasible"},
		{name: "lost revision claim leaves the hold to its writer", reserveUSD: .05, claimErr: errors.New("claim changed"), wantWarning: "deferred"},
		{name: "hold reported to root is not tried", reserveUSD: .05, alerted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hold := imageHold(1, tc.reserveUSD)
			if tc.reservedQuota > 0 {
				hold.PrivateData.Reconciliation.ReservedQuota = tc.reservedQuota
			}
			if tc.alerted {
				hold.PrivateData.OpenRouterSync.AlertedAt = 1
			}
			store := &fakeStore{tasks: []*domain.Task{hold}, feasibleErr: tc.feasibleErr, claimErr: tc.claimErr}
			settlement, diagnostics := &fakeSettlement{}, &fakeDiagnostics{}
			usecase := NewUsecase(store, settlement, fakeProjection{}, diagnostics, func() time.Time { return now })

			require.NoError(t, usecase.Execute(context.Background()))
			assert.Equal(t, tc.claimed, store.claims == 1)
			assert.Equal(t, tc.settled, settlement.applied == 1)
			if tc.wantWarning != "" {
				require.Len(t, diagnostics.warnings, 1)
				assert.Contains(t, diagnostics.warnings[0], tc.wantWarning)
			} else {
				assert.Empty(t, diagnostics.warnings)
			}
			if !tc.claimed {
				if tc.claimErr == nil {
					assert.Nil(t, hold.PrivateData.OpenRouterSync.OperatorDecision, "a declined hold records no decision")
				}
				return
			}
			decision := hold.PrivateData.OpenRouterSync.OperatorDecision
			require.NotNil(t, decision)
			assert.Equal(t, tc.wantCostUSD, decision.CostUSD)
			assert.Equal(t, decimal.RequireFromString(tc.wantCostUSD).InexactFloat64(), hold.PrivateData.Reconciliation.UpstreamCostUSD, "the priced cost is the rounded estimate")
			assert.Zero(t, decision.OperatorID)
			assert.Equal(t, domain.SyncEstimateSettledFailReason, hold.FailReason)
			assert.Equal(t, now.Unix(), hold.FinishTime)
			assert.True(t, hold.PrivateData.Reconciliation.UpstreamCostEstimated)
		})
	}
}

func TestExecuteRetriesAClaimedEstimateSettlementOnce(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	hold := imageHold(1, .05)
	store := &fakeStore{tasks: []*domain.Task{hold}}
	settlement, diagnostics := &fakeSettlement{settleErr: errors.New("funding row locked")}, &fakeDiagnostics{}
	usecase := NewUsecase(store, settlement, fakeProjection{}, diagnostics, func() time.Time { return now })

	require.NoError(t, usecase.Execute(context.Background()))
	assert.Equal(t, 1, store.claims)
	assert.Zero(t, settlement.applied)
	require.Len(t, diagnostics.warnings, 1)
	assert.Contains(t, diagnostics.warnings[0], "estimate settlement of 0.05 USD incomplete")
	assert.True(t, hold.PrivateData.Reconciliation.SettlementPending)
	assert.Equal(t, domain.SyncEstimatePendingFailReason, hold.FailReason, "an unapplied claim stays pending for the retry")

	settlement.settleErr = nil
	require.NoError(t, usecase.Execute(context.Background()))
	require.NoError(t, usecase.Execute(context.Background()))
	assert.Equal(t, 1, store.claims, "the decision is claimed once")
	assert.Equal(t, 1, settlement.applied, "the claimed settlement applies once")
	assert.True(t, hold.PrivateData.Reconciliation.Finalized)
	assert.Equal(t, domain.SyncEstimateSettledFailReason, hold.FailReason)
}
