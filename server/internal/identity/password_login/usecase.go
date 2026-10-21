package password_login

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	EncryptionEnabled() bool
	Decrypt(encrypted, keyID string) (string, error)
	Authenticate(username, password string) (Account, error)
	// StorageFailed reports a database failure of Authenticate.
	StorageFailed(error) bool
	// CredentialsEmpty reports that Authenticate rejected empty credentials.
	CredentialsEmpty(error) bool
	LogDatabaseError(username string, err error)
}

// Execute authenticates the login request. Rejected input is
// domain.ErrInvalidInput, wrong credentials are domain.ErrCredentialsInvalid
// and a database failure is domain.ErrStorageUnavailable.
func (usecase Usecase) Execute(input LoginRequest) (Account, error) {
	password := input.Password
	if usecase.operation.EncryptionEnabled() {
		if input.PasswordEncrypted == "" || input.EncryptionKeyID == "" {
			return Account{}, domain.ErrInvalidInput
		}
		var err error
		password, err = usecase.operation.Decrypt(input.PasswordEncrypted, input.EncryptionKeyID)
		if err != nil {
			return Account{}, domain.ErrCredentialsInvalid
		}
	}
	if input.Username == "" || password == "" {
		return Account{}, domain.ErrInvalidInput
	}
	account, err := usecase.operation.Authenticate(input.Username, password)
	if err != nil {
		switch {
		case usecase.operation.StorageFailed(err):
			usecase.operation.LogDatabaseError(input.Username, err)
			return Account{}, domain.ErrStorageUnavailable
		case usecase.operation.CredentialsEmpty(err):
			return Account{}, domain.ErrInvalidInput
		default:
			return Account{}, domain.ErrCredentialsInvalid
		}
	}
	return account, nil
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
