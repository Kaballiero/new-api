package record

// ModelCostFXRow stores the current market FX publication for model pricing.
// RatesJSON is normalized JSON text.
type ModelCostFXRow struct {
	Source      string `json:"source" gorm:"primaryKey;size:64"`
	EffectiveAt int64  `json:"effective_at"`
	FetchedAt   int64  `json:"fetched_at"`
	Version     int64  `json:"version"`
	RatesJSON   string `json:"rates_json" gorm:"type:text"`
}

func (ModelCostFXRow) TableName() string {
	return "model_cost_fx"
}
