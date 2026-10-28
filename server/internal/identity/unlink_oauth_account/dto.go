package unlink_oauth_account

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Input struct {
	Identity   domain.AuthIdentity
	ProviderID int
}
