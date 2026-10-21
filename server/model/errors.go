package model

import "errors"

// Common errors
var (
	ErrDatabase                = errors.New("database error")
	ErrModelNameEmpty          = errors.New("model name is empty")
	ErrModelAliasCycle         = errors.New("model alias cycle detected")
	ErrModelMappingCycle       = errors.New("model mapping cycle detected")
	ErrModelMappingSourceEmpty = errors.New("model mapping source is empty")
	ErrModelMappingTargetEmpty = errors.New("model mapping target is empty")
	ErrCanonicalModelCollision = errors.New("canonical model collision")
	ErrModelMappingConflict    = errors.New("model mapping conflict")
)

// User auth errors
var (
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrUserEmptyCredentials = errors.New("empty credentials")
	ErrUsernameAlreadyTaken = errors.New("username already taken")
	ErrEmailAlreadyTaken    = errors.New("email already taken")
	ErrEmailNotFound        = errors.New("email not found")
	ErrEmailAmbiguous       = errors.New("email matches multiple users")
)

// Redemption errors
var ErrRedeemFailed = errors.New("redeem.failed")
