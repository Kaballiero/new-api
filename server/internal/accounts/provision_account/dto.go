package provision_account

import "github.com/QuantumNous/new-api/internal/accounts"

type NewAccount struct {
	Username, Password, DisplayName, Group string
	Role, Status                           int
}

type Input = accounts.GetAPICreateUserRequest
type Output = accounts.GetAPICreateCredential
