package service

import (
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/relaykit/types"
)

type ModelLiveness = domain.ModelLiveness

const (
	ModelAlive     = domain.ModelAlive
	ModelDead      = domain.ModelDead
	ModelUncertain = domain.ModelUncertain
)

func parseUpstreamStatusCode(lower string) int { return domain.ParseUpstreamStatusCode(lower) }
func ClassifyModelValidation(localErr error, apiErr *types.NewAPIError) (ModelLiveness, int) {
	// A nil *NewAPIError must stay a nil error.
	var upstreamErr error
	if apiErr != nil {
		upstreamErr = apiErr
	}
	return domain.ClassifyModelValidation(localErr, upstreamErr, AcSearch)
}
