package channels

import (
	"github.com/QuantumNous/new-api/internal/channels/domain"
	textfilter "github.com/QuantumNous/new-api/pkg/textfilter"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func ClassifyModelValidation(localErr error, apiErr *types.NewAPIError) (domain.ModelLiveness, int) {
	// A nil *NewAPIError must stay a nil error.
	var upstreamErr error
	if apiErr != nil {
		upstreamErr = apiErr
	}
	return domain.ClassifyModelValidation(localErr, upstreamErr, textfilter.AcSearch)
}
