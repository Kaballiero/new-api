package domain

type AudioCost struct {
	Quota                            int
	Clamp                            *Clamp
	TieredResult                     *TieredResult
	TieredApplied, FixedPriceBilling bool
}
type AudioPrices struct {
	Read      func(string) (completion, audio, audioCompletion float64)
	QuotaUnit func() float64
}
