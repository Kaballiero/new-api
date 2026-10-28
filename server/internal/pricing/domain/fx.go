package domain

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

var (
	ErrModelCostFXUnavailable     = errors.New("model cost FX snapshot unavailable")
	ErrModelCostFXVersionConflict = errors.New("model cost FX snapshot version conflict")
	ErrModelCostFXStale           = errors.New("model cost FX snapshot is stale")
)

// ModelCostFXSnapshot is the validated in-memory representation of a
// publication. Rates are expressed as units of the target currency per one
// unit of the source currency.
type ModelCostFXSnapshot struct {
	Source      string
	EffectiveAt int64
	FetchedAt   int64
	Version     int64
	Rates       map[string]float64
}

// NormalizeModelCostFXSnapshot validates, normalizes currency keys, and returns an owned
// copy. Keeping this at the persistence boundary prevents invalid data from
// reaching either the database or the shared in-memory state.
func NormalizeModelCostFXSnapshot(snapshot ModelCostFXSnapshot) (ModelCostFXSnapshot, error) {
	snapshot.Source = strings.ToLower(strings.TrimSpace(snapshot.Source))
	if snapshot.Source == "" {
		return ModelCostFXSnapshot{}, errors.New("model cost FX source is required")
	}
	for _, ch := range snapshot.Source {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '.' && ch != '_' && ch != '-' {
			return ModelCostFXSnapshot{}, errors.New("model cost FX source must use lowercase ASCII identifier characters")
		}
	}
	if snapshot.EffectiveAt <= 0 || snapshot.FetchedAt <= 0 {
		return ModelCostFXSnapshot{}, errors.New("model cost FX timestamps must be positive")
	}
	if snapshot.Version < 0 {
		return ModelCostFXSnapshot{}, errors.New("model cost FX version must not be negative")
	}
	if len(snapshot.Rates) == 0 {
		return ModelCostFXSnapshot{}, errors.New("model cost FX rates must not be empty")
	}

	rates := make(map[string]float64, len(snapshot.Rates))
	for currency, rate := range snapshot.Rates {
		currency = strings.ToUpper(strings.TrimSpace(currency))
		if currency == "" {
			return ModelCostFXSnapshot{}, errors.New("model cost FX currency is required")
		}
		if _, exists := rates[currency]; exists {
			return ModelCostFXSnapshot{}, fmt.Errorf("duplicate model cost FX currency %q", currency)
		}
		if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			return ModelCostFXSnapshot{}, fmt.Errorf("invalid model cost FX rate for %q", currency)
		}
		rates[currency] = rate
	}
	snapshot.Rates = rates
	return snapshot, nil
}

const ModelCostFXSource = "cbr-xml-daily.ru"

type BillingFXBasis struct {
	USDRate            float64 `json:"usd_rate"`
	Factor             float64 `json:"factor"`
	Source             string  `json:"source"`
	PublicationVersion int64   `json:"publication_version"`
	EffectiveAt        int64   `json:"effective_at"`
	FetchedAt          int64   `json:"fetched_at"`
}
