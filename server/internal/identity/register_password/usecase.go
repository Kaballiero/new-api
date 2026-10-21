package register_password

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type Operation interface {
	NormalizeEmail(string) string
	Validate(User) error
	EmailVerificationEnabled() bool
	VerifyEmail(email, code string) bool
	EnsureEmailAvailable(string) error
	// EmailTaken reports that err rejects an e-mail address used by another account.
	EmailTaken(error) bool
	CheckExists(username, email string) (bool, error)
	Inviter(code string) (int, error)
	Insert(username, password, email string, inviter int) error
	FindInsertedID(username string) (int, error)
	DefaultTokenEnabled() bool
	GenerateTokenKey() (string, error)
	InsertDefaultToken(userID int, username, key string) error
}

// Execute registers a password account. Every rejection is a domain error with
// its own response; *domain.LoggedError carries details logged after it.
func (usecase Usecase) Execute(user User) error {
	user.Username = strings.TrimSpace(user.Username)
	user.Email = usecase.operation.NormalizeEmail(user.Email)
	if user.Username == "" {
		return domain.ErrInvalidInput
	}
	if err := usecase.operation.Validate(user); err != nil {
		return &domain.InputError{Detail: err.Error()}
	}
	if usecase.operation.EmailVerificationEnabled() {
		if user.Email == "" || user.VerificationCode == "" {
			return domain.ErrEmailVerificationRequired
		}
		if !usecase.operation.VerifyEmail(user.Email, user.VerificationCode) {
			return domain.ErrVerificationCodeInvalid
		}
		if err := usecase.operation.EnsureEmailAvailable(user.Email); err != nil {
			if usecase.operation.EmailTaken(err) {
				return domain.ErrEmailTaken
			}
			return domain.ErrStorageUnavailable
		}
	}
	emailForCheck := ""
	if usecase.operation.EmailVerificationEnabled() {
		emailForCheck = user.Email
	}
	exists, err := usecase.operation.CheckExists(user.Username, emailForCheck)
	if err != nil {
		return &domain.LoggedError{Err: domain.ErrStorageUnavailable, Log: fmt.Sprintf("CheckUserExistOrDeleted error: %v", err)}
	}
	if exists {
		return domain.ErrUserExists
	}
	inviter, _ := usecase.operation.Inviter(user.AffCode)
	email := ""
	if usecase.operation.EmailVerificationEnabled() {
		email = user.Email
	}
	if err := usecase.operation.Insert(user.Username, user.Password, email, inviter); err != nil {
		if usecase.operation.EmailTaken(err) {
			return domain.ErrEmailTaken
		}
		return err
	}
	id, err := usecase.operation.FindInsertedID(user.Username)
	if err != nil {
		return domain.ErrRegisterFailed
	}
	if usecase.operation.DefaultTokenEnabled() {
		key, err := usecase.operation.GenerateTokenKey()
		if err != nil {
			return &domain.LoggedError{Err: domain.ErrDefaultTokenFailed, Log: "failed to generate token key: " + err.Error()}
		}
		if err := usecase.operation.InsertDefaultToken(id, user.Username, key); err != nil {
			return domain.ErrDefaultTokenInsertFailed
		}
	}
	return nil
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
