package prepare_tiered_cost

import (
	"testing"

	adapter "github.com/QuantumNous/new-api/internal/adapter/billing"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectedGroupRecomposesFrozenFXWithoutMutatingOnFailure(t *testing.T) {
	u := NewUsecase(adapter.CostMath{})
	snap := &domain.TieredSnapshot{BillingMode: "tiered_expr", GroupRatio: 1.8, EstimatedQuotaBeforeGroup: 100, EstimatedQuotaAfterGroup: 180}
	result, err := u.Execute(Input{Snapshot: snap, GroupRatio: 1.8, PureGroupRatio: 3, FXFactor: 0.9})
	require.NoError(t, err)
	assert.Equal(t, 2.7, result.GroupRatio)
	assert.Equal(t, 270, result.EstimatedQuotaAfterGroup)
	assert.Equal(t, 1.8, snap.GroupRatio)
	snap.EstimatedQuotaBeforeGroup = 1e30
	_, err = u.Execute(Input{Snapshot: snap, GroupRatio: 2})
	require.Error(t, err)
	assert.Equal(t, 180, snap.EstimatedQuotaAfterGroup)
}
