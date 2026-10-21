package update_pricing_config

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The checks run before any storage access, so the store is never used.
func TestExecuteRejectsInvalidChangeSets(t *testing.T) {
	audited := false
	usecase := NewUsecase(nil, nil, func(string) {}, func() {}, func() {}, func([]string) { audited = true })

	err := usecase.Execute(nil)
	assert.EqualError(t, err, "select model pricing changes before saving")

	err = usecase.Execute([]domain.ModelPricingChange{{ModelName: "gpt", ExpectedVersion: "v"}, {ModelName: "gpt", ExpectedVersion: "v"}})
	assert.EqualError(t, err, "duplicate model pricing change")

	err = usecase.Execute([]domain.ModelPricingChange{{ModelName: "gpt"}})
	require.ErrorIs(t, err, domain.ErrModelPricingConflict)
	assert.EqualError(t, err, "model pricing changed; reload before saving")

	assert.False(t, audited)
}
