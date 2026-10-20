package password_login

import "errors"

type Operation interface {
	EncryptionEnabled() bool
	Decrypt(encrypted, keyID string) (string, error)
	Authenticate(username, password string) (Account, error)
	DatabaseError() error
	EmptyCredentialsError() error
	LogDatabaseError(username string, err error)
}

func (usecase Usecase) Execute(input LoginRequest) Result {
	password := input.Password
	if usecase.operation.EncryptionEnabled() {
		if input.PasswordEncrypted == "" || input.EncryptionKeyID == "" {
			return Result{Issue: "invalid"}
		}
		var err error
		password, err = usecase.operation.Decrypt(input.PasswordEncrypted, input.EncryptionKeyID)
		if err != nil {
			return Result{Issue: "credentials"}
		}
	}
	if input.Username == "" || password == "" {
		return Result{Issue: "invalid"}
	}
	account, err := usecase.operation.Authenticate(input.Username, password)
	if err != nil {
		switch {
		case errors.Is(err, usecase.operation.DatabaseError()):
			usecase.operation.LogDatabaseError(input.Username, err)
			return Result{Issue: "database"}
		case errors.Is(err, usecase.operation.EmptyCredentialsError()):
			return Result{Issue: "invalid"}
		default:
			return Result{Issue: "credentials"}
		}
	}
	return Result{Account: account}
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
