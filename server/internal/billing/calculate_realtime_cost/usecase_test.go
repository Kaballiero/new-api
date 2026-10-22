package calculate_realtime_cost

import (
	"errors"
	"testing"

	adapter "github.com/QuantumNous/new-api/internal/adapter/billing"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealtimeRejectsLegacyClampEvenWithValidTieredResult(t *testing.T) {
	u := NewUsecase(adapter.CostMath{}, adapter.Expression{}, domain.AudioPrices{QuotaUnit: func() float64 { return 500000 }, Read: func(string) (float64, float64, float64) { return 1, 1, 1 }})
	expr := `tier("request",fixed(0.00002))`
	in := Input{Usage: &domain.Usage{TotalTokens: 1, InputTokens: 1, InputTokenDetails: domain.TokenDetails{TextTokens: 1}}, Price: domain.AudioPrice{UsePrice: true, ModelPrice: 1e20, GroupRatio: 1}, Snapshot: &domain.TieredSnapshot{BillingMode: "tiered_expr", ExprString: expr, ExprHash: billingexpr.ExprHashString(expr), GroupRatio: 1, QuotaPerUnit: 500000}}
	result, err := u.Execute(in)
	require.Error(t, err)
	var clamp *domain.Clamp
	require.True(t, errors.As(err, &clamp))
	assert.Same(t, clamp, result.Clamp)
	in.Price.ModelPrice = 0.00002
	in.Usage = &domain.Usage{}
	result, err = u.Execute(in)
	require.NoError(t, err)
	assert.Zero(t, result.Quota)
	assert.False(t, result.Billable, "realtime zero usage stays unbillable even with a fixed tier")
	in.Usage = &domain.Usage{TotalTokens: 1, InputTokens: 1, InputTokenDetails: domain.TokenDetails{TextTokens: 1}}
	result, err = u.Execute(in)
	require.NoError(t, err)
	assert.True(t, result.Billable)
}
