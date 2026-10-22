package capture_fx_basis

import (
	"errors"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapturedBasisDoesNotReadNewPublicationAndRejectsAccountingDrift(t *testing.T) {
	rate := 100.0
	reads := 0
	u := NewUsecase(domain.FXSettings{QuotaUnit: func() float64 { return 500000 }, AccountingRate: func() float64 { return rate }, Read: func() (domain.FXPublication, error) {
		reads++
		return domain.FXPublication{Rates: map[string]float64{"USD": 137.5}, Version: 2}, nil
	}})
	basis, err := u.Execute(domain.FXBasis{})
	require.NoError(t, err)
	assert.Equal(t, 1.375, basis.Factor)
	assert.Equal(t, 1, reads)
	frozen, err := u.Execute(basis)
	require.NoError(t, err)
	assert.Equal(t, basis, frozen)
	assert.Equal(t, 1, reads)
	rate = 99
	_, err = u.Execute(basis)
	require.Error(t, err)
	var failure *domain.FXError
	assert.True(t, errors.As(err, &failure))
	assert.Equal(t, 1, reads)
}

func TestPublicationUSDValidationKeepsStatusIndependentFromFactor(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		rates                 map[string]float64
		available, capturable bool
	}{
		{name: "missing"},
		{name: "zero", rates: map[string]float64{"USD": 0}},
		{name: "negative", rates: map[string]float64{"USD": -1}},
		{name: "nan", rates: map[string]float64{"USD": math.NaN()}},
		{name: "infinity", rates: map[string]float64{"USD": math.Inf(1)}},
		{name: "positive", rates: map[string]float64{"USD": 137.5}, available: true, capturable: true},
		{name: "factor underflow", rates: map[string]float64{"USD": math.SmallestNonzeroFloat64}, available: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			publication := domain.FXPublication{Rates: tc.rates}
			rate, err := domain.PublishedUSD(publication)
			if tc.available {
				require.NoError(t, err)
				assert.Equal(t, tc.rates["USD"], rate)
			} else {
				require.Error(t, err)
			}
			u := NewUsecase(domain.FXSettings{QuotaUnit: func() float64 { return 500000 }, AccountingRate: func() float64 { return 100 }, Read: func() (domain.FXPublication, error) { return publication, nil }})
			_, err = u.Execute(domain.FXBasis{})
			if tc.capturable {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
