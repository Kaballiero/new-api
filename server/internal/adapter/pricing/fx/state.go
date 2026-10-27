package fx

import (
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"strings"
	"sync"
)

var modelCostFXState struct {
	sync.RWMutex
	snapshots map[string]domain.ModelCostFXSnapshot
}

func cloneModelCostFXSnapshot(snapshot domain.ModelCostFXSnapshot) domain.ModelCostFXSnapshot {
	copy := snapshot
	copy.Rates = make(map[string]float64, len(snapshot.Rates))
	for currency, rate := range snapshot.Rates {
		copy.Rates[currency] = rate
	}
	return copy
}

func Publish(snapshot domain.ModelCostFXSnapshot) {
	modelCostFXState.Lock()
	defer modelCostFXState.Unlock()
	if current, loaded := modelCostFXState.snapshots[snapshot.Source]; loaded &&
		(current.Version > snapshot.Version || (current.Version == snapshot.Version && current.FetchedAt >= snapshot.FetchedAt)) {
		return
	}
	if modelCostFXState.snapshots == nil {
		modelCostFXState.snapshots = make(map[string]domain.ModelCostFXSnapshot)
	}
	modelCostFXState.snapshots[snapshot.Source] = cloneModelCostFXSnapshot(snapshot)
}

// CurrentModelCostFX returns the last successfully loaded publication without
// database or JSON access. The returned map is owned by the caller.
func CurrentModelCostFX(source string) (domain.ModelCostFXSnapshot, error) {
	modelCostFXState.RLock()
	defer modelCostFXState.RUnlock()
	snapshot, loaded := modelCostFXState.snapshots[strings.ToLower(strings.TrimSpace(source))]
	if !loaded {
		return domain.ModelCostFXSnapshot{}, domain.ErrModelCostFXUnavailable
	}
	return cloneModelCostFXSnapshot(snapshot), nil
}
