package domain

// Tally counts OpenRouter requests and their quota.
type Tally struct {
	Count int
	Quota int
}

// UnknownOutcomeRefunds are the OpenRouter holds refunded in a period although
// the upstream charge was unknown, text requests and native images apart, and
// the requests charged an estimate of their unknown upstream cost.
type UnknownOutcomeRefunds struct {
	Text             Tally
	Image            Tally
	EstimatedCharges Tally
}
