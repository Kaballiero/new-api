package domain

// InitialAccount is set after the email check and password preparation, inside
// the account creation transaction.
type InitialAccount struct {
	Quota         int
	AffiliateCode string
}
