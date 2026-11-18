package pricing

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Check prices the reference request with the admission rules of the gateway
// from the given tariffs alone: a model they do not price is refused without a
// live discovery, which an unbound estimator refuses before any request.
func TestCatalogReservesCheck(t *testing.T) {
	tariff := func(tag, pricing string, output string) domain.NativeEndpointResponse {
		var prices map[string]json.RawMessage
		require.NoError(t, common.UnmarshalJsonStr(pricing, &prices))
		return domain.NativeEndpointResponse{Endpoints: []domain.NativePricedEndpoint{{Tag: tag, MaxCompletionTokens: 100, Pricing: prices}}, OutputModalities: []string{output}}
	}
	tariffs := map[string]domain.NativeEndpointResponse{
		"test/model":          tariff("provider", `{"prompt":"0.001","completion":"0.002"}`, "text"),
		"test/no-completion":  tariff("provider", `{"prompt":"0.001"}`, "text"),
		"test/unknown-item":   tariff("provider", `{"prompt":"0.001","completion":"0.002","per_moon":"1"}`, "text"),
		"test/embed":          tariff("provider", `{"prompt":"0.001"}`, "embeddings"),
		"cohere/embed-5-fast": tariff("cohere", `{"prompt":"0.0000001","image_token":"0.0000002"}`, "embeddings"),
	}

	for _, tc := range []struct {
		name, model, operation string
		wantErr                error
		// wantUnavailable is the text of a domain.EstimateUnavailableError.
		wantUnavailable string
	}{
		{name: "priced text", model: "test/model", operation: "chat/completions"},
		{name: "priced embeddings", model: "test/embed", operation: "embeddings"},
		{name: "Cohere Embed priced by image tokens", model: "cohere/embed-5-fast", operation: "embeddings"},
		{name: "outside the tariffs", model: "test/unpriced", operation: "chat/completions", wantErr: domain.ErrEndpointDiscoveryMissing},
		{name: "missing completion price", model: "test/no-completion", operation: "chat/completions",
			wantUnavailable: "OpenRouter estimate unavailable: invalid OpenRouter endpoint price"},
		{name: "unknown pricing item", model: "test/unknown-item", operation: "chat/completions",
			wantUnavailable: "OpenRouter estimate unavailable: OpenRouter endpoint has unsupported billable dimension per_moon"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CatalogReserves{Capabilities: pricingadapter.Capabilities(), Now: time.Now, MaxAge: func() time.Duration { return time.Hour }}.Check(tariffs, tc.model, tc.operation)

			switch {
			case tc.wantErr != nil:
				require.ErrorIs(t, err, tc.wantErr)
			case tc.wantUnavailable != "":
				var unavailable *domain.EstimateUnavailableError
				require.ErrorAs(t, err, &unavailable)
				assert.EqualError(t, err, tc.wantUnavailable)
			default:
				assert.NoError(t, err)
			}
		})
	}
}
