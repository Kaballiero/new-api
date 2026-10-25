package domain

import "errors"

var (
	ErrSetupUsername         = errors.New("setup username is too long")
	ErrSetupPasswordMismatch = errors.New("setup passwords do not match")
	ErrSetupPassword         = errors.New("setup password validation failed")
	ErrSetupHash             = errors.New("setup password hashing failed")
	ErrSetupRoot             = errors.New("setup root creation failed")
	ErrSetupSelfMode         = errors.New("setup self-use mode write failed")
	ErrSetupDemoMode         = errors.New("setup demo mode write failed")
	ErrSetupPersist          = errors.New("setup persistence failed")
)
