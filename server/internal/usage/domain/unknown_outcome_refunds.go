package domain

// RefundTally counts refunded holds and their quota.
type RefundTally struct {
	Count int
	Quota int
}

// UnknownOutcomeRefunds are the OpenRouter holds refunded in a period although
// the upstream charge was unknown: text requests and native images apart.
type UnknownOutcomeRefunds struct {
	Text  RefundTally
	Image RefundTally
}
