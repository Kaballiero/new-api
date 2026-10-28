package get_groups

import "github.com/QuantumNous/new-api/internal/accounts/domain"

// Input selects the account; Labels are the generated group texts in the
// request language.
type Input struct {
	UserID int
	Labels domain.GroupLabels
}
