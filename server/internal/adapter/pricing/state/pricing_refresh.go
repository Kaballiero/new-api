package pricingstate

// RefreshPricing rebuilds the pricing cache immediately.
// Management APIs use this path when they require current data,
// bypassing the default one-minute refresh delay.
func RefreshPricing(rebuild func()) {
	updatePricingLock.Lock()
	defer updatePricingLock.Unlock()

	modelSupportEndpointsLock.Lock()
	defer modelSupportEndpointsLock.Unlock()

	rebuild()
}
