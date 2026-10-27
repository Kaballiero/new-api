package domain

import "errors"

const UserNameMaxLength = 20

var (
	ErrInvalidCredentials          = errors.New("invalid credentials")
	ErrUserEmptyCredentials        = errors.New("empty credentials")
	ErrUsernameAlreadyTaken        = errors.New("username already taken")
	ErrEmailAlreadyTaken           = errors.New("email already taken")
	ErrEmailNotFound               = errors.New("email not found")
	ErrEmailAmbiguous              = errors.New("email matches multiple users")
	ErrCannotDeleteRootUser        = errors.New("The root account cannot be deleted.")
	ErrGetAPIInvalidRequest        = errors.New("GETAPI_INVALID_REQUEST")
	ErrGetAPICapabilityDenied      = errors.New("GETAPI_CAPABILITY_DENIED")
	ErrGetAPIAccountNotFound       = errors.New("GETAPI_ACCOUNT_NOT_FOUND")
	ErrGetAPICreateConflict        = errors.New("GETAPI_CREATE_CONFLICT")
	ErrGetAPIUsernameMismatch      = errors.New("GETAPI_USERNAME_MISMATCH")
	ErrGetAPITargetRoleDenied      = errors.New("GETAPI_TARGET_ROLE_DENIED")
	ErrGetAPICredentialUnavailable = errors.New("GETAPI_CREDENTIAL_UNAVAILABLE")
)
