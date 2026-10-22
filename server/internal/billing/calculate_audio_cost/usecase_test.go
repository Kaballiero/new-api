package calculate_audio_cost

import (
	"testing"

	adapter "github.com/QuantumNous/new-api/internal/adapter/billing"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/stretchr/testify/assert"
)

func TestAudioAmountsPreserveRoundingFXAndLegacyFixedZero(t *testing.T) {
	for _, tc := range []struct {
		name  string
		price domain.AudioPrice
		want  int
	}{
		{"mixed audio", domain.AudioPrice{ModelRatio: 1, GroupRatio: 1, InputDetails: domain.TokenDetails{TextTokens: 20, AudioTokens: 80}, OutputDetails: domain.TokenDetails{TextTokens: 5, AudioTokens: 15}}, 1130},
		{"FX", domain.AudioPrice{ModelRatio: 1, GroupRatio: 1, FXFactor: 1.375, InputDetails: domain.TokenDetails{TextTokens: 20, AudioTokens: 80}, OutputDetails: domain.TokenDetails{TextTokens: 5, AudioTokens: 15}}, 1554},
		{"missing model price stays zero", domain.AudioPrice{UsePrice: true, GroupRatio: 1}, 0},
		{"zero ratio", domain.AudioPrice{ModelRatio: 0, GroupRatio: 1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := NewUsecase(adapter.CostMath{}, adapter.Expression{}, domain.AudioPrices{QuotaUnit: func() float64 { return 500000 }, Read: func(string) (float64, float64, float64) { return 2, 10, 2 }})
			result := u.Execute(Input{Usage: domain.Usage{TotalTokens: 120}, Price: tc.price})
			assert.Equal(t, tc.want, result.Quota)
		})
	}
}
