package register_password

import (
	"errors"
	"fmt"
	"strings"
)

type Operation interface {
	NormalizeEmail(string) string
	Validate(User) error
	EmailVerificationEnabled() bool
	VerifyEmail(email, code string) bool
	EnsureEmailAvailable(string) error
	EmailTakenError() error
	CheckExists(username, email string) (bool, error)
	Inviter(code string) (int, error)
	Insert(username, password, email string, inviter int) error
	FindInsertedID(username string) (int, error)
	DefaultTokenEnabled() bool
	GenerateTokenKey() (string, error)
	InsertDefaultToken(userID int, username, key string) error
}

func (usecase Usecase) Execute(user User) Result {
	user.Username = strings.TrimSpace(user.Username)
	user.Email = usecase.operation.NormalizeEmail(user.Email)
	if user.Username == "" {
		return Result{MessageKey: "invalid_params"}
	}
	if err := usecase.operation.Validate(user); err != nil {
		return Result{MessageKey: "input_invalid", Params: map[string]any{"Error": err.Error()}}
	}
	if usecase.operation.EmailVerificationEnabled() {
		if user.Email == "" || user.VerificationCode == "" {
			return Result{MessageKey: "email_required"}
		}
		if !usecase.operation.VerifyEmail(user.Email, user.VerificationCode) {
			return Result{MessageKey: "code_invalid"}
		}
		if err := usecase.operation.EnsureEmailAvailable(user.Email); err != nil {
			if errors.Is(err, usecase.operation.EmailTakenError()) {
				return Result{MessageKey: "email_taken"}
			}
			return Result{MessageKey: "database"}
		}
	}
	emailForCheck := ""
	if usecase.operation.EmailVerificationEnabled() {
		emailForCheck = user.Email
	}
	exists, err := usecase.operation.CheckExists(user.Username, emailForCheck)
	if err != nil {
		return Result{MessageKey: "database", LogAfter: fmt.Sprintf("CheckUserExistOrDeleted error: %v", err)}
	}
	if exists {
		return Result{MessageKey: "exists"}
	}
	inviter, _ := usecase.operation.Inviter(user.AffCode)
	email := ""
	if usecase.operation.EmailVerificationEnabled() {
		email = user.Email
	}
	if err := usecase.operation.Insert(user.Username, user.Password, email, inviter); err != nil {
		if errors.Is(err, usecase.operation.EmailTakenError()) {
			return Result{MessageKey: "email_taken"}
		}
		return Result{Cause: err}
	}
	id, err := usecase.operation.FindInsertedID(user.Username)
	if err != nil {
		return Result{MessageKey: "register_failed"}
	}
	if usecase.operation.DefaultTokenEnabled() {
		key, err := usecase.operation.GenerateTokenKey()
		if err != nil {
			return Result{MessageKey: "token_failed", LogAfter: "failed to generate token key: " + err.Error()}
		}
		if err := usecase.operation.InsertDefaultToken(id, user.Username, key); err != nil {
			return Result{MessageKey: "token_insert_failed"}
		}
	}
	return Result{}
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}
