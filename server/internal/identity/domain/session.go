package domain

type AccountState struct {
	Status      int
	AuthVersion int64
}
type SessionState struct {
	UserID          int
	UserAuthVersion int64
	Version         int64
	Status          string
	RevokedAt       int64
	ExpiresAt       int64
}

// SessionPolicy сохраняет чтение текущей конфигурации в исходных точках решения.
// Errors — исходные экземпляры сервиса/репозитория, передаваемые композицией.
type SessionPolicy struct {
	EnabledStatus                                                                     int
	ActiveStatus                                                                      string
	ActiveLimit                                                                       func() int64
	IssuanceWindowSeconds                                                             func() int64
	IssuanceLimit                                                                     func() int64
	Invalid, Revoked, Mismatch, RefreshInvalid, RefreshRace                           func() error
	Inactive, RotationInvalid, RotationRace, RotationReuse, Limit, IssuanceLimitError func() error
}
