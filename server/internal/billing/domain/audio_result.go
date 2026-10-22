package domain

type AudioCost struct {
	Quota                                      int
	Clamp                                      *Clamp
	TieredResult                               *TieredResult
	TieredApplied, FixedPriceBilling, Billable bool
}
type AudioPrices struct {
	Read      func(string) (completion, audio, audioCompletion float64)
	QuotaUnit func() float64
}
